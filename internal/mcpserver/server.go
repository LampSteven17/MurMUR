package mcpserver

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/provision"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// Server wires murmur's cluster operations into MCP tools.
type Server struct {
	cfg    *config.Config
	client *proxmox.Client
	orch   *provision.Orchestrator
	active *config.ActiveUser
	audit  *auditLog
}

// Run serves MCP over stdio until the client disconnects. All logging goes to
// stderr — stdout carries the JSON-RPC stream.
func Run(ctx context.Context, cfg *config.Config, client *proxmox.Client, active *config.ActiveUser, version string) error {
	auditPath, err := AuditPath()
	if err != nil {
		return err
	}
	audit, err := newAuditLog(auditPath)
	if err != nil {
		return err
	}

	orch := provision.New(cfg, client)
	orch.SetActiveUser(active)
	orch.SetProgress(func(e provision.ProgressEvent) {
		log.Printf("progress: %s %s", e.Step, e.Message)
	})
	log.SetOutput(os.Stderr)

	s := &Server{cfg: cfg, client: client, orch: orch, active: active, audit: audit}

	impl := &mcp.Implementation{Name: "murmur", Title: "murmur cluster rails", Version: version}
	srv := mcp.NewServer(impl, nil)

	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mutating := &mcp.ToolAnnotations{DestructiveHint: boolPtr(false), IdempotentHint: false}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "cluster_status",
		Description: "Cluster overview: ProxMox version, per-node health and headroom, and guest counts by status.",
		Annotations: readOnly,
	}, s.clusterStatus)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_guests",
		Description: "List VMs and LXC containers (templates excluded) with node, status, tags, and sizing. Scoped operators only see guests they own.",
		Annotations: readOnly,
	}, s.listGuests)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "guest_info",
		Description: "Detail for one guest by name or vmid: PVE config, IP address (live, via guest agent / LXC interfaces), and HA registration.",
		Annotations: readOnly,
	}, s.guestInfo)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "app_status",
		Description: "Runtime status of a cataloged app (cluster.yaml apps:): where it runs, OS, IP, docker compose services, and whether component updates are available. Slower than list_guests — it SSHes into the guest to probe.",
		Annotations: readOnly,
	}, s.appStatus)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "guest_power",
		Description: "Start, shutdown (graceful), stop (hard), or reboot a guest. Requires the operator's role to permit the 'patch' action. Audited. Refuses hard-stop unless shutdown already failed or force=true.",
		Annotations: mutating,
	}, s.guestPower)
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "app_update",
		Description: "Run a cataloged app's update command (cluster.yaml apps: update:) on its guest over SSH — e.g. docker compose pull && up -d. Requires the 'patch' role action. Audited.",
		Annotations: mutating,
	}, s.appUpdate)

	log.Printf("murmur mcp: operator=%s role=%s audit=%s", operatorName(active), roleName(active), auditPath)
	return srv.Run(ctx, &mcp.StdioTransport{})
}

func boolPtr(b bool) *bool { return &b }

func operatorName(a *config.ActiveUser) string {
	if a == nil || a.Fallback {
		return "<implicit-admin>"
	}
	return a.Name
}

func roleName(a *config.ActiveUser) string {
	if a == nil || a.Fallback {
		return "admin(fallback)"
	}
	return a.Role.Name
}

// ---- guardrail helpers ----------------------------------------------------

// gate enforces role permission + writes the mandatory pre-call audit entry
// for a mutating tool. Returns an error the model sees verbatim.
func (s *Server) gate(tool, action string, args any) error {
	if !actionAllowed(s.active, action) {
		_ = s.audit.write(auditEntry{Operator: operatorName(s.active), Role: roleName(s.active), Tool: tool, Args: args, Outcome: "denied", Detail: "role lacks action " + action})
		return fmt.Errorf("permission denied: operator %s (role %s) lacks the %q action", operatorName(s.active), roleName(s.active), action)
	}
	if err := s.audit.write(auditEntry{Operator: operatorName(s.active), Role: roleName(s.active), Tool: tool, Args: args, Outcome: "allowed"}); err != nil {
		return fmt.Errorf("refusing to act: audit log unwritable: %w", err)
	}
	return nil
}

func (s *Server) auditResult(tool string, args any, callErr error) {
	e := auditEntry{Operator: operatorName(s.active), Role: roleName(s.active), Tool: tool, Args: args, Outcome: "ok"}
	if callErr != nil {
		e.Outcome = "error"
		e.Detail = callErr.Error()
	}
	_ = s.audit.write(e)
}

// findGuest resolves a guest by name or vmid among qemu/lxc resources
// (templates excluded), honoring owner scoping.
func (s *Server) findGuest(ctx context.Context, name string, vmid int) (*proxmox.Resource, error) {
	resources, err := s.client.GetResources(ctx, "vm")
	if err != nil {
		return nil, err
	}
	var match *proxmox.Resource
	for i := range resources {
		r := &resources[i]
		if r.Template == 1 || (r.Type != "qemu" && r.Type != "lxc") {
			continue
		}
		if (vmid != 0 && r.VMID == vmid) || (name != "" && r.Name == name) {
			if match != nil && match.VMID != r.VMID {
				return nil, fmt.Errorf("ambiguous guest %q: vmids %d and %d both match — pass vmid", name, match.VMID, r.VMID)
			}
			match = r
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no guest matches name=%q vmid=%d", name, vmid)
	}
	if ownScoped(s.active) && !carriesOwner(match.Tags, s.active.Name) {
		return nil, fmt.Errorf("permission denied: guest %s (vmid %d) is not owned by operator %s", match.Name, match.VMID, s.active.Name)
	}
	return match, nil
}

func carriesOwner(tags, owner string) bool {
	want := "murmur-owner-" + owner
	for _, t := range strings.FieldsFunc(tags, func(r rune) bool { return r == ';' || r == ',' }) {
		if strings.TrimSpace(t) == want {
			return true
		}
	}
	return false
}

func (s *Server) findApp(name string) (config.App, error) {
	for _, a := range s.cfg.Apps {
		if a.Name == name {
			if !appAllowed(s.active, name) {
				return config.App{}, fmt.Errorf("permission denied: role %s does not include app %q", roleName(s.active), name)
			}
			return a, nil
		}
	}
	return config.App{}, fmt.Errorf("no app %q in the cluster.yaml apps: catalog", name)
}
