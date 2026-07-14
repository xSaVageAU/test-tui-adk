package ui

import (
	"strings"
	"testing"

	"tui-testing/internal/theme"
)

// TestHelpBadgeActiveDiffersFromDefault is the actual point of this
// feature: a toggle's badge (AutoAccept, VerboseTools) needs to look
// different once it's on. Compared at the Style level (GetBackground/
// GetForeground) rather than by rendering to a string and diffing ANSI
// output — go test's stdout isn't a TTY, so lipgloss's color profile
// detection strips all styling from rendered output regardless of what
// colors were set, which would make this pass even if HelpBadge(true)
// and HelpBadge(false) were identical.
func TestHelpBadgeActiveDiffersFromDefault(t *testing.T) {
	s := theme.New(theme.Load()[0])

	off, on := s.HelpBadge(false), s.HelpBadge(true)
	if off.Key.GetBackground() == on.Key.GetBackground() {
		t.Error("HelpBadge(true) should share a differently-colored background from HelpBadge(false)")
	}
}

// TestHelpBadgeKeyDiffersFromDesc is the "make the bind text a bit
// darker" ask: within the default (inactive) badge, Key (the literal
// key combo) should have its own, darker foreground against Desc's — a
// light touch, not a separate background per half (tried, looked worse
// per direct feedback) or a Faint() attribute stacked on top (also
// tried; made Key harder to read rather than just quieter). Both halves
// always share one background regardless of toggle state.
func TestHelpBadgeKeyDiffersFromDesc(t *testing.T) {
	s := theme.New(theme.Load()[0])
	for _, active := range []bool{false, true} {
		badge := s.HelpBadge(active)
		if badge.Key.GetBackground() != badge.Desc.GetBackground() {
			t.Errorf("active=%v: Key and Desc should share one badge background", active)
		}
	}

	off := s.HelpBadge(false)
	if off.Key.GetForeground() == off.Desc.GetForeground() {
		t.Error("default badge: Key and Desc have the same foreground, want Key a shade darker")
	}
}

// TestRenderHelpFooterAlwaysShowsEveryBinding guards against a toggle
// state accidentally hiding a badge rather than just recoloring it —
// every key's description should be present regardless of
// autoAccept/verboseTools.
func TestRenderHelpFooterAlwaysShowsEveryBinding(t *testing.T) {
	s := theme.New(theme.Load()[0])
	for _, autoAccept := range []bool{false, true} {
		for _, verboseTools := range []bool{false, true} {
			got := renderHelpFooter(s, autoAccept, verboseTools, 200)
			if !strings.Contains(got, "auto-accept") || !strings.Contains(got, "verbose tools") || !strings.Contains(got, "quit") {
				t.Errorf("autoAccept=%v verboseTools=%v: footer missing a binding: %q", autoAccept, verboseTools, got)
			}
		}
	}
}

func TestRenderHelpFooterContainsAllBindings(t *testing.T) {
	s := theme.New(theme.Load()[0])
	got := renderHelpFooter(s, false, false, 200)

	for _, want := range []string{"enter", "send", "/", "commands", "pgup/pgdn", "shift+tab", "auto-accept", "f2", "verbose tools", "ctrl+c", "quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("footer missing %q; got %q", want, got)
		}
	}
}
