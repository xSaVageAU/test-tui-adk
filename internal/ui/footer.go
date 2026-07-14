package ui

import (
	"strings"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/bubbles/key"
)

// renderHelpFooter draws the bottom help line as one dim badge per
// keybind — each badge groups a key with the action it performs, rather
// than the plain "key desc  key desc  ..." row bubbles/help draws by
// default, which read as one continuous stream of text rather than a
// set of distinct actions. Within a badge, the key itself renders a
// shade darker than its description (see theme.Styles.HelpBadge) so the
// two halves — which key, versus what it does — stay visually distinct
// rather than blurring together. autoAccept/verboseTools light up their
// own badge while that setting is currently on, so the footer doubles
// as a quiet indicator of which toggles are active — not just a legend
// of what the keys do.
func renderHelpFooter(s theme.Styles, autoAccept, verboseTools bool) string {
	groups := []struct {
		b      key.Binding
		active bool
	}{
		{keys.Send, false},
		{keys.Commands, false},
		{keys.ScrollUp, false},
		{keys.AutoAccept, autoAccept},
		{keys.VerboseTools, verboseTools},
		{keys.Quit, false},
	}

	badges := make([]string, len(groups))
	for i, g := range groups {
		h := g.b.Help()
		badge := s.HelpBadge(g.active)
		badges[i] = badge.Key.Render(h.Key) + badge.Desc.Render(" "+h.Desc)
	}
	return strings.Join(badges, " ")
}
