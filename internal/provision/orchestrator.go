package provision

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// Orchestrator drives a deploy end-to-end against the ProxMox API.
//
// It is the single entry point a UI calls; callers attach a progress callback
// via SetProgress and invoke Deploy(ctx, req). All side effects (API calls,
// disk I/O for SSH keys) happen synchronously inside Deploy — wrap it in a
// goroutine / tea.Cmd at the call site if you don't want to block.
type Orchestrator struct {
	cfg      *config.Config
	client   *proxmox.Client
	progress func(ProgressEvent)

	// taskPoll is the polling cadence for WaitForTask. Override in tests.
	taskPoll time.Duration
	// agentMaxWait bounds the post-start wait for the guest agent / network.
	agentMaxWait time.Duration
}

func New(cfg *config.Config, client *proxmox.Client) *Orchestrator {
	return &Orchestrator{
		cfg:          cfg,
		client:       client,
		taskPoll: 2 * time.Second,
		// 10 min covers the first-boot apt-update + qemu-guest-agent install
		// on cold cloud images, including freshly-released distros with
		// slow mirrors. Once we bake the agent into the template, drop this
		// back to ~2 min (clone+start should be all that's left).
		agentMaxWait: 10 * time.Minute,
	}
}

// SetProgress installs a typed progress callback. Pass nil to disable.
func (o *Orchestrator) SetProgress(fn func(ProgressEvent)) { o.progress = fn }

func (o *Orchestrator) emit(step Step, msg string, pct float64) {
	if o.progress == nil {
		return
	}
	o.progress(ProgressEvent{Step: step, Message: msg, Percent: pct})
}

// Deploy provisions a guest per req. The request is validated and the
// flavor/image looked up in the config catalog before any API call is made,
// so misconfiguration fails before mutation.
func (o *Orchestrator) Deploy(ctx context.Context, req Request) (*Result, error) {
	if req.Name == "" {
		return nil, fmt.Errorf("deploy: Name is required")
	}
	if req.Type != "vm" && req.Type != "lxc" {
		return nil, fmt.Errorf("deploy: Type must be \"vm\" or \"lxc\", got %q", req.Type)
	}
	o.emit(StepResolve, "validating request", 5)

	flavor, err := o.resolveFlavor(req.Flavor)
	if err != nil {
		return nil, err
	}
	image, err := o.resolveImage(req.Image)
	if err != nil {
		return nil, err
	}
	r := resolvedRequest{req: req, flavor: flavor, image: image}

	if req.Type == "vm" {
		return o.deployVM(ctx, r)
	}
	return o.deployLXC(ctx, r)
}

func (o *Orchestrator) resolveFlavor(name string) (config.Flavor, error) {
	for _, f := range o.cfg.Flavors {
		if f.Name == name {
			return f, nil
		}
	}
	return config.Flavor{}, fmt.Errorf("deploy: unknown flavor %q (declare in cluster.yaml or use a builtin: 1vcpu.2gb / 2vcpu.4gb / 4vcpu.8gb / 8vcpu.16gb)", name)
}

func (o *Orchestrator) resolveImage(name string) (config.Image, error) {
	for _, i := range o.cfg.Images {
		if i.Name == name {
			return i, nil
		}
	}
	return config.Image{}, fmt.Errorf("deploy: unknown image %q (declare in cluster.yaml or use a builtin)", name)
}

// pickNode returns req.TargetNode if explicit, otherwise best-fit by free RAM.
func (o *Orchestrator) pickNode(ctx context.Context, flavor config.Flavor, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	resources, err := o.client.GetResources(ctx, "")
	if err != nil {
		return "", fmt.Errorf("deploy: listing cluster resources for placement: %w", err)
	}
	caps := ComputeCapacity(resources)
	return BestFit(caps, int64(flavor.MemoryMB)*1024*1024, flavor.CPU)
}

// findLXCTemplate scans the shared/iso storage on each node for a vztmpl
// tarball whose volid contains imageName. Returns the volid (e.g.
// "cephfs:vztmpl/debian-13-standard_13.0-1_amd64.tar.zst") on the first hit.
func (o *Orchestrator) findLXCTemplate(ctx context.Context, imageName, node string) (string, error) {
	// Prefer the shared template store, but fall back to vm_disk if shared is
	// empty.
	candidates := []string{o.cfg.Cluster.Storage.Shared, o.cfg.Cluster.Storage.ISO}
	seen := map[string]bool{}
	for _, store := range candidates {
		if store == "" || seen[store] {
			continue
		}
		seen[store] = true
		entries, err := o.client.ListStorageContent(ctx, node, store, "vztmpl")
		if err != nil {
			// Tolerate per-store errors and try the next candidate.
			continue
		}
		for _, e := range entries {
			if strings.Contains(e.VolID, imageName) {
				return e.VolID, nil
			}
		}
	}
	return "", fmt.Errorf(
		"deploy: no LXC template (vztmpl) matching %q on storages [%s] of node %s — "+
			"download one with `pveam download %s <template-name>` on the templates node "+
			"(murmur auto-build for LXC vztmpls is not yet implemented)",
		imageName, strings.Join(uniq(candidates), ", "), node,
		firstNonEmpty(o.cfg.Cluster.Storage.Shared, o.cfg.Cluster.Storage.ISO),
	)
}

// firstOnlineNode is a small helper: returns any online node name, useful when
// we just need a node to issue a query against shared storage.
func (o *Orchestrator) firstOnlineNode(ctx context.Context) string {
	resources, err := o.client.GetResources(ctx, "")
	if err != nil {
		return ""
	}
	for _, r := range resources {
		if r.Type == "node" && r.Status == "online" {
			return r.Node
		}
	}
	return ""
}

// authSummary describes which auth methods are in play, for the progress
// log. "key+password" is read as "either works to log in."
func authSummary(key, password bool) string {
	switch {
	case key && password:
		return "key+password"
	case key:
		return "key"
	case password:
		return "password"
	}
	return "no-auth"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// defaultUser returns the cloud-init / LXC default username for the image's
// distro: user-mapped users override builtins. Falls back to "root".
func (o *Orchestrator) defaultUser(image config.Image) string {
	if u, ok := o.cfg.Cluster.SSH.Users[image.Distro]; ok && u != "" {
		return u
	}
	return "root"
}

// readSSHPubKey reads the public-key counterpart of cluster.ssh.identity,
// expanding ~/ and resolving an optional trailing ".pub". Empty path → "".
func (o *Orchestrator) readSSHPubKey() (string, error) {
	id := o.cfg.Cluster.SSH.Identity
	if id == "" {
		return "", nil
	}
	if strings.HasPrefix(id, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding ~ in ssh.identity: %w", err)
		}
		id = filepath.Join(home, id[2:])
	}
	if !strings.HasSuffix(id, ".pub") {
		id += ".pub"
	}
	b, err := os.ReadFile(id)
	if err != nil {
		return "", fmt.Errorf("reading SSH public key at %s: %w", id, err)
	}
	return strings.TrimSpace(string(b)), nil
}

// waitTask polls a UPID until it finishes; returns a clean error if the task
// completed with non-OK exit status.
func (o *Orchestrator) waitTask(ctx context.Context, upid, label string) error {
	if upid == "" {
		return nil
	}
	status, err := o.client.WaitForTask(ctx, upid, o.taskPoll)
	if err != nil {
		return fmt.Errorf("%s (UPID %s): %w", label, upid, err)
	}
	if !status.OK() {
		return fmt.Errorf("%s (UPID %s): exit=%s", label, upid, status.ExitStatus)
	}
	return nil
}

// waitForVMIP polls the guest agent for the first IPv4. Returns the address
// or an error if maxWait elapses.
func (o *Orchestrator) waitForVMIP(ctx context.Context, node string, vmid int) (string, error) {
	deadline := time.Now().Add(o.agentMaxWait)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		ifaces, err := o.client.GuestAgentNetInterfaces(ctx, node, vmid)
		if err == nil {
			if ip := proxmox.FirstIPv4(ifaces); ip != "" {
				return ip, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out after %s waiting for guest agent IP", o.agentMaxWait)
		}
		time.Sleep(3 * time.Second)
	}
}

// waitForLXCIP polls /lxc/{vmid}/interfaces for the first IPv4. LXCs have no
// guest agent; PVE reads addresses directly from the container namespace.
func (o *Orchestrator) waitForLXCIP(ctx context.Context, node string, vmid int) (string, error) {
	deadline := time.Now().Add(o.agentMaxWait)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		ifaces, err := o.client.LXCInterfaces(ctx, node, vmid)
		if err == nil {
			if ip := proxmox.FirstLXCIPv4(ifaces); ip != "" {
				return ip, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("timed out after %s waiting for LXC IPv4", o.agentMaxWait)
		}
		time.Sleep(3 * time.Second)
	}
}

// deployVM: ensure template (build on demand) → clone → configure hw+
// cloud-init → resize disk → start → wait for guest agent IP.
func (o *Orchestrator) deployVM(ctx context.Context, r resolvedRequest) (*Result, error) {
	tplVMID, tplNode, err := o.EnsureVMTemplate(ctx, r.image)
	if err != nil {
		return nil, err
	}
	o.emit(StepResolve, fmt.Sprintf("template %s (VMID %d on %s)", r.image.Name, tplVMID, tplNode), 15)

	targetNode, err := o.pickNode(ctx, r.flavor, r.req.TargetNode)
	if err != nil {
		return nil, err
	}
	o.emit(StepResolve, fmt.Sprintf("target node %s", targetNode), 20)

	newVMID, err := o.client.NextVMID(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy: allocating VMID: %w", err)
	}
	o.emit(StepResolve, fmt.Sprintf("new VMID %d", newVMID), 25)

	// Clone.
	o.emit(StepClone, fmt.Sprintf("cloning template → %s/%d", targetNode, newVMID), 30)
	upid, err := o.client.CloneVM(ctx, proxmox.CloneVMRequest{
		SourceNode: tplNode,
		SourceVMID: tplVMID,
		NewVMID:    newVMID,
		Name:       r.req.Name,
		TargetNode: targetNode,
		Full:       true,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy: clone request: %w", err)
	}
	if err := o.waitTask(ctx, upid, "clone"); err != nil {
		return nil, err
	}

	// Configure hardware + cloud-init + agent.
	o.emit(StepConfigure, "applying hardware (cores/memory/agent)", 55)
	if err := o.client.ConfigureVMHardware(ctx, targetNode, newVMID, proxmox.VMHardware{
		Cores:        r.flavor.CPU,
		Sockets:      1,
		Memory:       r.flavor.MemoryMB,
		AgentEnabled: true,
	}); err != nil {
		return nil, fmt.Errorf("deploy: configure hardware: %w", err)
	}

	user := r.req.User
	if user == "" {
		user = o.defaultUser(r.image)
	}
	pubKey := r.req.SSHPubKey
	if pubKey == "" {
		k, err := o.readSSHPubKey()
		if err != nil {
			return nil, err
		}
		pubKey = k
	}
	password := o.cfg.Cluster.SSH.Password
	if pubKey == "" && password == "" {
		return nil, fmt.Errorf(
			"deploy: no guest auth configured — set cluster.ssh.identity (key) and/or cluster.ssh.password (password) in cluster.yaml; otherwise the guest will be unreachable")
	}
	// Agent is baked into the template at build time (installAgentInTemplate),
	// so clones don't need a vendor-data layer anymore. Standard cicustom-free
	// cloud-init: PVE auto-generates user-data from ciuser/sshkeys/ipconfig0.
	authNote := authSummary(pubKey != "", password != "")
	o.emit(StepConfigure, fmt.Sprintf("applying cloud-init (user=%s, %s, dhcp)", user, authNote), 65)
	ci := proxmox.CloudInitConfig{
		User:      user,
		Password:  password,
		IPConfig0: "ip=dhcp",
	}
	if pubKey != "" {
		ci.SSHKeys = []string{pubKey}
	}
	if err := o.client.ConfigureVMCloudInit(ctx, targetNode, newVMID, ci); err != nil {
		return nil, fmt.Errorf("deploy: configure cloud-init: %w", err)
	}

	// Resize the root disk to flavor.DiskGB. Templates conventionally use
	// scsi0; PVE rejects shrinks, so we only grow.
	o.emit(StepConfigure, fmt.Sprintf("resizing scsi0 → %dG", r.flavor.DiskGB), 75)
	if upid, err := o.client.ResizeVMDisk(ctx, targetNode, newVMID, "scsi0",
		fmt.Sprintf("%dG", r.flavor.DiskGB)); err == nil {
		_ = o.waitTask(ctx, upid, "resize") // non-fatal: shrink/no-op returns an error we tolerate
	}

	// Start.
	o.emit(StepStart, "starting VM", 80)
	upid, err = o.client.StartVM(ctx, targetNode, newVMID)
	if err != nil {
		return nil, fmt.Errorf("deploy: start VM: %w", err)
	}
	if err := o.waitTask(ctx, upid, "start"); err != nil {
		return nil, err
	}

	// Wait for guest agent IP.
	o.emit(StepIP, "waiting for guest agent IP", 90)
	ip, err := o.waitForVMIP(ctx, targetNode, newVMID)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	result := &Result{
		VMID: newVMID,
		Name: r.req.Name,
		Node: targetNode,
		IPv4: ip,
		User: user,
	}

	// Optional post-deploy step. The orchestrator stays runner-agnostic — it
	// just /bin/sh -c's whatever the caller passes, with GUEST_* env vars set.
	// AppsView wraps playbook paths with `ansible-playbook`; raw shell
	// commands flow through unchanged.
	if cmd := r.req.PostDeployCommand; cmd != "" {
		if err := o.runPostDeploy(ctx, cmd, r.req.WorkDir, result); err != nil {
			return result, fmt.Errorf("deploy: post-deploy: %w", err)
		}
	}

	o.emit(StepDone, fmt.Sprintf("ready · %s @ %s", r.req.Name, ip), 100)
	return result, nil
}

// deployLXC: pick node → resolve vztmpl → create → wait for IP.
//
// LXC create accepts a Start flag; we use it so create-and-start is one round
// trip. PVE reports IPs through /lxc/{vmid}/interfaces once the container
// userspace networking is up.
func (o *Orchestrator) deployLXC(ctx context.Context, r resolvedRequest) (*Result, error) {
	targetNode, err := o.pickNode(ctx, r.flavor, r.req.TargetNode)
	if err != nil {
		return nil, err
	}
	o.emit(StepResolve, fmt.Sprintf("target node %s", targetNode), 20)

	tplVolID, err := o.findLXCTemplate(ctx, r.image.Name, targetNode)
	if err != nil {
		return nil, err
	}
	o.emit(StepResolve, fmt.Sprintf("template %s", tplVolID), 30)

	newVMID, err := o.client.NextVMID(ctx)
	if err != nil {
		return nil, fmt.Errorf("deploy: allocating VMID: %w", err)
	}
	o.emit(StepResolve, fmt.Sprintf("new VMID %d", newVMID), 35)

	pubKey := r.req.SSHPubKey
	if pubKey == "" {
		k, err := o.readSSHPubKey()
		if err != nil {
			return nil, err
		}
		pubKey = k
	}

	storage := o.cfg.Cluster.Storage.VMDisk
	if storage == "" {
		return nil, fmt.Errorf("deploy: cluster.storage.vm_disk is required for LXC rootfs allocation")
	}
	bridge := o.cfg.Cluster.Network.DefaultBridge
	if bridge == "" {
		bridge = "vmbr0"
	}
	netIP := r.req.IPv4
	if netIP == "" {
		netIP = "dhcp"
	}

	createReq := proxmox.CreateLXCRequest{
		Node:         targetNode,
		VMID:         newVMID,
		OSTemplate:   tplVolID,
		OSType:       r.image.Distro,
		Hostname:     r.req.Name,
		Cores:        r.flavor.CPU,
		Memory:       r.flavor.MemoryMB,
		Swap:         r.flavor.MemoryMB / 2,
		DiskSize:     r.flavor.DiskGB,
		Storage:      storage,
		Bridge:       bridge,
		NetIP:        netIP,
		NetGateway:   r.req.Gateway,
		Unprivileged: true,
		Start:        true,
	}
	if pubKey != "" {
		createReq.SSHKeys = []string{pubKey}
	}
	if o.cfg.Cluster.Network.DefaultVLAN != nil {
		createReq.VLAN = *o.cfg.Cluster.Network.DefaultVLAN
	}

	o.emit(StepClone, fmt.Sprintf("creating LXC → %s/%d", targetNode, newVMID), 45)
	upid, err := o.client.CreateLXC(ctx, createReq)
	if err != nil {
		return nil, fmt.Errorf("deploy: create LXC: %w", err)
	}
	if err := o.waitTask(ctx, upid, "create-lxc"); err != nil {
		return nil, err
	}

	o.emit(StepStart, "LXC starting", 80)
	o.emit(StepIP, "waiting for network", 85)
	ip, err := o.waitForLXCIP(ctx, targetNode, newVMID)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	o.emit(StepDone, fmt.Sprintf("ready · %s @ %s", r.req.Name, ip), 100)
	return &Result{
		VMID: newVMID,
		Name: r.req.Name,
		Node: targetNode,
		IPv4: ip,
		User: "root",
	}, nil
}
