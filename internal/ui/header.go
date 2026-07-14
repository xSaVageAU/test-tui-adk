package ui

import (
	"strings"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/lipgloss"
)

// renderTopBar draws the fixed two-line panel pinned to the top of the
// screen: a plain meta line (session id, active agent — in that
// left-to-right order, plus an "auto-accept" badge when that permission
// mode is active) followed by a solid horizontal rule separating it from
// the chat below. No filled background panel and no platform branding —
// just enough to orient you.
//
// There used to be an "idle"/"thinking…" status word here too — removed
// at the user's request (didn't like it, wants something else in its
// place eventually). App.status/theme.StatusKind/Styles.HeaderStatus are
// deliberately still tracked and left alone, not ripped out — they're
// exactly the hook whatever replaces this will want to read. (A
// "reasoning" badge was tried here first, but it belonged next to the
// transcript's per-message "agent" label instead — see chat.go's
// renderMessage — not here.)
func renderTopBar(s theme.Styles, width int, agent, sessionID string, autoAccept bool) string {
	parts := []string{
		s.HeaderSession.Render(shortSessionID(sessionID)),
		s.HeaderTitle.Render(" · "),
		s.HeaderAgent.Render(agent),
	}
	// Only shown while active — the common (normal-mode) case stays as
	// uncluttered as it always was, and this is meant to be a "notice
	// something's different" flag, not a permanent fixture.
	if autoAccept {
		parts = append(parts, s.HeaderTitle.Render(" · "), s.HeaderAutoBadge.Render("AUTO-ACCEPT"))
	}
	meta := lipgloss.JoinHorizontal(lipgloss.Left, parts...)

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
