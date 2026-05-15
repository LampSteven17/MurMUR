package proxmox

import (
	"context"
	"fmt"
	"net/url"
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

// NodeTask is one entry from GET /nodes/{n}/tasks.
type NodeTask struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	Type      string `json:"type"`              // qmstart | qmstop | vzdump | …
	ID        string `json:"id,omitempty"`      // VMID as string when applicable
	User      string `json:"user"`
	Status    string `json:"status,omitempty"`
	StartTime int64  `json:"starttime,omitempty"`
	EndTime   int64  `json:"endtime,omitempty"` // 0 if still running
}

// Appliance is one row from GET /nodes/{n}/aplinfo — a downloadable LXC
// template from PVE's curated repository. Used by aplinfo POST to fetch a
// vztmpl into storage.
type Appliance struct {
	Template     string `json:"template"`     // "debian-13-standard_13.1-2_amd64.tar.zst"
	OS           string `json:"os"`           // "debian-13" — the natural join key with our image catalog
	Section      string `json:"section"`      // "system" | "turnkeylinux" | ...
	Package      string `json:"package"`      // "debian-13-standard"
	Version      string `json:"version"`      // "13.1-2"
	Architecture string `json:"architecture"` // "amd64"
	Description  string `json:"description,omitempty"`
	Headline     string `json:"headline,omitempty"`
	Location     string `json:"location,omitempty"`
}

// ListAppliances returns all downloadable LXC templates known to PVE
// (equivalent to `pveam available`). Bulk list; filter on the caller side.
func (c *Client) ListAppliances(ctx context.Context, node string) ([]Appliance, error) {
	var out []Appliance
	if err := c.GetJSON(ctx, fmt.Sprintf("/nodes/%s/aplinfo", node), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DownloadAppliance kicks off `pveam download <storage> <template>` via the
// API. Returns the UPID; use WaitForTask. The file lands as the volume id
// "<storage>:vztmpl/<template>" once the task completes.
func (c *Client) DownloadAppliance(ctx context.Context, node, storage, template string) (string, error) {
	form := url.Values{}
	form.Set("storage", storage)
	form.Set("template", template)
	raw, err := c.PostForm(ctx, fmt.Sprintf("/nodes/%s/aplinfo", node), form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// AptPackage is one row from GET /nodes/{n}/apt/update.
type AptPackage struct {
	Package     string `json:"Package"`
	Title       string `json:"Title,omitempty"`
	Description string `json:"Description,omitempty"`
	Section     string `json:"Section,omitempty"`
	Origin      string `json:"Origin,omitempty"`
	Priority    string `json:"Priority,omitempty"`
	Arch        string `json:"Arch,omitempty"`
	OldVersion  string `json:"OldVersion,omitempty"`
	Version     string `json:"Version"` // new version available
}

// ListAptUpdates returns the apt packages currently flagged as upgradable on
// a node. Reflects the cached package list — call RefreshAptIndex first if
// you want fresh data.
func (c *Client) ListAptUpdates(ctx context.Context, node string) ([]AptPackage, error) {
	var out []AptPackage
	if err := c.GetJSON(ctx, fmt.Sprintf("/nodes/%s/apt/update", node), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// RefreshAptIndex runs `apt-get update` on the node via PVE. Returns the
// UPID; use WaitForTask. Equivalent to clicking "Refresh" in PVE web UI
// per-node Updates tab.
func (c *Client) RefreshAptIndex(ctx context.Context, node string) (string, error) {
	raw, err := c.PostForm(ctx, fmt.Sprintf("/nodes/%s/apt/update", node), nil)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// ListNodeTasks returns tasks for one node. Source selects active|all|archive
// (default archive). Use "active" to find in-flight ops for transient status.
func (c *Client) ListNodeTasks(ctx context.Context, node, source string) ([]NodeTask, error) {
	path := fmt.Sprintf("/nodes/%s/tasks", node)
	if source != "" {
		path += "?source=" + source
	}
	var out []NodeTask
	if err := c.GetJSON(ctx, path, &out); err != nil {
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

// NextVMID returns the next free VMID per /cluster/nextid. ProxMox returns it
// as a JSON string; we parse to int.
func (c *Client) NextVMID(ctx context.Context) (int, error) {
	var s string
	if err := c.GetJSON(ctx, "/cluster/nextid", &s); err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, fmt.Errorf("proxmox: NextVMID: parsing %q: %w", s, err)
	}
	return n, nil
}

// StorageContent is one entry from GET /nodes/{n}/storage/{s}/content.
type StorageContent struct {
	VolID   string `json:"volid"`   // e.g. "cephfs:vztmpl/debian-13-standard_13.0-1_amd64.tar.zst"
	Content string `json:"content"` // iso | vztmpl | images | rootdir | backup
	Format  string `json:"format,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Ctime   int64  `json:"ctime,omitempty"` // unix epoch — file mtime on the storage
}

// ListStorageContent returns the content of a storage on a node, optionally
// filtered by content kind (iso | vztmpl | images | rootdir | backup).
func (c *Client) ListStorageContent(ctx context.Context, node, storage, contentKind string) ([]StorageContent, error) {
	path := fmt.Sprintf("/nodes/%s/storage/%s/content", node, storage)
	if contentKind != "" {
		path += "?content=" + contentKind
	}
	var out []StorageContent
	if err := c.GetJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteStorageVolume removes a single volume by volid (e.g.
// "cephfs:import/file.qcow2"). PVE's DELETE for storage content takes the
// volid as the final path segment, URL-encoded.
func (c *Client) DeleteStorageVolume(ctx context.Context, node, storage, volid string) (string, error) {
	path := fmt.Sprintf("/nodes/%s/storage/%s/content/%s", node, storage, url.PathEscape(volid))
	raw, err := c.Delete(ctx, path, nil)
	if err != nil {
		return "", err
	}
	upid, _ := decodeUPID(raw)
	return upid, nil
}

// DownloadURLToStorage downloads urlToFetch to the named storage on node as a
// file of the given content kind ("iso" for cloud-image qcow2/img,
// "vztmpl" for LXC template tarballs). Returns the UPID; the file lands as
// volume "<storage>:<content>/<filename>" once the task completes.
func (c *Client) DownloadURLToStorage(ctx context.Context, node, storage, urlToFetch, content, filename string) (string, error) {
	if node == "" || storage == "" || urlToFetch == "" || content == "" || filename == "" {
		return "", fmt.Errorf("proxmox: DownloadURLToStorage: node/storage/url/content/filename all required")
	}
	form := url.Values{}
	form.Set("url", urlToFetch)
	form.Set("content", content)
	form.Set("filename", filename)
	path := fmt.Sprintf("/nodes/%s/storage/%s/download-url", node, storage)
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
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
