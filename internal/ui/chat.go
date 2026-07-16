package ui

import (
	"fmt"
	"strings"
	"time"

	"tui-testing/internal/theme"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// renderTranscript renders the boot banner plus the full message log as a
// single string for the viewport's SetContent. Kept as a pure function of
// (styles, messages, width) so it's trivial to re-render after a theme
// swap or resize.
//
// It also returns the line (row) each RoleUser message's block starts
// at, so PgUp/PgDn can jump viewport.YOffset straight to a prompt instead
// of scrolling a fixed page height — cheap to compute here alongside the
// render pass instead of re-walking the content separately.
func renderTranscript(s theme.Styles, messages []ChatMessage, width int, highlightUser, verboseTools, showReasoning bool) (content string, userMsgLines []int) {
	var sb strings.Builder
	bootBlock := renderBootArt(s, width)
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
			startLine := writeBlock(renderMessage(s, m, width, highlightUser, verboseTools, showReasoning), false)
			if m.Role == RoleUser {
				userMsgLines = append(userMsgLines, startLine)
			}
		}
	}
	return sb.String(), userMsgLines
}

func renderMessage(s theme.Styles, m ChatMessage, width int, highlightUser, verboseTools, showReasoning bool) string {
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
			label += s.MessageContent.Render("  ") + badge
		}
		lines := []string{label}
		// Gated behind its own settings toggle (showReasoning), not
		// verboseTools — this is proof reasoning actually happened, not
		// routine tool chatter, so it gets its own on/off rather than
		// riding along with an unrelated setting. The "thinking Xs"/
		// "thought for Xs" badge above stays visible either way — that's
		// a separate, already-useful signal on its own (see
		// renderReasoningBadge). The transcript already scrolls, so
		// showing a long one doesn't force itself onto the screen the
		// way an unbounded single line would.
		if showReasoning && m.ReasoningText != "" {
			lines = append(lines, s.ReasoningText.Width(width).Render(m.ReasoningText))
		}
		lines = append(lines, s.MessageContent.Width(width).Render(m.Content))
		if m.FinishReason != "" {
			lines = append(lines, renderFinishReason(s, m.FinishReason))
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
			return callLine + s.MessageContent.Render("  ") + s.ToolResult.Render(text)
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
			return callLine + s.MessageContent.Render("  ") + statusStyle.Render(status)
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
