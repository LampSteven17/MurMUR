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

// NodesView lists ProxMox cluster nodes.
type NodesView struct {
	cfg     *config.Config
	client  *proxmox.Client
	styles  Styles
	table   table.Model
	nodes   []proxmox.NodeStatus
	loading bool
	loaded  bool
	err     error
	fetched time.Time
}

func NewNodesView(cfg *config.Config, client *proxmox.Client) *NodesView {
	styles := NewStyles(DefaultTheme)

	cols := []table.Column{
		{Title: "", Width: 2}, // status glyph
		{Title: "node", Width: 18},
		{Title: "status", Width: 9},
		{Title: "cpu", Width: 8},
		{Title: "mem", Width: 14},
		{Title: "uptime", Width: 12},
		{Title: "level", Width: 10},
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

	return &NodesView{
		cfg:    cfg,
		client: client,
		styles: styles,
		table:  t,
	}
}

func (v *NodesView) Init() tea.Cmd {
	v.loading = true
	return v.fetch()
}

func (v *NodesView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch m := msg.(type) {
	case RefreshMsg:
		if v.loading {
			return v, nil
		}
		v.loading = true
		v.err = nil
		return v, v.fetch()
	case NodesDataMsg:
		v.loading = false
		v.loaded = true
		if m.Err != nil {
			v.err = m.Err
			return v, nil
		}
		v.err = nil
		v.fetched = time.Now()
		v.applyNodes(m.Nodes)
		return v, nil
	}
	var cmd tea.Cmd
	v.table, cmd = v.table.Update(msg)
	return v, cmd
}

func (v *NodesView) applyNodes(nodes []proxmox.NodeStatus) {
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Node < nodes[j].Node })
	v.nodes = nodes
	rows := make([]table.Row, len(nodes))
	for i, n := range nodes {
		memUsed := formatBytes(n.Mem)
		memMax := formatBytes(n.MaxMem)
		rows[i] = table.Row{
			nodeStatusGlyph(n.Status),
			truncate(n.Node, 18),
			n.Status,
			formatPercent(n.CPU),
			fmt.Sprintf("%s/%s", memUsed, memMax),
			formatUptime(n.Uptime),
			n.Level,
		}
	}
	v.table.SetRows(rows)
	if v.table.Cursor() >= len(rows) {
		v.table.SetCursor(0)
	}
}

func (v *NodesView) View(width, height int) string {
	v.table.SetWidth(width)
	tableHeight := height - 6
	if tableHeight < 3 {
		tableHeight = 3
	}
	v.table.SetHeight(tableHeight)

	var b strings.Builder
	header := v.styles.Title.Render(Glyph.Node + "  NODES")
	count := v.styles.Subtle.Render(fmt.Sprintf("%d in the conclave", len(v.nodes)))
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
	case v.loaded && len(v.nodes) == 0:
		b.WriteString(v.styles.Subtle.Render(Glyph.Empty + " no nodes returned."))
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

func (v *NodesView) Title() string { return "nodes" }

func (v *NodesView) CapturesKeys() bool { return false }

func (v *NodesView) Help() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	}
}

func (v *NodesView) fetch() tea.Cmd {
	client := v.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		nodes, err := client.GetNodes(ctx)
		if err != nil {
			return NodesDataMsg{Err: err}
		}
		return NodesDataMsg{Nodes: nodes}
	}
}

// nodeStatusGlyph maps a node's status to an alchemical glyph.
func nodeStatusGlyph(status string) string {
	switch status {
	case "online":
		return Glyph.Running
	case "offline":
		return Glyph.Stopped
	default:
		return Glyph.Empty
	}
}
