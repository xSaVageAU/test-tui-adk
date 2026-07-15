package ui

import (
	"context"
	"strconv"
	"strings"

	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// textPopupKind distinguishes what paletteTextInput's Enter should do —
// one popup (a.keyInput), shared by /key's masked API-key field and
// /agents' unmasked model field, rather than two near-identical
// components. See openKeyInput/openAgentModelInput for how each is
// configured, and handleTextInputKey for how Enter routes on this.
type textPopupKind int

const (
	textPopupNone textPopupKind = iota
	textPopupAPIKey
	textPopupAgentModel
	textPopupPopupWidth  // /settings' "Popup width" field — see openPopupSizeInput/submitPopupSize
	textPopupPopupHeight // /settings' "Popup height" field
)

// providerGemini/providerOpenRouter mirror adk.ProviderGemini/
// adk.ProviderOpenRouter's exact string values — duplicated rather than
// imported since this package never imports internal/adk (see
// backend.go's doc comment on AgentConfigSummary); every provider value
// this package handles ultimately came from or goes to that package
// through the plain-string AppConfig closures, so drift would only ever
// show up as "provider not recognized" here, never a compile error.
const (
	providerGemini     = "gemini"
	providerOpenRouter = "openrouter"
)

// textPopupWidth is the popup's target outer width — same configured (or
// default) size every other popup uses (see effectivePopupWidth in
// app.go), clamped to the terminal so it doesn't overflow a narrow one.
func (a *App) textPopupWidth() int { return min(a.width-8, a.effectivePopupWidth()) }

// openKeyProviderMenu is /key's first step: which provider is this key
// for. Selecting one opens the masked text field (openKeyInput) scoped
// to that provider.
func (a *App) openKeyProviderMenu() {
	if a.newBackend == nil {
		a.systemMessage("Can't set a key: no backend factory configured.")
		return
	}
	items := []paletteItem{
		{id: providerGemini, title: "Gemini", desc: "Google's Gemini API"},
		{id: providerOpenRouter, title: "OpenRouter", desc: "OpenAI-compatible, many models"},
	}
	a.openMenu(paletteKeyProvider, "Set API key — choose provider", items)
}

// openKeyInput shows the masked API-key popup for provider, submitted to
// newBackend to build a fresh Backend without restarting the app.
func (a *App) openKeyInput(provider string) {
	ti := textinput.New()
	ti.Placeholder = keyPlaceholderFor(provider)
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	ti.SetWidth(a.textPopupWidth() - 4)
	styles := ti.Styles()
	styles.Focused.Prompt = a.styles.InputPrompt
	styles.Focused.Placeholder = a.styles.InputHint
	styles.Blurred.Prompt = a.styles.InputPrompt
	styles.Blurred.Placeholder = a.styles.InputHint
	ti.SetStyles(styles)
	ti.Focus()

	a.keyInput = ti
	a.textPopupKind = textPopupAPIKey
	a.textPopupProvider = provider
	a.textPopupLabel = "Set " + providerDisplayName(provider) + " API key"
	a.paletteKind = paletteTextInput
}

// openPopupSizeInput shows the numeric popup field for /settings' "Popup
// width"/"Popup height" rows — same text-field popup as /key and /agents'
// model field, just prefilled with current (the effective size in effect
// right now) instead of an empty/masked value. See submitPopupSize for
// where the typed value is parsed, clamped, and persisted.
func (a *App) openPopupSizeInput(kind textPopupKind, label string, current int) {
	ti := textinput.New()
	ti.CharLimit = 4
	ti.SetWidth(a.textPopupWidth() - 4)
	styles := ti.Styles()
	styles.Focused.Prompt = a.styles.InputPrompt
	styles.Focused.Placeholder = a.styles.InputHint
	styles.Blurred.Prompt = a.styles.InputPrompt
	styles.Blurred.Placeholder = a.styles.InputHint
	ti.SetStyles(styles)
	ti.SetValue(strconv.Itoa(current))
	ti.CursorEnd()
	ti.Focus()

	a.keyInput = ti
	a.textPopupKind = kind
	a.textPopupLabel = label
	a.paletteKind = paletteTextInput
}

func keyPlaceholderFor(provider string) string {
	if provider == providerOpenRouter {
		return "sk-or-..."
	}
	return "GOOGLE_API_KEY"
}

func providerDisplayName(provider string) string {
	if provider == providerOpenRouter {
		return "OpenRouter"
	}
	return "Gemini"
}

// handleTextInputKey runs while paletteTextInput has focus: Esc cancels,
// Enter submits (routed by textPopupKind to whichever field this popup
// is currently backing), everything else edits the field.
func (a *App) handleTextInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		return a, a.backOrClose(a.cancelMenu)

	case key.Matches(msg, keys.Send):
		switch a.textPopupKind {
		case textPopupAgentModel:
			return a, a.submitAgentModel()
		case textPopupPopupWidth, textPopupPopupHeight:
			return a, a.submitPopupSize()
		default:
			return a, a.submitKey()
		}
	}

	var cmd tea.Cmd
	a.keyInput, cmd = a.keyInput.Update(msg)
	return a, cmd
}

// submitKey closes the popup and, if a non-empty key was entered, asks
// newBackend to build a Backend from it for whichever provider the
// popup was opened for. Building a client isn't guaranteed to be
// network-free (or fast), so this runs as a tea.Cmd rather than
// blocking Update.
func (a *App) submitKey() tea.Cmd {
	apiKey := strings.TrimSpace(a.keyInput.Value())
	provider := a.textPopupProvider
	backCmd := a.backOrClose(a.closeMenuCmd)

	if apiKey == "" {
		return backCmd
	}
	if a.newBackend == nil {
		a.systemMessage("Can't set a key: no backend factory configured.")
		return backCmd
	}

	factory := a.newBackend
	label := providerDisplayName(provider) + " API key set."
	return tea.Batch(backCmd, func() tea.Msg {
		backend, err := factory(context.Background(), provider, apiKey)
		return keySetMsg{backend: backend, err: err, successMsg: label, failPrefix: "Could not connect with that key"}
	})
}

// keySetMsg carries the result of an async backend rebuild — submitKey's
// (a fresh key just typed into /key) or reloadBackend's (/agents,
// reconnecting after a config edit with no new key of its own).
type keySetMsg struct {
	backend Backend
	err     error
	// successMsg/failPrefix let one message type serve both callers with
	// a message that actually names what was being attempted — "" falls
	// back to a generic wording.
	successMsg string
	failPrefix string
}

// renderTextInputOverlay draws paletteTextInput the same way the
// list-backed popups do — centered over the already-rendered app frame
// — just with a single text field instead of a list. Shared by /key's
// masked field and /agents' unmasked model field; title comes from
// a.textPopupLabel, set when each is opened.
func renderTextInputOverlay(bg string, s theme.Styles, title string, ti textinput.Model, width, height int) string {
	// ti.Width is the field's own committed width (set in openKeyInput/
	// openAgentModelInput, kept in sync on resize by App.layout) —
	// reading it back here rather than recomputing keeps a single source
	// of truth for the popup's outer width instead of two formulas that
	// could drift apart.
	boxWidth := ti.Width() + 4

	titleRow := renderPaletteTitle(s, title, boxWidth)
	field := s.InputBarFocused.Width(boxWidth - 2).Render(ti.View())
	// Width() here isn't optional: any line shorter than its siblings
	// gets auto-padded by PaletteBorder's own border-drawing step below,
	// and that padding has no background of its own — see the whole
	// preceding saga about the popup title for why that shows up as a
	// stray black patch.
	hint := s.PaletteDesc.Width(boxWidth).Render("enter save · esc cancel")

	content := titleRow + "\n\n" + field + "\n\n" + hint
	box := s.PaletteBorder.Render(content)

	x := max((width-lipgloss.Width(box))/2, 0)
	y := max((height-lipgloss.Height(box))/2, 0)
	return overlay(bg, box, x, y, width)
}
