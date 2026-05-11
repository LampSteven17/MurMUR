// Package tui implements the murmur Bubble Tea TUI.
//
// Architecture invariants:
//   - No tea.Tick. Redraws happen on user input or async-fetch-complete messages.
//   - Alt-screen mode is used so the user's scrollback isn't trashed during a
//     session, but because there are zero periodic redraws, terminal text
//     selection in the alt-screen also works.
//   - Views implement the View interface and own their own state.
package tui

import (
	"fmt"

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
}

// KeyMap is the set of app-level keybindings. Per-view keys live on the view.
type KeyMap struct {
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "toggle help")),
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

// App is the root Bubble Tea model. Holds shared state and routes messages.
type App struct {
	cfg    *config.Config
	client *proxmox.Client
	view   View
	keys   KeyMap
	styles Styles
	help   help.Model
	width  int
	height int
}

// New constructs an App with the given config and client. The initial view
// is the cluster overview.
func New(cfg *config.Config, client *proxmox.Client) *App {
	a := &App{
		cfg:    cfg,
		client: client,
		keys:   DefaultKeyMap(),
		styles: NewStyles(DefaultTheme),
		help:   help.New(),
	}
	a.view = NewOverviewView(cfg, client)
	return a
}

// Run starts the TUI program. Blocks until quit.
func Run(cfg *config.Config, client *proxmox.Client) error {
	prog := tea.NewProgram(New(cfg, client), tea.WithAltScreen())
	_, err := prog.Run()
	return err
}

func (a *App) Init() tea.Cmd {
	return a.view.Init()
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = m.Width
		a.height = m.Height
		a.help.Width = m.Width
	case tea.KeyMsg:
		switch {
		case key.Matches(m, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(m, a.keys.Help):
			a.help.ShowAll = !a.help.ShowAll
			return a, nil
		case key.Matches(m, a.keys.Refresh):
			// Translate to RefreshMsg and let the view dispatch its fetch.
			v, cmd := a.view.Update(RefreshMsg{})
			a.view = v
			return a, cmd
		}
	}
	v, cmd := a.view.Update(msg)
	a.view = v
	return a, cmd
}

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "" // initial frame before WindowSizeMsg
	}
	header := a.renderHeader()
	footer := a.renderFooter()
	bodyHeight := a.height - lipgloss.Height(header) - lipgloss.Height(footer)
	body := a.view.View(a.width, bodyHeight)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (a *App) renderHeader() string {
	name := a.styles.Title.Render(fmt.Sprintf("murmur · %s", a.cfg.Cluster.Name))
	endpoint := a.styles.Subtle.Render(a.cfg.Cluster.API.Endpoint)
	left := lipgloss.JoinHorizontal(lipgloss.Top, name, "  ", endpoint)
	title := a.styles.Subtle.Render(a.view.Title())
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(title)
	if gap < 1 {
		gap = 1
	}
	return left + lipgloss.NewStyle().Width(gap).Render("") + title + "\n"
}

func (a *App) renderFooter() string {
	bindings := append(a.view.Help(),
		a.keys.Refresh, a.keys.Help, a.keys.Quit,
	)
	if a.help.ShowAll {
		return "\n" + a.help.FullHelpView([][]key.Binding{bindings})
	}
	return "\n" + a.help.ShortHelpView(bindings)
}
