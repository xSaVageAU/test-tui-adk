package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"tui-testing/internal/theme"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderTranscript renders the boot banner plus the full message log as a
// single string for the viewport's SetContent. Kept as a pure function of
// (styles, boot, messages, width) so it's trivial to re-render after a
// theme swap or resize.
//
// It also returns the line (row) each RoleUser message's block starts
// at, so PgUp/PgDn can jump viewport.YOffset straight to a prompt instead
// of scrolling a fixed page height — cheap to compute here alongside the
// render pass instead of re-walking the content separately.
func renderTranscript(s theme.Styles, boot BootInfo, messages []ChatMessage, width int, highlightUser, verboseTools bool) (content string, userMsgLines []int) {
	var sb strings.Builder
	bootBlock := renderBootArt(s, boot, width)
	sb.WriteString(bootBlock)
	line := lipgloss.Height(bootBlock)

	// writeBlock writes the gap before block and then block itself,
	// tracking the running line count as it goes, and returns the row
	// block's own content starts on (after the gap) for the caller to
	// record if it needs to. tight collapses the usual blank-line gap to
	// a single newline — used to visually group a tool call with its
	// result.
	writeBlock := func(block string, tight bool) int {
		gap := "\n\n"
		if tight {
			gap = "\n"
		}
		sb.WriteString(gap)
		line += strings.Count(gap, "\n")
		startLine := line
		sb.WriteString(block)
		line += lipgloss.Height(block)
		return startLine
	}

	if len(messages) == 0 {
		writeBlock(s.MessageSystem.Render("No messages yet — say something below."), false)
	} else {
		for _, m := range messages {
			startLine := writeBlock(renderMessage(s, m, width, highlightUser, verboseTools), false)
			if m.Role == RoleUser {
				userMsgLines = append(userMsgLines, startLine)
			}
		}
	}
	return sb.String(), userMsgLines
}

func renderMessage(s theme.Styles, m ChatMessage, width int, highlightUser, verboseTools bool) string {
	switch m.Role {
	case RoleUser:
		label := s.MessageUser.Render("you")
		if !highlightUser {
			content := s.MessageContent.Width(width).Render(m.Content)
			return lipgloss.JoinVertical(lipgloss.Left, label, content)
		}
		bubble := s.MessageUserBubble.Width(width - 2).Render(m.Content)
		return lipgloss.JoinVertical(lipgloss.Left, label, bubble)
	case RoleAgent:
		label := s.MessageAgent.Render("agent")
		if badge := renderReasoningBadge(s, m); badge != "" {
			label += "  " + badge
		}
		content := s.MessageContent.Width(width).Render(m.Content)
		lines := []string{label, content}
		if m.FinishReason != "" {
			lines = append(lines, renderFinishReason(s, m.FinishReason))
		}
		if m.Usage != nil {
			lines = append(lines, renderUsage(s, m.Usage))
		}
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	case RoleTool:
		return renderTool(s, m.ToolName, m.ToolArgs, m.ToolResult, m.ToolStatus, m.ToolPending, verboseTools, width)
	default:
		return s.MessageEvent.Render(m.Content)
	}
}

// toolGutter is the left-margin marker tool activity is prefixed with —
// visually groups a call and its status as one unit, distinct from
// ordinary prose, without the weight of a full bordered box.
const toolGutter = "▏ "

// renderTool draws one tool invocation's whole lifecycle as a single
// entry: "▏ [toolName]  <args>" (the name itself a filled badge — see
// theme.Styles.ToolCallName — so a tool call reads as a distinct event
// at a glance rather than blending into surrounding prose) plus a status
// once there's anything to report. That status is the one thing that
// changes in place as a call progresses from running to (optionally)
// awaiting approval to a final result — see App.upsertToolMessage/
// completeToolMessage, which is what makes that in-place update happen
// instead of a fresh entry appearing per event.
//
// verboseTools controls both the detail level and the layout. false (the
// default): args and status/result are tailored one-line summaries for
// this app's own built-in tools (formatToolArgs/formatToolResult),
// appended to the *same* line as the call — the whole invocation reads
// as one row whenever it reasonably can. true: args render as plain,
// generic key=value pairs (formatKV) and the result via the generic
// summarizeResult, each on their own wrapped line below the call — long
// output is expected here, so no effort is made to keep it to one line.
// Both fall back to the same generic formatting for a tool this file
// doesn't specifically know about (agent-as-tool specialist calls, a
// future third-party tool) — those are already lean (a specialist's
// result is its own prose, not noise), so the two modes look identical
// for them either way.
//
// While pending is true, the status always renders as its own
// full-width filled badge line regardless of verboseTools — feedback
// was that plain colored text blended in too easily to notice a
// conversation was blocked waiting on a decision, and that's still true
// whether or not the rest of this entry is lean.
func renderTool(s theme.Styles, name string, args, result map[string]any, status string, pending, verboseTools bool, width int) string {
	callLine := s.ToolGutter.Render(toolGutter) + s.ToolCallName.Render(name)
	if argsText := formatToolArgs(name, args, verboseTools); argsText != "" {
		callLine += s.ToolCallArgs.Render("  " + argsText)
	}

	switch {
	case result != nil:
		text := formatToolResult(name, result, verboseTools)
		if !verboseTools {
			return callLine + "  " + s.ToolResult.Render(text)
		}
		return callLine + "\n" + renderToolStatusLine(s, s.ToolResult, text, width)
	case pending:
		return callLine + "\n" + s.ToolConfirmPending.Width(width).Render(" "+status)
	case status != "":
		statusStyle := s.ToolResult
		switch status {
		case confirmStatusApproved:
			statusStyle = s.ToolConfirmApproved
		case confirmStatusDenied:
			statusStyle = s.ToolConfirmDenied
		}
		if !verboseTools {
			return callLine + "  " + statusStyle.Render(status)
		}
		return callLine + "\n" + renderToolStatusLine(s, statusStyle, status, width)
	default:
		return callLine
	}
}

// renderToolStatusLine word-wraps a tool's status text to width (minus
// the gutter's own width) and prefixes every wrapped line with the
// gutter, so a long line stays aligned under the call line above it
// instead of overflowing unbroken off the right edge. Short results
// (list_files-style counts/key=value pairs) render on one line same as
// before; this only matters once results can run to a sentence or more —
// which agent-as-tool specialist calls do, since their result is the
// specialist's full prose answer (see summarizeResult).
func renderToolStatusLine(s theme.Styles, style lipgloss.Style, text string, width int) string {
	prefix := s.ToolGutter.Render(toolGutter)
	wrapWidth := max(width-lipgloss.Width(prefix), 1)
	wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(text)

	lines := strings.Split(wrapped, "\n")
	for i, line := range lines {
		lines[i] = prefix + style.Render(line)
	}
	return strings.Join(lines, "\n")
}

// renderUsage draws the quiet per-turn token-cost line under an agent
// reply — a running total across every model call the turn made, not
// just its last one. See App.attachTurnUsage.
func renderUsage(s theme.Styles, u *TokenUsage) string {
	return s.MessageMeta.Render(fmt.Sprintf("%d in · %d out · %d tokens", u.Prompt, u.Output, u.Total))
}

// renderReasoningBadge draws whatever belongs next to the "agent" label
// for m's reasoning state — a filled "thinking <duration>" badge while
// active (App.reasoningStart/stopwatch live-update m.ReasoningDuration
// every reasoningTickInterval, so this re-renders with a new number each
// tick), or, once it's done, a quiet permanent "thought for <duration>"
// note — same weight as the usage line, not something that needs to
// keep grabbing attention once the number is final. "" (append nothing)
// if this message never reasoned at all.
func renderReasoningBadge(s theme.Styles, m ChatMessage) string {
	switch {
	case m.ReasoningActive:
		return s.ReasoningBadge.Render("thinking " + formatReasoningDuration(m.ReasoningDuration))
	case m.ReasoningDuration > 0:
		return s.ReasoningNote.Render("thought for " + formatReasoningDuration(m.ReasoningDuration))
	default:
		return ""
	}
}

// formatReasoningDuration favors milliseconds under a second — real
// reasoning bursts often finish well under one, and "0s" for all of
// them would defeat the entire point of timing this at all.
func formatReasoningDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// renderFinishReason draws a note under an agent reply when the model's
// last call in the turn ended on something other than a plain "stop" —
// styled by severity: a red/bold line for the model actually
// refusing/filtering (safety, recitation, prohibited content, ...) versus
// a plain warning-colored line for a benign truncation.
func renderFinishReason(s theme.Styles, reason string) string {
	text, blocked := finishReasonText(reason)
	if blocked {
		return s.MessageFinishBlocked.Render(text)
	}
	return s.MessageFinishWarning.Render(text)
}

// finishReasonText maps a genai.FinishReason string (kept as a plain
// string here, not the genai type, so this package never needs to import
// genai) to what a user should be told, plus whether it represents the
// model refusing/filtering the response (as opposed to just running out
// of room to finish it).
func finishReasonText(reason string) (text string, blocked bool) {
	switch reason {
	case "MAX_TOKENS":
		return "response truncated — hit the model's max output length", false
	case "SAFETY":
		return "response blocked by safety filters", true
	case "RECITATION":
		return "response blocked — flagged as recitation", true
	case "BLOCKLIST":
		return "response blocked — matched a blocked term", true
	case "PROHIBITED_CONTENT":
		return "response blocked — prohibited content", true
	case "SPII":
		return "response blocked — sensitive personal information", true
	case "MALFORMED_FUNCTION_CALL":
		return "model produced a malformed tool call", true
	case "UNEXPECTED_TOOL_CALL":
		return "model attempted an unexpected tool call", true
	case "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return "generated image blocked", true
	case "LANGUAGE":
		return "response stopped — unsupported language", false
	default:
		return "response ended early (" + reason + ")", false
	}
}

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

// stickyPromptMaxLines caps how tall the pinned "you: ..." overlay is
// allowed to grow — it composites over (replaces, doesn't add to) rows of
// the viewport, so an unbounded height would start eating the response
// it's supposed to be floating over instead of just its first line or two.
const stickyPromptMaxLines = 3

// renderStickyPrompt draws the pinned "you: ..." strip shown when the
// last prompt has scrolled out of view — word-wrapped up to
// stickyPromptMaxLines rather than hard-truncated to one line, so more of
// a longer prompt is actually readable. Only past that cap does it fall
// back to an ellipsis, on the last row. Filled to the full width so it
// reads as a solid strip overlaid on the content, not text floating over
// whatever was there.
func renderStickyPrompt(s theme.Styles, promptText string, width int) string {
	flat := strings.ReplaceAll(strings.TrimSpace(promptText), "\n", " ")

	// Word-wrap unstyled first so line count and per-line truncation can
	// be worked out on plain text, then style the whole block in one
	// pass — same reasoning as everywhere else in this app that a style's
	// Background needs to be applied after the content is already at
	// its final per-line width, not before.
	lines := strings.Split(lipgloss.NewStyle().Width(width).Render("you: "+flat), "\n")

	if len(lines) > stickyPromptMaxLines {
		lines = lines[:stickyPromptMaxLines]
		last := strings.TrimRight(lines[len(lines)-1], " ")
		lines[len(lines)-1] = ansi.Truncate(last, width-1, "") + "…"
	}

	return s.StickyPrompt.Width(width).Render(strings.Join(lines, "\n"))
}
