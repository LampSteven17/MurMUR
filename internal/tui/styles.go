package tui

import "github.com/charmbracelet/lipgloss"

// Theme is the Athanor palette: medieval-alchemical base with deliberate
// cyber accents. Tokens map to semantic roles, not raw colors — views use
// these by name, never raw hex.
type Theme struct {
	// Base
	Parchment lipgloss.Color // dark stained parchment ground (bg)
	Ink       lipgloss.Color // aged ivory ink (default fg)
	Muted     lipgloss.Color // faded marginalia

	// Alchemical accents
	Vermilion lipgloss.Color // rubrication: headers, errors, selection
	Verdigris lipgloss.Color // OK, sealed, copper-oxide patina
	Gold      lipgloss.Color // emphasis, "sealed" markers, numbers
	Mercury   lipgloss.Color // info, UPIDs, hex fragments

	// Cyber accents — used lightly
	Phosphor lipgloss.Color // live-data heartbeat dots
	Glitch   lipgloss.Color // selection edge

	// Compatibility aliases for legacy code paths
	Accent   lipgloss.Color
	Ok       lipgloss.Color
	Warn     lipgloss.Color
	Err      lipgloss.Color
	Border   lipgloss.Color
	BoldText lipgloss.Color
}

// DefaultTheme is the canonical Athanor palette.
var DefaultTheme = Theme{
	Parchment: lipgloss.Color("#1a1410"),
	Ink:       lipgloss.Color("#d4c5a0"),
	Muted:     lipgloss.Color("#6a5d4a"),

	Vermilion: lipgloss.Color("#b8324a"),
	Verdigris: lipgloss.Color("#4a8a6b"),
	Gold:      lipgloss.Color("#c9a04e"),
	Mercury:   lipgloss.Color("#7da8c7"),

	Phosphor: lipgloss.Color("#7eff5e"),
	Glitch:   lipgloss.Color("#9b4dff"),

	// Compatibility aliases
	Accent:   lipgloss.Color("#c9a04e"),
	Ok:       lipgloss.Color("#4a8a6b"),
	Warn:     lipgloss.Color("#c9a04e"),
	Err:      lipgloss.Color("#b8324a"),
	Border:   lipgloss.Color("#6a5d4a"),
	BoldText: lipgloss.Color("#d4c5a0"),
}

// Glyph is the alchemical signifier vocabulary. Glyphs are decoration only;
// every glyph in the UI has a plain-text label nearby.
var Glyph = struct {
	VM        string
	LXC       string
	Node      string
	Storage   string
	Running   string
	Stopped   string
	InFlight  string
	Selection string
	Sealed    string
	Error     string
	Empty     string
	Refresh   string
}{
	VM:        "⚗",
	LXC:       "☿",
	Node:      "🜂",
	Storage:   "🜔",
	Running:   "◉",
	Stopped:   "○",
	InFlight:  "⚹",
	Selection: "▶",
	Sealed:    "🜚",
	Error:     "✠",
	Empty:     "⚘",
	Refresh:   "☉",
}

// AthanorBorder is the ornate border: simple lines with elemental glyph
// corners. Use for splash/welcome and key dialogs; tables use lighter borders.
var AthanorBorder = lipgloss.Border{
	Top:         "─",
	Bottom:      "─",
	Left:        "│",
	Right:       "│",
	TopLeft:     "⚗",
	TopRight:    "☿",
	BottomLeft:  "🜂",
	BottomRight: "🜔",
}

// Styles is the rendered style collection callers reach for.
type Styles struct {
	// Text-level
	Title     lipgloss.Style // section headers (vermilion)
	Subtle    lipgloss.Style // marginalia (muted)
	Emphasis  lipgloss.Style // emphasized numbers/values (gold)
	Info      lipgloss.Style // info text (mercury)
	Heartbeat lipgloss.Style // live data dot (phosphor)
	Hex       lipgloss.Style // hex fragment subtitles (mercury, dim)

	// Badges
	Badge    lipgloss.Style
	BadgeOk  lipgloss.Style
	BadgeErr lipgloss.Style

	// Status markers
	StatusRunning  lipgloss.Style // ◉ verdigris
	StatusStopped  lipgloss.Style // ○ muted
	StatusInFlight lipgloss.Style // ⚹ gold
	StatusSealed   lipgloss.Style // 🜚 gold
	StatusError    lipgloss.Style // ✠ vermilion

	// Selection
	SelectionMark lipgloss.Style // ▶ vermilion
	SelectionRow  lipgloss.Style // glitch edge

	// Keys / help footer
	Key  lipgloss.Style
	Help lipgloss.Style

	// Borders / containers
	Border        lipgloss.Style
	AthanorBorder lipgloss.Style // ornate-corner border for splash and dialogs
}

func NewStyles(t Theme) Styles {
	badge := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	return Styles{
		Title:     lipgloss.NewStyle().Foreground(t.Vermilion).Bold(true),
		Subtle:    lipgloss.NewStyle().Foreground(t.Muted),
		Emphasis:  lipgloss.NewStyle().Foreground(t.Gold).Bold(true),
		Info:      lipgloss.NewStyle().Foreground(t.Mercury),
		Heartbeat: lipgloss.NewStyle().Foreground(t.Phosphor),
		Hex:       lipgloss.NewStyle().Foreground(t.Mercury).Faint(true),

		Badge:    badge.Background(t.Muted).Foreground(t.Parchment),
		BadgeOk:  badge.Background(t.Verdigris).Foreground(t.Parchment),
		BadgeErr: badge.Background(t.Vermilion).Foreground(t.Parchment),

		StatusRunning:  lipgloss.NewStyle().Foreground(t.Verdigris),
		StatusStopped:  lipgloss.NewStyle().Foreground(t.Muted),
		StatusInFlight: lipgloss.NewStyle().Foreground(t.Gold),
		StatusSealed:   lipgloss.NewStyle().Foreground(t.Gold).Bold(true),
		StatusError:    lipgloss.NewStyle().Foreground(t.Vermilion).Bold(true),

		SelectionMark: lipgloss.NewStyle().Foreground(t.Vermilion).Bold(true),
		SelectionRow:  lipgloss.NewStyle().Foreground(t.Ink).Background(t.Glitch),

		Key:  lipgloss.NewStyle().Foreground(t.Gold).Bold(true),
		Help: lipgloss.NewStyle().Foreground(t.Muted),

		Border:        lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(t.Muted).Padding(0, 1),
		AthanorBorder: lipgloss.NewStyle().Border(AthanorBorder).BorderForeground(t.Gold).Padding(1, 2),
	}
}
