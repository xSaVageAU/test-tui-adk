package ui

import (
	"fmt"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/lipgloss"
)

// bootMaxWidth caps the banner's width on a roomy terminal — it's meant
// to read like a compact splash card, not stretch edge to edge.
const bootMaxWidth = 60

// BootInfo is the snapshot of startup conditions shown once in the boot
// banner. Frozen at NewApp time and never updated afterward — this is a
// splash screen, a record of what things looked like at launch, not a
// live status widget (that's what the top bar is for).
type BootInfo struct {
	Agent     string
	Model     string // "" if unknown
	Connected bool
}

// renderBootArt draws the boot banner: a bordered panel with a title,
// a one-line project blurb, and a small agent/model/status table. Every
// line inside is forced to the same content width so the panel's
// background fills edge to edge — see the popup title saga elsewhere in
// this package for why that matters.
func renderBootArt(s theme.Styles, info BootInfo, width int) string {
	// Capped at bootMaxWidth on a roomy terminal, but never floored above
	// what's actually available — a floor here would make the box wider
	// than the terminal instead of protecting against a narrow one.
	boxWidth := max(min(width-4, bootMaxWidth), 1)
	contentWidth := max(boxWidth-6, 0) // minus BootBorder's Padding(1,2) + Border(2)

	title := s.BootTitle.Width(contentWidth).Render("◆ agent-platform")
	tagline := s.BootTagline.Width(contentWidth).
		Render("A terminal shell for talking to AI agents — built with Bubble Tea, Lip Gloss, and Google's ADK.")

	rows := []string{
		bootRow(s, "agent", s.BootValue, info.Agent, contentWidth),
		bootRow(s, "model", s.BootValue, orPlaceholder(info.Model, "unknown"), contentWidth),
		bootStatusRow(s, info.Connected, contentWidth),
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, "", tagline, "", lipgloss.JoinVertical(lipgloss.Left, rows...))

	box := s.BootBorder.Render(content)
	// Centered rather than flush left — it's meant to read as a one-time
	// splash card, visually distinct from the left-aligned chat log below
	// it once the conversation gets going.
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, box)
}

// bootRow renders one "label   value" line, the label at its natural
// width and the value stretched to fill the rest — same technique used
// for the /command suggestion rows.
func bootRow(s theme.Styles, label string, valueStyle lipgloss.Style, value string, width int) string {
	left := s.BootLabel.Render(fmt.Sprintf("%-8s", label))
	right := valueStyle.Width(max(width-lipgloss.Width(left), 0)).Render(value)
	return left + right
}

func bootStatusRow(s theme.Styles, connected bool, width int) string {
	style, text := s.BootValueErr, "not connected — try /key"
	if connected {
		style, text = s.BootValueOK, "connected"
	}
	return bootRow(s, "status", style, text, width)
}

func orPlaceholder(s, placeholder string) string {
	if s == "" {
		return placeholder
	}
	return s
}
