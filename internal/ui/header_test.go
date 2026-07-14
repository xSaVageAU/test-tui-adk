package ui

import (
	"strings"
	"testing"

	"tui-testing/internal/theme"
)

func TestHumanCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1k"},
		{128000, "128k"},
		{999_999, "999k"},
		{1_000_000, "1.0M"},
		{2_500_000, "2.5M"},
	}
	for _, c := range cases {
		if got := humanCount(c.n); got != c.want {
			t.Errorf("humanCount(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// TestRenderContextBar covers the one branch build+vet can't catch: a
// model resolveContextWindow couldn't identify (window == 0) must render
// nothing at all rather than a bar with a nonsensical/divide-by-zero
// ceiling.
func TestRenderContextBar(t *testing.T) {
	s := theme.New(theme.Load()[0])

	if got := renderContextBar(s, 100, 0); got != "" {
		t.Errorf("unknown window: got %q, want \"\"", got)
	}

	got := renderContextBar(s, 64_000, 128_000)
	if !strings.Contains(got, "64k/128k") {
		t.Errorf("got %q, want it to contain %q", got, "64k/128k")
	}

	// used > window (e.g. a provider's prompt count creeping past its
	// own advertised ceiling) must clamp the bar rather than overflow it
	// or panic on a >100% fraction.
	over := renderContextBar(s, 200_000, 128_000)
	full := renderContextBar(s, 128_000, 128_000)
	if strings.Count(over, "█") != strings.Count(full, "█") {
		t.Errorf("over-budget bar wasn't clamped to the same fill as exactly-full: over=%q full=%q", over, full)
	}
}

func TestJoinLeftRight(t *testing.T) {
	s := theme.New(theme.Load()[0])

	if got := joinLeftRight(s, "left", "", 20); got != "left" {
		t.Errorf("empty right: got %q, want %q", got, "left")
	}

	got := joinLeftRight(s, "left", "right", 11)
	if got != "left  right" {
		t.Errorf("got %q, want %q", got, "left  right")
	}

	// Not enough room for both: right is dropped silently rather than
	// truncated into something unreadable.
	if got := joinLeftRight(s, "left", "right", 5); got != "left" {
		t.Errorf("narrow width: got %q, want just %q", got, "left")
	}
}
