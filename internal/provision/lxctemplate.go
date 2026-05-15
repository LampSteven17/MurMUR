package provision

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// EnsureLXCTemplate returns the vztmpl volume id for the image, downloading
// it from PVE's appliance repository if it's not already on storage. node
// is the candidate node that will both host the cached file (via shared
// storage) and run the download. Returns e.g.
// "cephfs:vztmpl/debian-13-standard_13.1-2_amd64.tar.zst".
//
// LXC templates differ from VM templates in two important ways:
//   - They're tarballs (vztmpl content), not VM disk images.
//   - They come pre-baked from PVE's curated repo via `pveam download`,
//     so there's no agent-install bake-in to do — `pct create` configures
//     hostname/SSH/network at create time.
func (o *Orchestrator) EnsureLXCTemplate(ctx context.Context, image config.Image, node string) (string, error) {
	// First, see if we already have a vztmpl matching the image. Reuse
	// existing findLXCTemplate which scans shared+iso storages.
	if volid, err := o.findLXCTemplate(ctx, image.Name, node); err == nil {
		return volid, nil
	}

	// Auto-download. Query PVE's curated appliance list and pick the
	// vztmpl whose `os` field matches our image.Name. Fallback: same
	// distro prefix, newest version (handles cluster.yaml lagging the
	// PVE repo, e.g. image alpine-3.20 mapping to alpine-3.23).
	o.emit(StepResolve, fmt.Sprintf("no LXC template for %q — querying PVE appliance repo", image.Name), 25)
	appliances, err := o.client.ListAppliances(ctx, node)
	if err != nil {
		return "", fmt.Errorf("list appliances: %w", err)
	}

	pick := chooseLXCTemplate(appliances, image)
	if pick == nil {
		return "", fmt.Errorf(
			"no LXC template in PVE's repo matches image %q (distro %q). "+
				"Run `ssh %s pveam update` to refresh the repo metadata, "+
				"or pick a different image name that matches an `os` field in `pveam available`.",
			image.Name, image.Distro, node)
	}

	storage := o.cfg.Cluster.Storage.Shared
	if storage == "" {
		return "", fmt.Errorf("download LXC template: cluster.storage.shared is required (vztmpl needs a shared storage)")
	}

	o.emit(StepResolve, fmt.Sprintf("downloading %s → %s (PVE repo)", pick.Template, storage), 28)
	upid, err := o.client.DownloadAppliance(ctx, node, storage, pick.Template)
	if err != nil {
		return "", fmt.Errorf("download appliance: %w", err)
	}
	if err := o.waitTask(ctx, upid, "download-appliance"); err != nil {
		return "", err
	}

	volid := fmt.Sprintf("%s:vztmpl/%s", storage, pick.Template)
	o.emit(StepResolve, fmt.Sprintf("template ready: %s", volid), 32)
	return volid, nil
}

// chooseLXCTemplate picks the best appliance for an image. Preference order:
//
//  1. Exact match on `os` field (image.Name == appliance.OS).
//  2. Same distro prefix (image.Distro), newest version. Handles the case
//     where the user's image catalog pins an older minor (alpine-3.20) but
//     the PVE repo has only newer (alpine-3.22/3.23).
//  3. Nothing: caller surfaces a clear error.
func chooseLXCTemplate(appliances []proxmox.Appliance, image config.Image) *proxmox.Appliance {
	// Only the "system" section is relevant for our cloud-style images
	// (other sections include turnkeylinux/etc. which carry different
	// semantics and shouldn't be auto-matched).
	candidates := make([]proxmox.Appliance, 0, len(appliances))
	for _, a := range appliances {
		if a.Section == "system" {
			candidates = append(candidates, a)
		}
	}

	// Exact match.
	for i := range candidates {
		if candidates[i].OS == image.Name {
			return &candidates[i]
		}
	}

	// Fuzzy: same distro prefix.
	var fuzzy []proxmox.Appliance
	for _, a := range candidates {
		if strings.HasPrefix(a.OS, image.Distro+"-") || a.OS == image.Distro {
			fuzzy = append(fuzzy, a)
		}
	}
	if len(fuzzy) == 0 {
		return nil
	}
	// Newest first — appliance OS field is "<distro>-<version>", and version
	// sorts lexicographically for our distros (alpine-3.20 < 3.22 < 3.23;
	// ubuntu-22.04 < 24.04 < 26.04). Not perfect but right for every
	// distro in our catalog as of 2026.
	sort.Slice(fuzzy, func(i, j int) bool {
		return fuzzy[i].OS > fuzzy[j].OS
	})
	return &fuzzy[0]
}
