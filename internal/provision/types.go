// Package provision orchestrates guest deployment against the ProxMox API.
// It's a pure-API path: clone (VM) or create (LXC), configure, start, wait
// for the guest agent / network, return the result. No tofu, no ansible.
package provision

import (
	"github.com/rtx-monster/murmur/internal/config"
)

// Step is a coarse stage of the deploy pipeline, suitable for progress UI.
type Step int

const (
	StepPending Step = iota
	StepResolve   // resolve flavor/image/node (deploy) or look up target (teardown)
	StepClone     // VM: clone template · LXC: create
	StepConfigure // hardware + cloud-init (VM) or post-create config (LXC)
	StepStart     // start guest, wait for status=running
	StepIP        // wait for guest-agent / network to report IP
	StepStop        // teardown: stopping a running guest before destroy
	StepDestroy     // teardown: removing guest + disks
	StepPostDeploy  // deploy: post-deploy shell command (ansible playbook, etc.)
	StepDone
)

func (s Step) String() string {
	switch s {
	case StepPending:
		return "pending"
	case StepResolve:
		return "resolve"
	case StepClone:
		return "clone"
	case StepConfigure:
		return "configure"
	case StepStart:
		return "start"
	case StepIP:
		return "ip"
	case StepStop:
		return "stop"
	case StepDestroy:
		return "destroy"
	case StepPostDeploy:
		return "post-deploy"
	case StepDone:
		return "done"
	}
	return "?"
}

// ProgressEvent is emitted by the orchestrator as the deploy advances. Views
// translate this into a streaming log.
type ProgressEvent struct {
	Step    Step
	Message string
	Percent float64 // 0..100 for the overall deploy
}

// Request is the input to Orchestrator.Deploy.
type Request struct {
	Name   string // guest hostname / display name
	Type   string // "vm" | "lxc"
	Image  string // image name (cluster.yaml catalog) — used to pick template & default user
	Flavor string // flavor name (cluster.yaml catalog) — cpu/mem/disk

	// TargetNode forces an explicit node. Empty = best-fit placement.
	TargetNode string

	// VM-specific. Empty SSHPubKey = cluster.yaml ssh.identity + ".pub" is read.
	SSHPubKey string
	User      string // cloud-init user; empty = builtin default for the image distro

	// LXC-specific. Empty IPv4 = DHCP.
	IPv4    string // CIDR notation, e.g. "10.0.0.50/24"
	Gateway string

	// PostDeployCommand, if set, is executed after the guest IP is resolved.
	// Where it runs depends on PostDeployRemote:
	//   false (default) — runs locally on the murmur host via /bin/sh -c,
	//     with GUEST_*/MURMUR_CONFIG_DIR env vars exported. Used for ansible
	//     playbook invocations where ansible itself handles SSH.
	//   true            — runs ON the guest via `ssh GUEST_USER@GUEST_IP
	//     bash -s`. Env vars are prepended as `export K=V` lines (single-
	//     quoted, escape-safe). Used for raw shell commands that operate on
	//     guest state (apt install, docker run, file writes under /etc, ...).
	// Stdout/stderr stream into the progress callback as StepPostDeploy
	// events either way; non-zero exit → partial-success error.
	PostDeployCommand string

	// PostDeployRemote selects local vs. remote execution. See above.
	PostDeployRemote bool

	// WorkDir is the working directory for local PostDeployCommand
	// execution. Typically the directory containing cluster.yaml, so
	// relative playbook paths resolve. Ignored when PostDeployRemote=true.
	WorkDir string

	// SecretEnv carries per-replica secrets prompted from the operator at
	// deploy time (Twingate tokens, API keys, etc.). Each key=value pair is
	// exported as an env var to PostDeployCommand. Values are never logged —
	// progress events refer to them by name only.
	SecretEnv map[string]string
}

// Result is what Deploy returns on success.
type Result struct {
	VMID int
	Name string
	Node string
	IPv4 string
	User string // cloud-init user (VM) or "root" (LXC)
}

// resolvedRequest carries the resolved catalog entries alongside the raw
// request — produced once by the orchestrator at the top of Deploy.
type resolvedRequest struct {
	req    Request
	flavor config.Flavor
	image  config.Image
}
