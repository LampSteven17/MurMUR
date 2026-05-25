package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// murmurBanner is the ANSI Shadow figlet rendering of "MURMUR".
const murmurBanner = ` ███╗   ███╗██╗   ██╗██████╗ ███╗   ███╗██╗   ██╗██████╗
 ████╗ ████║██║   ██║██╔══██╗████╗ ████║██║   ██║██╔══██╗
 ██╔████╔██║██║   ██║██████╔╝██╔████╔██║██║   ██║██████╔╝
 ██║╚██╔╝██║██║   ██║██╔══██╗██║╚██╔╝██║██║   ██║██╔══██╗
 ██║ ╚═╝ ██║╚██████╔╝██║  ██║██║ ╚═╝ ██║╚██████╔╝██║  ██║
 ╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═╝`

// RenderBanner returns the full Athanor splash: gold MURMUR block flanked by
// alchemical glyphs, a "Welcome <operator>" greeting, then the cluster name
// and tagline below it. welcomeName is the resolved operator (empty falls back
// to "operator").
func RenderBanner(theme Theme, clusterName, configPath, welcomeName string) string {
	styles := NewStyles(theme)

	// Banner block, gold-on-parchment, with a glyph frieze above and below.
	bannerStyle := lipgloss.NewStyle().Foreground(theme.Gold)
	frieze := styles.Subtle.Render("⚗  ⊹  ☿  ⊹  🜂  ⊹  🜔  ⊹  🜚")

	bannerWidth := lipgloss.Width(murmurBanner)

	if welcomeName == "" {
		welcomeName = "operator"
	}
	// Big-ish greeting centered under the MURMUR block.
	welcome := lipgloss.NewStyle().
		Foreground(theme.Gold).Bold(true).
		Width(bannerWidth).Align(lipgloss.Center).
		Render("✦  WELCOME " + strings.ToUpper(welcomeName) + "  ✦")

	tagline := styles.Title.Render("athanor")
	suffix := styles.Subtle.Render("· the cluster always burns ·")

	cluster := styles.Emphasis.Render(clusterName)
	configLine := styles.Hex.Render(configPath)

	body := strings.Join([]string{
		frieze,
		"",
		bannerStyle.Render(murmurBanner),
		"",
		welcome,
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, tagline, "  ", suffix),
		"",
		styles.Subtle.Render("conclave: ") + cluster,
		styles.Subtle.Render("loaded:   ") + configLine,
	}, "\n")

	return styles.AthanorBorder.Render(body)
}
