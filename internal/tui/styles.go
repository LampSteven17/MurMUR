package tui

import "github.com/charmbracelet/lipgloss"

// Theme is the murmur color palette. Subdued, terminal-friendly.
type Theme struct {
	Accent   lipgloss.Color
	Muted    lipgloss.Color
	Ok       lipgloss.Color
	Warn     lipgloss.Color
	Err      lipgloss.Color
	Border   lipgloss.Color
	BoldText lipgloss.Color
}

var DefaultTheme = Theme{
	Accent:   lipgloss.Color("212"), // pink-magenta
	Muted:    lipgloss.Color("241"),
	Ok:       lipgloss.Color("78"),  // green
	Warn:     lipgloss.Color("214"), // amber
	Err:      lipgloss.Color("203"), // red
	Border:   lipgloss.Color("240"),
	BoldText: lipgloss.Color("252"),
}

type Styles struct {
	Title    lipgloss.Style
	Subtle   lipgloss.Style
	Badge    lipgloss.Style
	BadgeOk  lipgloss.Style
	BadgeErr lipgloss.Style
	Key      lipgloss.Style
	Help     lipgloss.Style
	Border   lipgloss.Style
}

func NewStyles(t Theme) Styles {
	badge := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	return Styles{
		Title:    lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		Subtle:   lipgloss.NewStyle().Foreground(t.Muted),
		Badge:    badge.Background(t.Muted).Foreground(lipgloss.Color("236")),
		BadgeOk:  badge.Background(t.Ok).Foreground(lipgloss.Color("236")),
		BadgeErr: badge.Background(t.Err).Foreground(lipgloss.Color("236")),
		Key:      lipgloss.NewStyle().Foreground(t.Accent).Bold(true),
		Help:     lipgloss.NewStyle().Foreground(t.Muted),
		Border:   lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(t.Border).Padding(0, 1),
	}
}
