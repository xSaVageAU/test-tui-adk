package ui

import (
	"context"
	"strings"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// keyInputMaxWidth caps how wide the /key popup grows on a roomy
// terminal — plenty of headroom for a full API key at a glance without
// it stretching edge-to-edge on a wide window.
const keyInputMaxWidth = 64

// keyInputWidth is the popup's target outer width: roomy but clamped to
// the terminal so it doesn't overflow a narrow one.
func (a *App) keyInputWidth() int { return min(a.width-8, keyInputMaxWidth) }

// openKeyInput shows the /key popup: a single masked field for pasting an
// API key, submitted to newBackend to build a fresh Backend without
// restarting the app.
func (a *App) openKeyInput() {
	ti := textinput.New()
	ti.Placeholder = "GOOGLE_API_KEY"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	ti.Width = a.keyInputWidth() - 4
	ti.PromptStyle = a.styles.InputPrompt
	ti.PlaceholderStyle = a.styles.InputHint
	ti.Focus()

	a.keyInput = ti
	a.paletteKind = paletteKeyInput
}

// handleKeyInputKey runs while the /key popup has focus: Esc cancels
// without changing the backend, Enter submits, everything else edits the
// masked field.
func (a *App) handleKeyInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		a.closeMenu()
		return a, nil

	case key.Matches(msg, keys.Send):
		return a, a.submitKey()
	}

	var cmd tea.Cmd
	a.keyInput, cmd = a.keyInput.Update(msg)
	return a, cmd
}

// submitKey closes the popup and, if a non-empty key was entered, asks
// newBackend to build a Backend from it. Building a client isn't
// guaranteed to be network-free (or fast), so this runs as a tea.Cmd
// rather than blocking Update.
func (a *App) submitKey() tea.Cmd {
	apiKey := strings.TrimSpace(a.keyInput.Value())
	a.closeMenu()

	if apiKey == "" {
		return nil
	}
	if a.newBackend == nil {
		a.systemMessage("Can't set a key: no backend factory configured.")
		return nil
	}

	factory := a.newBackend
	return func() tea.Msg {
		backend, err := factory(context.Background(), apiKey)
		return keySetMsg{backend: backend, err: err}
	}
}

// keySetMsg carries the result of submitKey's async backend construction.
type keySetMsg struct {
	backend Backend
	err     error
}

// renderKeyInputOverlay draws the /key popup the same way the list-backed
// popups do — centered over the already-rendered app frame — just with a
// masked text field instead of a list.
func renderKeyInputOverlay(bg string, s theme.Styles, ti textinput.Model, width, height int) string {
	// ti.Width is the field's own committed width (set in openKeyInput,
	// kept in sync on resize by App.layout) — reading it back here rather
	// than recomputing keeps a single source of truth for the popup's
	// outer width instead of two formulas that could drift apart.
	boxWidth := ti.Width + 4

	title := renderPaletteTitle(s, "Set API key", boxWidth)
	field := s.InputBarFocused.Width(boxWidth - 2).Render(ti.View())
	// Width() here isn't optional: any line shorter than its siblings
	// gets auto-padded by PaletteBorder's own border-drawing step below,
	// and that padding has no background of its own — see the whole
	// preceding saga about the popup title for why that shows up as a
	// stray black patch.
	hint := s.PaletteDesc.Width(boxWidth).Render("enter save · esc cancel")

	content := title + "\n\n" + field + "\n\n" + hint
	box := s.PaletteBorder.Render(content)

	x := max((width-lipgloss.Width(box))/2, 0)
	y := max((height-lipgloss.Height(box))/2, 0)
	return overlay(bg, box, x, y, width)
}
