package mcpserver

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rtx-monster/murmur/internal/proxmox"
)

// ---- cluster_status --------------------------------------------------------

type ClusterStatusIn struct{}

type NodeSummary struct {
	Node     string  `json:"node"`
	Status   string  `json:"status"`
	CPUPct   float64 `json:"cpu_pct"`
	MemUsedG float64 `json:"mem_used_gb"`
	MemMaxG  float64 `json:"mem_max_gb"`
	UptimeH  int64   `json:"uptime_hours"`
}

type ClusterStatusOut struct {
	Cluster  string         `json:"cluster"`
	Endpoint string         `json:"endpoint"`
	Proxmox  string         `json:"proxmox_version"`
	Operator string         `json:"operator"`
	Role     string         `json:"role"`
	Nodes    []NodeSummary  `json:"nodes"`
	Guests   map[string]int `json:"guests" jsonschema:"guest counts keyed by '<type>/<status>', e.g. qemu/running"`
}

func (s *Server) clusterStatus(ctx context.Context, req *mcp.CallToolRequest, in ClusterStatusIn) (*mcp.CallToolResult, ClusterStatusOut, error) {
	out := ClusterStatusOut{
		Cluster:  s.cfg.Cluster.Name,
		Endpoint: s.cfg.Cluster.API.Endpoint,
		Operator: operatorName(s.active),
		Role:     roleName(s.active),
		Guests:   map[string]int{},
	}
	version, err := s.client.GetVersion(ctx)
	if err != nil {
		return nil, out, fmt.Errorf("connect: %w", err)
	}
	out.Proxmox = version.Version

	nodes, err := s.client.GetNodes(ctx)
	if err != nil {
		return nil, out, err
	}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, NodeSummary{
			Node:     n.Node,
			Status:   n.Status,
			CPUPct:   round1(n.CPU * 100),
			MemUsedG: gb(n.Mem),
			MemMaxG:  gb(n.MaxMem),
			UptimeH:  n.Uptime / 3600,
		})
	}

	resources, err := s.client.GetResources(ctx, "vm")
	if err != nil {
		return nil, out, err
	}
	for _, r := range resources {
		if r.Template == 1 {
			out.Guests[r.Type+"/template"]++
			continue
		}
		out.Guests[r.Type+"/"+r.Status]++
	}
	return nil, out, nil
}

func gb(b int64) float64     { return round1(float64(b) / (1 << 30)) }
func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// ---- list_guests -----------------------------------------------------------

type ListGuestsIn struct {
	Status string `json:"status,omitempty" jsonschema:"optional filter: running or stopped"`
}

type GuestSummary struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Type   string `json:"type"` // qemu | lxc
	Node   string `json:"node"`
	Status string `json:"status"`
	Tags   string `json:"tags,omitempty"`
	CPUs   int    `json:"cpus"`
	MemG   float64 `json:"mem_max_gb"`
}

type ListGuestsOut struct {
	Guests []GuestSummary `json:"guests"`
	Scoped bool           `json:"owner_scoped" jsonschema:"true when the list is filtered to guests owned by the operator"`
}

func (s *Server) listGuests(ctx context.Context, req *mcp.CallToolRequest, in ListGuestsIn) (*mcp.CallToolResult, ListGuestsOut, error) {
	out := ListGuestsOut{Scoped: ownScoped(s.active)}
	resources, err := s.client.GetResources(ctx, "vm")
	if err != nil {
		return nil, out, err
	}
	for _, r := range resources {
		if r.Template == 1 || (r.Type != "qemu" && r.Type != "lxc") {
			continue
		}
		if in.Status != "" && r.Status != in.Status {
			continue
		}
		if out.Scoped && !carriesOwner(r.Tags, s.active.Name) {
			continue
		}
		out.Guests = append(out.Guests, GuestSummary{
			VMID: r.VMID, Name: r.Name, Type: r.Type, Node: r.Node,
			Status: r.Status, Tags: r.Tags, CPUs: int(r.MaxCPU), MemG: gb(r.MaxMem),
		})
	}
	return nil, out, nil
}

// ---- guest_info ------------------------------------------------------------

type GuestRefIn struct {
	Name string `json:"name,omitempty" jsonschema:"guest name (or pass vmid)"`
	VMID int    `json:"vmid,omitempty" jsonschema:"guest vmid (or pass name)"`
}

type GuestInfoOut struct {
	GuestSummary
	IP     string            `json:"ip,omitempty" jsonschema:"live IPv4 from guest agent / LXC interfaces; empty if unreachable"`
	HA     string            `json:"ha,omitempty" jsonschema:"HA resource state if registered, else empty"`
	Config map[string]string `json:"config"`
}

func (s *Server) guestInfo(ctx context.Context, req *mcp.CallToolRequest, in GuestRefIn) (*mcp.CallToolResult, GuestInfoOut, error) {
	var out GuestInfoOut
	r, err := s.findGuest(ctx, in.Name, in.VMID)
	if err != nil {
		return nil, out, err
	}
	out.GuestSummary = GuestSummary{
		VMID: r.VMID, Name: r.Name, Type: r.Type, Node: r.Node,
		Status: r.Status, Tags: r.Tags, CPUs: int(r.MaxCPU), MemG: gb(r.MaxMem),
	}

	switch r.Type {
	case "qemu":
		var raw map[string]any
		if err := s.client.GetJSON(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/config", r.Node, r.VMID), &raw); err != nil {
			return nil, out, err
		}
		out.Config = stringify(raw)
		if r.Status == "running" {
			if ifaces, err := s.client.GuestAgentNetInterfaces(ctx, r.Node, r.VMID); err == nil {
				out.IP = proxmox.FirstIPv4(ifaces)
			}
		}
	case "lxc":
		cfg, err := s.client.GetLXCConfig(ctx, r.Node, r.VMID)
		if err != nil {
			return nil, out, err
		}
		out.Config = cfg
		if r.Status == "running" {
			if ifaces, err := s.client.LXCInterfaces(ctx, r.Node, r.VMID); err == nil {
				out.IP = proxmox.FirstLXCIPv4(ifaces)
			}
		}
	}

	if has, err := s.client.ListHAResources(ctx); err == nil {
		want := fmt.Sprintf("vm:%d", r.VMID)
		if r.Type == "lxc" {
			want = fmt.Sprintf("ct:%d", r.VMID)
		}
		for _, h := range has {
			if h.SID == want {
				out.HA = h.State
			}
		}
	}
	return nil, out, nil
}

func stringify(raw map[string]any) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// ---- app_status ------------------------------------------------------------

type AppRefIn struct {
	App string `json:"app" jsonschema:"app name from the cluster.yaml apps: catalog"`
}

type ReplicaOut struct {
	VMID             int    `json:"vmid"`
	Node             string `json:"node"`
	Status           string `json:"status"`
	IP               string `json:"ip,omitempty"`
	OS               string `json:"os,omitempty"`
	UpdatesAvailable int    `json:"updates_available"`
	UpdatesTotal     int    `json:"updates_total"`
	UpdateChecked    bool   `json:"update_checked"`
}

type AppStatusOut struct {
	App      string       `json:"app"`
	Deployed bool         `json:"deployed"`
	VMID     int          `json:"vmid,omitempty"`
	Node     string       `json:"node,omitempty"`
	Status   string       `json:"status,omitempty"`
	OS       string       `json:"os,omitempty"`
	IP       string       `json:"ip,omitempty"`
	Compose  []string     `json:"compose,omitempty" jsonschema:"docker compose services as 'service state image'"`
	UpdatesAvailable int  `json:"updates_available"`
	UpdatesTotal     int  `json:"updates_total"`
	UpdateChecked    bool `json:"update_checked"`
	Replicas []ReplicaOut `json:"replicas,omitempty" jsonschema:"per-instance detail for match_all apps"`
	Error    string       `json:"error,omitempty"`
}

func (s *Server) appStatus(ctx context.Context, req *mcp.CallToolRequest, in AppRefIn) (*mcp.CallToolResult, AppStatusOut, error) {
	var out AppStatusOut
	app, err := s.findApp(in.App)
	if err != nil {
		return nil, out, err
	}
	info := s.orch.InspectApp(ctx, app)
	out = AppStatusOut{
		App: app.Name, Deployed: info.VMID != 0, VMID: info.VMID, Node: info.Node,
		Status: info.Status, OS: info.OS, IP: info.IP,
		UpdatesAvailable: info.UpdatesAvailable, UpdatesTotal: info.UpdatesTotal, UpdateChecked: info.UpdateChecked,
	}
	for _, c := range info.Compose {
		out.Compose = append(out.Compose, fmt.Sprintf("%s %s %s", c.Service, c.State, c.Image))
	}
	for _, rep := range info.Replicas {
		out.Replicas = append(out.Replicas, ReplicaOut{
			VMID: rep.VMID, Node: rep.Node, Status: rep.Status, IP: rep.IP, OS: rep.OS,
			UpdatesAvailable: rep.UpdatesAvailable, UpdatesTotal: rep.UpdatesTotal, UpdateChecked: rep.UpdateChecked,
		})
	}
	if info.Err != nil {
		out.Error = info.Err.Error()
	}
	return nil, out, nil
}

// ---- guest_power -----------------------------------------------------------

type GuestPowerIn struct {
	Name   string `json:"name,omitempty" jsonschema:"guest name (or pass vmid)"`
	VMID   int    `json:"vmid,omitempty" jsonschema:"guest vmid (or pass name)"`
	Action string `json:"action" jsonschema:"one of: start, shutdown, stop, reboot"`
	Force  bool   `json:"force,omitempty" jsonschema:"required for hard 'stop' unless a graceful shutdown already failed"`
}

type GuestPowerOut struct {
	VMID   int    `json:"vmid"`
	Action string `json:"action"`
	Task   string `json:"task_status"`
}

func (s *Server) guestPower(ctx context.Context, req *mcp.CallToolRequest, in GuestPowerIn) (*mcp.CallToolResult, GuestPowerOut, error) {
	var out GuestPowerOut
	if err := s.gate("guest_power", "patch", in); err != nil {
		return nil, out, err
	}
	r, err := s.findGuest(ctx, in.Name, in.VMID)
	if err != nil {
		s.auditResult("guest_power", in, err)
		return nil, out, err
	}
	if in.Action == "stop" && !in.Force {
		err := fmt.Errorf("hard stop refused: try action=shutdown first (graceful); pass force=true only if shutdown fails or the guest is wedged")
		s.auditResult("guest_power", in, err)
		return nil, out, err
	}

	var upid string
	switch {
	case r.Type == "qemu" && in.Action == "start":
		upid, err = s.client.StartVM(ctx, r.Node, r.VMID)
	case r.Type == "qemu" && in.Action == "shutdown":
		upid, err = s.client.ShutdownVM(ctx, r.Node, r.VMID, 60, false)
	case r.Type == "qemu" && in.Action == "stop":
		upid, err = s.client.StopVM(ctx, r.Node, r.VMID)
	case r.Type == "qemu" && in.Action == "reboot":
		upid, err = s.client.RebootVM(ctx, r.Node, r.VMID)
	case r.Type == "lxc" && in.Action == "start":
		upid, err = s.client.StartLXC(ctx, r.Node, r.VMID)
	case r.Type == "lxc" && in.Action == "shutdown":
		upid, err = s.client.ShutdownLXC(ctx, r.Node, r.VMID, 60, false)
	case r.Type == "lxc" && in.Action == "stop":
		upid, err = s.client.StopLXC(ctx, r.Node, r.VMID)
	case r.Type == "lxc" && in.Action == "reboot":
		upid, err = s.client.RebootLXC(ctx, r.Node, r.VMID)
	default:
		err = fmt.Errorf("unknown action %q: want start|shutdown|stop|reboot", in.Action)
	}
	if err != nil {
		s.auditResult("guest_power", in, err)
		return nil, out, err
	}

	status, err := s.client.WaitForTask(ctx, upid, 2*time.Second)
	out = GuestPowerOut{VMID: r.VMID, Action: in.Action, Task: status.Status}
	if err == nil && !status.OK() {
		err = fmt.Errorf("task finished with status %q: %s", status.Status, status.ExitStatus)
	}
	s.auditResult("guest_power", in, err)
	return nil, out, err
}

// ---- app_update ------------------------------------------------------------

type AppUpdateIn struct {
	App  string `json:"app" jsonschema:"app name from the cluster.yaml apps: catalog"`
	VMID int    `json:"vmid,omitempty" jsonschema:"for match_all apps: update only this replica; omit to update all instances"`
}

type AppUpdateOut struct {
	App     string `json:"app"`
	Updated bool   `json:"updated"`
}

func (s *Server) appUpdate(ctx context.Context, req *mcp.CallToolRequest, in AppUpdateIn) (*mcp.CallToolResult, AppUpdateOut, error) {
	out := AppUpdateOut{App: in.App}
	if err := s.gate("app_update", "patch", in); err != nil {
		return nil, out, err
	}
	app, err := s.findApp(in.App)
	if err != nil {
		s.auditResult("app_update", in, err)
		return nil, out, err
	}
	if app.Update == "" {
		err = fmt.Errorf("app %q has no update: command in cluster.yaml", in.App)
		s.auditResult("app_update", in, err)
		return nil, out, err
	}

	if in.VMID != 0 {
		r, ferr := s.findGuest(ctx, "", in.VMID)
		if ferr != nil {
			s.auditResult("app_update", in, ferr)
			return nil, out, ferr
		}
		err = s.orch.PatchAppInstance(ctx, app, r.VMID, r.Node)
	} else {
		err = s.orch.PatchApp(ctx, app)
	}
	out.Updated = err == nil
	s.auditResult("app_update", in, err)
	return nil, out, err
}
