package ui

import (
	"context"
	"testing"
)

// These are the queueing/interrupt state-transition tests — the async
// message-ordering logic in sendMessage/popQueuedMessage/interruptAndSend
// is exactly the kind of thing worth pinning down with a real test rather
// than trusting a read-through, per feedback_testing-scope: it fixes a
// real concurrency gap (turnInProgress used to miss non-streaming Send
// turns and the async dispatch-to-streamStartMsg window entirely), not
// routine wiring.

// noopBackend is a minimal Backend stub — nothing here needs a live
// agent, only something non-nil for dispatchToBackend/resolveConfirmation
// to call so their real branching runs the same as it would live.
type noopBackend struct{}

func (noopBackend) Send(ctx context.Context, sessionID, message string) (string, error) {
	return "", nil
}

func (noopBackend) Stream(ctx context.Context, sessionID, message string) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (noopBackend) RespondToConfirmation(ctx context.Context, sessionID string, decisions []ConfirmationDecision) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (noopBackend) ListSessions(ctx context.Context) ([]SessionSummary, error) { return nil, nil }

func (noopBackend) GetTranscript(ctx context.Context, sessionID string) ([]TranscriptEntry, error) {
	return nil, nil
}

func (noopBackend) DeleteSession(ctx context.Context, sessionID string) error { return nil }
func (noopBackend) ModelName() string                                         { return "" }
func (noopBackend) Specialists() []string                                     { return nil }
func (noopBackend) ContextWindow() int                                        { return 0 }

// newTestAppWithBackend builds on newTestApp (menustack_test.go) with
// the two extra pieces turn.go's send/interrupt path actually touches —
// a real input textarea (sendMessage calls SetValue on it) and a
// backend (dispatchToBackend needs a non-nil one to call).
func newTestAppWithBackend() *App {
	a := newTestApp()
	a.backend = noopBackend{}
	a.input = newInput(a.styles)
	return a
}

// TestSendMessageQueuesWhileTurnInProgressWithoutTouchingTranscript is
// the behavior the user actually asked for on a second pass: a queued
// message must NOT appear in the scrolling transcript at all (no bubble,
// no "Queued" system note) — only the pinned queuedPromptOverlay shows
// it exists, until popQueuedMessage actually sends it later.
func TestSendMessageQueuesWhileTurnInProgressWithoutTouchingTranscript(t *testing.T) {
	a := newTestAppWithBackend()
	a.turnCancel = func() {} // simulate a turn already running
	messagesBefore := len(a.messages)

	cmd := a.sendMessage("second message")
	if cmd != nil {
		t.Error("sendMessage while a turn is running should not return a dispatch cmd")
	}
	if len(a.queuedMessages) != 1 || a.queuedMessages[0] != "second message" {
		t.Errorf("queuedMessages = %v, want [\"second message\"]", a.queuedMessages)
	}
	if len(a.messages) != messagesBefore {
		t.Errorf("a.messages grew from %d to %d — queuing must not touch the transcript", messagesBefore, len(a.messages))
	}
}

func TestSendMessageDispatchesWhenIdle(t *testing.T) {
	a := newTestAppWithBackend()
	cmd := a.sendMessage("hello")
	if cmd == nil {
		t.Fatal("sendMessage with no turn running should return a dispatch cmd")
	}
	if a.turnCancel == nil {
		t.Error("dispatchToBackend should have set turnCancel")
	}
	if len(a.queuedMessages) != 0 {
		t.Errorf("queuedMessages = %v, want empty", a.queuedMessages)
	}
	if a.messages[len(a.messages)-1].Content != "hello" {
		t.Error("an immediately-sent message should appear in the transcript right away")
	}
}

// TestPopQueuedMessageDrainsInOrderAndAppendsOnSend covers both the FIFO
// drain order and that each pop is the moment the message *first*
// appears in the transcript — appended fresh, same as sendNow's ordinary
// send path, not just an update to something already sitting there.
func TestPopQueuedMessageDrainsInOrderAndAppendsOnSend(t *testing.T) {
	a := newTestAppWithBackend()
	a.queuedMessages = []string{"first", "second"}
	messagesBefore := len(a.messages)

	if cmd := a.popQueuedMessage(); cmd == nil {
		t.Fatal("expected a dispatch cmd for the first queued message")
	}
	if len(a.queuedMessages) != 1 || a.queuedMessages[0] != "second" {
		t.Errorf("queuedMessages after one pop = %v, want [\"second\"]", a.queuedMessages)
	}
	if got := len(a.messages) - messagesBefore; got != 1 {
		t.Fatalf("expected exactly one new message appended by the pop, got %d", got)
	}
	if a.messages[len(a.messages)-1].Content != "first" {
		t.Errorf("appended message = %q, want %q (the oldest queued text)", a.messages[len(a.messages)-1].Content, "first")
	}

	if cmd := a.popQueuedMessage(); cmd == nil {
		t.Fatal("expected a dispatch cmd for the second queued message")
	}
	if len(a.queuedMessages) != 0 {
		t.Errorf("queuedMessages after draining = %v, want empty", a.queuedMessages)
	}
	if a.messages[len(a.messages)-1].Content != "second" {
		t.Errorf("appended message = %q, want %q", a.messages[len(a.messages)-1].Content, "second")
	}

	if cmd := a.popQueuedMessage(); cmd != nil {
		t.Error("popQueuedMessage on an empty queue should return nil")
	}
}

// TestInterruptAndSendReplacesQueueNotAppends is the one behavior that's
// genuinely different from plain queuing: interrupting means redirecting
// right now, not getting in line behind whatever else was already
// waiting — and, since nothing queued ever touches the transcript until
// it's actually sent (see the test above), replacing the queue leaves no
// trace of either the abandoned "already waiting" prompt or the new
// "redirect now" one as a RoleUser bubble (stopTurn's own "Stopping..."
// system note is expected, so messages does grow by exactly that one).
func TestInterruptAndSendReplacesQueueNotAppends(t *testing.T) {
	a := newTestAppWithBackend()
	a.turnCancel = func() {}
	a.sendMessage("already waiting")

	cmd := a.interruptAndSend("redirect now")
	if cmd != nil {
		t.Error("interrupting a plain running turn should just stop it — the redirect fires later, once the stop lands")
	}
	if len(a.queuedMessages) != 1 || a.queuedMessages[0] != "redirect now" {
		t.Errorf("queuedMessages = %v, want [\"redirect now\"] (replaced, not appended)", a.queuedMessages)
	}
	for _, m := range a.messages {
		if m.Role == RoleUser {
			t.Errorf("no RoleUser message should exist yet — found %q", m.Content)
		}
	}
}

func TestInterruptAndSendWithNothingRunningSendsImmediately(t *testing.T) {
	a := newTestAppWithBackend()
	cmd := a.interruptAndSend("just send this")
	if cmd == nil {
		t.Fatal("interrupting with nothing running should behave like an ordinary send")
	}
	if a.turnCancel == nil {
		t.Error("expected dispatchToBackend to have run")
	}
}

func TestInterruptAndSendBareCommandJustStops(t *testing.T) {
	a := newTestAppWithBackend()
	stopped := false
	a.turnCancel = func() { stopped = true }

	if cmd := a.interruptAndSend(""); cmd != nil {
		t.Error("bare /interrupt with a plain turn running should return nil")
	}
	if !stopped {
		t.Error("bare /interrupt should have called turnCancel")
	}
	if len(a.queuedMessages) != 0 {
		t.Errorf("queuedMessages = %v, want empty — no prompt was given to redirect to", a.queuedMessages)
	}
}

// TestInterruptAndSendDuringHITLPauseDeniesInstead covers the case a
// live network call can't be cancelled — the run is blocked waiting on
// us, not on the backend — so interrupting has to deny the pending
// confirmation instead, same as ctrl+c already does in that state.
func TestInterruptAndSendDuringHITLPauseDeniesInstead(t *testing.T) {
	a := newTestAppWithBackend()
	a.pendingConfirmation = &pendingConfirmation{id: "c1", tool: "write_file", msgIndex: 0}
	a.messages = []ChatMessage{{Role: RoleTool, ToolName: "write_file"}}
	a.toolMsgIndex = map[string]int{"c1": 0}

	a.interruptAndSend("do something else")

	if a.pendingConfirmation != nil {
		t.Error("interrupting during a HITL pause should resolve (deny) the pending confirmation")
	}
	if len(a.queuedMessages) != 1 || a.queuedMessages[0] != "do something else" {
		t.Errorf("queuedMessages = %v, want the redirect prompt queued for once things settle", a.queuedMessages)
	}
}
