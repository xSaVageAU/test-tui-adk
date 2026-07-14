package ui

import (
	"strings"
	"testing"
)

// These are pure-function tests, not TUI-driving — formatToolArgs/
// formatToolResult carry real branching risk (verbose/lean fallback,
// and genuine uncertainty over whether a tool result's numeric field
// arrives as a Go int or a JSON-decoded float64 by the time it reaches
// here) that build+vet can't catch, unlike routine wiring elsewhere in
// this package.

func TestFormatToolArgsLean(t *testing.T) {
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"read_file", map[string]any{"path": "foo.txt"}, "foo.txt"},
		{"write_file", map[string]any{"path": "foo.txt", "content": "a very long file body that should never appear"}, "foo.txt"},
		{"list_files", map[string]any{"path": "src"}, "src"},
		{"list_files", map[string]any{}, "."},
		{"read_file", map[string]any{}, ""}, // missing path falls back to formatKV, which is "" for an empty map
		{"research", map[string]any{"request": "what does this do"}, "request=what does this do"},
	}
	for _, c := range cases {
		if got := formatToolArgs(c.name, c.args, false); got != c.want {
			t.Errorf("formatToolArgs(%q, %v, false) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestFormatToolArgsVerboseMatchesFormatKV(t *testing.T) {
	args := map[string]any{"path": "foo.txt", "content": "hello"}
	got := formatToolArgs("write_file", args, true)
	want := formatKV(args)
	if got != want {
		t.Errorf("verbose formatToolArgs = %q, want formatKV's %q", got, want)
	}
}

// TestFormatToolArgsListFilesAlwaysShowsPath guards the one place
// formatToolArgs deliberately ignores the verbose flag: list_files'
// target directory is the whole point of the call, so it should never
// disappear just because verbose mode would otherwise defer to
// formatKV(args) — which renders "" for an empty args map (the common
// case: a model calling list_files with no explicit path at all).
func TestFormatToolArgsListFilesAlwaysShowsPath(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		if got := formatToolArgs("list_files", map[string]any{"path": "src"}, verbose); got != "src" {
			t.Errorf("verbose=%v: got %q, want %q", verbose, got, "src")
		}
		if got := formatToolArgs("list_files", map[string]any{}, verbose); got != "." {
			t.Errorf("verbose=%v: got %q, want %q", verbose, got, ".")
		}
	}
}

func TestFormatToolResultLean(t *testing.T) {
	if got := formatToolResult("read_file", map[string]any{"content": "12345"}, false); got != "read 5 bytes" {
		t.Errorf("read_file result = %q, want %q", got, "read 5 bytes")
	}

	// bytesWritten as a plain Go int (the handler's own return type).
	if got := formatToolResult("write_file", map[string]any{"bytesWritten": 2048}, false); got != "wrote 2.0 KB" {
		t.Errorf("write_file (int) result = %q, want %q", got, "wrote 2.0 KB")
	}
	// bytesWritten as float64 — what a JSON decode step would produce;
	// covering both is the actual point of this test, since it's not
	// certain from here which one ADK's event pipeline hands back.
	if got := formatToolResult("write_file", map[string]any{"bytesWritten": float64(2048)}, false); got != "wrote 2.0 KB" {
		t.Errorf("write_file (float64) result = %q, want %q", got, "wrote 2.0 KB")
	}

	// Lean list_files is a bare count, full stop — no names shown even
	// when the list is short enough that showing them would still fit,
	// since "verbose only changes the count's label" was exactly the
	// confusing middle ground this replaced.
	short := map[string]any{"files": []any{"a.go", "b.go"}}
	if got := formatToolResult("list_files", short, false); got != "2 entries" {
		t.Errorf("list_files (short) result = %q, want %q", got, "2 entries")
	}
}

// TestFormatToolResultListFilesVerboseShowsNames is list_files' other
// half: verbose should show the actual names (via summarizeResult),
// categorically more than lean's bare count — not the same count with
// different wording.
func TestFormatToolResultListFilesVerboseShowsNames(t *testing.T) {
	result := map[string]any{"files": []any{"a.go", "b.go"}}
	got := formatToolResult("list_files", result, true)
	want := summarizeResult(result)
	if got != want || got == "2 entries" {
		t.Errorf("list_files verbose = %q, want the full listing (%q)", got, want)
	}
}

// TestFormatToolResultReadFileVerboseTruncates covers the 50-line cap:
// even verbose mode — "show more" — has a ceiling, so a huge file can't
// flood the transcript no matter what setting is on.
func TestFormatToolResultReadFileVerboseTruncates(t *testing.T) {
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = "line"
	}
	content := strings.Join(lines, "\n")
	got := formatToolResult("read_file", map[string]any{"content": content}, true)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != readFilePreviewMaxLines+1 { // +1 for the "… (N more lines)" note
		t.Fatalf("got %d lines, want %d (cap) + 1 (note); output:\n%s", len(gotLines), readFilePreviewMaxLines, got)
	}
	if gotLines[readFilePreviewMaxLines] != "… (30 more lines)" {
		t.Errorf("last line = %q, want the truncation note", gotLines[readFilePreviewMaxLines])
	}

	short := strings.Join(lines[:10], "\n")
	if got := formatToolResult("read_file", map[string]any{"content": short}, true); got != short {
		t.Errorf("short content should pass through unchanged, got %q", got)
	}
}

func TestFormatToolResultVerboseMatchesSummarizeResult(t *testing.T) {
	result := map[string]any{"content": "the whole file, in full, no matter how long"}
	got := formatToolResult("read_file", result, true)
	want := summarizeResult(result)
	if got != want {
		t.Errorf("verbose formatToolResult = %q, want summarizeResult's %q", got, want)
	}
}

func TestFormatToolResultUnrecognizedToolAlwaysGeneric(t *testing.T) {
	result := map[string]any{"result": "the specialist's full prose answer"}
	lean := formatToolResult("research", result, false)
	verbose := formatToolResult("research", result, true)
	if lean != verbose {
		t.Errorf("lean/verbose diverged for an unrecognized tool: lean=%q verbose=%q", lean, verbose)
	}
	if lean != "the specialist's full prose answer" {
		t.Errorf("got %q, want the bare prose (summarizeResult's single-string case)", lean)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 bytes"},
		{1023, "1023 bytes"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1 << 20, "1.0 MB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.n); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestIntFromAny(t *testing.T) {
	if n, ok := intFromAny(42); !ok || n != 42 {
		t.Errorf("intFromAny(int 42) = %d, %v", n, ok)
	}
	if n, ok := intFromAny(float64(42)); !ok || n != 42 {
		t.Errorf("intFromAny(float64 42) = %d, %v", n, ok)
	}
	if _, ok := intFromAny("42"); ok {
		t.Error("intFromAny(string) should not be ok")
	}
	if _, ok := intFromAny(nil); ok {
		t.Error("intFromAny(nil) should not be ok")
	}
}
