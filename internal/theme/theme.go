// Package theme defines the color/style vocabulary the whole TUI is built
// on. A Theme is just data (semantic color tokens); Styles is the compiled
// set of lipgloss.Style values derived from a Theme. Nothing else in the
// app should construct a lipgloss.Style with a raw color — everything goes
// through here so that swapping a Theme repaints the entire UI.
package theme

import "github.com/charmbracelet/lipgloss"

// Theme is a palette of semantic color tokens. Keep it to roles ("what is
// this color for"), not specific widgets ("what color is the header") —
// that mapping lives in Styles.
type Theme struct {
	Name string

	// Surfaces
	Background lipgloss.Color
	Surface    lipgloss.Color // slightly raised panels (input bar, palette)
	Highlight  lipgloss.Color // backdrop tint for callout blocks (e.g. a highlighted message)

	// Borders
	Border      lipgloss.Color
	BorderFocus lipgloss.Color

	// Text
	Text       lipgloss.Color
	TextMuted  lipgloss.Color
	TextFaint  lipgloss.Color
	TextOnFill lipgloss.Color // text drawn on top of an Accent-filled background

	// Brand / accent
	Accent      lipgloss.Color
	AccentMuted lipgloss.Color

	// Status
	Success lipgloss.Color
	Warning lipgloss.Color
	Error   lipgloss.Color
}
