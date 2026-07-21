// This file owns one turn's whole lifecycle: how Enter becomes either a
// "/command" or a real message (handleSend/sendMessage), how that
// message reaches the backend (dispatchToBackend), how it can be
// cancelled mid-flight (stopTurn), the message types the resulting
// stream/reply produces, and the usage/reasoning bookkeeping that
// accumulates across a turn's model calls. Update (app.go) is what
// actually consumes the message types defined here; this file is where
// they're produced and where a turn's own state (turnUsage,
// turnFinishReason, reasoning, turnCancel) gets updated in response to
// them, plus what's needed to actually kick off/redirect keystrokes into
// this pipeline via handleSend.
package ui

import (
	"context"
	"strings"
	"time"

	"tui-testing/internal/theme"

	"charm.land/bubbles/v2/stopwatch"
	tea "charm.land/bubbletea/v2"
)

// handleSend runs on Enter outside a popup menu: a leading "/" makes the
// input a command instead of a chat message.
func (a *App) handleSend() tea.Cmd {
	text := strings.TrimSpace(a.input.Value())
	if text == "" {
		return nil
	}

	if name, ok := strings.CutPrefix(text, "/"); ok {
		a.input.SetValue("")
		cmd := a.runCommand(name)
		a.layout()
		return cmd
	}

	return a.sendMessage(text)
}

func (a *App) sendMessage(text string) tea.Cmd {
	a.input.SetValue("")
	a.layout()

	if a.backend == nil {
		// Held rather than dropped or errored: the /key popup opens right
		// here, and the keySetMsg handler in Update sends this same text
		// the moment a key is successfully set. Appended to the
		// transcript right away, unlike the queued case below — there's
		// no turn to hide behind here, the message is just waiting on a
		// key, not competing with one already in flight.
		a.messages = append(a.messages, ChatMessage{Role: RoleUser, Content: text, At: time.Now()})
		a.touchMessages()
		a.lastPromptText = text
		a.viewport.GotoBottom()
		a.pendingMessage = text
		a.openKeyProviderMenu()
		return nil
	}

	// A turn already running (streaming, a plain blocking Send, or paused
	// on a HITL confirmation) means dispatching now would race it — two
	// concurrent calls would both write through the single shared
	// turnCancel/streamChan/streamingMsgIndex, corrupting whichever one
	// loses. Queue instead, without touching the transcript at all — only
	// the pinned queuedPromptOverlay (transcript.go) shows it exists until
	// popQueuedMessage below actually sends it, at which point it appears
	// in the transcript for the first time, exactly as if it had just
	// been typed.
	if a.turnInProgress() {
		a.queuedMessages = append(a.queuedMessages, text)
		return nil
	}

	return a.sendNow(text)
}

// sendNow is the actual "send" step — append text as a fresh user
// message and dispatch it — shared by sendMessage's immediate-send path
// and popQueuedMessage, which reaches here once the turn that was
// blocking a queued message has genuinely ended.
func (a *App) sendNow(text string) tea.Cmd {
	a.messages = append(a.messages, ChatMessage{Role: RoleUser, Content: text, At: time.Now()})
	a.touchMessages()
	a.lastPromptText = text
	// Sending is a deliberate action, unlike the steady trickle of
	// keystroke-driven refreshes elsewhere — always follow it to the
	// bottom even if you'd scrolled up to read history first.
	a.viewport.GotoBottom()
	return a.dispatchToBackend(text)
}

// popQueuedMessage sends the next message queued by sendMessage (or set
// by /interrupt), if any — called from every place a turn genuinely
// concludes: a normal finish, an error, or a stop, but *not* a HITL
// pause (turnInProgress is still true then; resolveConfirmation's own
// resumption is what eventually reaches one of those real endings).
// Returns nil, a no-op, when nothing's queued.
func (a *App) popQueuedMessage() tea.Cmd {
	if len(a.queuedMessages) == 0 {
		return nil
	}
	next := a.queuedMessages[0]
	a.queuedMessages = a.queuedMessages[1:]
	return a.sendNow(next)
}

// concludeTurn is the actual call site, at all four places in Update a
// turn genuinely ends, for both popQueuedMessage and a pending
// reload_agents request (see App.reloadRequested) — settled together
// since both need the exact same "turnInProgress is false again" moment:
// popQueuedMessage to avoid racing the turn that was just running, and
// reloadRequested because reloadBackend()'s own guard would silently
// no-op if called any earlier (still mid-turn).
func (a *App) concludeTurn() tea.Cmd {
	cmd := a.popQueuedMessage()
	if a.reloadRequested {
		a.reloadRequested = false
		cmd = tea.Batch(cmd, a.reloadBackend())
	}
	return cmd
}

// dispatchToBackend sends text to the current backend, streamed or in one
// shot depending on streamReplies, and returns the tea.Cmd that kicks
// that off. Shared by sendMessage's normal path and by the keySetMsg
// handler resuming a held message.
func (a *App) dispatchToBackend(text string) tea.Cmd {
	a.status = theme.StatusThinking
	a.workingLabel = "thinking"
	a.reasoning = false
	a.turnUsage, a.turnFinishReason = nil, ""
	backend := a.backend
	animCmd := a.startWorkingAnim()

	ctx, cancel := context.WithCancel(context.Background())
	a.turnCancel = cancel

	if !a.streamReplies {
		return tea.Batch(animCmd, func() tea.Msg {
			reply, err := backend.Send(ctx, a.sessionID, text)
			if err != nil {
				return agentReplyMsg{err: err}
			}
			return agentReplyMsg{text: reply}
		})
	}

	return tea.Batch(animCmd, func() tea.Msg {
		ch, err := backend.Stream(ctx, a.sessionID, text)
		if err != nil {
			return agentReplyMsg{err: err}
		}
		return streamStartMsg{ch: ch}
	})
}

// stopTurn cancels whichever Send/Stream/RespondToConfirmation call is
// currently in flight (see turnCancel) — ctrl+c's handler while the
// agent is working (see keyrouting.go's handleKey). Cancellation itself
// is async: the backend call notices ctx.Done() and returns a
// context.Canceled-wrapped error same as any other failure, so the
// actual teardown (clearing streamChan, ending the working animation)
// happens once that arrives at streamChunkMsg/agentReplyMsg's handlers
// in Update, worded as a deliberate stop there rather than an error.
func (a *App) stopTurn() {
	a.turnCancel()
	// Transient progress — the definitive "Stopped." transcript marker
	// still lands when the cancellation actually completes (see Update's
	// agentReplyMsg/streamChunkMsg handlers).
	a.setNotice("Stopping...")
}

// interruptAndSend is /interrupt's handler — stop whatever's running
// right now and, if given a prompt, redirect to it the instant that
// stop actually lands. Unlike sendMessage's own queuing (which waits
// politely for the current turn to finish), this replaces the queue
// outright: redirecting means skipping whatever else was already
// waiting, not getting in line behind it.
//
// A HITL pause has no live network call for turnCancel to cancel — the
// run is blocked on us, not blocked on the backend — so that case is
// handled the same way ctrl+c already treats it (see keyrouting.go's
// handleKey): deny the pending confirmation. Whatever the agent does in
// response to that denial still ends up at one of popQueuedMessage's
// call sites eventually, so the redirect fires once things genuinely
// settle either way.
func (a *App) interruptAndSend(prompt string) tea.Cmd {
	if !a.turnInProgress() {
		if prompt == "" {
			return nil // nothing running, nothing to redirect to either
		}
		return a.sendMessage(prompt)
	}

	if prompt != "" {
		a.queuedMessages = []string{prompt} // replaces, doesn't append — see the doc comment above
	}

	var cmd tea.Cmd
	if a.pendingConfirmation != nil {
		cmd = a.resolveConfirmation(false)
	} else {
		a.stopTurn()
	}
	a.followTranscript()
	return cmd
}

type agentReplyMsg struct {
	text string
	err  error
}

// streamStartMsg carries the channel a fresh Backend.Stream call is
// delivering chunks on.
type streamStartMsg struct {
	ch <-chan StreamChunk
}

// streamChunkMsg wraps one receive from that channel; ok is false once
// the channel's closed (the stream is over).
type streamChunkMsg struct {
	chunk StreamChunk
	ok    bool
}

// readStreamChunk blocks on one channel receive — the standard Bubble
// Tea pattern for draining a channel: each chunk re-arms this same Cmd
// for the next one (see the streamChunkMsg case in Update), so the
// program only ever has one outstanding read at a time.
func readStreamChunk(ch <-chan StreamChunk) tea.Cmd {
	return func() tea.Msg {
		chunk, ok := <-ch
		return streamChunkMsg{chunk: chunk, ok: ok}
	}
}

// accumulateUsage folds one model call's usage into the running total for
// the turn in progress — a turn can invoke the model more than once (e.g.
// once to decide on a tool call, again after the result comes back).
//
// Prompt is NOT summed across calls: each call's PromptTokenCount is
// already a cumulative snapshot of the entire conversation sent to the
// model up to that point (the whole point of a "prompt" is it includes
// everything before it), so a later call's prompt count already contains
// an earlier call's in full. Summing them double-counted that shared
// history — a turn with one intermediate tool call was reporting roughly
// (first call's full context) + (second call's full context, which
// already includes the first) instead of just the latter. Since context
// only grows within a turn, the last call's Prompt is the correct total;
// max is used rather than "just take the latest value" so this stays
// correct even if a chunk somehow arrived out of order.
//
// Output, in contrast, genuinely is new content each call — the first
// call's function-call tokens and the second call's final prose are both
// real generation cost — so that part is correctly summed. Total is
// recomputed from the corrected Prompt/Output rather than summed from
// each call's own Total field, which would carry the same double-count.
func (a *App) accumulateUsage(u *TokenUsage) {
	if a.turnUsage == nil {
		a.turnUsage = &TokenUsage{}
	}
	if u.Prompt > a.turnUsage.Prompt {
		a.turnUsage.Prompt = u.Prompt
	}
	a.turnUsage.Output += u.Output
	a.turnUsage.Total = a.turnUsage.Prompt + a.turnUsage.Output
	a.contextUsed = a.turnUsage.Prompt
}

// reasoningTickInterval drives both the live "thinking Xms/Xs" re-render
// cadence and, indirectly, the finest resolution a burst that ends
// between ticks could show live (the final frozen duration is always
// precise regardless — see endReasoning — this only affects what's
// visible while still counting up). 100ms rather than stopwatch's own
// 1s default: real reasoning bursts turned out to often finish well
// under a second, and at a 1s interval the very first tick usually
// never even fired before reasoning ended, leaving the badge stuck at
// "0s" the whole time and the final duration landing on exactly zero.
const reasoningTickInterval = 100 * time.Millisecond

// startReasoning marks the current streaming message as actively
// reasoning, records when it started, and starts a stopwatch purely as
// a periodic wake-up source for live re-renders (see App.reasoning's
// doc comment for why the displayed duration itself comes from
// reasoningStart, not the stopwatch's own Elapsed()). Idempotent (a
// no-op if already reasoning), since every reasoning chunk in a burst
// hits this same case, not just the first.
func (a *App) startReasoning() tea.Cmd {
	if a.reasoning {
		return nil
	}
	a.reasoning = true
	a.reasoningStart = time.Now()
	a.stopwatch = stopwatch.New(stopwatch.WithInterval(reasoningTickInterval))
	if a.streamingMsgIndex < len(a.messages) {
		a.messages[a.streamingMsgIndex].ReasoningActive = true
		a.messages[a.streamingMsgIndex].ReasoningDuration = 0
		a.touchMessages()
	}
	return a.stopwatch.Start()
}

// endReasoning freezes the current streaming message's reasoning
// duration at the precise wall-clock time elapsed since startReasoning
// and stops the stopwatch — called from every place a turn's reasoning
// phase can end: real text arriving, a tool call, the stream closing,
// or an error. Idempotent (a no-op, returning nil, if reasoning wasn't
// active) so every one of those call sites can call it unconditionally.
func (a *App) endReasoning() tea.Cmd {
	if !a.reasoning {
		return nil
	}
	a.reasoning = false
	if a.streamingMsgIndex < len(a.messages) {
		a.messages[a.streamingMsgIndex].ReasoningActive = false
		a.messages[a.streamingMsgIndex].ReasoningDuration = time.Since(a.reasoningStart)
		a.touchMessages()
	}
	return a.stopwatch.Stop()
}

// attachTurnFinishReason lands the turn's finish reason (if any) on the
// last RoleAgent message it produced, then clears both accumulators.
// Called once the turn is genuinely over (see the streamChunkMsg !ok
// case) — never mid-turn, since a tool call in progress would otherwise
// get an intermediate model call's reason attached to the wrong bubble.
//
// turnUsage itself isn't attached anywhere here — it's a running
// accumulator only, already reflected live in a.contextUsed as it
// updates (see accumulateUsage); nothing renders it per-message.
//
// If the model's very last call in the turn produced no closing prose,
// its placeholder was already dropped by dropEmptyStreamingPlaceholder by
// the time this runs, leaving no RoleAgent message left to attach to —
// rare, and not worth inventing a bubble just to hold a finish reason,
// so it's silently skipped rather than attached somewhere misleading.
func (a *App) attachTurnFinishReason() {
	reason := a.turnFinishReason
	a.turnUsage, a.turnFinishReason = nil, ""
	if reason == "" {
		return
	}
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role == RoleAgent {
			a.messages[i].FinishReason = reason
			a.touchMessages()
			return
		}
	}
}
