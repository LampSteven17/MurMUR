package provision

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/rtx-monster/murmur/internal/config"
)

// ImageStatusKind summarises an image's template freshness for the update UI.
type ImageStatusKind int

const (
	ImageMissing ImageStatusKind = iota // no template for this image
	ImageStale                          // template exists but cache/marker is older than upstream
	ImageCurrent                        // template exists, marker is set, cache is at-or-newer than upstream
)

func (k ImageStatusKind) String() string {
	switch k {
	case ImageMissing:
		return "MISSING"
	case ImageStale:
		return "STALE"
	case ImageCurrent:
		return "CURRENT"
	}
	return "?"
}

// ImageStatus is the per-image snapshot the update tab renders.
type ImageStatus struct {
	Image    config.Image
	Status   ImageStatusKind
	Upstream time.Time // upstream Last-Modified; zero if HEAD failed
	Cached   time.Time // cached qcow2 mtime in <storage>:import/; zero if no cache
	Template int       // VMID of matching template, 0 if missing
	Node     string    // node hosting the template, "" if missing
	HasMark  bool      // template carries the murmur-bake-v1 marker
}

// CheckImageStatus computes the per-image freshness snapshot. It does an
// HTTP HEAD on the image URL (best-effort — if the HEAD fails we still
// return a useful status based on local state) and looks up the cached
// qcow2 + template in the cluster.
func (o *Orchestrator) CheckImageStatus(ctx context.Context, image config.Image) ImageStatus {
	st := ImageStatus{Image: image}

	// Upstream Last-Modified via HEAD.
	if image.URL != "" {
		if t, err := headLastModified(ctx, image.URL); err == nil {
			st.Upstream = t
		}
	}

	// Cached qcow2 ctime via PVE storage content.
	if storage := o.cfg.Cluster.Storage.ISO; storage != "" {
		node := o.templatesNode()
		if node == "" {
			node = o.firstOnlineNode(ctx)
		}
		if node != "" {
			filename := normalizeImportFilename(path.Base(image.URL))
			target := fmt.Sprintf("%s:import/%s", storage, filename)
			if entries, err := o.client.ListStorageContent(ctx, node, storage, "import"); err == nil {
				for _, e := range entries {
					if e.VolID == target {
						st.Cached = time.Unix(e.Ctime, 0)
						break
					}
				}
			}
		}
	}

	// Template lookup via the marker-aware helper from template.go.
	if vmid, node, stale, err := o.locateVMTemplate(ctx, image.Name); err == nil {
		st.Template = vmid
		st.Node = node
		st.HasMark = !stale
	}

	// Derive overall status.
	switch {
	case st.Template == 0:
		st.Status = ImageMissing
	case !st.HasMark:
		st.Status = ImageStale
	case !st.Upstream.IsZero() && st.Cached.Before(st.Upstream):
		st.Status = ImageStale
	default:
		st.Status = ImageCurrent
	}
	return st
}

// RefreshTemplate destroys any existing template + cached qcow2 for the image
// and rebuilds from scratch via the standard buildVMTemplate flow. The
// resulting template carries the current templateBuildMarker.
func (o *Orchestrator) RefreshTemplate(ctx context.Context, image config.Image) error {
	// Destroy existing template (if any).
	if vmid, node, _, err := o.locateVMTemplate(ctx, image.Name); err == nil && vmid > 0 {
		o.emit(StepDestroy, fmt.Sprintf("removing existing template %q (VMID %d on %s)", image.Name, vmid, node), 5)
		upid, derr := o.client.DestroyVM(ctx, node, vmid, true, true)
		if derr != nil {
			return fmt.Errorf("refresh: destroy old template: %w", derr)
		}
		if derr := o.waitTask(ctx, upid, "destroy-old-template"); derr != nil {
			return derr
		}
	}

	// Delete cached qcow2 to force a fresh download. We can't issue a
	// regular VM destroy on a plain file — list and delete via volume id.
	if storage := o.cfg.Cluster.Storage.ISO; storage != "" {
		node := o.templatesNode()
		if node == "" {
			node = o.firstOnlineNode(ctx)
		}
		if node != "" {
			filename := normalizeImportFilename(path.Base(image.URL))
			target := fmt.Sprintf("%s:import/%s", storage, filename)
			if entries, err := o.client.ListStorageContent(ctx, node, storage, "import"); err == nil {
				for _, e := range entries {
					if e.VolID == target {
						o.emit(StepDestroy, fmt.Sprintf("deleting stale cache %s", target), 6)
						_, _ = o.client.DeleteStorageVolume(ctx, node, storage, e.VolID)
						break
					}
				}
			}
		}
	}

	// Build fresh.
	if _, _, err := o.buildVMTemplate(ctx, image); err != nil {
		return err
	}
	return nil
}

// headLastModified is a small helper that does an HTTP HEAD and parses the
// Last-Modified response header. Falls back to zero time + error on any
// transport / parse failure so callers can render "(unknown)" gracefully.
func headLastModified(ctx context.Context, url string) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	// Follow redirects (Fedora's downloader uses 302 to a mirror).
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return time.Time{}, fmt.Errorf("HEAD %s: HTTP %d", url, resp.StatusCode)
	}
	lm := resp.Header.Get("Last-Modified")
	if lm == "" {
		return time.Time{}, fmt.Errorf("no Last-Modified header")
	}
	// Mirror servers can return RFC1123 (with GMT) or RFC850; try both.
	for _, layout := range []string{time.RFC1123, time.RFC850, time.RFC1123Z} {
		if t, err := time.Parse(layout, lm); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable Last-Modified: %s", strings.TrimSpace(lm))
}
