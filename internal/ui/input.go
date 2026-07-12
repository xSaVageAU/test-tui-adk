package ui

import (
	"strings"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
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
func applyInputStyles(ta *textarea.Model, s theme.Styles) {
	style := textarea.Style{
		Base:        lipgloss.NewStyle(),
		CursorLine:  lipgloss.NewStyle(), // no line-highlight band; this isn't a code editor
		Placeholder: s.InputHint,
		Prompt:      s.InputPrompt,
		Text:        s.MessageContent,
	}
	ta.FocusedStyle = style
	ta.BlurredStyle = style
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

	return box.Width(width - 2).Render(strings.Join(rows, "\n"))
}
