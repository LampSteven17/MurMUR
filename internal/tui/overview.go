package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// OverviewView is the cluster overview screen: login + quota bars + actions
// menu + cluster summary. Action keys (d/t/u) are handled at the app level
// — they switch to the corresponding action tab.
type OverviewView struct {
	cfg     *config.Config
	client  *proxmox.Client
	active  *config.ActiveUser
	styles  Styles
	loading bool
	loaded  bool
	err     error
	data    ClusterDataMsg
	stats   clusterStats
}

// clusterStats is the rolled-up summary shown beneath the skull.
type clusterStats struct {
	NodesOnline int
	NodesTotal  int

	VMsRunning  int
	VMsStopped  int
	LXCsRunning int
	LXCsStopped int
	Templates   int

	// Cluster-wide capacity (summed across online nodes).
	CoresTotal int
	MemUsed    int64
	MemTotal   int64
	DiskUsed   int64
	DiskTotal  int64

	// Allocated to running VMs+LXCs (what's spoken for).
	CoresAllocated int
	MemAllocated   int64
	DiskAllocated  int64

	StorageCount int
}

func NewOverviewView(cfg *config.Config, client *proxmox.Client, active *config.ActiveUser) *OverviewView {
	return &OverviewView{
		cfg:    cfg,
		client: client,
		active: active,
		styles: NewStyles(DefaultTheme),
	}
}

func (v *OverviewView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *OverviewView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case RefreshMsg:
		if v.loading {
			return v, nil
		}
		v.loading = true
		v.err = nil
		return v, v.fetch()
	case ClusterDataMsg:
		v.loading = false
		v.loaded = true
		if m.Err != nil {
			v.err = m.Err
			return v, nil
		}
		v.err = nil
		v.data = m
		v.stats = computeStats(m.Resources)
	}
	return v, nil
}

func (v *OverviewView) View(width, height int) string {
	var b strings.Builder

	// Header line — mirrors the list views' shape: glyph + LABEL left,
	// subtle info right.
	header := v.styles.Title.Render("☉  OVERVIEW")
	right := ""
	if v.loaded {
		right = v.styles.Subtle.Render(fmt.Sprintf(
			"proxmox %s · release %s",
			v.data.Version.Version, v.data.Version.Release,
		))
	}
	gap := width - lipgloss.Width(header) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(header + lipgloss.NewStyle().Width(gap).Render("") + right)
	b.WriteString("\n")
	b.WriteString(v.renderLogin())
	b.WriteString("\n\n")

	switch {
	case v.err != nil:
		b.WriteString(v.styles.StatusError.Render(Glyph.Error+" ") +
			lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error()))
	case !v.loaded:
		b.WriteString(v.styles.Subtle.Render(Glyph.InFlight + " consulting the conclave…"))
	default:
		b.WriteString(v.renderBars())
		b.WriteString("\n\n")
		b.WriteString(v.renderCommands())
		b.WriteString("\n\n")
		b.WriteString(v.renderClusterSummary())
		b.WriteString("\n\n")
		fetched := v.styles.Subtle.Render(fmt.Sprintf("fetched %s", time.Now().Format("15:04:05")))
		heartbeat := v.styles.Heartbeat.Render("◉")
		b.WriteString(" " + heartbeat + " " + fetched)
	}

	// Force exact Width × Height. Done manually because lipgloss.Style.Height
	// doesn't reliably pad short content with blank rows — we explicitly
	// append spaces-per-line up to width and blank rows up to height to
	// fully overwrite anything from the previous view.
	return padBlock(b.String(), width, height)
}

// renderLogin shows the resolved operator identity + role. In fallback mode
// (no users: section) it falls back to the cluster.api.token_id with an
// "implicit admin" annotation so the operator can tell from a glance whether
// the multi-user layer is in effect.
func (v *OverviewView) renderLogin() string {
	subtle := v.styles.Subtle
	info := v.styles.Info
	if v.active == nil || v.active.Fallback {
		token := v.cfg.Cluster.API.TokenID
		if token == "" {
			token = "(no token configured)"
		}
		return " " + subtle.Render("logged in as ") + info.Render(token) +
			subtle.Render("  ·  implicit admin (no users: section)")
	}
	return " " + subtle.Render("operator ") + info.Render(v.active.Name) +
		subtle.Render("  ·  role ") + info.Render(v.active.Role.Name) +
		subtle.Render("  ·  token ") + info.Render(v.active.TokenID)
}

// padBlock pads s to exactly width columns × height rows with spaces.
// Lines wider than width are left as-is; rows beyond height are truncated.
func padBlock(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		w := lipgloss.Width(ln)
		if w < width {
			lines[i] = ln + strings.Repeat(" ", width-w)
		}
	}
	blank := strings.Repeat(" ", width)
	for len(lines) < height {
		lines = append(lines, blank)
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

// barCells is the visual length of each quota bar.
const barCells = 28

// renderBars draws the three primary quota bars: vCPU, RAM, DISK.
// vCPU uses cores-allocated against cores-total (what's spoken for).
// RAM and DISK use actual usage against cluster-wide capacity.
func (v *OverviewView) renderBars() string {
	s := v.stats
	lines := []string{
		v.renderBar("vCPU",
			int64(s.CoresAllocated), int64(s.CoresTotal),
			fmt.Sprintf("%d / %d", s.CoresAllocated, s.CoresTotal)),
		v.renderBar("RAM",
			s.MemUsed, s.MemTotal,
			fmt.Sprintf("%s / %s", formatBytes(s.MemUsed), formatBytes(s.MemTotal))),
		v.renderBar("DISK",
			s.DiskUsed, s.DiskTotal,
			fmt.Sprintf("%s / %s", formatBytes(s.DiskUsed), formatBytes(s.DiskTotal))),
	}
	return strings.Join(lines, "\n")
}

// renderBar draws one labeled bar: " LABEL  ███░░░  used / total   NN%".
// Color of the filled portion shifts with utilization: verdigris < 60%,
// gold < 85%, vermilion ≥ 85%.
func (v *OverviewView) renderBar(label string, used, total int64, value string) string {
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)

	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total)
	}
	if pct > 1 {
		pct = 1
	}
	filled := int(pct * float64(barCells))

	var fillColor lipgloss.Color
	switch {
	case pct >= 0.85:
		fillColor = DefaultTheme.Vermilion
	case pct >= 0.60:
		fillColor = DefaultTheme.Gold
	default:
		fillColor = DefaultTheme.Verdigris
	}
	fillStyle := lipgloss.NewStyle().Foreground(fillColor)

	bar := fillStyle.Render(strings.Repeat("█", filled)) +
		muted.Render(strings.Repeat("░", barCells-filled))

	return fmt.Sprintf(" %s  %s  %s   %s",
		labelStyle.Render(padRight(label, 4)),
		bar,
		gold.Render(padRight(value, 17)),
		muted.Render(fmt.Sprintf("%3d%%", int(pct*100))),
	)
}

// commandEntry is one row in the overview commands panel.
type commandEntry struct {
	key, name, desc string
	tab             string // tab name for role.tabs gating; empty = global (always shown)
}

// actionCommands lists every action tab the operator can reach plus a
// one-line description. Order matches the top tab bar.
var actionCommands = []commandEntry{
	{key: "a", name: "apps", desc: "deploy or update from the cluster.yaml app catalog", tab: "apps"},
	{key: "d", name: "deploy", desc: "provision one raw guest from a form (no catalog)", tab: "deploy"},
	{key: "t", name: "teardown", desc: "stop + destroy guests (multi-select)", tab: "teardown"},
	{key: "u", name: "update", desc: "apt dist-upgrade on PVE host nodes", tab: "update"},
	{key: "p", name: "patch", desc: "re-run `update:` on running app guests", tab: "patch"},
	{key: "x", name: "users", desc: "add / remove / rotate murmur operators (admin only)", tab: "users"},
}

// viewCommands lists every read-only tab.
var viewCommands = []commandEntry{
	{key: "1", name: "overview", desc: "this page — cluster summary + bars", tab: "overview"},
	{key: "2", name: "VMs", desc: "list all virtual machines", tab: "vms"},
	{key: "3", name: "LXCs", desc: "list all containers", tab: "lxcs"},
	{key: "4", name: "nodes", desc: "list PVE host nodes", tab: "nodes"},
	{key: "5", name: "templates", desc: "list VM/LXC templates", tab: "templates"},
}

// renderCommands draws a two-column listing of the tabs the active role can
// actually reach + a one-line description. Tabs the role doesn't include are
// hidden entirely (matching the top tab bar). Global keys (r/q) always show.
func (v *OverviewView) renderCommands() string {
	titleStyle := v.styles.Title
	keyStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Ink).Bold(true)
	descStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)

	allowedTab := func(name string) bool {
		if v.active == nil || v.active.Fallback {
			return true // legacy single-operator path → all tabs allowed
		}
		return v.active.Role.Allows(v.active.Role.Tabs, name)
	}

	row := func(c commandEntry) string {
		return fmt.Sprintf("   %s  %s  %s",
			keyStyle.Render(" "+c.key+" "),
			labelStyle.Render(padRight(c.name, 10)),
			descStyle.Render("· "+c.desc),
		)
	}
	section := func(b *strings.Builder, title string, cmds []commandEntry, gate bool) {
		var rows []string
		for _, c := range cmds {
			if gate && !allowedTab(c.tab) {
				continue // hidden — role doesn't include this tab
			}
			rows = append(rows, row(c))
		}
		if len(rows) == 0 {
			return
		}
		b.WriteString(titleStyle.Render(title) + "\n")
		b.WriteString(strings.Join(rows, "\n") + "\n\n")
	}

	var b strings.Builder
	section(&b, " COMMANDS  ·  ACTIONS", actionCommands, true)
	section(&b, " COMMANDS  ·  VIEWS", viewCommands, true)
	section(&b, " COMMANDS  ·  GLOBAL", []commandEntry{
		{key: "r", name: "refresh", desc: "re-fetch the current view's data"},
		{key: "q", name: "quit", desc: "exit murmur (also Ctrl+C)"},
	}, false)
	return strings.TrimRight(b.String(), "\n")
}


// renderClusterSummary draws the existing rollup beneath the bars+actions.
func (v *OverviewView) renderClusterSummary() string {
	s := v.stats
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	merc := lipgloss.NewStyle().Foreground(DefaultTheme.Mercury)
	verd := lipgloss.NewStyle().Foreground(DefaultTheme.Verdigris)
	muted := lipgloss.NewStyle().Foreground(DefaultTheme.Muted)

	row := func(glyph, label, value, free string) string {
		left := fmt.Sprintf("   %s  %s", glyph, muted.Render(label))
		leftWidth := 22
		pad := leftWidth - lipgloss.Width(left)
		if pad < 1 {
			pad = 1
		}
		line := left + strings.Repeat(" ", pad) + gold.Render(value)
		if free != "" {
			line += "   " + merc.Render(free)
		}
		return line
	}

	lines := []string{
		v.styles.Title.Render(" CLUSTER"),
		row(Glyph.Node, "nodes",
			fmt.Sprintf("%d/%d", s.NodesOnline, s.NodesTotal),
			verd.Render("online")),
		row(Glyph.VM, "VMs",
			fmt.Sprintf("%d", s.VMsRunning+s.VMsStopped),
			fmt.Sprintf("%d running · %d stopped", s.VMsRunning, s.VMsStopped)),
		row(Glyph.LXC, "LXCs",
			fmt.Sprintf("%d", s.LXCsRunning+s.LXCsStopped),
			fmt.Sprintf("%d running · %d stopped", s.LXCsRunning, s.LXCsStopped)),
		row(Glyph.VM, "templates", fmt.Sprintf("%d", s.Templates), ""),
		row(Glyph.Storage, "storages", fmt.Sprintf("%d", s.StorageCount), ""),
	}
	return strings.Join(lines, "\n")
}

// padRight pads s with spaces to reach the given display width.
func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func (v *OverviewView) Title() string { return "overview" }

func (v *OverviewView) CapturesKeys() bool { return false }

func (v *OverviewView) Help() []key.Binding { return nil }

// fetch returns a tea.Cmd that calls the API and emits ClusterDataMsg.
func (v *OverviewView) fetch() tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		version, err := client.GetVersion(ctx)
		if err != nil {
			return ClusterDataMsg{Err: err}
		}
		resources, err := client.GetResources(ctx, "")
		if err != nil {
			return ClusterDataMsg{Err: err}
		}
		return ClusterDataMsg{Version: version, Resources: resources}
	}
}

// computeStats rolls up a /cluster/resources response.
//
// Node rows give cluster-wide capacity (sum of MaxCPU/MaxMem/MaxDisk across
// online nodes), plus their current cpu/mem/disk usage which approximates
// "what's actually consumed right now". VM+LXC rows give per-guest
// allocations; summing the running ones gives "what's spoken for".
//
// Storage rows are deduped by storage name: Proxmox returns one row per
// (storage, node) pair, so a shared pool visible on N nodes shows up N
// times. Without dedupe the cluster-wide DiskTotal/DiskUsed are inflated
// by the share-count of every shared storage. First-seen wins; the dedup
// is by `storage` field on the row (the storage id, not the node).
func computeStats(resources []proxmox.Resource) clusterStats {
	var s clusterStats
	seenStorage := map[string]bool{}
	for _, r := range resources {
		switch r.Type {
		case "node":
			s.NodesTotal++
			if r.Status == "online" {
				s.NodesOnline++
				s.CoresTotal += int(r.MaxCPU + 0.5)
				s.MemTotal += r.MaxMem
				s.MemUsed += r.Mem
				// MaxDisk on node rows is the root filesystem only.
				// Storage capacity is summed separately below.
			}
		case "qemu":
			if r.Template == 1 {
				s.Templates++
				continue
			}
			if r.Status == "running" {
				s.VMsRunning++
				s.CoresAllocated += int(r.MaxCPU + 0.5)
				s.MemAllocated += r.MaxMem
				s.DiskAllocated += r.MaxDisk
			} else {
				s.VMsStopped++
			}
		case "lxc":
			if r.Template == 1 {
				s.Templates++
				continue
			}
			if r.Status == "running" {
				s.LXCsRunning++
				s.CoresAllocated += int(r.MaxCPU + 0.5)
				s.MemAllocated += r.MaxMem
				s.DiskAllocated += r.MaxDisk
			} else {
				s.LXCsStopped++
			}
		case "storage":
			// Dedupe by storage name. r.Storage is the storage id;
			// fall back to r.ID for rows where the API didn't echo it.
			key := r.Storage
			if key == "" {
				key = r.ID
			}
			if seenStorage[key] {
				continue
			}
			seenStorage[key] = true
			s.StorageCount++
			s.DiskTotal += r.MaxDisk
			s.DiskUsed += r.Disk
		}
	}
	return s
}
