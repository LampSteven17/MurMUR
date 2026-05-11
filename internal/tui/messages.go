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

// RefreshMsg is dispatched when the user explicitly requests a refresh.
// Views translate it into a fetch tea.Cmd.
type RefreshMsg struct{}
