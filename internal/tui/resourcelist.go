package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// resourceMode picks which rows pass the filter.
type resourceMode int

const (
	modeVMs       resourceMode = iota // qemu rows that are NOT templates
	modeLXCs                          // lxc rows that are NOT templates
	modeTemplates                     // qemu rows marked template=1
)

// ResourceListView is a generic table view of /cluster/resources rows
// filtered by mode (VMs, LXCs, or Templates).
type ResourceListView struct {
	cfg     *config.Config
	client  *proxmox.Client
	styles  Styles
	mode    resourceMode
	label   string // "VMs" | "LXCs" | "templates"
	glyph   string // ⚗ | ☿ | ⚗ (templates reuse VM glyph)
	table   table.Model
	rows    []proxmox.Resource // backing rows aligned with table rows
	loading bool
	loaded  bool
	err     error
	fetched time.Time
}

// NewVMsView returns a ResourceListView wired for running/stopped VMs (non-template qemu).
func NewVMsView(cfg *config.Config, client *proxmox.Client) *ResourceListView {
	return newResourceList(cfg, client, modeVMs, "VMs", Glyph.VM)
}

// NewLXCsView returns a ResourceListView wired for LXCs.
func NewLXCsView(cfg *config.Config, client *proxmox.Client) *ResourceListView {
	return newResourceList(cfg, client, modeLXCs, "LXCs", Glyph.LXC)
}

// NewTemplatesView returns a ResourceListView wired for qemu templates.
func NewTemplatesView(cfg *config.Config, client *proxmox.Client) *ResourceListView {
	return newResourceList(cfg, client, modeTemplates, "templates", Glyph.VM)
}

func newResourceList(cfg *config.Config, client *proxmox.Client, mode resourceMode, label, glyph string) *ResourceListView {
	styles := NewStyles(DefaultTheme)

	cols := []table.Column{
		{Title: "vmid", Width: 5},
		{Title: "", Width: 2}, // status glyph
		{Title: "name", Width: 20},
		{Title: "node", Width: 14},
		{Title: "cpu", Width: 6},
		{Title: "mem", Width: 8},
		{Title: "disk", Width: 8},
		{Title: "uptime", Width: 10},
	}

	tStyles := table.DefaultStyles()
	tStyles.Header = tStyles.Header.
		Foreground(DefaultTheme.Muted).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(DefaultTheme.Muted).
		BorderBottom(true).
		Bold(false)
	tStyles.Selected = tStyles.Selected.
		Foreground(DefaultTheme.Ink).
		Background(DefaultTheme.Glitch).
		Bold(false)
	tStyles.Cell = tStyles.Cell.Foreground(DefaultTheme.Ink)

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(nil),
		table.WithFocused(true),
		table.WithHeight(10),
		table.WithStyles(tStyles),
	)

	return &ResourceListView{
		cfg:    cfg,
		client: client,
		styles: styles,
		mode:   mode,
		label:  label,
		glyph:  glyph,
		table:  t,
	}
}

// match reports whether a resource row passes the view's filter.
func (v *ResourceListView) match(r proxmox.Resource) bool {
	switch v.mode {
	case modeVMs:
		return r.Type == "qemu" && r.Template == 0
	case modeLXCs:
		return r.Type == "lxc" && r.Template == 0
	case modeTemplates:
		return r.Type == "qemu" && r.Template == 1
	}
	return false
}

func (v *ResourceListView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *ResourceListView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		// Height handled at App level via View(width, height); ignore.
		_ = m
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
		v.fetched = time.Now()
		v.applyResources(m.Resources)
		return v, nil
	}
	// Route key/mouse messages into the table for cursor nav.
	var cmd tea.Cmd
	v.table, cmd = v.table.Update(msg)
	return v, cmd
}

// applyResources filters and sorts the resource set, populating v.rows
// and the table.
func (v *ResourceListView) applyResources(all []proxmox.Resource) {
	var filtered []proxmox.Resource
	for _, r := range all {
		if v.match(r) {
			filtered = append(filtered, r)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].VMID < filtered[j].VMID
	})
	v.rows = filtered

	rows := make([]table.Row, len(filtered))
	for i, r := range filtered {
		rows[i] = table.Row{
			fmt.Sprintf("%d", r.VMID),
			statusGlyph(r.Status),
			truncate(r.Name, 20),
			truncate(r.Node, 14),
			fmt.Sprintf("%dc", int(r.MaxCPU+0.5)),
			formatBytes(r.MaxMem),
			formatBytes(r.MaxDisk),
			formatUptime(r.Uptime),
		}
	}
	v.table.SetRows(rows)
	if v.table.Cursor() >= len(rows) {
		v.table.SetCursor(0)
	}
}

func (v *ResourceListView) View(width, height int) string {
	v.table.SetWidth(width)
	tableHeight := height - 6 // header line + status line + spacing
	if tableHeight < 3 {
		tableHeight = 3
	}
	v.table.SetHeight(tableHeight)

	var b strings.Builder
	header := v.styles.Title.Render(fmt.Sprintf("%s  %s", v.glyph, strings.ToUpper(v.label)))
	count := v.styles.Subtle.Render(fmt.Sprintf("%d known", len(v.rows)))
	gap := width - lipgloss.Width(header) - lipgloss.Width(count)
	if gap < 1 {
		gap = 1
	}
	b.WriteString(header + lipgloss.NewStyle().Width(gap).Render("") + count)
	b.WriteString("\n\n")

	switch {
	case v.err != nil:
		b.WriteString(v.styles.StatusError.Render(Glyph.Error+" ") +
			lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Render(v.err.Error()))
		b.WriteString("\n")
		return b.String()
	case v.loading && !v.loaded:
		b.WriteString(v.styles.Subtle.Render(Glyph.InFlight + " consulting the conclave…"))
		return b.String()
	case v.loaded && len(v.rows) == 0:
		b.WriteString(v.styles.Subtle.Render(Glyph.Empty + " no " + v.label + " present in the cluster."))
		return b.String()
	}

	b.WriteString(v.table.View())
	b.WriteString("\n")
	if !v.fetched.IsZero() {
		ts := v.styles.Subtle.Render(fmt.Sprintf("fetched %s", v.fetched.Format("15:04:05")))
		hb := v.styles.Heartbeat.Render("◉")
		b.WriteString(" " + hb + " " + ts)
	}
	return b.String()
}

func (v *ResourceListView) Title() string { return v.label }

func (v *ResourceListView) Help() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	}
}

// Selected returns the currently selected resource, or false if none.
func (v *ResourceListView) Selected() (proxmox.Resource, bool) {
	if len(v.rows) == 0 {
		return proxmox.Resource{}, false
	}
	idx := v.table.Cursor()
	if idx < 0 || idx >= len(v.rows) {
		return proxmox.Resource{}, false
	}
	return v.rows[idx], true
}

func (v *ResourceListView) fetch() tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		resources, err := client.GetResources(ctx, "")
		if err != nil {
			return ClusterDataMsg{Err: err}
		}
		return ClusterDataMsg{Resources: resources}
	}
}

// statusGlyph maps a ProxMox status string to an alchemical glyph.
func statusGlyph(status string) string {
	switch status {
	case "running":
		return Glyph.Running
	case "stopped":
		return Glyph.Stopped
	default:
		return Glyph.Empty
	}
}

func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
