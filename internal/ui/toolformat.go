// This file turns a tool call's raw args/result (plain map[string]any,
// whatever a Backend handed back) into readable text — pure string
// formatting, no theme.Styles or rendering involved. See chat.go's
// renderTool for how these get placed into the transcript.
package ui

import (
	"fmt"
	"sort"
	"strings"
)

func formatKV(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + formatValue(m[k])
	}
	return strings.Join(parts, " ")
}

// summarizeResult formats a tool result generically. A single-string
// value (e.g. agent-as-tool's {"result": "<specialist's full answer>"})
// renders bare, with no "key=" prefix — it reads as prose, not data, and
// the tool name on the line above already says where it came from. A
// single-list value (e.g. list_files's {"files": [...]}) summarizes as a
// count plus the list. Anything else falls back to plain key=value pairs.
func summarizeResult(result map[string]any) string {
	if len(result) == 1 {
		for k, v := range result {
			switch val := v.(type) {
			case string:
				return strings.TrimSpace(val)
			case []any:
				items := make([]string, len(val))
				for i, e := range val {
					items[i] = fmt.Sprint(e)
				}
				return fmt.Sprintf("%d %s — %s", len(val), k, strings.Join(items, ", "))
			}
		}
	}
	return formatKV(result)
}

// formatToolArgs is renderTool's call-line formatter. list_files always
// shows its target path regardless of verboseTools — there's nothing
// else on its call line worth showing either way, and the directory
// being listed is exactly the thing you'd want to know at a glance, not
// something worth hiding behind a toggle. read_file/write_file show just
// the path in lean mode (dropping write_file's content argument entirely
// rather than the truncated-but-still-noisy preview formatKV would
// otherwise show); verbose falls back to the generic formatKV, same as
// an unrecognized tool name gets in either mode.
func formatToolArgs(name string, args map[string]any, verbose bool) string {
	if name == "list_files" {
		if path, ok := args["path"].(string); ok && path != "" {
			return path
		}
		return "." // listFiles' own default when no path is given
	}
	if verbose {
		return formatKV(args)
	}
	if name == "read_file" || name == "write_file" {
		if path, ok := args["path"].(string); ok && path != "" {
			return path
		}
	}
	return formatKV(args)
}

// readFilePreviewMaxLines caps how much of a file's content verbose mode
// actually prints — verbose means "more than the lean one-liner," not
// "however many thousand lines the file happens to have."
const readFilePreviewMaxLines = 50

// formatToolResult is renderTool's status-line formatter. For this
// app's own three built-in tools, lean and verbose deliberately show
// categorically different things rather than the same information at
// two lengths: lean is always a bare count/size (no file names, no file
// content, no exception for a short list that would technically still
// fit), verbose is the actual content/listing. Falls back to the
// generic summarizeResult both for an unrecognized tool and for
// whichever piece a recognized tool's result doesn't match (e.g.
// write_file has nothing more to say in verbose mode than lean already
// shows, just formatted generically instead).
func formatToolResult(name string, result map[string]any, verbose bool) string {
	switch name {
	case "read_file":
		if content, ok := result["content"].(string); ok {
			if !verbose {
				return "read " + humanBytes(len(content))
			}
			return truncateLines(content, readFilePreviewMaxLines)
		}
	case "write_file":
		if !verbose {
			if n, ok := intFromAny(result["bytesWritten"]); ok {
				return "wrote " + humanBytes(n)
			}
		}
	case "list_files":
		if files, ok := result["files"].([]any); ok && !verbose {
			return fmt.Sprintf("%d entries", len(files))
		}
		// verbose falls through to summarizeResult, which lists every name.
	}
	return summarizeResult(result)
}

// truncateLines caps s at maxLines, noting how many lines were hidden
// rather than silently dropping them — used for read_file's verbose
// preview, so even "the full content" has a sane ceiling instead of
// however long the file actually is.
func truncateLines(s string, maxLines int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= maxLines {
		return s
	}
	return strings.Join(lines[:maxLines], "\n") + fmt.Sprintf("\n… (%d more lines)", len(lines)-maxLines)
}

// humanBytes renders a byte count the way a person would read it aloud
// rather than as a raw integer — mainly matters for read_file, where a
// real file's content can run from a few bytes to several megabytes.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// intFromAny covers both plausible runtime types for a tool result's
// numeric field: the handler itself returns a Go int (writeFileResult.
// BytesWritten), but by the time it's round-tripped through ADK's event
// model as a map[string]any it may have gone through a JSON encode/
// decode step, which turns any number into float64 — check both rather
// than assume one.
func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// maxArgValuePreview caps how much of a single argument value's string
// form formatValue will show. Args are rendered as one unwrapped
// "key=value" line (both in the tool-call line and, via formatKV, in the
// HITL confirm-modal's title) — fine for something like a file path, but
// an agent-as-tool specialist call's "request" argument (research/coder/
// planner's default agenttool schema) can run to a full paragraph, and
// without a cap that would dump the whole thing onto one line. A future
// tool with a similarly long argument (e.g. a file's content) benefits
// from this for free too.
const maxArgValuePreview = 60

func formatValue(v any) string {
	if list, ok := v.([]any); ok {
		items := make([]string, len(list))
		for i, e := range list {
			items[i] = fmt.Sprint(e)
		}
		return strings.Join(items, ",")
	}
	return truncatePreview(fmt.Sprint(v), maxArgValuePreview)
}

// truncatePreview flattens s to one line (embedded newlines would
// otherwise break the single-line key=value format) and, past max
// characters, cuts it short with a note of how much was hidden rather
// than showing it all.
func truncatePreview(s string, max int) string {
	flat := strings.Join(strings.Fields(s), " ")
	if len(flat) <= max {
		return flat
	}
	return fmt.Sprintf("%s… (%d chars)", flat[:max], len(flat))
}
