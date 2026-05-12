package proxmox

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Version is the response of GET /version.
type Version struct {
	Release string `json:"release"`
	RepoID  string `json:"repoid"`
	Version string `json:"version"`
}

// Major parses the major version number from Version, e.g. "9" from "9.1.2".
func (v Version) Major() int {
	var n int
	_, _ = fmt.Sscanf(v.Version, "%d", &n)
	return n
}

// Resource is one row from GET /cluster/resources. Fields vary by Type;
// callers should inspect Type before using Type-specific fields.
type Resource struct {
	Type    string  `json:"type"`             // node | qemu | lxc | storage | sdn | pool
	ID      string  `json:"id"`               // e.g. "qemu/107", "node/pve1"
	Node    string  `json:"node,omitempty"`   // host node for VMs/LXCs/storage
	Name    string  `json:"name,omitempty"`   // VM/LXC name; node name for nodes
	VMID    int     `json:"vmid,omitempty"`   // for qemu / lxc
	Status  string  `json:"status,omitempty"` // running | stopped | online | offline
	Template int    `json:"template,omitempty"` // 1 if the qemu/lxc row is a template
	MaxCPU  float64 `json:"maxcpu,omitempty"`
	MaxMem  int64   `json:"maxmem,omitempty"`
	MaxDisk int64   `json:"maxdisk,omitempty"`
	CPU     float64 `json:"cpu,omitempty"`
	Mem     int64   `json:"mem,omitempty"`
	Disk    int64   `json:"disk,omitempty"`
	Uptime  int64   `json:"uptime,omitempty"`
	Tags    string  `json:"tags,omitempty"`
	Storage string  `json:"storage,omitempty"`
}

// StorageEntry is one row from GET /storage.
type StorageEntry struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Nodes   string `json:"nodes,omitempty"`
	Shared  int    `json:"shared,omitempty"`
}

// NodeStatus is one row from GET /nodes.
type NodeStatus struct {
	Node   string  `json:"node"`
	Status string  `json:"status"` // online | offline | unknown
	Type   string  `json:"type"`
	CPU    float64 `json:"cpu,omitempty"`
	MaxCPU int     `json:"maxcpu,omitempty"`
	Mem    int64   `json:"mem,omitempty"`
	MaxMem int64   `json:"maxmem,omitempty"`
	Uptime int64   `json:"uptime,omitempty"`
	Level  string  `json:"level,omitempty"`
	SSLFP  string  `json:"ssl_fingerprint,omitempty"`
}

// GetVersion returns /version.
func (c *Client) GetVersion(ctx context.Context) (Version, error) {
	var v Version
	if err := c.GetJSON(ctx, "/version", &v); err != nil {
		return Version{}, err
	}
	return v, nil
}

// GetResources returns /cluster/resources. Pass an empty filter to fetch all,
// or one of: "vm", "storage", "node", "sdn".
func (c *Client) GetResources(ctx context.Context, filter string) ([]Resource, error) {
	path := "/cluster/resources"
	if filter != "" {
		path += "?type=" + filter
	}
	var out []Resource
	if err := c.GetJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetStorages returns /storage.
func (c *Client) GetStorages(ctx context.Context) ([]StorageEntry, error) {
	var out []StorageEntry
	if err := c.GetJSON(ctx, "/storage", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetNodes returns /nodes.
func (c *Client) GetNodes(ctx context.Context) ([]NodeStatus, error) {
	var out []NodeStatus
	if err := c.GetJSON(ctx, "/nodes", &out); err != nil {
		return nil, err
	}
	return out, nil
}

// EnsureStorages verifies that every required storage ID exists on the cluster.
// Returns a single error naming all missing IDs (loud-fail semantic check).
func (c *Client) EnsureStorages(ctx context.Context, required []string) error {
	have, err := c.GetStorages(ctx)
	if err != nil {
		return fmt.Errorf("proxmox: list storages: %w", err)
	}
	present := make(map[string]struct{}, len(have))
	for _, s := range have {
		present[s.Storage] = struct{}{}
	}
	var missing []string
	for _, r := range required {
		if _, ok := present[r]; !ok {
			missing = append(missing, r)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("proxmox: storage IDs not found on cluster: %s", strings.Join(missing, ", "))
	}
	return nil
}
