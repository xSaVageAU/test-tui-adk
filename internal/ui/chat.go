package ui

import (
	"fmt"
	"sort"
	"strings"

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
func renderTranscript(s theme.Styles, boot BootInfo, messages []ChatMessage, width int, highlightUser bool) (content string, userMsgLines []int) {
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
			startLine := writeBlock(renderMessage(s, m, width, highlightUser), false)
			if m.Role == RoleUser {
				userMsgLines = append(userMsgLines, startLine)
			}
		}
	}
	return sb.String(), userMsgLines
}

func renderMessage(s theme.Styles, m ChatMessage, width int, highlightUser bool) string {
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
		content := s.MessageContent.Width(width).Render(m.Content)
		return lipgloss.JoinVertical(lipgloss.Left, label, content)
	case RoleTool:
		return renderTool(s, m.ToolName, m.ToolArgs, m.ToolStatus, m.ToolPending, width)
	default:
		return s.MessageEvent.Render(m.Content)
	}
}

// toolGutter is the left-margin marker tool activity is prefixed with —
// visually groups a call and its status as one unit, distinct from
// ordinary prose, without the weight of a full bordered box.
const toolGutter = "▏ "

// renderTool draws one tool invocation's whole lifecycle as a single
// entry: "▏ toolName  key=value ..." (args rendered generically — plain
// key=value, sorted by key since map iteration order isn't stable and
// this re-renders on every keystroke — works for any tool, not just
// list_files), plus a status line beneath it once there's anything to
// report. That second line is the one thing that changes in place as a
// call progresses from running to (optionally) awaiting approval to a
// final result — see App.upsertToolMessage, which is what makes that
// in-place update happen instead of a fresh entry appearing per event.
//
// While pending is true, the status line renders as a full-width filled
// badge rather than another quiet gutter line — feedback was that plain
// colored text blended in too easily to notice a conversation was
// blocked waiting on a decision. Once resolved it drops back to
// gutter-line weight, same as a plain result, since at that point it's
// just a record.
func renderTool(s theme.Styles, name string, args map[string]any, status string, pending bool, width int) string {
	callLine := s.ToolGutter.Render(toolGutter) + s.ToolCallName.Render(name)
	if kv := formatKV(args); kv != "" {
		callLine += s.ToolCallArgs.Render("  " + kv)
	}
	if status == "" {
		return callLine
	}
	if pending {
		return callLine + "\n" + s.ToolConfirmPending.Width(width).Render(" "+status)
	}

	statusStyle := s.ToolResult
	switch status {
	case confirmStatusApproved:
		statusStyle = s.ToolConfirmApproved
	case confirmStatusDenied:
		statusStyle = s.ToolConfirmDenied
	}
	return callLine + "\n" + s.ToolGutter.Render(toolGutter) + statusStyle.Render(status)
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

func summarizeResult(result map[string]any) string {
	if len(result) == 1 {
		for k, v := range result {
			if list, ok := v.([]any); ok {
				items := make([]string, len(list))
				for i, e := range list {
					items[i] = fmt.Sprint(e)
				}
				return fmt.Sprintf("%d %s — %s", len(list), k, strings.Join(items, ", "))
			}
		}
	}
	return formatKV(result)
}

func formatValue(v any) string {
	if list, ok := v.([]any); ok {
		items := make([]string, len(list))
		for i, e := range list {
			items[i] = fmt.Sprint(e)
		}
		return strings.Join(items, ",")
	}
	return fmt.Sprint(v)
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
