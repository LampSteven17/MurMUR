package provision

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// errTailLines is how many stderr lines are kept in memory and surfaced when
// post_deploy fails. Enough to show a docker error or an ansible stack trace;
// small enough to keep the in-TUI summary readable.
const errTailLines = 12

// runPostDeploy executes `cmd` after the guest is up.
//
//   - When remote=false the command runs locally via /bin/sh -c with GUEST_*
//     env vars exported. Used for ansible playbook invocations: ansible
//     handles its own SSH to the guest, so murmur's wrapper just needs to
//     fork the local process and pipe its output.
//
//   - When remote=true the command runs ON the guest via `ssh GUEST_USER@
//     GUEST_IP bash -s`, with env vars prepended as quoted `export K=V`
//     lines. Used for raw shell commands that operate on guest state
//     (`apt install`, `docker run`, file writes under /etc, etc.). This
//     is what 99% of `post_deploy:` blocks actually want — without it,
//     `apt-get install` would try to run on the murmur host.
//
// Either way stdout+stderr stream into the orchestrator's progress callback
// as StepPostDeploy events; non-zero exit returns an error with the captured
// stderr tail attached for failure-context.
//
// secretEnv carries operator-supplied per-replica secrets (Twingate tokens,
// etc.). Each k=v is exported alongside GUEST_*. Values are never logged —
// the progress emit only mentions the key names.
func (o *Orchestrator) runPostDeploy(ctx context.Context, cmd, workDir string, r *Result, secretEnv map[string]string, remote bool) error {
	o.emit(StepPostDeploy, "starting post-deploy: "+truncatePreview(cmd, 80), 95)
	if len(secretEnv) > 0 {
		names := make([]string, 0, len(secretEnv))
		for k := range secretEnv {
			names = append(names, k)
		}
		o.emit(StepPostDeploy, fmt.Sprintf("injecting %d secret(s): %v", len(names), names), 95)
	}

	var shell *exec.Cmd
	if remote {
		// Build a remote script: export env vars, then the user's command.
		// Values are single-quoted with embedded '\'' escaping so secrets
		// containing shell metacharacters survive the round-trip intact.
		script := buildRemoteScript(cmd, r, secretEnv)
		o.emit(StepPostDeploy, fmt.Sprintf("ssh %s@%s — running %d-byte script", r.User, r.IPv4, len(script)), 95)
		shell = exec.CommandContext(ctx, "ssh",
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "ConnectTimeout=30",
			"-o", "ConnectionAttempts=8",
			r.User+"@"+r.IPv4,
			"bash -s",
		)
		shell.Stdin = strings.NewReader(script)
	} else {
		shell = exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
		shell.Env = append(os.Environ(),
			"GUEST_IP="+r.IPv4,
			"GUEST_USER="+r.User,
			"GUEST_NAME="+r.Name,
			"GUEST_VMID="+strconv.Itoa(r.VMID),
			"GUEST_NODE="+r.Node,
		)
		for k, v := range secretEnv {
			shell.Env = append(shell.Env, k+"="+v)
		}
		if workDir != "" {
			shell.Dir = workDir
			shell.Env = append(shell.Env, "MURMUR_CONFIG_DIR="+workDir)
		}
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
	// stderr can block stdout (or vice versa) and stall the command. We
	// also keep a ring of the last few stderr lines so the failure message
	// can carry the actual command output (e.g. docker's "Error response
	// from daemon: …") rather than just "exit 125".
	tail := newTailBuffer(errTailLines)
	var wg sync.WaitGroup
	wg.Add(2)
	go o.drain(&wg, stdout, "", nil)
	go o.drain(&wg, stderr, "err: ", tail)
	wg.Wait()

	if err := shell.Wait(); err != nil {
		// Wait returns *exec.ExitError when the command exits non-zero.
		// Surface a clean message and attach the captured stderr tail so
		// the operator can see what blew up without rerunning.
		if ee, ok := err.(*exec.ExitError); ok {
			msg := fmt.Sprintf("command exited with code %d", ee.ExitCode())
			if t := tail.Render(); t != "" {
				msg += "\n  stderr tail:\n" + t
			}
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	o.emit(StepPostDeploy, "post-deploy completed", 99)
	return nil
}

// tailBuffer keeps the last `cap` items in a ring. Used to retain a window
// of stderr lines so post_deploy failures can show the actual error context.
type tailBuffer struct {
	mu    sync.Mutex
	cap   int
	lines []string
}

func newTailBuffer(cap int) *tailBuffer { return &tailBuffer{cap: cap} }

func (b *tailBuffer) Push(line string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
	if len(b.lines) > b.cap {
		b.lines = b.lines[len(b.lines)-b.cap:]
	}
}

func (b *tailBuffer) Render() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lines) == 0 {
		return ""
	}
	out := make([]string, len(b.lines))
	for i, ln := range b.lines {
		out[i] = "    " + ln
	}
	return strings.Join(out, "\n")
}

// drain reads lines from r and emits each as a StepPostDeploy progress event
// (prefixed for stderr) so the UI's log mirrors the playbook's output live.
// If tail is non-nil, every drained line is also pushed into the ring buffer
// for use in the failure-context summary.
func (o *Orchestrator) drain(wg *sync.WaitGroup, r io.Reader, prefix string, tail *tailBuffer) {
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
		tail.Push(line)
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

// buildRemoteScript renders a shell script that exports GUEST_*/secret env
// vars then executes the operator's post_deploy body. Used as stdin for
// `ssh user@host bash -s`. Values are single-quoted with embedded '\''
// escaping so tokens containing special shell chars survive intact.
func buildRemoteScript(body string, r *Result, secretEnv map[string]string) string {
	var sb strings.Builder
	// Strict mode so a missing semicolon doesn't mask a failure mid-script.
	// `-e` exits on error; `-u` errors on undefined var; `-o pipefail` makes
	// `set | grep …` pipelines fail loudly if the first stage dies.
	sb.WriteString("set -euo pipefail\n")
	// GUEST_* mirrors the local-execution env so scripts that worked in the
	// old local mode keep working when moved to remote. MURMUR_CONFIG_DIR
	// is intentionally omitted — it's a murmur-host path, meaningless on
	// the guest.
	writeExport(&sb, "GUEST_IP", r.IPv4)
	writeExport(&sb, "GUEST_USER", r.User)
	writeExport(&sb, "GUEST_NAME", r.Name)
	writeExport(&sb, "GUEST_VMID", strconv.Itoa(r.VMID))
	writeExport(&sb, "GUEST_NODE", r.Node)
	for k, v := range secretEnv {
		writeExport(&sb, k, v)
	}
	sb.WriteString("\n")
	sb.WriteString(body)
	return sb.String()
}

// writeExport appends `export K='V'` with proper single-quote escaping.
func writeExport(sb *strings.Builder, k, v string) {
	sb.WriteString("export ")
	sb.WriteString(k)
	sb.WriteString("=")
	sb.WriteString(shellSingleQuote(v))
	sb.WriteString("\n")
}

// shellSingleQuote wraps s in single quotes, escaping embedded ones as
// '\'' — the standard POSIX-safe pattern. Survives values containing $,
// `, \, ", whitespace, newlines, etc.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
