// This file manages the transcript as state — appending/updating tool
// messages as their events arrive, re-rendering into the viewport, and
// deciding when scrolling should follow new content vs. stay put. It's
// the read/write counterpart to chat.go, which only knows how to render a
// given []ChatMessage, not how the messages themselves get built up over
// a turn.
package ui

import "time"

// upsertToolMessage tracks a tool call's opening state — running, then
// (if applicable) awaiting approval or approved/denied — as a single
// transcript entry found and updated by call ID rather than appended
// fresh each event; see completeToolMessage for how the final result
// lands, in a distinct field so it can be reformatted live if
// verboseTools changes later instead of baking in a summary once at
// event time. Without upserting-by-ID, a call's confirmation request and
// its eventual result each used to print as their own separate message,
// so the same invocation showed up two or three times in a row as it
// progressed.
//
// On every later sighting of the same ID, only status/pending are
// touched — args aren't overwritten, since only the initial call event
// carries them; a confirmation event reuses them via nil.
func (a *App) upsertToolMessage(id, name string, args map[string]any, status string, pending bool) {
	if idx, ok := a.toolMsgIndex[id]; ok && idx < len(a.messages) {
		a.messages[idx].ToolStatus = status
		a.messages[idx].ToolPending = pending
		return
	}
	a.newToolMessage(id, ChatMessage{ToolName: name, ToolArgs: args, ToolStatus: status, ToolPending: pending, At: time.Now()})
}

// completeToolMessage records a finished call's raw result. Kept
// separate from upsertToolMessage (rather than another status string)
// specifically so renderTool can reformat it live if verboseTools is
// toggled after the fact — see ChatMessage's doc comment. A nil result
// is normalized to an empty map so a genuinely-empty result stays
// distinguishable from "no result yet" (ChatMessage.ToolResult == nil),
// which is what renderTool actually checks.
func (a *App) completeToolMessage(id, name string, result map[string]any) {
	if result == nil {
		result = map[string]any{}
	}
	if idx, ok := a.toolMsgIndex[id]; ok && idx < len(a.messages) {
		a.messages[idx].ToolResult = result
		a.messages[idx].ToolStatus = ""
		a.messages[idx].ToolPending = false
		return
	}
	// Shouldn't happen — a result always follows its own call, whose
	// ToolCall event already created this entry — but mirrors
	// upsertToolMessage's fallback rather than silently dropping a
	// result with nowhere to attach to.
	a.newToolMessage(id, ChatMessage{ToolName: name, ToolResult: result, At: time.Now()})
}

// newToolMessage appends a fresh RoleTool entry plus the trailing empty
// agent placeholder every tool entry gets — same as the old
// insertToolMessage — a fresh empty placeholder is opened after it for
// whatever prose the agent sends next, dropping the previous placeholder
// first if it never received any text (a tool call visually interrupts
// the streaming reply in progress, so that reply's text shouldn't keep
// appending into the same bubble as if nothing happened, but an
// untouched placeholder isn't worth preserving as a stray empty bubble).
// Records id -> index in toolMsgIndex so later events for the same call
// find their way back here instead of appending a duplicate entry.
func (a *App) newToolMessage(id string, msg ChatMessage) {
	msg.Role = RoleTool
	a.dropEmptyStreamingPlaceholder()
	a.messages = append(a.messages, msg)
	if a.toolMsgIndex == nil {
		a.toolMsgIndex = map[string]int{}
	}
	a.toolMsgIndex[id] = len(a.messages) - 1

	a.messages = append(a.messages, ChatMessage{Role: RoleAgent, Content: "", At: time.Now()})
	a.streamingMsgIndex = len(a.messages) - 1
}

// dropEmptyStreamingPlaceholder removes the in-progress agent placeholder
// if it never received any text — called both when a tool event is about
// to interrupt it and when the stream ends outright (e.g. it closed right
// after a tool result with no closing remarks).
//
// The Role check is not optional: RoleTool messages legitimately have an
// empty Content too (they carry their data in other fields), so checking
// Content alone would delete the very message just inserted in front of
// this one — which is exactly what was happening to every confirmation
// entry, since a confirmation pauses the stream immediately after, and
// that pause's cleanup ran this same check without ever re-pointing
// streamingMsgIndex away from it first.
func (a *App) dropEmptyStreamingPlaceholder() {
	if a.streamingMsgIndex < len(a.messages) &&
		a.messages[a.streamingMsgIndex].Role == RoleAgent &&
		a.messages[a.streamingMsgIndex].Content == "" {
		a.messages = append(a.messages[:a.streamingMsgIndex], a.messages[a.streamingMsgIndex+1:]...)
	}
}

func (a *App) applyTheme() {
	a.styles = a.themeMgr.Styles()
	// Keeps the boot banner's "theme" row in sync too — every caller
	// (a real /theme confirm, a live preview while arrowing through the
	// menu, cancelMenu reverting a preview) goes through here, so this
	// is the one place that needs to know about BootInfo.Theme at all.
	a.bootInfo.Theme = a.themeMgr.Current().Name
	a.viewport.Style = a.styles.Viewport
	applyInputStyles(&a.input, a.styles)
	if a.paletteKind != paletteNone {
		restylePalette(&a.paletteList, a.styles)
	}
	a.refreshTranscript()
}

// refreshTranscript re-renders the transcript into the viewport at
// whatever scroll position the viewport is already at — it never moves
// YOffset itself. This is the one layout() calls on nearly every
// keystroke, resize, and cursor blink, so it has to leave scrolling
// alone; see followTranscript for the variant that's allowed to move it.
func (a *App) refreshTranscript() {
	content, userMsgLines := renderTranscript(a.styles, a.messages, a.viewport.Width(), a.highlightUser, a.verboseTools, a.showReasoning)
	a.viewport.SetContent(content)
	a.userMsgLines = userMsgLines
}

// followTranscript is refreshTranscript's counterpart for the call sites
// that just appended genuinely new content (a reply, a streamed chunk, a
// system event). Only here — not in the generic keystroke/resize refresh
// above — do we ask "was the user following the conversation?" and, if
// so, keep following, all the way to the true bottom, same as always.
//
// Keeping the last prompt visible during an oversized response is handled
// separately, in View() — as a sticky overlay pinned over whatever's
// scrolled beneath it, not by capping where auto-follow can scroll to.
func (a *App) followTranscript() {
	wasAtBottom := a.viewport.AtBottom()
	a.refreshTranscript()
	if wasAtBottom {
		a.viewport.GotoBottom()
	}
}

// stickyPromptOverlay returns the pinned "you: ..." strip to composite
// over the top of the viewport in View(), or "" if there's nothing to
// pin — either no prompt's been sent yet, or the last one is already
// visible at (or below) the current scroll position, in which case
// overlaying it too would just draw a duplicate on top of itself.
func (a *App) stickyPromptOverlay() string {
	n := len(a.userMsgLines)
	if n == 0 || a.lastPromptText == "" {
		return ""
	}
	if a.viewport.YOffset() <= a.userMsgLines[n-1] {
		return ""
	}
	return renderStickyPrompt(a.styles, a.lastPromptText, a.viewport.Width())
}

// jumpToPrevPrompt / jumpToNextPrompt back PgUp/PgDn: rather than
// scrolling a fixed page height, they jump straight to the start of the
// nearest earlier/later user message. Falls back to a plain page
// scroll when there's no prompt in that direction — the top of the
// conversation still has the boot banner above the first one, and the
// bottom has nothing past the last one.
func (a *App) jumpToPrevPrompt() {
	for i := len(a.userMsgLines) - 1; i >= 0; i-- {
		if a.userMsgLines[i] < a.viewport.YOffset() {
			a.viewport.SetYOffset(a.userMsgLines[i])
			return
		}
	}
	a.viewport.PageUp()
}

func (a *App) jumpToNextPrompt() {
	for _, line := range a.userMsgLines {
		if line > a.viewport.YOffset() {
			a.viewport.SetYOffset(line)
			return
		}
	}
	a.viewport.PageDown()
}
