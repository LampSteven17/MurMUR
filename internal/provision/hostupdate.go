package provision

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/rtx-monster/murmur/internal/proxmox"
)

// HostUpdateStatus is the per-node snapshot the [u]update tab renders.
type HostUpdateStatus struct {
	Node         string
	Pending      int               // count of upgradable packages
	Packages     []proxmox.AptPackage
	NeedsReboot  bool              // /var/run/reboot-required exists
	Err          error             // any error encountered while checking
}

// CheckHostUpdates pulls the pending apt list (via PVE API) and checks the
// reboot-required marker (via SSH stat). Both are best-effort; errors land
// in Status.Err so the UI can show "—" instead of pretending.
func (o *Orchestrator) CheckHostUpdates(ctx context.Context, node string) HostUpdateStatus {
	st := HostUpdateStatus{Node: node}

	pkgs, err := o.client.ListAptUpdates(ctx, node)
	if err != nil {
		st.Err = err
		return st
	}
	st.Packages = pkgs
	st.Pending = len(pkgs)

	// Reboot-required check via SSH. Quick test -e, exits 0 or 1; either
	// way we get a definitive answer in under a second.
	rebootCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(rebootCtx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		"-o", "StrictHostKeyChecking=accept-new",
		"root@"+node,
		"test -e /var/run/reboot-required && echo YES || echo NO")
	if out, err := cmd.Output(); err == nil {
		st.NeedsReboot = strings.TrimSpace(string(out)) == "YES"
	}
	return st
}

// UpgradeHost runs `apt-get update + dist-upgrade` over SSH against `node`,
// streams output line-by-line via the orchestrator's progress callback, and
// auto-reboots if /var/run/reboot-required appears post-upgrade. Caller is
// responsible for sequencing (don't run two nodes in parallel — quorum).
func (o *Orchestrator) UpgradeHost(ctx context.Context, node string) error {
	o.emit(StepConfigure, fmt.Sprintf("apt update + dist-upgrade on %s (streaming…)", node), 10)

	upgradeCmd := "DEBIAN_FRONTEND=noninteractive apt-get update && " +
		"DEBIAN_FRONTEND=noninteractive apt-get -y -o Dpkg::Options::='--force-confdef' " +
		"-o Dpkg::Options::='--force-confold' dist-upgrade && " +
		"echo '__MURMUR_OK__'"

	if err := o.sshStream(ctx, "root@"+node, upgradeCmd, 30*time.Minute); err != nil {
		return fmt.Errorf("upgrade %s: %w", node, err)
	}
	o.emit(StepConfigure, fmt.Sprintf("upgrade completed on %s", node), 70)

	// Reboot check. apt may install a new kernel — flag and reboot.
	cmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		"-o", "StrictHostKeyChecking=accept-new",
		"root@"+node,
		"test -e /var/run/reboot-required && echo YES || echo NO")
	out, _ := cmd.Output()
	if strings.TrimSpace(string(out)) != "YES" {
		o.emit(StepDone, fmt.Sprintf("%s up to date — no reboot needed", node), 100)
		return nil
	}

	o.emit(StepStop, fmt.Sprintf("kernel update — rebooting %s", node), 75)
	// SSH `reboot` exits with a hangup error as the connection drops; that's
	// expected, so we don't surface it as a failure.
	rebootCmd := exec.Command("ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		"-o", "StrictHostKeyChecking=accept-new",
		"root@"+node,
		"systemctl reboot")
	_ = rebootCmd.Run()

	// Wait for the node to come back. Poll PVE's per-node /version (which
	// requires the daemon to be running, so it's a good liveness signal).
	o.emit(StepIP, fmt.Sprintf("waiting for %s to come back online…", node), 85)
	if err := o.waitForNodeUp(ctx, node, 8*time.Minute); err != nil {
		return fmt.Errorf("reboot %s: %w", node, err)
	}
	o.emit(StepDone, fmt.Sprintf("%s back online after reboot", node), 100)
	return nil
}

// sshStream shells out to `ssh <target> <cmd>` (target is user@host) and
// pumps stdout+stderr line-by-line into the orchestrator's progress
// callback as StepConfigure events. Non-zero exit → error.
// timeoutForCommand caps wall-clock.
//
// Progress percentage asymptotes from 20% → ~95% as output flows in, so the
// TUI bar visibly advances during long operations (apt update, docker pull)
// without claiming completion. Stderr is NOT prefixed: docker, apt, and many
// other tools write legitimate progress info to stderr, and tagging it as
// "err:" trains operators to ignore real errors when they happen.
func (o *Orchestrator) sshStream(ctx context.Context, target, command string, timeoutForCommand time.Duration) error {
	sshCtx, cancel := context.WithTimeout(ctx, timeoutForCommand)
	defer cancel()

	cmd := exec.CommandContext(sshCtx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ServerAliveInterval=30",
		target,
		command)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("ssh start: %w", err)
	}

	// Shared per-call progress state — both streams contribute to the same
	// asymptotic curve so the bar advances regardless of which pipe is
	// chatty. 30 = "feels like quick progress" knee point in the curve.
	var (
		mu    sync.Mutex
		lines int64
	)
	advance := func(line string) {
		mu.Lock()
		lines++
		n := lines
		mu.Unlock()
		// pct = 20 + 75*(1 - 1/(1 + n/30)), capped just under 100.
		pct := 20.0 + 75.0*(1.0-1.0/(1.0+float64(n)/30.0))
		if pct > 95.0 {
			pct = 95.0
		}
		o.emit(StepConfigure, line, pct)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go o.drainSSH(&wg, stdout, advance)
	go o.drainSSH(&wg, stderr, advance)
	wg.Wait()

	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("ssh exited %d", ee.ExitCode())
		}
		return err
	}
	return nil
}

// drainSSH reads lines from r and routes each into `advance` (which emits
// the progress event with a ratcheted percent).
func (o *Orchestrator) drainSSH(wg *sync.WaitGroup, r io.Reader, advance func(string)) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		advance(line)
	}
}

// waitForNodeUp polls /nodes/{node}/version until it answers or maxWait
// elapses. PVE's pvedaemon comes up shortly after networking + cluster
// services are ready, so this is a decent "node is fully back" signal.
func (o *Orchestrator) waitForNodeUp(ctx context.Context, node string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var v proxmox.Version
		err := o.client.GetJSON(probeCtx, fmt.Sprintf("/nodes/%s/version", node), &v)
		cancel()
		if err == nil && v.Version != "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for node to come back online", maxWait)
		}
		time.Sleep(10 * time.Second)
	}
}
