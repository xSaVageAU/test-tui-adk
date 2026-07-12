package ui

import (
	"strings"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/lipgloss"
)

// renderTopBar draws the fixed two-line panel pinned to the top of the
// screen: a plain meta line (session id, active agent, status — in that
// left-to-right order) followed by a solid horizontal rule separating it
// from the chat below. No filled background panel and no platform
// branding — just enough to orient you.
func renderTopBar(s theme.Styles, width int, agent string, status theme.StatusKind, sessionID string) string {
	meta := lipgloss.JoinHorizontal(lipgloss.Left,
		s.HeaderSession.Render(sessionID),
		s.HeaderTitle.Render(" · "),
		s.HeaderAgent.Render(agent),
		s.HeaderTitle.Render(" · "),
		s.HeaderStatus(status).Render(statusLabel(status)),
	)

	line := s.Header.Width(width - 2).Render(meta)
	rule := s.HeaderRule.Render(strings.Repeat("─", width))
	return line + "\n" + rule
}

func statusLabel(status theme.StatusKind) string {
	switch status {
	case theme.StatusThinking:
		return "thinking…"
	case theme.StatusError:
		return "error"
	default:
		return "idle"
	}
}
