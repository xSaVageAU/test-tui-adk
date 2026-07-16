package ui

import (
	"strings"
	"testing"
	"time"

	"tui-testing/internal/theme"
)

// These are pure-function tests, not TUI-driving — formatToolArgs/
// formatToolResult carry real branching risk (verbose/lean fallback,
// and genuine uncertainty over whether a tool result's numeric field
// arrives as a Go int or a JSON-decoded float64 by the time it reaches
// here) that build+vet can't catch, unlike routine wiring elsewhere in
// this package.

// TestFormatToolArgs covers the call-line formatter — no lean/verbose
// axis here any more: read_file/write_file/list_files always show just
// their path (write_file's content argument in particular never belongs
// on the single call line — see formatToolResult's write_file case for
// where it actually shows up), and an unrecognized tool always falls
// back to the generic formatKV.
func TestFormatToolArgs(t *testing.T) {
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
		if got := formatToolArgs(c.name, c.args); got != c.want {
			t.Errorf("formatToolArgs(%q, %v) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}

func TestFormatToolResultLean(t *testing.T) {
	if got := formatToolResult("read_file", nil, map[string]any{"content": "12345"}, false, toolPreviewMaxLinesDefault); got != "read 5 bytes" {
		t.Errorf("read_file result = %q, want %q", got, "read 5 bytes")
	}

	// bytesWritten as a plain Go int (the handler's own return type).
	if got := formatToolResult("write_file", nil, map[string]any{"bytesWritten": 2048}, false, toolPreviewMaxLinesDefault); got != "wrote 2.0 KB" {
		t.Errorf("write_file (int) result = %q, want %q", got, "wrote 2.0 KB")
	}
	// bytesWritten as float64 — what a JSON decode step would produce;
	// covering both is the actual point of this test, since it's not
	// certain from here which one ADK's event pipeline hands back.
	if got := formatToolResult("write_file", nil, map[string]any{"bytesWritten": float64(2048)}, false, toolPreviewMaxLinesDefault); got != "wrote 2.0 KB" {
		t.Errorf("write_file (float64) result = %q, want %q", got, "wrote 2.0 KB")
	}

	// Lean list_files is a bare count, full stop — no names shown even
	// when the list is short enough that showing them would still fit,
	// since "verbose only changes the count's label" was exactly the
	// confusing middle ground this replaced.
	short := map[string]any{"files": []any{"a.go", "b.go"}}
	if got := formatToolResult("list_files", nil, short, false, toolPreviewMaxLinesDefault); got != "2 entries" {
		t.Errorf("list_files (short) result = %q, want %q", got, "2 entries")
	}
}

// TestFormatToolResultWriteFileVerboseShowsContent is write_file's
// counterpart to read_file's verbose preview: the actual written text
// comes from args (the call only ever gets bytesWritten back in
// result), truncated the same way and to the same cap.
func TestFormatToolResultWriteFileVerboseShowsContent(t *testing.T) {
	args := map[string]any{"path": "foo.txt", "content": "line one\nline two"}
	result := map[string]any{"bytesWritten": 17}
	got := formatToolResult("write_file", args, result, true, toolPreviewMaxLinesDefault)
	if got != "line one\nline two" {
		t.Errorf("write_file verbose result = %q, want the written content", got)
	}
}

// TestFormatToolResultListFilesVerboseShowsNames is list_files' other
// half: verbose should show the actual names (via summarizeResult),
// categorically more than lean's bare count — not the same count with
// different wording.
func TestFormatToolResultListFilesVerboseShowsNames(t *testing.T) {
	result := map[string]any{"files": []any{"a.go", "b.go"}}
	got := formatToolResult("list_files", nil, result, true, toolPreviewMaxLinesDefault)
	want := summarizeResult(result)
	if got != want || got == "2 entries" {
		t.Errorf("list_files verbose = %q, want the full listing (%q)", got, want)
	}
}

// TestFormatToolResultVerboseTruncatesAtConfiguredCap covers the
// configurable-line-cap behavior — the same truncateLines mechanism
// read_file and write_file's verbose preview both use — with an
// explicit cap rather than toolPreviewMaxLinesDefault, so this test's
// own expected numbers stay self-consistent regardless of what that
// default happens to be.
func TestFormatToolResultVerboseTruncatesAtConfiguredCap(t *testing.T) {
	const maxLines = 50
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = "line"
	}
	content := strings.Join(lines, "\n")
	got := formatToolResult("read_file", nil, map[string]any{"content": content}, true, maxLines)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != maxLines+1 { // +1 for the "… (N more lines)" note
		t.Fatalf("got %d lines, want %d (cap) + 1 (note); output:\n%s", len(gotLines), maxLines, got)
	}
	if gotLines[maxLines] != "… (30 more lines)" {
		t.Errorf("last line = %q, want the truncation note", gotLines[maxLines])
	}

	short := strings.Join(lines[:10], "\n")
	if got := formatToolResult("read_file", nil, map[string]any{"content": short}, true, maxLines); got != short {
		t.Errorf("short content should pass through unchanged, got %q", got)
	}
}

func TestFormatToolResultVerboseMatchesSummarizeResult(t *testing.T) {
	result := map[string]any{"content": "the whole file, in full, no matter how long"}
	got := formatToolResult("read_file", nil, result, true, toolPreviewMaxLinesDefault)
	want := summarizeResult(result)
	if got != want {
		t.Errorf("verbose formatToolResult = %q, want summarizeResult's %q", got, want)
	}
}

func TestFormatToolResultUnrecognizedToolAlwaysGeneric(t *testing.T) {
	result := map[string]any{"result": "the specialist's full prose answer"}
	lean := formatToolResult("research", nil, result, false, toolPreviewMaxLinesDefault)
	verbose := formatToolResult("research", nil, result, true, toolPreviewMaxLinesDefault)
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

func TestRenderReasoningBadge(t *testing.T) {
	s := theme.New(theme.Load()[0])

	if got := renderReasoningBadge(s, ChatMessage{}); got != "" {
		t.Errorf("never-reasoned message: got %q, want \"\"", got)
	}

	active := ChatMessage{ReasoningActive: true, ReasoningDuration: 3200 * time.Millisecond}
	if got := renderReasoningBadge(s, active); !strings.Contains(got, "thinking 3.2s") {
		t.Errorf("active message: got %q, want it to contain %q", got, "thinking 3.2s")
	}

	done := ChatMessage{ReasoningActive: false, ReasoningDuration: 7 * time.Second}
	if got := renderReasoningBadge(s, done); !strings.Contains(got, "thought for 7.0s") {
		t.Errorf("done message: got %q, want it to contain %q", got, "thought for 7.0s")
	}

	// ReasoningActive takes priority if somehow both are set (shouldn't
	// happen in practice — endReasoning always clears Active in the same
	// write that sets Duration — but the render logic should still pick
	// one deterministically rather than, say, concatenating both).
	both := ChatMessage{ReasoningActive: true, ReasoningDuration: 2 * time.Second}
	if got := renderReasoningBadge(s, both); !strings.Contains(got, "thinking") || strings.Contains(got, "thought for") {
		t.Errorf("active+duration message: got %q, want only the active form", got)
	}
}

// TestFormatReasoningDuration is the actual fix this round: a reasoning
// burst that finishes in well under a second (common, per live
// feedback) used to always read "0s" — quantized to stopwatch's 1s tick
// interval and truncated to whole seconds on top of that — which made
// the whole feature look broken even when it was working correctly.
func TestFormatReasoningDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{340 * time.Millisecond, "340ms"},
		{0, "0ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1.0s"},
		{3200 * time.Millisecond, "3.2s"},
	}
	for _, c := range cases {
		if got := formatReasoningDuration(c.d); got != c.want {
			t.Errorf("formatReasoningDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
