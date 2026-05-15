package proxmox

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CreateVMRequest creates a new VM via POST /nodes/{n}/qemu. Optional fields
// are emitted only if non-zero/non-empty. The typical template-build pattern
// passes SCSI0Import as a volume id (e.g. "cephfs:iso/debian-13.qcow2") so
// the create operation imports the cloud-image disk in one shot.
type CreateVMRequest struct {
	Node   string
	VMID   int
	Name   string
	Cores  int
	Memory int // MB

	// Firmware / machine type. Defaults are fine for cloud-image templates.
	BIOS    string // "ovmf" for UEFI; "" → SeaBIOS
	Machine string // "q35" for modern PCIe; "" → default

	// SCSI disk. SCSI0Import accepts an absolute path or a storage volume id
	// (e.g. "cephfs:iso/debian-13.qcow2") — PVE imports it on create.
	SCSIHW       string // "virtio-scsi-pci" recommended
	SCSI0Storage string // target storage for the imported disk, e.g. "ceph-rbd"
	SCSI0Import  string // source volume / path to import

	// Cloud-init drive. Format: "STORAGE:cloudinit".
	IDE2CloudInit string

	// Network. Format: "virtio,bridge=vmbr0[,tag=N]".
	Net0 string

	// Boot order. "order=scsi0" is correct for the disk pattern above.
	Boot string

	// Console.
	Serial0 string // "socket"
	VGA     string // "serial0" routes console to serial0

	// Misc.
	Agent       string // "enabled=1"
	OSType      string // "l26" for modern Linux
	CPU         string // "host" for nested perf
	Description string
	Tags        string
}

// CreateVM creates a VM. Returns the UPID; use WaitForTask.
func (c *Client) CreateVM(ctx context.Context, r CreateVMRequest) (string, error) {
	if r.Node == "" || r.VMID == 0 {
		return "", fmt.Errorf("proxmox: CreateVM: Node and VMID are required")
	}
	form := url.Values{}
	form.Set("vmid", strconv.Itoa(r.VMID))
	if r.Name != "" {
		form.Set("name", r.Name)
	}
	if r.Cores > 0 {
		form.Set("cores", strconv.Itoa(r.Cores))
	}
	if r.Memory > 0 {
		form.Set("memory", strconv.Itoa(r.Memory))
	}
	if r.BIOS != "" {
		form.Set("bios", r.BIOS)
	}
	if r.Machine != "" {
		form.Set("machine", r.Machine)
	}
	if r.SCSIHW != "" {
		form.Set("scsihw", r.SCSIHW)
	}
	if r.SCSI0Import != "" {
		storage := r.SCSI0Storage
		if storage == "" {
			storage = "local"
		}
		form.Set("scsi0", fmt.Sprintf("%s:0,import-from=%s,discard=on,ssd=1", storage, r.SCSI0Import))
	}
	if r.IDE2CloudInit != "" {
		form.Set("ide2", r.IDE2CloudInit)
	}
	if r.Net0 != "" {
		form.Set("net0", r.Net0)
	}
	if r.Boot != "" {
		form.Set("boot", r.Boot)
	}
	if r.Serial0 != "" {
		form.Set("serial0", r.Serial0)
	}
	if r.VGA != "" {
		form.Set("vga", r.VGA)
	}
	if r.Agent != "" {
		form.Set("agent", r.Agent)
	}
	if r.OSType != "" {
		form.Set("ostype", r.OSType)
	}
	if r.CPU != "" {
		form.Set("cpu", r.CPU)
	}
	if r.Description != "" {
		form.Set("description", r.Description)
	}
	if r.Tags != "" {
		form.Set("tags", r.Tags)
	}
	path := fmt.Sprintf("/nodes/%s/qemu", r.Node)
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// ConvertVMToTemplate marks a VM as a template (POST /qemu/{vmid}/template).
// PVE returns the UPID for the conversion task (which performs base-disk
// snapshotting on supported storages).
func (c *Client) ConvertVMToTemplate(ctx context.Context, node string, vmid int) (string, error) {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/template", node, vmid)
	raw, err := c.PostForm(ctx, path, nil)
	if err != nil {
		return "", err
	}
	// PVE returns null body when template conversion is synchronous on the
	// storage; otherwise a UPID. Tolerate both.
	upid, _ := decodeUPID(raw)
	return upid, nil
}

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

	// CICustom is PVE's cicustom string, e.g.
	//   "vendor=cephfs:snippets/cloud-init-ubuntu.yml"
	// vendor= is appended to the auto-generated user-data, keeping User/
	// SSHKeys/IPConfig0 intact while letting a snippet install qemu-guest-
	// agent (or similar). user= would REPLACE the auto-generated data.
	CICustom string
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
		// PVE form-decodes once, then validates the resulting string against
		// the `urlencoded` format regex (unreserved + %, no `+` for spaces).
		// QueryEscape uses form-encoding (space → +) which fails the regex —
		// StrictPercentEncode does RFC 3986 unreserved-only encoding.
		cfg["sshkeys"] = StrictPercentEncode(strings.Join(ci.SSHKeys, "\n"))
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
	if ci.CICustom != "" {
		cfg["cicustom"] = ci.CICustom
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
