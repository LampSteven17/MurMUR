package provision

import (
	"fmt"

	"github.com/rtx-monster/murmur/internal/proxmox"
)

// NodeCapacity is a snapshot of one node's headroom for placement decisions.
// FreeRAM and FreeCPU subtract what's currently *allocated* to running guests
// (MaxMem / MaxCPU summed), not live consumption — placement reasons about
// reservations, not utilization.
type NodeCapacity struct {
	Name    string
	TotalRAM int64 // bytes; node-reported maxmem
	FreeRAM int64 // bytes
	TotalCPU int   // cores
	FreeCPU int    // cores
}

// ComputeCapacity rolls up /cluster/resources into per-online-node free
// capacity, subtracting allocations of running VMs and LXCs. Stopped guests
// don't count against capacity (they're not consuming reservations on PVE).
// Templates are skipped.
func ComputeCapacity(resources []proxmox.Resource) []NodeCapacity {
	byName := map[string]*NodeCapacity{}
	for _, r := range resources {
		if r.Type != "node" {
			continue
		}
		if r.Status != "online" {
			continue
		}
		// For type=node rows, the node identifier is in r.Node — r.Name is
		// empty. Indexing by r.Name silently collapsed every node into "".
		byName[r.Node] = &NodeCapacity{
			Name:     r.Node,
			TotalRAM: r.MaxMem,
			FreeRAM:  r.MaxMem,
			TotalCPU: int(r.MaxCPU + 0.5),
			FreeCPU:  int(r.MaxCPU + 0.5),
		}
	}
	for _, r := range resources {
		if r.Type != "qemu" && r.Type != "lxc" {
			continue
		}
		if r.Template == 1 {
			continue
		}
		if r.Status != "running" {
			continue
		}
		n := byName[r.Node]
		if n == nil {
			continue
		}
		n.FreeRAM -= r.MaxMem
		n.FreeCPU -= int(r.MaxCPU + 0.5)
	}
	out := make([]NodeCapacity, 0, len(byName))
	for _, n := range byName {
		out = append(out, *n)
	}
	return out
}

// BestFit picks the node with the *smallest* leftover RAM after fitting the
// request — i.e. the tightest eligible fit. This biases small guests onto
// constrained nodes and naturally reserves the big nodes for big guests
// (because nothing else fits them as snugly).
//
// nodes may be restricted upstream (e.g. filtered by required role); BestFit
// applies no role logic itself. Returns the chosen node name or an error
// naming the constraint that excluded every candidate.
func BestFit(nodes []NodeCapacity, reqRAM int64, reqCPU int) (string, error) {
	if len(nodes) == 0 {
		return "", fmt.Errorf("placement: no eligible nodes")
	}
	bestName := ""
	var bestLeftover int64 = -1
	for _, n := range nodes {
		if n.FreeRAM < reqRAM || n.FreeCPU < reqCPU {
			continue
		}
		leftover := n.FreeRAM - reqRAM
		if bestLeftover < 0 || leftover < bestLeftover {
			bestName = n.Name
			bestLeftover = leftover
		}
	}
	if bestName == "" {
		return "", fmt.Errorf("placement: no node has capacity for %d MiB RAM + %d vCPU (across %d online node(s))",
			reqRAM/1024/1024, reqCPU, len(nodes))
	}
	return bestName, nil
}
