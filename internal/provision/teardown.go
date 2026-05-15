package provision

import (
	"context"
	"fmt"

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

	// Stop if running. Hard-stop via the /status/stop endpoint — a running
	// guest about to be destroyed doesn't deserve a graceful ACPI shutdown
	// dance that can hang on a misbehaving guest agent.
	if found.Status == "running" {
		o.emit(StepStop, fmt.Sprintf("stopping running %s %d", req.Type, req.VMID), 40)
		var upid string
		var err error
		if req.Type == "vm" {
			upid, err = o.client.StopVM(ctx, req.Node, req.VMID)
		} else {
			upid, err = o.client.StopLXC(ctx, req.Node, req.VMID)
		}
		if err != nil {
			return fmt.Errorf("teardown: stop: %w", err)
		}
		if err := o.waitTask(ctx, upid, "stop"); err != nil {
			return err
		}
	}

	// Destroy. purge=true removes backup/replication entries;
	// destroyUnreferencedDisks=true cleans up any orphaned volumes that the
	// VM config no longer references (defensive — usually a no-op).
	o.emit(StepDestroy, fmt.Sprintf("destroying %s %d (purge + orphan-disk cleanup)", req.Type, req.VMID), 70)
	var upid string
	if req.Type == "vm" {
		upid, err = o.client.DestroyVM(ctx, req.Node, req.VMID, true, true)
	} else {
		upid, err = o.client.DestroyLXC(ctx, req.Node, req.VMID, true, true)
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
