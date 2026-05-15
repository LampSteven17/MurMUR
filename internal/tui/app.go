// Package tui implements the murmur Bubble Tea TUI.
//
// Architecture invariants:
//   - No tea.Tick. Redraws happen on user input or async-fetch-complete messages.
//   - Alt-screen mode is used so the user's scrollback isn't trashed during a
//     session, but because there are zero periodic redraws, terminal text
//     selection in the alt-screen also works.
//   - Views implement the View interface and own their own state.
//   - App holds a stack: stack[0] is the active tab; overlays (modals, stream
//     view) push above. Top of stack handles input; nav keys (1-4) are
//     swallowed when overlays are present.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
	"github.com/rtx-monster/murmur/internal/proxmox"
)

// View is one screen of the TUI. Views own their state and respond to messages.
type View interface {
	Init() tea.Cmd
	Update(tea.Msg) (View, tea.Cmd)
	View(width, height int) string
	Title() string
	Help() []key.Binding
	// CapturesKeys signals that the view is in a text-entry mode (e.g. a
	// focused textinput) and wants ALL key events delivered raw. While true,
	// app-level shortcuts (tab keys, refresh, help, q) are bypassed; only
	// ctrl+c still quits.
	CapturesKeys() bool
}

// KeyMap is the set of app-level keybindings. Per-view keys live on the view.
type KeyMap struct {
	Refresh  key.Binding
	Help     key.Binding
	Quit     key.Binding
	Tab1     key.Binding
	Tab2     key.Binding
	Tab3     key.Binding
	Tab4     key.Binding
	Tab5     key.Binding
	Apps     key.Binding
	Deploy   key.Binding
	Teardown key.Binding
	Update   key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Tab1:     key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "overview")),
		Tab2:     key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "VMs")),
		Tab3:     key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "LXCs")),
		Tab4:     key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "nodes")),
		Tab5:     key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "templates")),
		Apps:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "apps")),
		Deploy:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "deploy")),
		Teardown: key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "teardown")),
		Update:   key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update")),
	}
}

// tabSpec is one entry in the tab bar.
type tabSpec struct {
	name     string // key for SwitchTabMsg
	label    string // shown in tab bar
	keyLabel string // single-char label rendered in brackets (e.g. "1", "d")
	action   bool   // separates action tabs (deploy/teardown/update) from view tabs
	make     func() View
}

// App is the root Bubble Tea model.
type App struct {
	cfg        *config.Config
	client     *proxmox.Client
	configPath string

	tabs    []tabSpec
	tabByID map[string]int
	// tabCache holds the latest constructed instance per tab so navigation
	// doesn't reset fetched data.
	tabCache map[string]View
	activeID string

	stack []View // stack[0] = active tab; entries above are overlays
	keys  KeyMap
	styles Styles
	help   help.Model
	width  int
	height int
}

// New constructs an App. configPath is the resolved cluster.yaml path; it is
// surfaced in the header.
func New(cfg *config.Config, client *proxmox.Client, configPath string) *App {
	a := &App{
		cfg:        cfg,
		client:     client,
		configPath: configPath,
		keys:       DefaultKeyMap(),
		styles:     NewStyles(DefaultTheme),
		help:       help.New(),
		tabByID:    map[string]int{},
		tabCache:   map[string]View{},
	}
	a.tabs = []tabSpec{
		{name: "apps", label: "apps", keyLabel: "a", action: true, make: func() View { return NewAppsView(cfg, client, configPath) }},
		{name: "deploy", label: "deploy", keyLabel: "d", action: true, make: func() View { return NewDeployView(cfg, client) }},
		{name: "teardown", label: "teardown", keyLabel: "t", action: true, make: func() View { return NewTeardownView(cfg, client) }},
		{name: "update", label: "update", keyLabel: "u", action: true, make: func() View { return NewUpdateView(cfg, client) }},
		{name: "overview", label: "overview", keyLabel: "1", make: func() View { return NewOverviewView(cfg, client) }},
		{name: "vms", label: "VMs", keyLabel: "2", make: func() View { return NewVMsView(cfg, client) }},
		{name: "lxcs", label: "LXCs", keyLabel: "3", make: func() View { return NewLXCsView(cfg, client) }},
		{name: "nodes", label: "nodes", keyLabel: "4", make: func() View { return NewNodesView(cfg, client) }},
		{name: "templates", label: "templates", keyLabel: "5", make: func() View { return NewTemplatesView(cfg, client) }},
	}
	for i, t := range a.tabs {
		a.tabByID[t.name] = i
	}
	// Initial stack: welcome splash. First keypress switches to overview.
	a.stack = []View{NewWelcomeView(cfg, configPath)}
	return a
}

// Run starts the TUI program. Blocks until quit.
func Run(cfg *config.Config, client *proxmox.Client, configPath string) error {
	prog := tea.NewProgram(New(cfg, client, configPath), tea.WithAltScreen())
	_, err := prog.Run()
	return err
}

func (a *App) Init() tea.Cmd {
	return a.top().Init()
}

func (a *App) top() View {
	return a.stack[len(a.stack)-1]
}

func (a *App) replaceTop(v View) {
	a.stack[len(a.stack)-1] = v
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.help.Width = m.Width

	case SwitchTabMsg:
		return a, a.switchTab(m.Name)

	case PushViewMsg:
		a.stack = append(a.stack, m.View)
		return a, m.View.Init()

	case PopViewMsg:
		if len(a.stack) > 1 {
			a.stack = a.stack[:len(a.stack)-1]
		}
		return a, nil

	case tea.KeyMsg:
		// In text-entry modes the view consumes raw keys; only ctrl+c (a
		// terminal convention, not typeable) still escapes.
		if a.top().CapturesKeys() {
			if m.Type == tea.KeyCtrlC {
				return a, tea.Quit
			}
			v, cmd := a.top().Update(msg)
			a.replaceTop(v)
			return a, cmd
		}
		// App-level keys: always quit/help. Tab keys only at stack depth 1.
		switch {
		case key.Matches(m, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(m, a.keys.Help):
			a.help.ShowAll = !a.help.ShowAll
			return a, nil
		case key.Matches(m, a.keys.Refresh):
			v, cmd := a.top().Update(RefreshMsg{})
			a.replaceTop(v)
			return a, cmd
		}
		if len(a.stack) == 1 {
			switch {
			case key.Matches(m, a.keys.Tab1):
				return a, a.switchTab("overview")
			case key.Matches(m, a.keys.Tab2):
				return a, a.switchTab("vms")
			case key.Matches(m, a.keys.Tab3):
				return a, a.switchTab("lxcs")
			case key.Matches(m, a.keys.Tab4):
				return a, a.switchTab("nodes")
			case key.Matches(m, a.keys.Tab5):
				return a, a.switchTab("templates")
			case key.Matches(m, a.keys.Apps):
				return a, a.switchTab("apps")
			case key.Matches(m, a.keys.Deploy):
				return a, a.switchTab("deploy")
			case key.Matches(m, a.keys.Teardown):
				return a, a.switchTab("teardown")
			case key.Matches(m, a.keys.Update):
				return a, a.switchTab("update")
			}
		}
	}
	// Route everything else to the top view.
	v, cmd := a.top().Update(msg)
	a.replaceTop(v)
	return a, cmd
}

// placeholderView is shown for not-yet-implemented tabs.
type placeholderView struct {
	name   string
	label  string
	styles Styles
}

func newPlaceholderView(name, label string) *placeholderView {
	return &placeholderView{name: name, label: label, styles: NewStyles(DefaultTheme)}
}

func (p *placeholderView) Init() tea.Cmd                       { return nil }
func (p *placeholderView) Update(msg tea.Msg) (View, tea.Cmd)  { return p, nil }
func (p *placeholderView) Title() string                       { return p.label }
func (p *placeholderView) Help() []key.Binding                 { return nil }
func (p *placeholderView) CapturesKeys() bool                  { return false }
func (p *placeholderView) View(width, height int) string {
	msg := p.styles.Subtle.Render(Glyph.Empty + " " + p.label + " — not yet inscribed.")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg)
}

// switchTab replaces stack[0] with the named tab (caching the instance).
// Drops any overlays above. Returns Init cmd for the destination tab.
func (a *App) switchTab(name string) tea.Cmd {
	idx, ok := a.tabByID[name]
	if !ok {
		return nil
	}
	spec := a.tabs[idx]
	v, cached := a.tabCache[name]
	if !cached {
		v = spec.make()
		if v == nil {
			// Placeholder for not-yet-implemented tabs.
			v = newPlaceholderView(name, spec.label)
		}
		a.tabCache[name] = v
	}
	a.activeID = name
	a.stack = []View{v}
	return v.Init()
}

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "" // initial frame before WindowSizeMsg
	}
	// Welcome view renders full-bleed (centered banner) — no chrome.
	if _, isWelcome := a.top().(*WelcomeView); isWelcome {
		return a.top().View(a.width, a.height)
	}
	header := a.renderHeader()
	footer := a.renderFooter()
	bodyHeight := a.height - lipgloss.Height(header) - lipgloss.Height(footer)
	body := a.top().View(a.width, bodyHeight)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (a *App) renderHeader() string {
	title := a.styles.Title.Render(fmt.Sprintf("⚗ murmur · %s", a.cfg.Cluster.Name))
	path := a.styles.Hex.Render(a.configPath)
	gap := a.width - lipgloss.Width(title) - lipgloss.Width(path)
	if gap < 1 {
		gap = 1
	}
	titleLine := title + lipgloss.NewStyle().Width(gap).Render("") + path

	// Tab bar: action tabs left, view tabs right of a divider.
	var tb strings.Builder
	tb.WriteString(" ")
	insertedDivider := false
	for _, t := range a.tabs {
		if !t.action && !insertedDivider {
			tb.WriteString(a.styles.Subtle.Render("│  "))
			insertedDivider = true
		}
		k := a.styles.Key.Render("[" + t.keyLabel + "]")
		label := t.label
		if t.name == a.activeID {
			label = a.styles.Title.Render(label)
		} else {
			label = a.styles.Subtle.Render(label)
		}
		tb.WriteString(k + label + "  ")
	}
	divider := a.styles.Subtle.Render(strings.Repeat("─", a.width))
	return strings.Join([]string{titleLine, tb.String(), divider}, "\n")
}

func (a *App) renderFooter() string {
	bindings := append(a.top().Help(),
		a.keys.Apps, a.keys.Deploy, a.keys.Teardown, a.keys.Update,
		a.keys.Refresh, a.keys.Help, a.keys.Quit,
	)
	divider := a.styles.Subtle.Render(strings.Repeat("─", a.width))
	if a.help.ShowAll {
		return "\n" + divider + "\n" + a.help.FullHelpView([][]key.Binding{bindings})
	}
	return "\n" + divider + "\n" + a.help.ShortHelpView(bindings)
}
