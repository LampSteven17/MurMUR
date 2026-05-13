package tui

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/rtx-monster/murmur/internal/config"
)

// frameInterval is the tick cadence for the 3D skull animation. ~12.5fps.
const frameInterval = 80 * time.Millisecond

// yawPerFrame is the rotation step per tick. 360° in 4 seconds.
const yawPerFrame = 2.0 * math.Pi / 50.0

// skull rasterization size. Bigger frame → emblem reads more clearly. The
// composition is now skull + crossbones, so we need extra rows.
const (
	skull3DCols = 60
	skull3DRows = 28
)

// skullTickMsg is emitted by tea.Tick during the welcome splash. Sanctioned
// exception to the no-tick rule — see CLAUDE.md. Ticks stop on first keypress.
type skullTickMsg struct{}

func skullTickCmd() tea.Cmd {
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return skullTickMsg{} })
}

// WelcomeView is the animated splash on TUI launch: a continuously-rotating
// 3D ASCII skull above the MURMUR banner. Any keypress dismisses to overview.
type WelcomeView struct {
	cfg        *config.Config
	configPath string
	styles     Styles
	frame      int
	dismissed  bool
}

func NewWelcomeView(cfg *config.Config, configPath string) *WelcomeView {
	return &WelcomeView{
		cfg:        cfg,
		configPath: configPath,
		styles:     NewStyles(DefaultTheme),
	}
}

func (v *WelcomeView) Init() tea.Cmd { return skullTickCmd() }

func (v *WelcomeView) Update(msg tea.Msg) (View, tea.Cmd) {
	switch msg.(type) {
	case tea.KeyMsg:
		v.dismissed = true
		// ClearScreen forces a full repaint on the next frame. Without it,
		// BT's diff renderer can leave the banner's border row on screen
		// after the layout switches from full-bleed welcome to the chromed
		// (header+body+footer) overview frame.
		return v, tea.Batch(
			tea.ClearScreen,
			func() tea.Msg { return SwitchTabMsg{Name: "overview"} },
		)
	case skullTickMsg:
		if v.dismissed {
			return v, nil
		}
		v.frame++
		return v, skullTickCmd()
	}
	return v, nil
}

func (v *WelcomeView) View(width, height int) string {
	yaw := float64(v.frame) * yawPerFrame
	skull := RenderSkull3D(DefaultTheme, yaw, skull3DCols, skull3DRows)

	laugh := v.renderLaugh()
	banner := RenderBanner(DefaultTheme, v.cfg.Cluster.Name, v.configPath)
	prompt := v.styles.Subtle.Render("press any key to enter")

	// Pad each row of the skull to a fixed width so the stack below doesn't
	// drift between frames.
	skullPadded := padBlockToWidth(skull, skull3DCols)

	stack := lipgloss.JoinVertical(lipgloss.Center,
		skullPadded,
		laugh,
		"",
		"",
		banner,
		"",
		prompt,
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, stack)
}

func (v *WelcomeView) Title() string { return "welcome" }

func (v *WelcomeView) Help() []key.Binding { return nil }

// renderLaugh returns the laugh-tag line, centered to the skull frame width.
// Cycles independently of the skull rotation.
func (v *WelcomeView) renderLaugh() string {
	laughCycle := []string{
		"",
		"ha",
		"ha ha",
		"HA HA HA",
		"HA HAA HA",
		"ha ha",
		"ha",
		"",
	}
	// Slow the laugh cycle relative to the rotation so it doesn't blur.
	idx := (v.frame / 4) % len(laughCycle)
	text := laughCycle[idx]
	gold := lipgloss.NewStyle().Foreground(DefaultTheme.Gold).Bold(true)
	rendered := gold.Render(text)
	pad := (skull3DCols - lipgloss.Width(rendered)) / 2
	if pad < 0 {
		pad = 0
	}
	return strings.Repeat(" ", pad) + rendered
}

// padBlockToWidth pads each line of a multi-line block to exactly the given
// display width using spaces — keeps layout stable.
func padBlockToWidth(s string, width int) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		w := lipgloss.Width(ln)
		if w < width {
			lines[i] = ln + strings.Repeat(" ", width-w)
		}
	}
	return strings.Join(lines, "\n")
}
