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
		s.HeaderSession.Render(shortSessionID(sessionID)),
		s.HeaderTitle.Render(" · "),
		s.HeaderAgent.Render(agent),
		s.HeaderTitle.Render(" · "),
		s.HeaderStatus(status).Render(statusLabel(status)),
	)

	line := s.Header.Width(width - 2).Render(meta)
	rule := s.HeaderRule.Render(strings.Repeat("─", width))
	return line + "\n" + rule
}

// shortSessionID truncates a full UUID down to a compact display form —
// "sess_" plus its first 8 hex characters, matching the old hardcoded
// placeholder's width/style ("sess_7f3d2a19") — since the small header
// badge has no room for a full 36-character UUID. a.sessionID itself
// stays the full UUID everywhere it's actually used (backend calls,
// session storage); this is purely cosmetic.
func shortSessionID(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return "sess_" + id
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
