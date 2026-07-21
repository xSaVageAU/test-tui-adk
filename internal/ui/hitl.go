package ui

import (
	"context"
	"fmt"

	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// hitlMode picks how a pending tool-approval request is presented — both
// share the same underlying data flow (App.pendingConfirmation,
// insertConfirmMessage, resolveConfirmation); only the UI differs. Cycled
// via /settings so the two can be compared live instead of guessed at.
//
// A third "hybrid" mode (inline marker, Enter opens the same popup Modal
// uses) existed here and was cut after trying all three live — it added a
// state without adding a real choice: you still ended up at the popup.
type hitlMode int

const (
	hitlModal hitlMode = iota
	hitlInline
)

func (m hitlMode) String() string {
	if m == hitlInline {
		return "inline"
	}
	return "modal"
}

func (m hitlMode) next() hitlMode {
	return (m + 1) % 2
}

// parseHITLMode is String's inverse, used when restoring hitlMode from
// settings.json — anything other than exactly "inline" (including "",
// an old/malformed file, or a future value this build doesn't know
// about) falls back to the modal default rather than erroring.
func parseHITLMode(s string) hitlMode {
	if s == "inline" {
		return hitlInline
	}
	return hitlModal
}

// The exact strings a RoleTool message's ToolStatus holds while a
// confirmation is in play. Centralized here (rather than let chat.go
// infer state from arbitrary text) so renderTool's color switch and the
// code that sets these can't drift apart.
const (
	confirmStatusPendingModal  = "awaiting your decision"
	confirmStatusPendingInline = "awaiting approval — y to allow, n to deny"
	confirmStatusApproved      = "approved — running…"
	confirmStatusDenied        = "denied"

	// toolStatusRunning is a call's status before anything else — a
	// confirmation request or a result — updates it.
	toolStatusRunning = "running…"
)

// pendingConfirmation is one tool-approval request. App.pendingConfirmation
// tracks whichever one is currently being shown/decided; App.confirmQueue
// holds any more from the same parallel batch waiting their turn — see
// insertConfirmMessage/resolveConfirmation.
type pendingConfirmation struct {
	id       string // pass back to Backend.RespondToConfirmation
	tool     string
	args     map[string]any
	msgIndex int // index into App.messages of the call's RoleTool entry
}

func (a *App) pendingStatusText() string {
	if a.hitlMode == hitlInline {
		return confirmStatusPendingInline
	}
	return confirmStatusPendingModal
}

// insertConfirmMessage folds a tool-approval request into the same
// transcript entry its originating call already created — keyed by
// OriginalID, the ID that call's own ToolCall.ID carried — rather than
// appending a second entry for what's really one invocation.
//
// If nothing's currently pending, this one becomes pendingConfirmation
// right away (opening the popup immediately in Modal mode) exactly as
// before. If something's already showing — a parallel batch delivering a
// second or third request before the first has been decided — this one
// queues instead of replacing it; resolveConfirmation activates queued
// entries one at a time as each is decided. Every request in the
// transcript gets its "awaiting decision" status the moment it arrives
// either way, queued or not.
func (a *App) insertConfirmMessage(c *ToolConfirmationRequest) {
	key := c.OriginalID
	if key == "" {
		key = c.ID
	}
	a.upsertToolMessage(key, c.Tool, c.Args, a.pendingStatusText(), true)
	pc := &pendingConfirmation{
		id:       c.ID,
		tool:     c.Tool,
		args:     c.Args,
		msgIndex: a.toolMsgIndex[key],
	}

	if a.pendingConfirmation != nil {
		a.confirmQueue = append(a.confirmQueue, pc)
		return
	}
	a.pendingConfirmation = pc
	if a.hitlMode == hitlModal {
		a.openConfirmModal()
	}
}

// openConfirmModal shows the Approve/Deny popup — Modal mode opens this
// automatically the moment a confirmation request arrives, and again for
// each subsequent request in the same batch as resolveConfirmation
// activates it. The "(N of M)" suffix only shows once there's more than
// one in the batch, so a lone confirmation looks exactly as it always
// has.
func (a *App) openConfirmModal() {
	pc := a.pendingConfirmation
	title := fmt.Sprintf("Approve %s(%s)?", pc.tool, formatKV(pc.args))
	if total := len(a.confirmDecisions) + 1 + len(a.confirmQueue); total > 1 {
		title = fmt.Sprintf("%s (%d of %d)", title, len(a.confirmDecisions)+1, total)
	}
	items := []paletteItem{
		{id: "approve", title: "Approve"},
		{id: "deny", title: "Deny"},
	}
	a.openMenu(paletteConfirm, title, items)
}

// handleConfirmModalKey runs while the Approve/Deny popup has focus.
// Distinct from handlePaletteKey because Escape here needs to resolve the
// confirmation (as a denial) rather than just close the menu, which means
// it — unlike every other popup's Escape — has to return a tea.Cmd.
func (a *App) handleConfirmModalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Escape):
		return a, a.resolveConfirmation(false)

	case key.Matches(msg, keys.Send):
		approved := false
		if item, ok := a.paletteList.SelectedItem().(paletteItem); ok {
			approved = item.id == "approve"
		}
		return a, a.resolveConfirmation(approved)
	}

	var cmd tea.Cmd
	a.paletteList, cmd = a.paletteList.Update(msg)
	return a, cmd
}

// handlePendingConfirmKey runs while a confirmation is pending with no
// popup showing for it — which in practice only happens in Inline mode
// (Modal opens its popup the instant the confirmation arrives, so
// paletteKind == paletteConfirm takes priority over this in handleKey's
// switch before this is ever reached). y/n resolve it; everything else is
// swallowed — the turn really is blocked until this resolves, so there's
// nothing else useful for a keypress to do here.
func (a *App) handlePendingConfirmKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		return a, a.resolveConfirmation(true)
	case "n":
		return a, a.resolveConfirmation(false)
	}
	return a, nil
}

// resolveConfirmation records the decision on the pending confirmation's
// transcript entry, then either activates the next request queued from
// the same parallel batch (see insertConfirmMessage) — leaving the run
// paused — or, once every request in the batch has an answer, sends them
// all to Backend.RespondToConfirmation together and resumes the run,
// which streams exactly like a fresh message send, so the result rides
// the existing streamStartMsg/streamChunkMsg machinery rather than
// needing a parallel path. See RespondToConfirmation's doc comment for
// why the batch has to go in one round trip rather than as each is
// decided.
func (a *App) resolveConfirmation(approved bool) tea.Cmd {
	pc := a.pendingConfirmation
	if pc == nil {
		return nil
	}

	status := confirmStatusDenied
	if approved {
		status = confirmStatusApproved
	}
	if pc.msgIndex < len(a.messages) {
		a.messages[pc.msgIndex].ToolStatus = status
		a.messages[pc.msgIndex].ToolPending = false
		a.touchMessages()
	}
	a.confirmDecisions = append(a.confirmDecisions, ConfirmationDecision{ID: pc.id, Approved: approved})

	if len(a.confirmQueue) > 0 {
		a.pendingConfirmation, a.confirmQueue = a.confirmQueue[0], a.confirmQueue[1:]
		if a.hitlMode == hitlModal {
			a.openConfirmModal()
		}
		a.followTranscript()
		return nil
	}

	a.pendingConfirmation = nil
	a.closeMenu()
	decisions := a.confirmDecisions
	a.confirmDecisions = nil

	a.status = theme.StatusThinking
	a.workingLabel = "thinking"
	// Should already be false by this point — a tool call ends reasoning
	// the moment it arrives, and a confirmation only ever follows one —
	// but going through endReasoning rather than a raw a.reasoning=false
	// keeps the stopwatch's own running flag from ever going stale, in
	// case some future ordering doesn't hold.
	reasonCmd := a.endReasoning()
	a.followTranscript()

	backend := a.backend
	sessionID := a.sessionID
	animCmd := a.startWorkingAnim()

	ctx, cancel := context.WithCancel(context.Background())
	a.turnCancel = cancel

	return tea.Batch(animCmd, reasonCmd, func() tea.Msg {
		ch, err := backend.RespondToConfirmation(ctx, sessionID, decisions)
		if err != nil {
			return agentReplyMsg{err: err}
		}
		return streamStartMsg{ch: ch}
	})
}
