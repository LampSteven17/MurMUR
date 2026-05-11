package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// OverviewView is the cluster overview screen.
type OverviewView struct {
	cfg     *config.Config
	client  *proxmox.Client
	styles  Styles
	loading bool
	loaded  bool
	err     error
	data    ClusterDataMsg
}

func NewOverviewView(cfg *config.Config, client *proxmox.Client) *OverviewView {
	return &OverviewView{
		cfg:    cfg,
		client: client,
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
			return v, nil // ignore concurrent refresh
		}
		v.loading = true
		v.err = nil
		return v, v.fetch()
	case ClusterDataMsg:
		v.loading = false
		v.loaded = true
		if m.Err != nil {
			v.err = m.Err
		} else {
			v.err = nil
			v.data = m
		}
	}
	return v, nil
}

func (v *OverviewView) View(width, height int) string {
	var b strings.Builder

	// Status line
	switch {
	case v.loading:
		b.WriteString(v.styles.Badge.Render("LOADING"))
	case v.err != nil:
		b.WriteString(v.styles.BadgeErr.Render("ERROR"))
	case v.loaded:
		b.WriteString(v.styles.BadgeOk.Render("READY"))
	default:
		b.WriteString(v.styles.Badge.Render("IDLE"))
	}
	b.WriteString("\n\n")

	if v.err != nil {
		b.WriteString(v.styles.Border.Width(width - 4).Render(
			lipgloss.NewStyle().Foreground(DefaultTheme.Err).Render(v.err.Error()),
		))
		b.WriteString("\n")
		return b.String()
	}

	if !v.loaded {
		return b.String()
	}

	// Version line
	b.WriteString(fmt.Sprintf("%s %s  %s %s\n",
		v.styles.Subtle.Render("proxmox"),
		lipgloss.NewStyle().Bold(true).Render(v.data.Version.Version),
		v.styles.Subtle.Render("release"),
		v.data.Version.Release,
	))
	b.WriteString(fmt.Sprintf("%s %s\n\n",
		v.styles.Subtle.Render("fetched"),
		time.Now().Format("15:04:05"),
	))

	// Tally by type
	tally := map[string]int{}
	for _, r := range v.data.Resources {
		tally[r.Type]++
	}
	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteString(v.styles.Title.Render("Resources") + "\n")
	for _, k := range keys {
		b.WriteString(fmt.Sprintf("  %-10s %d\n", k, tally[k]))
	}

	return b.String()
}

func (v *OverviewView) Title() string { return "overview" }

func (v *OverviewView) Help() []key.Binding { return nil }

// fetch returns a tea.Cmd that calls the API and emits ClusterDataMsg.
// Runs on a goroutine managed by Bubble Tea.
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
