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
// alchemical glyphs, with the cluster name and tagline below it.
func RenderBanner(theme Theme, clusterName, configPath string) string {
	styles := NewStyles(theme)

	// Banner block, gold-on-parchment, with a glyph frieze above and below.
	bannerStyle := lipgloss.NewStyle().Foreground(theme.Gold)
	frieze := styles.Subtle.Render("⚗  ⊹  ☿  ⊹  🜂  ⊹  🜔  ⊹  🜚")

	tagline := styles.Title.Render("athanor")
	suffix := styles.Subtle.Render("· the cluster always burns ·")

	cluster := styles.Emphasis.Render(clusterName)
	configLine := styles.Hex.Render(configPath)

	body := strings.Join([]string{
		frieze,
		"",
		bannerStyle.Render(murmurBanner),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, tagline, "  ", suffix),
		"",
		styles.Subtle.Render("conclave: ") + cluster,
		styles.Subtle.Render("loaded:   ") + configLine,
	}, "\n")

	return styles.AthanorBorder.Render(body)
}
