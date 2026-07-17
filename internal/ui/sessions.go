package ui

import (
	"context"
	"fmt"
	"time"

	"tui-testing/internal/theme"

	tea "charm.land/bubbletea/v2"
)

// turnInProgress reports whether a turn is actively running (streaming
// or a plain blocking Send — see the note below) or paused on a HITL
// confirmation — either way it's mid-flight against the *current*
// a.sessionID, so switching sessions out from under it would mean a
// stale streamChunkMsg or resolveConfirmation call landing on the wrong
// session (or a freshly-reset, unrelated transcript). /new and
// /sessions both refuse while this is true rather than trying to cancel
// and switch — cancelling an in-flight turn cleanly is a separate
// feature (see /interrupt in turn.go).
//
// Checks turnCancel rather than streamChan for two reasons, not just
// one: streamChan is never set at all in non-streaming (Send) mode, so
// the old streamChan-based check silently reported "not busy" for the
// whole duration of a non-streaming turn — a real gap, not just an
// asymmetry. turnCancel also covers both modes and, just as importantly,
// is set *synchronously* the moment dispatchToBackend runs, before its
// tea.Cmd even starts — streamChan only becomes non-nil once
// streamStartMsg lands, a whole async round trip later. Checking
// streamChan left a real window, between calling dispatchToBackend and
// that message arriving, where a second send would race the first
// (see sendMessage's own turnInProgress guard) instead of being queued.
func (a *App) turnInProgress() bool {
	return a.turnCancel != nil || a.pendingConfirmation != nil
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
	a.queuedMessages = nil
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

// openDeleteSessionConfirm opens /sessions' DEL-key confirm popup for one
// session — reached from the /sessions list itself (see keyrouting.go's
// handlePaletteKey). Escape/Cancel returns to that list rather than
// closing the whole popup, the same pushMenuBack pattern every other
// nested step in this app uses.
func (a *App) openDeleteSessionConfirm(id string) {
	a.deleteSessionTarget = id
	a.pushMenuBack(func() tea.Cmd { return a.openSessionsMenu() })
	a.openMenu(paletteConfirmDeleteSession, "Delete session "+shortSessionID(id)+"?", []paletteItem{
		{id: "delete", title: "Delete"},
		{id: "cancel", title: "Cancel"},
	})
}

// openDeleteAllSessionsConfirm is openDeleteSessionConfirm's ctrl+DEL
// counterpart — deleteSessionTarget "" is the sentinel confirmDeleteSession
// reads to mean "every session" rather than one specific ID.
func (a *App) openDeleteAllSessionsConfirm(count int) {
	a.deleteSessionTarget = ""
	a.pushMenuBack(func() tea.Cmd { return a.openSessionsMenu() })
	a.openMenu(paletteConfirmDeleteSession, fmt.Sprintf("Delete ALL %d sessions?", count), []paletteItem{
		{id: "delete", title: "Delete"},
		{id: "cancel", title: "Cancel"},
	})
}

// confirmDeleteSession runs the DEL/ctrl+DEL confirm popup's chosen row —
// id is "delete" or "cancel"; Cancel is a no-op here, since backOrClose
// (already run by confirmMenuSelection's caller) pops back to the
// /sessions list on its own, unchanged data and all.
//
// Delete deliberately does NOT reopen /sessions itself the way every
// other nested step's pushMenuBack would — whether there's anything left
// to show isn't known until the delete actually finishes, so
// openDeleteSessionConfirm/openDeleteAllSessionsConfirm's pushed reopen
// is discarded here via closeMenu (back to the plain chat view) and
// sessionsDeletedMsg's handler in Update decides whether to reopen
// /sessions once the real remaining count comes back with it — that's
// also what fixes the bug where deleting the last session(s) landed back
// on a list that still (stale) showed them.
func (a *App) confirmDeleteSession(id string) tea.Cmd {
	if id != "delete" {
		return nil
	}
	a.closeMenu()
	backend := a.backend
	target := a.deleteSessionTarget

	if target != "" {
		a.systemMessage("Deleting session...")
		return func() tea.Msg {
			ctx := context.Background()
			err := backend.DeleteSession(ctx, target)
			if err != nil {
				return sessionsDeletedMsg{err: err, remaining: -1}
			}
			remaining := -1
			if sessions, lerr := backend.ListSessions(ctx); lerr == nil {
				remaining = len(sessions)
			}
			return sessionsDeletedMsg{deletedIDs: []string{target}, remaining: remaining}
		}
	}

	a.systemMessage("Deleting all sessions...")
	return func() tea.Msg {
		ctx := context.Background()
		sessions, err := backend.ListSessions(ctx)
		if err != nil {
			return sessionsDeletedMsg{err: err, remaining: -1}
		}
		var deleted []string
		var firstErr error
		for _, s := range sessions {
			if err := backend.DeleteSession(ctx, s.ID); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			deleted = append(deleted, s.ID)
		}
		remaining := -1
		if after, lerr := backend.ListSessions(ctx); lerr == nil {
			remaining = len(after)
		}
		return sessionsDeletedMsg{deletedIDs: deleted, remaining: remaining, err: firstErr}
	}
}

// sessionsDeletedMsg carries confirmDeleteSession's async result.
// deletedIDs is whatever actually got removed — for a delete-all batch,
// err (if non-nil) is only the *first* failure hit; the rest of the
// batch is still attempted rather than aborting partway through.
// remaining is a fresh post-delete session count (-1 if it couldn't be
// determined) — Update reopens /sessions only when it's > 0, so deleting
// the last session(s) lands back on the plain chat view instead of a
// list that (stale) still shows what was just removed.
type sessionsDeletedMsg struct {
	deletedIDs []string
	remaining  int
	err        error
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
