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
	Patch    key.Binding
	Users    key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
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
		Patch:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "patch")),
		// `x` because lowercase `u` is host-update. Nothing else uses `x`.
		Users:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "users")),
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
	active     *config.ActiveUser
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

	// toast is a transient one-line message shown in the footer (e.g.
	// "permission denied: deployer role doesn't allow [u]pdate"). Cleared
	// at the top of the next KeyMsg handling so it persists exactly one
	// keypress past the trigger — no tea.Tick required.
	toast string
}

// New constructs an App. configPath is the resolved cluster.yaml path; it is
// surfaced in the header. active is the resolved operator identity (used by
// deploy/teardown/patch/apps to stamp ownership tags + pools).
func New(cfg *config.Config, client *proxmox.Client, active *config.ActiveUser, configPath string) *App {
	a := &App{
		cfg:        cfg,
		client:     client,
		active:     active,
		configPath: configPath,
		keys:       DefaultKeyMap(),
		styles:     NewStyles(DefaultTheme),
		help:       help.New(),
		tabByID:    map[string]int{},
		tabCache:   map[string]View{},
	}
	a.tabs = []tabSpec{
		{name: "apps", label: "apps", keyLabel: "a", action: true, make: func() View { return NewAppsView(cfg, client, active, configPath) }},
		{name: "deploy", label: "deploy", keyLabel: "d", action: true, make: func() View { return NewDeployView(cfg, client, active) }},
		{name: "teardown", label: "teardown", keyLabel: "t", action: true, make: func() View { return NewTeardownView(cfg, client, active) }},
		{name: "update", label: "update", keyLabel: "u", action: true, make: func() View { return NewUpdateView(cfg, client, active) }},
		{name: "patch", label: "patch", keyLabel: "p", action: true, make: func() View { return NewPatchView(cfg, client, active) }},
		{name: "users", label: "users", keyLabel: "x", action: true, make: func() View { return NewUsersView(cfg, client, active, configPath) }},
		{name: "overview", label: "overview", keyLabel: "1", make: func() View { return NewOverviewView(cfg, client, active) }},
		{name: "vms", label: "VMs", keyLabel: "2", make: func() View { return NewVMsView(cfg, client, active) }},
		{name: "lxcs", label: "LXCs", keyLabel: "3", make: func() View { return NewLXCsView(cfg, client, active) }},
		{name: "nodes", label: "nodes", keyLabel: "4", make: func() View { return NewNodesView(cfg, client) }},
		{name: "templates", label: "templates", keyLabel: "5", make: func() View { return NewTemplatesView(cfg, client) }},
	}
	for i, t := range a.tabs {
		a.tabByID[t.name] = i
	}
	// Initial stack: welcome splash. First keypress switches to overview.
	a.stack = []View{NewWelcomeView(cfg, active, configPath)}
	return a
}

// Run starts the TUI program. Blocks until quit.
func Run(cfg *config.Config, client *proxmox.Client, active *config.ActiveUser, configPath string) error {
	prog := tea.NewProgram(New(cfg, client, active, configPath), tea.WithAltScreen())
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
		// Any keypress clears the standing toast (denied-tab notice etc).
		// Handlers below may set a new one before we render again.
		a.toast = ""
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
		// App-level keys: always quit. Tab keys only at stack depth 1.
		switch {
		case key.Matches(m, a.keys.Quit):
			return a, tea.Quit
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
			case key.Matches(m, a.keys.Patch):
				return a, a.switchTab("patch")
			case key.Matches(m, a.keys.Users):
				return a, a.switchTab("users")
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
//
// If the active operator's role doesn't include this tab, switchTab refuses
// and sets a toast in the footer instead. The fallback admin path (no users:
// section) treats all tabs as allowed.
func (a *App) switchTab(name string) tea.Cmd {
	idx, ok := a.tabByID[name]
	if !ok {
		return nil
	}
	spec := a.tabs[idx]
	if !a.tabAllowed(name) {
		a.toast = fmt.Sprintf("permission denied: role %q does not include tab [%s] %s",
			a.active.Role.Name, spec.keyLabel, spec.label)
		return nil
	}
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

// tabAbbrev returns the compressed-state label for the tab strip — first 3
// chars of the label, preserving case (so "VMs" stays "VMs"). Labels of 3 or
// fewer chars pass through unchanged.
func tabAbbrev(label string) string {
	if len(label) <= 3 {
		return label
	}
	return label[:3]
}

// tabAllowed reports whether the active operator's role permits viewing the
// given tab. Fallback admin (no users: section) → all tabs allowed.
func (a *App) tabAllowed(name string) bool {
	if a.active == nil || a.active.Fallback {
		return true
	}
	return a.active.Role.Allows(a.active.Role.Tabs, name)
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

	// Tab strip: compressed "browser-tab" style without brackets. Inactive
	// tabs render as just their hotkey letter in key color; the active tab
	// expands to `<key> <label>` (key in gold, label bold) and gets a heavy
	// (━) underline segment in the divider directly beneath it. Tabs the
	// active role doesn't include are hidden entirely. Action tabs and view
	// tabs are separated by a doubled vertical bar.
	keyStyle := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	sep := a.styles.Subtle.Render(" │ ")
	groupSep := a.styles.Subtle.Render(" ║ ")

	var tb strings.Builder
	tb.WriteString(" ")

	// Track each tab's column span so the divider line can place ━━━ exactly
	// beneath the active tab's full rendered chunk (key + space + label).
	type tabSpan struct {
		start, end int
		active     bool
	}
	var spans []tabSpan
	cursorCol := 1 // we wrote one leading space

	insertedDivider := false
	firstRendered := true
	for _, t := range a.tabs {
		if !a.tabAllowed(t.name) {
			continue // hidden — the active role doesn't include this tab
		}
		switch {
		case !t.action && !insertedDivider:
			// Crossing from the action group into the view group.
			if !firstRendered {
				tb.WriteString(groupSep)
				cursorCol += lipgloss.Width(groupSep)
			}
			insertedDivider = true
		case !firstRendered:
			tb.WriteString(sep)
			cursorCol += lipgloss.Width(sep)
		}
		firstRendered = false

		abbrev := tabAbbrev(t.label) // 3-char prefix for the compressed (inactive) state
		var (
			rendered string
			width    int
		)
		if t.name == a.activeID {
			rendered = keyStyle.Render(t.keyLabel) + " " + a.styles.Title.Render(t.label)
			width = lipgloss.Width(t.keyLabel) + 1 + lipgloss.Width(t.label)
		} else {
			rendered = keyStyle.Render(t.keyLabel) + " " + a.styles.Subtle.Render(abbrev)
			width = lipgloss.Width(t.keyLabel) + 1 + lipgloss.Width(abbrev)
		}
		spans = append(spans, tabSpan{start: cursorCol, end: cursorCol + width, active: t.name == a.activeID})
		tb.WriteString(rendered)
		cursorCol += width
	}

	// Build the divider with ─ everywhere except the active tab's span which
	// gets ━ (heavy). Use rune slice so non-ASCII counting stays sane.
	divRunes := make([]rune, a.width)
	for i := range divRunes {
		divRunes[i] = '─'
	}
	for _, s := range spans {
		if !s.active {
			continue
		}
		for c := s.start; c < s.end && c < a.width; c++ {
			divRunes[c] = '━'
		}
	}
	// Render with a single style — Subtle for ─, gold for the heavy run, so
	// the active underline catches the eye.
	var dvb strings.Builder
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	i := 0
	for i < len(divRunes) {
		if divRunes[i] == '━' {
			j := i
			for j < len(divRunes) && divRunes[j] == '━' {
				j++
			}
			dvb.WriteString(gold.Render(string(divRunes[i:j])))
			i = j
		} else {
			j := i
			for j < len(divRunes) && divRunes[j] == '─' {
				j++
			}
			dvb.WriteString(a.styles.Subtle.Render(string(divRunes[i:j])))
			i = j
		}
	}
	return strings.Join([]string{titleLine, tb.String(), dvb.String()}, "\n")
}

func (a *App) renderFooter() string {
	divider := a.styles.Subtle.Render(strings.Repeat("─", a.width))
	// Toast takes priority so the operator actually sees the permission-denied
	// message. Cleared on next keypress (see Update).
	if a.toast != "" {
		warn := lipgloss.NewStyle().Foreground(DefaultTheme.Vermilion).Bold(true)
		return "\n" + divider + "\n " + warn.Render(Glyph.Error+" "+a.toast)
	}
	// Minimal footer: the active view's contextual keys (if any) + quit. The
	// tab keys live in the top bar already, so there's no global cheatsheet.
	bindings := append(a.top().Help(), a.keys.Quit)
	return "\n" + divider + "\n" + a.help.ShortHelpView(bindings)
}
