package ui

import (
	"fmt"
	"strings"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/lipgloss"
)

// bootMaxWidth caps the banner's width on a roomy terminal — it's meant
// to read like a compact splash card, not stretch edge to edge.
const bootMaxWidth = 60

// BootInfo backs the boot banner. Seeded at NewApp time, then kept in
// sync afterward — Model/Specialists on every successful backend
// reconnect (see the keySetMsg handler in Update), Theme on every theme
// change (see applyTheme) — so the banner reflects current reality
// rather than being a frozen record of launch conditions; it just still
// only appears once, at the top of the transcript, rather than being a
// live status widget in its own right (that's what the top bar is for).
// There's no Agent field: with agent-as-tool as the only delegation
// pattern, there's exactly one voice in every conversation, so naming it
// here would just be trivia, not information — see /agents for the
// configured name if it's ever needed.
type BootInfo struct {
	Model       string // "" if unknown
	Theme       string
	Specialists []string // sub-agents discovered at startup; nil/empty if none
}

// renderBootArt draws the boot banner: a bordered panel with a title,
// a one-line project blurb, and a small info table. Every line inside is
// forced to the same content width so the panel's background fills edge
// to edge — see the popup title saga elsewhere in this package for why
// that matters.
func renderBootArt(s theme.Styles, info BootInfo, width int) string {
	// Capped at bootMaxWidth on a roomy terminal, but never floored above
	// what's actually available — a floor here would make the box wider
	// than the terminal instead of protecting against a narrow one.
	boxWidth := max(min(width-4, bootMaxWidth), 1)
	contentWidth := max(boxWidth-6, 0) // minus BootBorder's Padding(1,2) + Border(2)

	title := s.BootTitle.Width(contentWidth).Render("◆ agent-platform")
	tagline := s.BootTagline.Width(contentWidth).
		Render("A terminal shell for talking to AI agents — built with Bubble Tea, Lip Gloss, and Google's ADK.")
	rule := s.BootRule.Width(contentWidth).Render(strings.Repeat("─", contentWidth))

	rows := []string{
		bootRow(s, "model", s.BootValue, orPlaceholder(info.Model, "unknown"), contentWidth),
		bootRow(s, "theme", s.BootValue, orPlaceholder(info.Theme, "unknown"), contentWidth),
		bootSpecialistsRow(s, info.Specialists, contentWidth),
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "", tagline, "", rule, "", lipgloss.JoinVertical(lipgloss.Left, rows...))

	box := s.BootBorder.Render(content)
	// Centered rather than flush left — it's meant to read as a one-time
	// splash card, visually distinct from the left-aligned chat log below
	// it once the conversation gets going. WithWhitespaceBackground so the
	// margin PlaceHorizontal adds on either side carries the app's
	// background instead of the terminal's raw default — this content
	// already sits at its full target width by the time it reaches
	// App.viewport, so viewport.Style's own per-line repaint (see
	// App.layout/applyTheme) never gets a chance to add that padding
	// itself.
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, box, lipgloss.WithWhitespaceBackground(s.Theme.Background))
}

// bootRow renders one "label   value" line, the label at its natural
// width and the value stretched to fill the rest — same technique used
// for the /command suggestion rows.
func bootRow(s theme.Styles, label string, valueStyle lipgloss.Style, value string, width int) string {
	left := s.BootLabel.Render(fmt.Sprintf("%-13s", label))
	right := valueStyle.Width(max(width-lipgloss.Width(left), 0)).Render(value)
	return left + right
}

func bootSpecialistsRow(s theme.Styles, names []string, width int) string {
	value := "none — see ~/.tui-testing/subagents"
	style := s.BootValue
	if len(names) > 0 {
		value = strings.Join(names, ", ")
	}
	return bootRow(s, "specialists", style, value, width)
}

func orPlaceholder(s, placeholder string) string {
	if s == "" {
		return placeholder
	}
	return s
}
