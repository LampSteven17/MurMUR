package proxmox

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// CreateLXCRequest creates a new LXC container on Node.
type CreateLXCRequest struct {
	Node         string   // target node
	VMID         int      // vmid for the new container
	OSTemplate   string   // e.g. "cephfs:vztmpl/debian-13-standard_13.0-1_amd64.tar.zst"
	OSType       string   // optional: debian | ubuntu | alpine | fedora | centos | archlinux
	Hostname     string   // container hostname
	Cores        int      // cpu cores
	Memory       int      // memory in MB
	Swap         int      // swap in MB
	DiskSize     int      // rootfs size in GB
	Storage      string   // rootfs storage (e.g. "ceph-rbd")
	Bridge       string   // network bridge (e.g. "vmbr0")
	NetIP        string   // "dhcp" or "10.0.0.50/24"
	NetGateway   string   // gateway (ignored when NetIP="dhcp")
	VLAN         int      // optional VLAN tag for net0
	SSHKeys      []string // authorized keys for root
	Password     string   // root password (optional; key-only common)
	Unprivileged bool     // unprivileged container (default true is safer)
	Nesting      bool     // enable nesting (required for docker-in-lxc)
	KeyCtl       bool     // enable keyctl (required by some apps)
	Description  string
	Tags         string
	Start        bool // start after create
}

// CreateLXC creates the container. Returns the UPID.
func (c *Client) CreateLXC(ctx context.Context, r CreateLXCRequest) (string, error) {
	if r.Node == "" {
		return "", fmt.Errorf("proxmox: CreateLXC: Node is required")
	}
	if r.VMID <= 0 {
		return "", fmt.Errorf("proxmox: CreateLXC: VMID must be > 0")
	}
	if r.OSTemplate == "" {
		return "", fmt.Errorf("proxmox: CreateLXC: OSTemplate is required")
	}
	if r.Storage == "" || r.DiskSize <= 0 {
		return "", fmt.Errorf("proxmox: CreateLXC: Storage and DiskSize are required")
	}
	if r.Bridge == "" {
		return "", fmt.Errorf("proxmox: CreateLXC: Bridge is required")
	}

	form := url.Values{}
	form.Set("vmid", strconv.Itoa(r.VMID))
	form.Set("ostemplate", r.OSTemplate)
	if r.OSType != "" {
		form.Set("ostype", r.OSType)
	}
	if r.Hostname != "" {
		form.Set("hostname", r.Hostname)
	}
	if r.Cores > 0 {
		form.Set("cores", strconv.Itoa(r.Cores))
	}
	if r.Memory > 0 {
		form.Set("memory", strconv.Itoa(r.Memory))
	}
	if r.Swap > 0 {
		form.Set("swap", strconv.Itoa(r.Swap))
	}
	form.Set("rootfs", fmt.Sprintf("%s:%d", r.Storage, r.DiskSize))

	netParts := []string{"name=eth0", "bridge=" + r.Bridge}
	if r.NetIP == "" || r.NetIP == "dhcp" {
		netParts = append(netParts, "ip=dhcp")
	} else {
		netParts = append(netParts, "ip="+r.NetIP)
		if r.NetGateway != "" {
			netParts = append(netParts, "gw="+r.NetGateway)
		}
	}
	if r.VLAN > 0 {
		netParts = append(netParts, "tag="+strconv.Itoa(r.VLAN))
	}
	form.Set("net0", strings.Join(netParts, ","))

	if len(r.SSHKeys) > 0 {
		// LXC's ssh-public-keys field is plain OpenSSH text (one key per
		// line) — type:string with no format constraint. PVE runs ssh-keygen
		// -l -f against each line, so we send the raw key. (Contrast with
		// VM's sshkeys, which IS format=urlencoded and needs
		// StrictPercentEncode — different field, different validator.)
		form.Set("ssh-public-keys", strings.Join(r.SSHKeys, "\n"))
	}
	if r.Password != "" {
		form.Set("password", r.Password)
	}
	// Default to unprivileged when not explicitly set: safer baseline.
	// Callers who need privileged must opt in by setting Unprivileged=false
	// *and* leaving the default flow — explicit field-only API for now.
	if r.Unprivileged {
		form.Set("unprivileged", "1")
	}

	var feats []string
	if r.Nesting {
		feats = append(feats, "nesting=1")
	}
	if r.KeyCtl {
		feats = append(feats, "keyctl=1")
	}
	if len(feats) > 0 {
		form.Set("features", strings.Join(feats, ","))
	}
	if r.Description != "" {
		form.Set("description", r.Description)
	}
	if r.Tags != "" {
		form.Set("tags", r.Tags)
	}
	if r.Start {
		form.Set("start", "1")
	}

	path := fmt.Sprintf("/nodes/%s/lxc", r.Node)
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// lxcStatusAction posts to /status/{action} and returns the UPID.
func (c *Client) lxcStatusAction(ctx context.Context, node string, vmid int, action string, form url.Values) (string, error) {
	path := fmt.Sprintf("/nodes/%s/lxc/%d/status/%s", node, vmid, action)
	raw, err := c.PostForm(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// StartLXC starts the container. Returns the UPID.
func (c *Client) StartLXC(ctx context.Context, node string, vmid int) (string, error) {
	return c.lxcStatusAction(ctx, node, vmid, "start", nil)
}

// StopLXC hard-stops the container. Returns the UPID.
func (c *Client) StopLXC(ctx context.Context, node string, vmid int) (string, error) {
	return c.lxcStatusAction(ctx, node, vmid, "stop", nil)
}

// ShutdownLXC requests a clean shutdown. timeoutSec=0 uses the ProxMox default.
// If forceStop is true, ProxMox falls back to hard stop after timeout.
func (c *Client) ShutdownLXC(ctx context.Context, node string, vmid int, timeoutSec int, forceStop bool) (string, error) {
	form := url.Values{}
	if timeoutSec > 0 {
		form.Set("timeout", strconv.Itoa(timeoutSec))
	}
	if forceStop {
		form.Set("forceStop", "1")
	}
	return c.lxcStatusAction(ctx, node, vmid, "shutdown", form)
}

// RebootLXC cleanly reboots the container. Returns the UPID.
func (c *Client) RebootLXC(ctx context.Context, node string, vmid int) (string, error) {
	return c.lxcStatusAction(ctx, node, vmid, "reboot", nil)
}

// GetLXCConfig returns the current container config (ostype, hostname,
// rootfs, net0, etc.) as a flat string map. PVE returns mixed types in the
// JSON (some int, some string), so we decode through `any` and stringify
// every value — useful for reading distro family from `ostype` since LXCs
// don't have a guest agent.
func (c *Client) GetLXCConfig(ctx context.Context, node string, vmid int) (map[string]string, error) {
	var raw map[string]any
	if err := c.GetJSON(ctx, fmt.Sprintf("/nodes/%s/lxc/%d/config", node, vmid), &raw); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if v == nil {
			continue
		}
		out[k] = fmt.Sprintf("%v", v)
	}
	return out, nil
}

// SetLXCConfig issues PUT /config with arbitrary keys (rootfs, hostname,
// description, net0, memory, cores, swap, features, tags, ...).
func (c *Client) SetLXCConfig(ctx context.Context, node string, vmid int, cfg map[string]string) error {
	if len(cfg) == 0 {
		return nil
	}
	form := url.Values{}
	for k, v := range cfg {
		form.Set(k, v)
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d/config", node, vmid)
	if _, err := c.PutForm(ctx, path, form); err != nil {
		return err
	}
	return nil
}

// DestroyLXC deletes the container. purge removes from backup/replication;
// destroyUnreferencedDisks removes orphaned disk volumes.
func (c *Client) DestroyLXC(ctx context.Context, node string, vmid int, purge, destroyUnreferencedDisks bool) (string, error) {
	form := url.Values{}
	if purge {
		form.Set("purge", "1")
	}
	if destroyUnreferencedDisks {
		form.Set("destroy-unreferenced-disks", "1")
	}
	path := fmt.Sprintf("/nodes/%s/lxc/%d", node, vmid)
	raw, err := c.Delete(ctx, path, form)
	if err != nil {
		return "", err
	}
	return decodeUPID(raw)
}

// LXCInterface is one entry from /nodes/{n}/lxc/{vmid}/interfaces.
type LXCInterface struct {
	Name   string `json:"name"`
	HWAddr string `json:"hwaddr"`
	Inet   string `json:"inet,omitempty"`  // "10.0.0.50/24"
	Inet6  string `json:"inet6,omitempty"` // "fe80::.../64"
}

// LXCInterfaces returns the network interfaces inside the running container.
// Empty result while the container is still booting / DHCP hasn't completed.
func (c *Client) LXCInterfaces(ctx context.Context, node string, vmid int) ([]LXCInterface, error) {
	var out []LXCInterface
	path := fmt.Sprintf("/nodes/%s/lxc/%d/interfaces", node, vmid)
	if err := c.GetJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// FirstLXCIPv4 returns the first non-loopback IPv4 address (without CIDR) from
// an LXC interfaces list, or "" if none reported yet.
func FirstLXCIPv4(ifaces []LXCInterface) string {
	for _, i := range ifaces {
		if strings.HasPrefix(i.Name, "lo") {
			continue
		}
		if i.Inet == "" {
			continue
		}
		addr := i.Inet
		if idx := strings.Index(addr, "/"); idx >= 0 {
			addr = addr[:idx]
		}
		if strings.HasPrefix(addr, "127.") {
			continue
		}
		return addr
	}
	return ""
}
