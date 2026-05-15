package provision

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

// runPostDeploy executes `cmd` via /bin/sh -c with GUEST_* env vars set,
// streams stdout+stderr line-by-line into the orchestrator's progress
// callback as StepPostDeploy events, and returns an error on non-zero exit.
//
// Murmur stays runner-agnostic — `cmd` can be `ansible-playbook -i ${GUEST_IP},
// ... immich-deploy.yml` or any other shell command. The convention is that
// AppsView wraps known playbook paths with ansible-playbook; everything else
// flows through as raw shell.
func (o *Orchestrator) runPostDeploy(ctx context.Context, cmd, workDir string, r *Result) error {
	o.emit(StepPostDeploy, "starting post-deploy: "+truncatePreview(cmd, 80), 95)

	shell := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	shell.Env = append(os.Environ(),
		"GUEST_IP="+r.IPv4,
		"GUEST_USER="+r.User,
		"GUEST_NAME="+r.Name,
		"GUEST_VMID="+strconv.Itoa(r.VMID),
		"GUEST_NODE="+r.Node,
	)
	if workDir != "" {
		shell.Dir = workDir
		shell.Env = append(shell.Env, "MURMUR_CONFIG_DIR="+workDir)
	}

	stdout, err := shell.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}
	if err := shell.Start(); err != nil {
		return fmt.Errorf("starting %q: %w", cmd, err)
	}

	// Drain both streams concurrently — without parallel readers a chatty
	// stderr can block stdout (or vice versa) and stall the command.
	var wg sync.WaitGroup
	wg.Add(2)
	go o.drain(&wg, stdout, "")
	go o.drain(&wg, stderr, "err: ")
	wg.Wait()

	if err := shell.Wait(); err != nil {
		// Wait returns *exec.ExitError when the command exits non-zero.
		// Surface a clean message rather than the raw exec error.
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("command exited with code %d", ee.ExitCode())
		}
		return err
	}
	o.emit(StepPostDeploy, "post-deploy completed", 99)
	return nil
}

// drain reads lines from r and emits each as a StepPostDeploy progress event
// (prefixed for stderr) so the UI's log mirrors the playbook's output live.
func (o *Orchestrator) drain(wg *sync.WaitGroup, r io.Reader, prefix string) {
	defer wg.Done()
	scanner := bufio.NewScanner(r)
	// Long ansible task names can exceed bufio's default 64K cap; bump it.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		o.emit(StepPostDeploy, prefix+line, 95)
	}
}

// truncatePreview shortens a long command for the "starting…" header so
// later log lines don't get pushed off-screen by a 200-char ansible call.
func truncatePreview(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
