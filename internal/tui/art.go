package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// skullArt is the memento-mori centerpiece of the overview screen.
// Eye sockets render in phosphor green (cyber accent); the rest is ivory ink.
const skullArt = `              ▄▄▄▄▄▄▄▄▄▄▄▄▄
           ▄██▀▀▀▀▀▀▀▀▀▀▀▀▀██▄
         ▄█▀                   ▀█▄
        █▀                       ▀█
       █                           █
       █    ███████   ███████      █
       █    █     █   █     █      █
       █    █  @  █   █  @  █      █
       █    █     █   █     █      █
       █    ███████   ███████      █
       █                           █
        █      ▄▄▄▄▄▄▄▄▄▄▄        █
         █▄  ▀█▌▐█▌▐█▌▐█▌▐█▀  ▄█
          ██▄  ▀▀▀▀▀▀▀▀▀▀▀  ▄██
            ▀█▄             ▄█▀
              ▀█▄▄▄▄▄▄▄▄▄▄▄█▀`

// RenderSkull returns the skull art with the Athanor palette applied.
// Eye sockets (the `@` placeholders) are swapped to ☉ in gold, with a phosphor
// glow underneath: a deliberate cyber pinprick on a medieval frame.
func RenderSkull(theme Theme) string {
	body := lipgloss.NewStyle().Foreground(theme.Ink).Render(skullArt)
	body = strings.ReplaceAll(
		body,
		"@",
		lipgloss.NewStyle().Foreground(theme.Phosphor).Bold(true).Render("☉"),
	)
	return body
}
