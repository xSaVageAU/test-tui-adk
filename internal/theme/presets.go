package theme

import "github.com/charmbracelet/lipgloss"

// Mono is the default theme: near-grayscale with a single cyan accent.
// Low chrome, high density — meant to be the "just get out of the way"
// baseline every other theme is judged against.
var Mono = Theme{
	Name: "Mono",

	Background: lipgloss.Color("#0d0d0d"),
	Surface:    lipgloss.Color("#1a1a1a"),
	Highlight:  lipgloss.Color("#1c2b2b"),

	Border:      lipgloss.Color("#3a3a3a"),
	BorderFocus: lipgloss.Color("#6fd1d1"),

	Text:       lipgloss.Color("#e6e6e6"),
	TextMuted:  lipgloss.Color("#a0a0a0"),
	TextFaint:  lipgloss.Color("#666666"),
	TextOnFill: lipgloss.Color("#0d0d0d"),

	Accent:      lipgloss.Color("#6fd1d1"),
	AccentMuted: lipgloss.Color("#3f7d7d"),

	Success: lipgloss.Color("#8fd694"),
	Warning: lipgloss.Color("#e6c07b"),
	Error:   lipgloss.Color("#e06c75"),
}

// Vibrant leans into the colorful, high-personality look of Charm's own
// tools (Glow, Crush, Soft Serve) — bold magenta/purple accents on dark.
var Vibrant = Theme{
	Name: "Vibrant",

	Background: lipgloss.Color("#0f0a1e"),
	Surface:    lipgloss.Color("#1f1533"),
	Highlight:  lipgloss.Color("#2a1b45"),

	Border:      lipgloss.Color("#4a3a6a"),
	BorderFocus: lipgloss.Color("#ff6ac1"),

	Text:       lipgloss.Color("#f2eaff"),
	TextMuted:  lipgloss.Color("#b9a8db"),
	TextFaint:  lipgloss.Color("#75619e"),
	TextOnFill: lipgloss.Color("#0f0a1e"),

	Accent:      lipgloss.Color("#ff6ac1"),
	AccentMuted: lipgloss.Color("#a34d82"),

	Success: lipgloss.Color("#5df4a3"),
	Warning: lipgloss.Color("#ffd166"),
	Error:   lipgloss.Color("#ff5c7c"),
}

// Nord is a muted, low-contrast pastel palette in the vein of Nord /
// Catppuccin — easier on the eyes for long sessions.
var Nord = Theme{
	Name: "Nord",

	Background: lipgloss.Color("#2e3440"),
	Surface:    lipgloss.Color("#3b4252"),
	Highlight:  lipgloss.Color("#434c5e"),

	Border:      lipgloss.Color("#4c566a"),
	BorderFocus: lipgloss.Color("#88c0d0"),

	Text:       lipgloss.Color("#e5e9f0"),
	TextMuted:  lipgloss.Color("#b8c2d6"),
	TextFaint:  lipgloss.Color("#7b869a"),
	TextOnFill: lipgloss.Color("#2e3440"),

	Accent:      lipgloss.Color("#88c0d0"),
	AccentMuted: lipgloss.Color("#5e81ac"),

	Success: lipgloss.Color("#a3be8c"),
	Warning: lipgloss.Color("#ebcb8b"),
	Error:   lipgloss.Color("#bf616a"),
}

// Defaults lists the built-in themes in cycle order. Mono is first so it's
// the default on startup.
func Defaults() []Theme {
	return []Theme{Mono, Vibrant, Nord}
}
