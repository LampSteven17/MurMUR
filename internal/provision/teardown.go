package provision

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rtx-monster/murmur/internal/proxmox"
)

// TeardownRequest is the input to Orchestrator.Teardown.
type TeardownRequest struct {
	Type string // "vm" | "lxc"
	Node string
	VMID int
	Name string // free-form display label for messages; not used in API calls
}

// Teardown stops (if running) and destroys a guest. Disks and references
// (backup/replication entries) are purged; this is irreversible. The caller
// is responsible for any confirmation UX.
func (o *Orchestrator) Teardown(ctx context.Context, req TeardownRequest) error {
	if req.Type != "vm" && req.Type != "lxc" {
		return fmt.Errorf("teardown: Type must be \"vm\" or \"lxc\", got %q", req.Type)
	}
	if req.Node == "" || req.VMID <= 0 {
		return fmt.Errorf("teardown: Node and VMID required")
	}

	// Re-resolve the guest from /cluster/resources — the UI's snapshot may be
	// stale (someone else could have destroyed it in the interim, or it could
	// be running/stopped differently than the list believed).
	o.emit(StepResolve, fmt.Sprintf("locating %s %d on %s", req.Type, req.VMID, req.Node), 10)
	resources, err := o.client.GetResources(ctx, "vm")
	if err != nil {
		return fmt.Errorf("teardown: list cluster resources: %w", err)
	}
	pveType := "qemu"
	if req.Type == "lxc" {
		pveType = "lxc"
	}
	var found *proxmox.Resource
	for i := range resources {
		r := &resources[i]
		if r.Type == pveType && r.VMID == req.VMID && r.Node == req.Node {
			found = r
			break
		}
	}
	if found == nil {
		return fmt.Errorf("teardown: %s %d on %s not found (already gone?)", req.Type, req.VMID, req.Node)
	}
	o.emit(StepResolve, fmt.Sprintf("target %s/%d (%s, status=%s)", req.Type, req.VMID, found.Name, found.Status), 20)

	// HA-managed guests get restarted by the HA stack the instant we stop them,
	// so the destroy below would 500 forever with "is running". Remove the
	// guest from HA first. Detection is best-effort: if listing HA fails (not
	// configured, or insufficient perms) we skip it — non-HA guests are
	// unaffected. The delete itself fails loudly if it's HA-managed but we
	// can't remove it.
	if ha, herr := o.client.ListHAResources(ctx); herr == nil {
		sid := haSID(req.Type, req.VMID)
		for _, h := range ha {
			if h.SID != sid {
				continue
			}
			o.emit(StepStop, fmt.Sprintf("removing %s from HA before teardown", sid), 30)
			if derr := o.client.DeleteHAResource(ctx, sid); derr != nil {
				return fmt.Errorf("teardown: remove %s from HA: %w", sid, derr)
			}
			// Give the HA manager a beat to relinquish the resource.
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			break
		}
	}

	// Destroy. We deliberately DON'T gate the stop on found.Status — that comes
	// from /cluster/resources (the pvestatd cache) and lags reality in both
	// directions: a guest it calls "stopped" may still be running (destroy then
	// 500s with "is running"), and one it calls "running" may already be down
	// (a pre-emptive stop would then fail). Instead: attempt the destroy, and
	// only if PVE refuses it as running do we hard-stop, wait, and retry once.
	// purge=true removes backup/replication entries; destroyUnreferencedDisks
	// cleans orphaned volumes the config no longer references.
	destroy := func() (string, error) {
		if req.Type == "vm" {
			return o.client.DestroyVM(ctx, req.Node, req.VMID, true, true)
		}
		return o.client.DestroyLXC(ctx, req.Node, req.VMID, true, true)
	}

	o.emit(StepDestroy, fmt.Sprintf("destroying %s %d (purge + orphan-disk cleanup)", req.Type, req.VMID), 70)
	upid, err := destroy()
	if err != nil && strings.Contains(err.Error(), "is running") {
		// Hard-stop via /status/stop — a guest about to be destroyed doesn't
		// deserve a graceful ACPI dance that can hang on a bad guest agent.
		o.emit(StepStop, fmt.Sprintf("%s %d still running — hard-stopping before destroy", req.Type, req.VMID), 60)
		var supid string
		if req.Type == "vm" {
			supid, err = o.client.StopVM(ctx, req.Node, req.VMID)
		} else {
			supid, err = o.client.StopLXC(ctx, req.Node, req.VMID)
		}
		if err != nil {
			return fmt.Errorf("teardown: stop: %w", err)
		}
		if err := o.waitTask(ctx, supid, "stop"); err != nil {
			return err
		}
		// Even after the stop task reports OK, an LXC can briefly still read as
		// running while cgroup/mount cleanup finishes — destroy then 500s again.
		// VMs don't hit this; for LXC we poll the destroy a few times.
		for attempt := 0; ; attempt++ {
			upid, err = destroy()
			if err == nil || !strings.Contains(err.Error(), "is running") || attempt >= 4 {
				break
			}
			o.emit(StepDestroy, fmt.Sprintf("%s %d still settling — retrying destroy", req.Type, req.VMID), 80)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	if err != nil {
		return fmt.Errorf("teardown: destroy: %w", err)
	}
	if err := o.waitTask(ctx, upid, "destroy"); err != nil {
		return err
	}

	o.emit(StepDone, fmt.Sprintf("destroyed %s %d (%s)", req.Type, req.VMID, found.Name), 100)
	return nil
}

// haSID builds the HA resource identifier for a guest: qemu VMs are "vm:<id>",
// LXCs are "ct:<id>".
func haSID(reqType string, vmid int) string {
	prefix := "vm"
	if reqType == "lxc" {
		prefix = "ct"
	}
	return fmt.Sprintf("%s:%d", prefix, vmid)
}
