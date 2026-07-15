package ui

import (
	"strings"

	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/key"
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
//
// The gap between badges is rendered through HeaderTitle (a background
// on the app's own color, same reasoning as header.go's joinLeftRight),
// not a bare " " literal — each badge is already fully rendered with
// its own reset, so an unstyled separator between them would show the
// terminal's raw default rather than the theme's background. The whole
// line is then padded out to width for the same reason: past the last
// badge, there's nothing else claiming that background either.
func renderHelpFooter(s theme.Styles, autoAccept, verboseTools bool, width int) string {
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
	gap := s.HeaderTitle.Render(" ")
	return s.HeaderTitle.Width(width).Render(strings.Join(badges, gap))
}
