package tui

import (
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// ClusterDataMsg is dispatched when an async fetch of cluster state completes.
// Either Err is set, or Version+Resources are set. IPs and Statuses are
// best-effort — IPs is empty for guests whose agent isn't reporting;
// Statuses is empty when no transient ops are in flight (callers fall back
// to Resource.Status for the steady-state value).
type ClusterDataMsg struct {
	Version   proxmox.Version
	Resources []proxmox.Resource
	IPs       map[int]string // VMID → IPv4
	Statuses  map[int]string // VMID → transient status (starting, rebooting, …)
	Err       error
}

// NodesDataMsg is dispatched when /nodes fetch completes.
type NodesDataMsg struct {
	Nodes []proxmox.NodeStatus
	Err   error
}

// RefreshMsg is dispatched when the user explicitly requests a refresh.
// Views translate it into a fetch tea.Cmd.
type RefreshMsg struct{}

// SwitchTabMsg swaps the root view to the named tab.
// Names: "overview" | "vms" | "lxcs" | "nodes".
type SwitchTabMsg struct{ Name string }

// PushViewMsg overlays a new view (modal/stream) on top of the stack.
type PushViewMsg struct{ View View }

// PopViewMsg removes the top overlay, returning to the previous view.
type PopViewMsg struct{}
