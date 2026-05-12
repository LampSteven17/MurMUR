package tui

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
)

// WelcomeView is the splash shown on TUI launch. Any keypress (except quit/help)
// dismisses to the overview tab.
type WelcomeView struct {
	cfg        *config.Config
	configPath string
	styles     Styles
}

func NewWelcomeView(cfg *config.Config, configPath string) *WelcomeView {
	return &WelcomeView{
		cfg:        cfg,
		configPath: configPath,
		styles:     NewStyles(DefaultTheme),
	}
}

func (v *WelcomeView) Init() tea.Cmd { return nil }

func (v *WelcomeView) Update(msg tea.Msg) (View, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return v, func() tea.Msg { return SwitchTabMsg{Name: "overview"} }
	}
	return v, nil
}

func (v *WelcomeView) View(width, height int) string {
	banner := RenderBanner(DefaultTheme, v.cfg.Cluster.Name, v.configPath)
	prompt := v.styles.Subtle.Render("press any key to enter")
	stack := lipgloss.JoinVertical(lipgloss.Center, banner, "", prompt)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, stack)
}

func (v *WelcomeView) Title() string { return "welcome" }

func (v *WelcomeView) Help() []key.Binding { return nil }
