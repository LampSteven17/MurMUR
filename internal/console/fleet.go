package console

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// Fleet is the console's optional live cluster view. It is enabled only when
// the console is started with a --config: without one the console stays
// credential-free and serves events alone.
//
// The PVE identity used here should be read-only (PVEAuditor) — the console
// renders state, it never mutates the cluster.
type Fleet struct {
	client *proxmox.Client
	cfg    *config.Config

	mu       sync.Mutex
	cached   []Guest
	cachedAt time.Time
}

// Guest is one row of the fleet view.
type Guest struct {
	VMID       int     `json:"vmid"`
	Name       string  `json:"name"`
	Type       string  `json:"type"` // qemu | lxc
	Node       string  `json:"node"`
	Status     string  `json:"status"` // running | stopped
	IP         string  `json:"ip,omitempty"`
	Route      string  `json:"route,omitempty"` // PVE description → Traefik hint
	URL        string  `json:"url,omitempty"`   // derived public URL when routable
	Tags       string  `json:"tags,omitempty"`
	CPUs       int     `json:"cpus"`
	MemGB      float64 `json:"mem_gb"`
	Cataloged  bool    `json:"cataloged"`         // present in cluster.yaml apps:
	Unattended bool    `json:"unattended"`        // allowlisted for unattended updates
	Managed    string  `json:"managed,omitempty"` // murmuration role, e.g. "console", "warden"
}

func NewFleet(client *proxmox.Client, cfg *config.Config) *Fleet {
	return &Fleet{client: client, cfg: cfg}
}

// snapshot returns the fleet, cached briefly so a browser refresh (or several
// open tabs) doesn't hammer the PVE API.
func (f *Fleet) snapshot(ctx context.Context) ([]Guest, error) {
	f.mu.Lock()
	if time.Since(f.cachedAt) < 20*time.Second && f.cached != nil {
		defer f.mu.Unlock()
		return f.cached, nil
	}
	f.mu.Unlock()

	resources, err := f.client.GetResources(ctx, "vm")
	if err != nil {
		return nil, err
	}

	catalog := map[string]bool{}
	unattended := map[string]bool{}
	for _, a := range f.cfg.Apps {
		catalog[a.Name] = true
	}
	for _, ag := range f.cfg.Agents {
		for _, app := range ag.UnattendedApps {
			unattended[app] = true
		}
	}

	var guests []Guest
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i := range resources {
		r := resources[i]
		if r.Template == 1 || (r.Type != "qemu" && r.Type != "lxc") {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			g := Guest{
				VMID: r.VMID, Name: r.Name, Type: r.Type, Node: r.Node, Status: r.Status,
				Tags: r.Tags, CPUs: int(r.MaxCPU), MemGB: round1(float64(r.MaxMem) / (1 << 30)),
				Cataloged: catalog[r.Name], Unattended: unattended[r.Name],
			}
			g.Route, g.IP = f.detail(ctx, r)
			g.URL = f.publicURL(g)
			mu.Lock()
			guests = append(guests, g)
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(guests, func(i, j int) bool { return guests[i].VMID < guests[j].VMID })

	f.mu.Lock()
	f.cached, f.cachedAt = guests, time.Now()
	f.mu.Unlock()
	return guests, nil
}

// detail fetches the guest's description (Traefik route hint) and live IP.
// Failures degrade to empty strings — a console panel must never block on one
// unreachable guest.
func (f *Fleet) detail(ctx context.Context, r proxmox.Resource) (route, ip string) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	if r.Type == "lxc" {
		cfg, err := f.client.GetLXCConfig(ctx, r.Node, r.VMID)
		if err == nil {
			route = cfg["description"]
		}
		if r.Status == "running" {
			if ifaces, err := f.client.LXCInterfaces(ctx, r.Node, r.VMID); err == nil {
				ip = proxmox.FirstLXCIPv4(ifaces)
			}
		}
		return strings.TrimSpace(route), ip
	}

	var raw map[string]any
	if err := f.client.GetJSON(ctx, "/nodes/"+r.Node+"/qemu/"+itoa(r.VMID)+"/config", &raw); err == nil {
		if d, ok := raw["description"].(string); ok {
			route = d
		}
	}
	if r.Status == "running" {
		if ifaces, err := f.client.GuestAgentNetInterfaces(ctx, r.Node, r.VMID); err == nil {
			ip = proxmox.FirstIPv4(ifaces)
		}
	}
	return strings.TrimSpace(route), ip
}

// publicURL derives the browsable URL from the guest's route hint, mirroring
// the description formats the reverse-proxy sync understands:
//
//	traefik-port:8080          → <guest-name>.<domain>
//	traefik:grafana:3000,...   → <first subdomain>.<domain>
func (f *Fleet) publicURL(g Guest) string {
	domain := f.cfg.Cluster.Domain
	if domain == "" || g.Route == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(g.Route, "traefik-port:"):
		return "https://" + strings.ToLower(strings.ReplaceAll(g.Name, "_", "-")) + "." + domain
	case strings.HasPrefix(g.Route, "traefik:"):
		spec := strings.TrimPrefix(g.Route, "traefik:")
		first := strings.SplitN(strings.SplitN(spec, ",", 2)[0], ":", 2)[0]
		if first != "" {
			return "https://" + first + "." + domain
		}
	}
	return ""
}

func (f *Fleet) handleFleet(w http.ResponseWriter, r *http.Request) {
	guests, err := f.snapshot(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	nodes := make([]string, 0, len(f.cfg.Cluster.Nodes))
	for _, n := range f.cfg.Cluster.Nodes {
		nodes = append(nodes, n.Name)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"cluster": f.cfg.Cluster.Name,
		"nodes":   nodes,
		"guests":  guests,
	})
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [12]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
