// Package theme defines the color/style vocabulary the whole TUI is built
// on. A Theme is just data (semantic color tokens); Styles is the compiled
// set of lipgloss.Style values derived from a Theme. Nothing else in the
// app should construct a lipgloss.Style with a raw color — everything goes
// through here so that swapping a Theme repaints the entire UI.
//
// Themes are config, not code: the built-ins live as JSON files under
// defaults/, embedded into the binary (see load.go's Load), and a user's
// own themes are discovered from a "themes" directory in appdir at
// runtime — same shape, just read from disk instead of go:embed.
package theme

import "github.com/charmbracelet/lipgloss"

// Theme is a palette of semantic color tokens. Keep it to roles ("what is
// this color for"), not specific widgets ("what color is the header") —
// that mapping lives in Styles. json tags are what both the embedded
// defaults and a user's custom theme files are parsed against — see
// load.go.
type Theme struct {
	Name string `json:"name"`

	// Surfaces
	Background lipgloss.Color `json:"background"`
	Surface    lipgloss.Color `json:"surface"`   // slightly raised panels (input bar, palette)
	Highlight  lipgloss.Color `json:"highlight"` // backdrop tint for callout blocks (e.g. a highlighted message)

	// Borders
	Border      lipgloss.Color `json:"border"`
	BorderFocus lipgloss.Color `json:"borderFocus"`

	// Text
	Text       lipgloss.Color `json:"text"`
	TextMuted  lipgloss.Color `json:"textMuted"`
	TextFaint  lipgloss.Color `json:"textFaint"`
	TextOnFill lipgloss.Color `json:"textOnFill"` // text drawn on top of an Accent-filled background

	// Brand / accent
	Accent      lipgloss.Color `json:"accent"`
	AccentMuted lipgloss.Color `json:"accentMuted"`
	// Reasoning is the model's "thinking" badge (see Styles.ReasoningBadge)
	// — split out from Accent so a theme can give that indicator its own
	// hue instead of it always matching every other branded/interactive
	// element in the app.
	Reasoning lipgloss.Color `json:"reasoning"`

	// Status
	Success lipgloss.Color `json:"success"`
	Warning lipgloss.Color `json:"warning"`
	Error   lipgloss.Color `json:"error"`

	// Attention marks tool activity and an active toggle (see
	// Styles.ToolCallName/ToolConfirmPending/HelpBadge) — "something
	// notable or in progress," not literally a warning, so it's its own
	// token rather than continuing to borrow Warning for a meaning
	// Warning doesn't actually have. Kept as one token rather than a
	// separate one per widget so all of that chrome moves together —
	// tool badges and active-toggle badges were always meant to read as
	// the same family of indicator.
	Attention lipgloss.Color `json:"attention"`
}
