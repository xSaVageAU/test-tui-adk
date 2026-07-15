package ui

import (
	"strings"

	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

// The input box starts at one line tall and grows as wrapped text needs
// more room, up to maxInputLines.
const (
	minInputLines = 1
	maxInputLines = 4
)

// newInput builds the message textarea.Model, styled from the active
// theme. The textarea's own internal height is fixed at maxInputLines for
// its whole lifetime (see the comment on renderInputBar for why) — only
// its width is resized later, in App.layout.
func newInput(s theme.Styles) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message… (/ for commands)"
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0 // unbounded rows of content; maxInputLines caps what we *display*, not what's typeable
	ta.SetHeight(maxInputLines)
	applyInputStyles(&ta, s)
	return ta
}

// wrappedLines returns how many display rows the current (single-paragraph)
// input value wraps to at its current width, clamped to
// [minInputLines, maxInputLines]. Assumes the input never contains an
// explicit newline (Enter sends rather than inserting one), so the
// cursor's line-wrap height equals the whole value's wrap height.
func wrappedLines(ta textarea.Model) int {
	return min(max(ta.LineInfo().Height, minInputLines), maxInputLines)
}

// applyInputStyles repaints the textarea from the given theme. Called once
// at construction and again on every theme swap.
//
// Base/CursorLine/Text carry Background(Surface) directly — built here
// rather than through dedicated theme.Styles fields, matching
// InputBar/InputPrompt/InputHint's box, all Surface for the same
// reason: the textarea's own content area needs to visually read as
// part of the same raised panel as its border, not a gap of a
// different background sitting inside it. No line-highlight band on
// CursorLine; this isn't a code editor.
func applyInputStyles(ta *textarea.Model, s theme.Styles) {
	surface := lipgloss.NewStyle().Background(lipgloss.Color(s.Theme.Surface))
	state := textarea.StyleState{
		Base:        surface,
		CursorLine:  surface,
		Placeholder: s.InputHint,
		Prompt:      s.InputPrompt,
		Text:        surface.Foreground(lipgloss.Color(s.Theme.Text)),
	}
	ta.SetStyles(textarea.Styles{Focused: state, Blurred: state})
}

// renderInputBar wraps the textarea in the themed border box, focused or
// not depending on whether the palette currently has focus instead.
//
// The textarea's internal height never changes (see newInput) — resizing
// it live and asking it to rescroll itself (via SetHeight + a synthetic
// update) turned out to leave its internal viewport's scroll offset
// wrong after the first grow, permanently hiding the first line. Instead
// we always render at the fixed max height and crop to the top `lines`
// rows ourselves. Since content only ever grows past maxInputLines by
// scrolling internally (cursor-follow), the rows we keep are always the
// ones that should be visible.
func renderInputBar(s theme.Styles, ta textarea.Model, width, lines int, focused bool) string {
	box := s.InputBar
	if focused {
		box = s.InputBarFocused
	}

	rows := strings.Split(ta.View(), "\n")
	if lines < len(rows) {
		rows = rows[:lines]
	}

	// textarea.Model renders through its own internal viewport.Model — a
	// private field this package has no way to reach — which pads every
	// row out to its own configured width using a plain, colorless style
	// before ta.View() ever returns. Since that internal width already
	// matches this box's content area exactly, box.Width(width) below
	// finds no shortfall of its own left to (correctly) fill — the
	// padding's already there, just uncolored. TrimRight strips that raw
	// trailing padding back off (safe: real ANSI escape codes never
	// contain a literal space, so this can only ever remove bare
	// padding, or a run of genuinely-typed trailing spaces — which look
	// identical either way once repainted) so box's own Width() call has
	// a real shortfall again, and regenerates it correctly colored — the
	// same fix as chat.go's padLinesBackground, for the same underlying
	// reason.
	for i, row := range rows {
		rows[i] = strings.TrimRight(row, " ")
	}

	// Full width, not width-2: box's own border (2) + padding (2) already
	// account for the 4-column difference from a.input.SetWidth(a.width-4)
	// — rendering at width-2 here left a 2-column gap of raw terminal
	// background on the right, short of the true edge.
	return box.Width(width).Render(strings.Join(rows, "\n"))
}
