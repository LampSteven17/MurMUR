package tui

import (
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// ClusterDataMsg is dispatched when an async fetch of cluster state completes.
// Either Err is set, or Version+Resources are set.
type ClusterDataMsg struct {
	Version   proxmox.Version
	Resources []proxmox.Resource
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
