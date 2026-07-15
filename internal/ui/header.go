package ui

import (
	"fmt"
	"strings"

	"tui-testing/internal/theme"

	"charm.land/lipgloss/v2"
)

// renderTopBar draws the fixed two-line panel pinned to the top of the
// screen: the session id on the left, a context-window usage bar (see
// renderContextBar) on the right, followed by a solid horizontal rule
// separating it from the chat below. No filled background panel and no
// platform branding — just enough to orient you.
//
// The active agent's name and the "auto-accept" badge used to live here
// too — removed at the user's request now that auto-accept has its own
// help-footer badge (see footer.go's renderHelpFooter) that already
// flips color while the mode is active, making a second indicator here
// redundant. The agent's name isn't shown anywhere else in the running
// UI now (with agent-as-tool as the only delegation pattern there's
// exactly one voice in every conversation regardless) — see /agents for
// the configured name if it's ever needed.
//
// There used to be an "idle"/"thinking…" status word here too — removed
// at the user's request (didn't like it, wants something else in its
// place eventually). App.status/theme.StatusKind/Styles.HeaderStatus are
// deliberately still tracked and left alone, not ripped out — they're
// exactly the hook whatever replaces this will want to read. (A
// "reasoning" badge was tried here first, but it belonged next to the
// transcript's per-message "agent" label instead — see chat.go's
// renderMessage — not here.)
func renderTopBar(s theme.Styles, width int, sessionID string, contextUsed, contextWindow int) string {
	meta := s.HeaderSession.Render(shortSessionID(sessionID))

	// s.Header.Width(width) below renders at the terminal's full width —
	// matching the viewport below it, so there's no gap on the right
	// where the background stops short of the true edge — with its own
	// Padding(0,1) applied inside that, so the content area actually
	// available for meta+bar is 2 narrower than width, not width itself.
	contentWidth := max(width-2, 0)
	content := joinLeftRight(s, meta, renderContextBar(s, contextUsed, contextWindow), contentWidth)

	line := s.Header.Width(width).Render(content)
	rule := s.HeaderRule.Render(strings.Repeat("─", width))
	return line + "\n" + rule
}

// joinLeftRight places right at the far end of a width-wide line, left
// at the near end, with the gap between filled with background-colored
// spaces — or, if left alone already fills width (a narrow terminal),
// just left with right silently dropped rather than truncated into
// something unreadable. right == "" also just returns left unchanged.
//
// The gap is rendered through a style, not a bare string literal: left
// and right are each already fully rendered (with their own resets), so
// a raw, unstyled gap between them would show the terminal's default
// background rather than the theme's — wrapping the *whole* line in a
// background afterward doesn't fix that, since each side's own trailing
// reset code cuts the outer background off from reaching this gap. See
// theme.Styles.HeaderTitle's doc comment for the same issue in miniature.
func joinLeftRight(s theme.Styles, left, right string, width int) string {
	if right == "" {
		return left
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + s.HeaderTitle.Render(strings.Repeat(" ", gap)) + right
}

// contextBarWidth is the fixed number of filled/empty block characters
// in the context-usage bar — compact enough to sit comfortably in the
// top bar alongside the session/agent meta.
const contextBarWidth = 10

// renderContextBar draws "<used>/<window> <bar>" for the top bar's right
// side — the model's context window and how much of it the current
// conversation has used so far (App.contextUsed: the most recent model
// call's prompt token count, which — since a prompt is always the whole
// conversation sent up to that point — already is the running total,
// not something to sum across calls; see App.accumulateUsage). Replaces
// the per-message "x in · x out · x tokens" line this app used to show
// under every agent reply — a single always-visible indicator of how
// close the conversation is to running out of room, instead of a number
// that only ever described one turn's cost.
//
// "" (render nothing) if window is 0 — resolveContextWindow couldn't
// determine it for this model (see internal/adk/contextwindow.go), and
// a bar with no known ceiling isn't useful half-drawn.
func renderContextBar(s theme.Styles, used, window int) string {
	if window <= 0 {
		return ""
	}
	frac := float64(used) / float64(window)
	if frac > 1 {
		frac = 1
	}
	filled := int(frac * contextBarWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", contextBarWidth-filled)

	label := fmt.Sprintf("%s/%s", humanCount(used), humanCount(window))
	return s.HeaderTitle.Render(label+" ") + s.HeaderContextBar(frac).Render(bar)
}

// humanCount renders a token count the way a person would read it aloud
// — 128000 -> "128k", 1000000 -> "1.0M" — same reasoning as chat.go's
// humanBytes, but base-1000 (a token count, not a byte size) and with no
// unit suffix, since the bar's own "/" already makes the shared unit
// obvious.
func humanCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
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
