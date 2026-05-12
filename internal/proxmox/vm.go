package proxmox

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CloneVMRequest clones SourceVMID on SourceNode into NewVMID on TargetNode.
// SourceNode is required because clone is a per-node endpoint; TargetNode may
// equal SourceNode for same-host clones. The source VM is typically a
// template, though running VMs can be cloned via Snapname.
type CloneVMRequest struct {
	SourceNode   string // node hosting the template
	SourceVMID   int    // template/source vmid
	NewVMID      int    // vmid of the new VM
	Name         string // name of the new VM
	TargetNode   string // node the clone should land on; empty = same as SourceNode
	Full         bool   // true for full clone (decoupled disks), false for linked
	Storage      string // override target storage for full clones
	Description  string // optional description on the new VM
	Pool         string // optional resource pool
	Snapname     string // clone from a specific snapshot
}

// CloneVM creates a clone. Returns the UPID; use WaitForTask to block until
// completion.
func (c *Client) CloneVM(ctx context.Context, r CloneVMRequest) (string, error) {
	if r.SourceNode == "" {
		return "", fmt.Errorf("proxmox: CloneVM: SourceNode is required")
	}
	if r.SourceVMID <= 0 || r.NewVMID <= 0 {
		return "", fmt.Errorf("proxmox: CloneVM: SourceVMID and NewVMID must be > 0")
	}
	form := url.Values{}
	form.Set("newid", strconv.Itoa(r.NewVMID))
	if r.Name != "" {
		form.Set("name", r.Name)
	}
	if r.TargetNode != "" && r.TargetNode != r.SourceNode {
		form.Set("target", r.TargetNode)
	}
	if r.Full {
		form.Set("full", "1")
	}
	if r.Storage != "" {
		form.Set("storage", r.Storage)
	}
	if r.Description != "" {
		form.Set("description", r.Description)
	}
	if r.Pool != "" {
		form.Set("pool", r.Pool)
	}
	if r.Snapname != "" {
		form.Set("snapname", r.Snapname)
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", r.SourceNode, r.SourceVMID)
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// VMConfig is a free-form key/value map for /nodes/{n}/qemu/{vmid}/config.
// Use the typed helpers below for common fields; this exists for advanced
// callers who need to set keys murmur doesn't model yet.
type VMConfig map[string]string

// SetVMConfig issues PUT /config with the given fields. Returns the UPID if
// the call is async (background=1), or empty string on synchronous success.
func (c *Client) SetVMConfig(ctx context.Context, node string, vmid int, cfg VMConfig) error {
	if len(cfg) == 0 {
		return nil
	}
	form := url.Values{}
	for k, v := range cfg {
		form.Set(k, v)
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid)
	if _, err := c.PutForm(ctx, path, form); err != nil {
		return err
	}
	return nil
}

// CloudInitConfig is the subset of cloud-init keys murmur sets on a VM.
type CloudInitConfig struct {
	User         string   // ciuser
	Password     string   // cipassword (omit for key-only)
	SSHKeys      []string // sshkeys; URL-encoded for the API
	IPConfig0    string   // ipconfig0, e.g. "ip=dhcp" or "ip=10.0.0.50/24,gw=10.0.0.1"
	Nameserver   string   // nameserver
	SearchDomain string   // searchdomain
}

// ConfigureVMCloudInit sets cloud-init keys on a VM. Idempotent; only
// non-empty fields are written.
func (c *Client) ConfigureVMCloudInit(ctx context.Context, node string, vmid int, ci CloudInitConfig) error {
	cfg := VMConfig{}
	if ci.User != "" {
		cfg["ciuser"] = ci.User
	}
	if ci.Password != "" {
		cfg["cipassword"] = ci.Password
	}
	if len(ci.SSHKeys) > 0 {
		// ProxMox expects sshkeys URL-encoded once at the application layer;
		// the form encoder URL-encodes again, and ProxMox decodes once on
		// receipt, leaving our QueryEscape'd value for ProxMox to decode.
		cfg["sshkeys"] = url.QueryEscape(strings.Join(ci.SSHKeys, "\n"))
	}
	if ci.IPConfig0 != "" {
		cfg["ipconfig0"] = ci.IPConfig0
	}
	if ci.Nameserver != "" {
		cfg["nameserver"] = ci.Nameserver
	}
	if ci.SearchDomain != "" {
		cfg["searchdomain"] = ci.SearchDomain
	}
	return c.SetVMConfig(ctx, node, vmid, cfg)
}

// VMHardware is the subset of hardware fields murmur sets on a VM.
// Memory is in MB. Cores=0 means "leave unchanged".
type VMHardware struct {
	Cores        int    // cores
	Sockets      int    // sockets (default 1 if Cores set and Sockets 0)
	Memory       int    // memory in MB
	CPUType      string // cpu (e.g. "host")
	AgentEnabled bool   // enables qemu-guest-agent
	Description  string // description on the VM (used by traefik-port stamping etc.)
	Tags         string // tags (comma-separated)
}

// ConfigureVMHardware sets hardware fields. Non-zero/non-empty fields only.
func (c *Client) ConfigureVMHardware(ctx context.Context, node string, vmid int, h VMHardware) error {
	cfg := VMConfig{}
	if h.Cores > 0 {
		cfg["cores"] = strconv.Itoa(h.Cores)
	}
	if h.Sockets > 0 {
		cfg["sockets"] = strconv.Itoa(h.Sockets)
	}
	if h.Memory > 0 {
		cfg["memory"] = strconv.Itoa(h.Memory)
	}
	if h.CPUType != "" {
		cfg["cpu"] = h.CPUType
	}
	if h.AgentEnabled {
		cfg["agent"] = "enabled=1"
	}
	if h.Description != "" {
		cfg["description"] = h.Description
	}
	if h.Tags != "" {
		cfg["tags"] = h.Tags
	}
	return c.SetVMConfig(ctx, node, vmid, cfg)
}

// ResizeVMDisk grows a VM disk. size accepts ProxMox shorthand ("+8G", "64G").
// Returns the UPID.
func (c *Client) ResizeVMDisk(ctx context.Context, node string, vmid int, disk, size string) (string, error) {
	if disk == "" || size == "" {
		return "", fmt.Errorf("proxmox: ResizeVMDisk: disk and size are required")
	}
	form := url.Values{}
	form.Set("disk", disk)
	form.Set("size", size)
	path := fmt.Sprintf("/nodes/%s/qemu/%d/resize", node, vmid)
	raw, err := c.PutForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// vmStatusAction posts to /status/{action} and returns the UPID.
func (c *Client) vmStatusAction(ctx context.Context, node string, vmid int, action string, form url.Values) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/%s", node, vmid, action)
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// StartVM starts the VM. Returns the UPID.
func (c *Client) StartVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.vmStatusAction(ctx, node, vmid, "start", nil)
}

// StopVM hard-stops (power off) the VM. Returns the UPID.
func (c *Client) StopVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.vmStatusAction(ctx, node, vmid, "stop", nil)
}

// ShutdownVM requests an ACPI shutdown. timeoutSec=0 uses the ProxMox default.
// If forceStop is true, ProxMox falls back to hard stop after timeout.
func (c *Client) ShutdownVM(ctx context.Context, node string, vmid int, timeoutSec int, forceStop bool) (string, error) {
	form := url.Values{}
	if timeoutSec > 0 {
		form.Set("timeout", strconv.Itoa(timeoutSec))
	}
	if forceStop {
		form.Set("forceStop", "1")
	}
	return c.vmStatusAction(ctx, node, vmid, "shutdown", form)
}

// RebootVM cleanly reboots the VM (ACPI). Returns the UPID.
func (c *Client) RebootVM(ctx context.Context, node string, vmid int) (string, error) {
	return c.vmStatusAction(ctx, node, vmid, "reboot", nil)
}

// DestroyVM deletes the VM. If purge is true, removes from replication jobs
// and backup/HA configuration. If destroyUnreferencedDisks is true, removes
// disks not referenced in the current config (useful for orphaned state).
func (c *Client) DestroyVM(ctx context.Context, node string, vmid int, purge, destroyUnreferencedDisks bool) (string, error) {
	form := url.Values{}
	if purge {
		form.Set("purge", "1")
	}
	if destroyUnreferencedDisks {
		form.Set("destroy-unreferenced-disks", "1")
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d", node, vmid)
	raw, err := c.Delete(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}
