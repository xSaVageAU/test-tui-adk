package ui

import (
	"context"
	"fmt"
	"time"

	"tui-testing/internal/theme"

	tea "charm.land/bubbletea/v2"
)

// turnInProgress reports whether a turn is actively streaming or paused
// on a HITL confirmation — either way it's mid-flight against the
// *current* a.sessionID, so switching sessions out from under it would
// mean a stale streamChunkMsg or resolveConfirmation call landing on the
// wrong session (or a freshly-reset, unrelated transcript). /new and
// /sessions both refuse while this is true rather than trying to cancel
// and switch — cancelling an in-flight turn cleanly is a separate,
// not-yet-built feature (see the interrupt-and-redirect idea).
func (a *App) turnInProgress() bool {
	return a.streamChan != nil || a.pendingConfirmation != nil
}

// resetTranscriptState clears everything tied to the conversation
// currently on screen — shared by startNewSession and switchSession,
// which differ only in which sessionID gets set and what they tell the
// user about it.
func (a *App) resetTranscriptState() {
	a.messages = nil
	a.streamingMsgIndex = 0
	a.toolMsgIndex = nil
	a.pendingConfirmation = nil
	a.confirmQueue = nil
	a.confirmDecisions = nil
	a.turnCancel = nil
	a.turnUsage, a.turnFinishReason = nil, ""
	a.contextUsed = 0
	a.lastPromptText = ""
	a.status = theme.StatusIdle
}

// startNewSession begins a fresh conversation: a new session ID and an
// empty transcript, same as what NewApp sets up at launch. Purely
// local — ADK's AutoCreateSession means nothing is actually created on
// the backend until the first message is sent under this ID.
func (a *App) startNewSession() {
	if a.turnInProgress() {
		a.systemMessage("Wait for the current response to finish before starting a new session.")
		return
	}
	a.sessionID = newSessionID()
	a.resetTranscriptState()
	a.systemMessage("Started a new session.")
}

// switchSession changes which session subsequent messages go to, and
// kicks off replaying its stored history back into the transcript.
// Purely local as far as which session subsequent messages go to — ADK
// resolves a session by ID transparently on the next Send/Stream call,
// no "activate" call needed on the backend — but populating what's
// already on screen needs one GetTranscript round trip, landed via
// transcriptLoadedMsg (see Update) rather than blocking here.
func (a *App) switchSession(id string) tea.Cmd {
	a.sessionID = id
	a.resetTranscriptState()
	a.systemMessage("Switched to " + shortSessionID(id) + " — loading history...")

	backend := a.backend
	return func() tea.Msg {
		entries, err := backend.GetTranscript(context.Background(), id)
		return transcriptLoadedMsg{sessionID: id, entries: entries, err: err}
	}
}

// transcriptLoadedMsg carries switchSession's async GetTranscript
// result. sessionID is checked against a.sessionID before it's applied
// (see Update) — a second switch fired before the first's round trip
// lands would otherwise stamp a stale reply onto whatever's now current.
type transcriptLoadedMsg struct {
	sessionID string
	entries   []TranscriptEntry
	err       error
}

// replayTranscript rebuilds a.messages from a session's full stored
// history — GetTranscript's ordered entries, replayed through the same
// upsertToolMessage/completeToolMessage bookkeeping live streaming uses
// (see transcript.go), just synchronously in one pass instead of
// arriving one event at a time over a channel. A user-text entry opens a
// fresh agent placeholder right after it, mirroring sendMessage +
// streamStartMsg's combined effect for a live turn, so every following
// Text/ToolCall entry always has a legitimate open target to land in.
func (a *App) replayTranscript(entries []TranscriptEntry) {
	for _, e := range entries {
		switch {
		case e.UserText != "":
			a.dropEmptyStreamingPlaceholder()
			a.messages = append(a.messages, ChatMessage{Role: RoleUser, Content: e.UserText, At: time.Now()})
			a.messages = append(a.messages, ChatMessage{Role: RoleAgent, Content: "", At: time.Now()})
			a.streamingMsgIndex = len(a.messages) - 1
		case e.ToolCall != nil:
			a.upsertToolMessage(e.ToolCall.ID, e.ToolCall.Name, e.ToolCall.Args, "", false)
		case e.ToolResult != nil:
			a.completeToolMessage(e.ToolResult.ID, e.ToolResult.Name, e.ToolResult.Result)
		case e.Text != "":
			if a.streamingMsgIndex < len(a.messages) {
				a.messages[a.streamingMsgIndex].Content += e.Text
			}
		}
	}
	a.dropEmptyStreamingPlaceholder()
}

// sessionsLoadedMsg carries the result of the async ListSessions call
// openSessionsMenu kicks off — unlike every other /command menu here,
// this one needs a backend round-trip before there's anything to show,
// so the popup itself only opens once this arrives (see Update).
type sessionsLoadedMsg struct {
	sessions []SessionSummary
	err      error
}

// openSessionsMenu kicks off the async ListSessions call backing
// /sessions. Returns a tea.Cmd rather than opening the popup directly.
func (a *App) openSessionsMenu() tea.Cmd {
	if a.backend == nil {
		a.systemMessage("No backend connected — use /key first.")
		return nil
	}
	if a.turnInProgress() {
		a.systemMessage("Wait for the current response to finish before switching sessions.")
		return nil
	}

	backend := a.backend
	return func() tea.Msg {
		sessions, err := backend.ListSessions(context.Background())
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

// relativeTime renders a compact "how long ago" label for the /sessions
// picker — good enough for telling recent conversations apart without
// needing a full timestamp; falls back to an absolute date once it's
// old enough that "N days ago" stops being the more readable option.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		n := int(d / time.Minute)
		return fmt.Sprintf("%d min ago", n)
	case d < 24*time.Hour:
		n := int(d / time.Hour)
		return fmt.Sprintf("%d hr ago", n)
	case d < 7*24*time.Hour:
		n := int(d / (24 * time.Hour))
		if n == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", n)
	default:
		return t.Format("Jan 2, 2006")
	}
}
