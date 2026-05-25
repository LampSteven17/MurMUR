package provision

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// templateBuildMarker is the PVE tag murmur sets on every template it builds.
// EnsureVMTemplate uses its presence on an exact-name template to decide
// whether the template is current. Bump the version suffix when the build
// flow changes meaningfully (e.g., a different snippet semantic) — existing
// templates will be detected as stale and auto-rebuilt on the next deploy.
const templateBuildMarker = "murmur-bake-v1"

// errTemplateNotFound is the sentinel returned by locateVMTemplate when no
// template name matches the image (exact or fuzzy).
var errTemplateNotFound = errors.New("no matching VM template")

// EnsureVMTemplate returns (vmid, node) of a template matching the image,
// building it on demand if no existing template matches. The build path:
//
//  1. Pick a node tagged role=templates (or fail loudly if none).
//  2. Download the image's URL to ISO storage via PVE's server-side
//     download-url endpoint.
//  3. Create a new VM with scsi0=STORAGE:0,import-from=<volid>, a cloud-init
//     drive, serial console, and the qemu-guest-agent flag set.
//  4. Convert the VM to a template.
//
// Cluster-shared storage (e.g. cephfs) is assumed for the iso content; the
// imported disk lands on cluster.storage.vm_disk so subsequent clones can
// target any node.
func (o *Orchestrator) EnsureVMTemplate(ctx context.Context, image config.Image) (int, string, error) {
	vmid, node, stale, err := o.locateVMTemplate(ctx, image.Name)
	switch {
	case errors.Is(err, errTemplateNotFound):
		return o.buildVMTemplate(ctx, image)
	case err != nil:
		return 0, "", err
	case stale:
		o.emit(StepResolve, fmt.Sprintf(
			"template %q (VMID %d on %s) lacks the %q marker — destroying + rebuilding",
			image.Name, vmid, node, templateBuildMarker), 4)
		upid, derr := o.client.DestroyVM(ctx, node, vmid, true, true)
		if derr != nil {
			return 0, "", fmt.Errorf("destroy stale template: %w", derr)
		}
		if derr := o.waitTask(ctx, upid, "destroy-stale-template"); derr != nil {
			return 0, "", derr
		}
		return o.buildVMTemplate(ctx, image)
	}
	return vmid, node, nil
}

// locateVMTemplate looks for an existing template matching the image name.
// Exact-name matches are expected to carry the templateBuildMarker tag —
// missing the marker means the template is stale (pre-bake-in or from an
// older bake version) and caller will destroy + rebuild it. Fuzzy-name
// matches (substring) are assumed user-built; we don't auto-destroy them.
func (o *Orchestrator) locateVMTemplate(ctx context.Context, imageName string) (int, string, bool, error) {
	resources, err := o.client.GetResources(ctx, "vm")
	if err != nil {
		return 0, "", false, fmt.Errorf("locate template: listing VMs: %w", err)
	}
	var exact, fuzzy *proxmox.Resource
	for i := range resources {
		r := &resources[i]
		if r.Type != "qemu" || r.Template != 1 {
			continue
		}
		if r.Name == imageName {
			exact = r
			break
		}
		if fuzzy == nil && strings.Contains(r.Name, imageName) {
			fuzzy = r
		}
	}
	if exact != nil {
		return exact.VMID, exact.Node, !hasTemplateMarker(exact.Tags), nil
	}
	if fuzzy != nil {
		// user-named template — trust it, don't touch.
		return fuzzy.VMID, fuzzy.Node, false, nil
	}
	return 0, "", false, errTemplateNotFound
}

// importFileExists checks whether a file by the given name is already
// present in <storage>:import/. We use this to skip re-download on
// retried/stale template builds — PVE's download-url returns
// "refusing to override existing file" otherwise.
func (o *Orchestrator) importFileExists(ctx context.Context, node, storage, filename string) (bool, error) {
	target := fmt.Sprintf("%s:import/%s", storage, filename)
	entries, err := o.client.ListStorageContent(ctx, node, storage, "import")
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.VolID == target {
			return true, nil
		}
	}
	return false, nil
}

// hasTemplateMarker returns true if tags contains the templateBuildMarker.
// PVE serialises tags as ";" separated (newer) or "," separated (older); we
// accept either.
func hasTemplateMarker(tags string) bool {
	if tags == "" {
		return false
	}
	for _, sep := range []string{";", ","} {
		for _, t := range strings.Split(tags, sep) {
			if strings.TrimSpace(t) == templateBuildMarker {
				return true
			}
		}
	}
	return false
}

// buildVMTemplate executes the full create-from-URL pipeline.
func (o *Orchestrator) buildVMTemplate(ctx context.Context, image config.Image) (int, string, error) {
	if image.URL == "" {
		return 0, "", fmt.Errorf("build template %q: image.URL is empty in cluster.yaml catalog", image.Name)
	}
	node := o.templatesNode()
	if node == "" {
		return 0, "", fmt.Errorf("build template %q: no node tagged role=templates in cluster.yaml", image.Name)
	}
	isoStorage := o.cfg.Cluster.Storage.ISO
	if isoStorage == "" {
		return 0, "", fmt.Errorf("build template %q: cluster.storage.iso is required", image.Name)
	}
	diskStorage := o.cfg.Cluster.Storage.VMDisk
	if diskStorage == "" {
		return 0, "", fmt.Errorf("build template %q: cluster.storage.vm_disk is required", image.Name)
	}

	// Pre-flight: the chosen iso storage must advertise "import" content so
	// that the qcow2 we land there can be referenced by VM-create's
	// `scsi0=STORAGE:0,import-from=VOLID` parameter. PVE rejects volids on
	// iso-content storages with HTTP 400 ("wrong type 'iso' — needs to be
	// 'images' or 'import'").
	if err := o.checkStorageSupports(ctx, isoStorage, "import"); err != nil {
		return 0, "", err
	}

	// PVE's download-url with content=import validates the destination
	// filename against UPLOAD_IMPORT_EXT_RE_1 = /\.(ova|qcow2|raw|vmdk)/.
	// Ubuntu's cloud-image URLs end in .img (despite being qcow2-format),
	// so we rename on download. Everything else falls through unchanged.
	filename := normalizeImportFilename(path.Base(image.URL))

	// Cache check: if the qcow2 is already in <storage>:import/, reuse it.
	// PVE's download-url refuses to overwrite, and there's no point burning
	// 800MB of network for the same bytes. The operator can blow the cache
	// manually if they want a fresh fetch.
	cached, err := o.importFileExists(ctx, node, isoStorage, filename)
	if err != nil {
		return 0, "", fmt.Errorf("check import cache: %w", err)
	}
	if cached {
		o.emit(StepResolve, fmt.Sprintf("cache hit: reusing %s:import/%s", isoStorage, filename), 10)
	} else {
		o.emit(StepResolve, fmt.Sprintf("building template %q on %s — downloading %s → %s:import",
			image.Name, node, filename, isoStorage), 8)
		upid, err := o.client.DownloadURLToStorage(ctx, node, isoStorage, image.URL, "import", filename)
		if err != nil {
			return 0, "", fmt.Errorf("download URL: %w", err)
		}
		if err := o.waitTask(ctx, upid, "download-url"); err != nil {
			return 0, "", err
		}
	}

	vmid, err := o.client.NextVMID(ctx)
	if err != nil {
		return 0, "", fmt.Errorf("allocate template VMID: %w", err)
	}
	o.emit(StepResolve, fmt.Sprintf("creating template VM %d (import from %s:import/%s)",
		vmid, isoStorage, filename), 12)

	createReq := proxmox.CreateVMRequest{
		Node:          node,
		VMID:          vmid,
		Name:          image.Name,
		Cores:         1,
		Memory:        512,
		SCSIHW:        "virtio-scsi-pci",
		SCSI0Storage:  diskStorage,
		SCSI0Import:   fmt.Sprintf("%s:import/%s", isoStorage, filename),
		IDE2CloudInit: fmt.Sprintf("%s:cloudinit", diskStorage),
		Net0:          fmt.Sprintf("virtio,bridge=%s", o.netBridge()),
		Boot:          "order=scsi0",
		Serial0:       "socket",
		VGA:           "serial0",
		Agent:         "enabled=1",
		OSType:        "l26",
		CPU:           "host",
		Description:   fmt.Sprintf("murmur template · %s · %s", image.Distro, image.URL),
	}
	createUPID, err := o.client.CreateVM(ctx, createReq)
	if err != nil {
		return 0, "", fmt.Errorf("create template VM: %w", err)
	}
	if err := o.waitTask(ctx, createUPID, "create-template-vm"); err != nil {
		return 0, "", err
	}

	// Bake qemu-guest-agent into the template: boot once with cicustom
	// user-data that installs+starts the agent, wait for it to respond,
	// shut down cleanly, clear the build-time cicustom. Future clones come
	// up with the agent already running — no first-boot apt install dance.
	// This also unlocks tiny-cloud distros (Alpine) which don't honor
	// vendor-data; user-data is universally supported.
	if err := o.installAgentInTemplate(ctx, node, vmid, image); err != nil {
		return 0, "", err
	}

	o.emit(StepResolve, fmt.Sprintf("converting VM %d → template", vmid), 18)
	convertUPID, err := o.client.ConvertVMToTemplate(ctx, node, vmid)
	if err != nil {
		return 0, "", fmt.Errorf("convert to template: %w", err)
	}
	if err := o.waitTask(ctx, convertUPID, "convert-template"); err != nil {
		return 0, "", err
	}
	// Drop the template into the shared templates pool so deployers (who hold
	// PVETemplateUser on that pool, and nothing on the templates otherwise) can
	// clone it. Non-fatal: an unpooled template still works for admin, but
	// surface it loudly since deployers won't be able to clone until it's pooled.
	if err := o.ensureTemplatePooled(ctx, vmid); err != nil {
		o.emit(StepResolve, "warning: "+err.Error()+" — deployers can't clone this template until it's pooled", 19)
	}
	o.emit(StepResolve, fmt.Sprintf("template %q ready (VMID %d on %s)", image.Name, vmid, node), 19)
	return vmid, node, nil
}

// ensureTemplatePooled creates the shared templates pool if absent and adds the
// template VMID to it. Deployers are granted PVETemplateUser on this pool, so
// pool membership is what lets them clone the template.
func (o *Orchestrator) ensureTemplatePooled(ctx context.Context, vmid int) error {
	if err := o.client.CreatePool(ctx, config.TemplatesPoolID, "murmur shared templates — operators clone from here"); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("ensure %s pool: %w", config.TemplatesPoolID, err)
		}
	}
	if err := o.client.AddPoolMember(ctx, config.TemplatesPoolID, []int{vmid}); err != nil {
		return fmt.Errorf("add template %d to %s pool: %w", vmid, config.TemplatesPoolID, err)
	}
	return nil
}

// installAgentInTemplate boots the freshly-created template VM once with a
// distro-specific cloud-init snippet that installs qemu-guest-agent. We poll
// the guest agent as the success signal (it only responds when installed +
// started), then shut down and clear the build-time cicustom so clones don't
// re-run the install on every boot.
func (o *Orchestrator) installAgentInTemplate(ctx context.Context, node string, vmid int, image config.Image) error {
	storage := o.cfg.Cluster.Storage.Shared
	if storage == "" {
		return fmt.Errorf("build template: cluster.storage.shared is required (need a snippets-capable storage for the agent-install user-data)")
	}
	snippet := fmt.Sprintf("%s:snippets/cloud-init-%s.yml", storage, image.Distro)
	if err := o.verifySnippetExists(ctx, storage, snippet); err != nil {
		return err
	}

	o.emit(StepConfigure, fmt.Sprintf("setting build-time user-data → %s", snippet), 13)
	// ipconfig0 is REQUIRED for cloud-init to generate network-config in the
	// cidata drive. Without it, snippets that need `apt-get update` (Debian/
	// Ubuntu/Fedora) silently fail because the network never comes up.
	// Alpine's snippet brings up DHCP manually in bootcmd, but every other
	// distro relies on PVE-generated network-config.
	if err := o.client.SetVMConfig(ctx, node, vmid, proxmox.VMConfig{
		"cicustom":  "user=" + snippet,
		"ipconfig0": "ip=dhcp",
	}); err != nil {
		return fmt.Errorf("build template: set cicustom + ipconfig0: %w", err)
	}

	o.emit(StepStart, "booting template VM to install agent", 14)
	upid, err := o.client.StartVM(ctx, node, vmid)
	if err != nil {
		return fmt.Errorf("build template: start: %w", err)
	}
	if err := o.waitTask(ctx, upid, "start-template"); err != nil {
		return err
	}

	o.emit(StepIP, "waiting for guest-agent to come up (running cloud-init)…", 15)
	if err := o.waitForAgentInTemplate(ctx, node, vmid); err != nil {
		return fmt.Errorf("build template: %w", err)
	}

	o.emit(StepStop, "agent installed; shutting down template", 17)
	upid, err = o.client.ShutdownVM(ctx, node, vmid, 60, true)
	if err != nil {
		return fmt.Errorf("build template: shutdown: %w", err)
	}
	if err := o.waitTask(ctx, upid, "shutdown-template"); err != nil {
		return err
	}

	// One config write does two things: clear the build-time cicustom (so
	// clones don't re-run the install snippet) and stamp the bake-in marker
	// tag (so EnsureVMTemplate can tell murmur-built templates from older
	// ones / user-built ones on later deploys).
	o.emit(StepConfigure, "clearing cicustom + stamping bake marker", 18)
	if err := o.client.SetVMConfig(ctx, node, vmid, proxmox.VMConfig{
		"delete": "cicustom",
		"tags":   templateBuildMarker,
	}); err != nil {
		return fmt.Errorf("build template: clean config: %w", err)
	}
	return nil
}

// waitForAgentInTemplate polls the guest agent until it responds, capped at
// 15 min to forgive slow apt/apk mirrors on fresh distros. Successful
// response = agent installed + started + serial port wired up.
func (o *Orchestrator) waitForAgentInTemplate(ctx context.Context, node string, vmid int) error {
	deadline := time.Now().Add(15 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		// GuestAgentNetInterfaces requires the agent to be running. We don't
		// need the IP for templates — just the fact that the call succeeds.
		if _, err := o.client.GuestAgentNetInterfaces(ctx, node, vmid); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent never came up within 15 min (cloud-init may be hung — check the template VM's serial console)")
		}
		time.Sleep(10 * time.Second)
	}
}

// verifySnippetExists confirms the snippet volid is listed by the storage's
// snippets content. Same logic as the older deploy-time resolveAgentSnippet
// but used at template build time now.
func (o *Orchestrator) verifySnippetExists(ctx context.Context, storage, volid string) error {
	node := o.templatesNode()
	if node == "" {
		node = o.firstOnlineNode(ctx)
	}
	if node == "" {
		return fmt.Errorf("no online node available to verify snippet on storage %q", storage)
	}
	entries, err := o.client.ListStorageContent(ctx, node, storage, "snippets")
	if err != nil {
		return fmt.Errorf("listing %s:snippets to verify %s: %w", storage, volid, err)
	}
	for _, e := range entries {
		if e.VolID == volid {
			return nil
		}
	}
	return fmt.Errorf(
		"build template: missing snippet %q — needed to install qemu-guest-agent in the template. "+
			"Provide a #cloud-config file at /mnt/pve/%s/snippets/%s that installs qemu-guest-agent and starts the service.",
		volid, storage, filepath.Base(volid))
}

// templatesNode returns the first node tagged role=templates in cluster.yaml,
// or "" if none.
func (o *Orchestrator) templatesNode() string {
	for _, n := range o.cfg.Cluster.Nodes {
		for _, r := range n.Roles {
			if r == "templates" {
				return n.Name
			}
		}
	}
	return ""
}

// netBridge returns cluster.network.default_bridge (defaulting to "vmbr0").
func (o *Orchestrator) netBridge() string {
	if b := o.cfg.Cluster.Network.DefaultBridge; b != "" {
		return b
	}
	return "vmbr0"
}

// checkStorageSupports verifies that the named storage advertises the given
// content type (e.g. "iso", "vztmpl"). PVE returns a flat content string like
// "iso,vztmpl,backup,snippets". Failure surfaces an actionable error that
// names both ways to resolve: enable the content in PVE or change cluster.yaml.
func (o *Orchestrator) checkStorageSupports(ctx context.Context, storage, content string) error {
	all, err := o.client.GetStorages(ctx)
	if err != nil {
		return fmt.Errorf("checking storage capabilities: %w", err)
	}
	for _, s := range all {
		if s.Storage != storage {
			continue
		}
		for _, c := range strings.Split(s.Content, ",") {
			if strings.TrimSpace(c) == content {
				return nil
			}
		}
		return fmt.Errorf(
			"storage %q does not have %q content enabled (currently: %q) — "+
				"enable it in PVE (Datacenter → Storage → %s → Edit → check the matching content), "+
				"or change cluster.storage.%s in cluster.yaml to a storage that already advertises %q",
			storage, content, s.Content, storage, contentField(content), content)
	}
	return fmt.Errorf("storage %q not found on cluster", storage)
}

// normalizeImportFilename rewrites filenames whose extensions PVE's
// content=import filter rejects (notably Ubuntu's .img cloud images, which
// are qcow2 in everything but name) into a .qcow2 filename. Other extensions
// pass through unchanged.
func normalizeImportFilename(name string) string {
	switch {
	case strings.HasSuffix(name, ".img"):
		return strings.TrimSuffix(name, ".img") + ".qcow2"
	}
	return name
}

// contentField maps a PVE content kind to the cluster.yaml field name it
// belongs to, so error messages can point users at the exact knob to turn.
func contentField(content string) string {
	switch content {
	case "iso":
		return "iso"
	case "vztmpl":
		return "shared"
	case "images", "rootdir":
		return "vm_disk"
	}
	return content
}
