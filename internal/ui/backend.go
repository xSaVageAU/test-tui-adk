package ui

import (
	"context"
	"time"
)

// Backend is the entire surface App needs to reach an agent — real or
// mock. *adk.Client satisfies this structurally; ui never imports the adk
// package (or its Gemini/genai dependency tree) directly, only this
// interface (adk imports ui for StreamChunk instead — the implementer
// depending on the consumer's contract types, not the other way around).
// Swapping backends, or running with none at all, never touches this
// package.
type Backend interface {
	// Send sends a single user message within sessionID and returns the
	// agent's reply text, blocking until it's ready.
	Send(ctx context.Context, sessionID, message string) (string, error)

	// Stream is Send's token-by-token counterpart: it returns immediately
	// with a channel of incremental chunks. The channel closes after the
	// final chunk, or after a chunk carrying a non-nil Err — or, if a
	// tool on this turn requires confirmation, after a chunk carrying a
	// non-nil Confirmation instead: the run is paused there until
	// RespondToConfirmation is called with that chunk's ID.
	Stream(ctx context.Context, sessionID, message string) (<-chan StreamChunk, error)

	// RespondToConfirmation answers one or more pending
	// ToolConfirmationRequests from the same turn in a single round trip
	// and resumes the run — approved lets a tool execute, denied reports
	// it as declined. Every decision from one parallel batch (see
	// ToolConfirmationRequest's doc comment) must be sent together: ADK
	// matches answers back to their original calls by scanning the single
	// most recent message for FunctionResponses, so a decision sent in
	// its own separate call would leave any others from the same batch
	// unresolved indefinitely rather than getting picked up by a later
	// call. Returns a channel shaped exactly like Stream's, since
	// resuming can itself produce more text, tool calls, or even another
	// confirmation request before the turn finishes.
	RespondToConfirmation(ctx context.Context, sessionID string, decisions []ConfirmationDecision) (<-chan StreamChunk, error)

	// ListSessions returns every past session for the current user, in
	// no particular order (the caller sorts) — backs the /sessions
	// picker. Listing is metadata-only (ID + last-updated time); it does
	// not include a session's messages.
	ListSessions(ctx context.Context) ([]SessionSummary, error)

	// GetTranscript returns sessionID's full history as an ordered list
	// of already-resolved entries — backs replaying a session's messages
	// back into the transcript on switch (see App.switchSession). Unlike
	// Stream, nothing here is in-flight or partial: the caller can walk
	// the whole result in one synchronous pass instead of draining a
	// channel.
	GetTranscript(ctx context.Context, sessionID string) ([]TranscriptEntry, error)

	// DeleteSession permanently removes sessionID and its history — backs
	// /sessions' DEL-key delete (confirmed via a popup first, see
	// App.openDeleteSessionConfirm). Deleting every session (ctrl+DEL)
	// isn't a separate backend call: the caller already has every ID from
	// ListSessions, so it just calls this once per session instead.
	DeleteSession(ctx context.Context, sessionID string) error

	// ModelName and Specialists report the root agent's resolved model
	// and the sub-agents it loaded — read once at startup and again
	// after every successful reconnect (a fresh /key, or /agents saving
	// a config change) so the boot banner reflects whichever backend is
	// actually live instead of what was true when the app first opened.
	ModelName() string
	Specialists() []string

	// ContextWindow reports the current model's max input tokens (0 if
	// unknown) — backs the top bar's context-usage indicator, read at
	// the same points as ModelName/Specialists.
	ContextWindow() int
}

// SessionSummary is one entry in Backend.ListSessions' result.
type SessionSummary struct {
	ID        string
	UpdatedAt time.Time
}

// TranscriptEntry is one already-settled item from a past session's full
// history — Backend.GetTranscript's building block. Unlike StreamChunk
// (built for an in-flight turn's deltas — partial text, dedup against
// events an in-progress run can emit more than once), every field here
// is a finished value with nothing left to resolve: a session's stored
// history only ever contains fully-aggregated events (see GetTranscript's
// implementation for why), so there's no partial text and no pending
// confirmation to represent — only the tool call/result a resolved one
// left behind. Exactly one field is set per entry, same one-field
// convention as StreamChunk.
type TranscriptEntry struct {
	UserText   string
	Text       string
	ToolCall   *ToolCall
	ToolResult *ToolResult
}

// StreamChunk is one increment of a streamed reply: exactly one field is
// set per chunk (never more than one) — ToolCall/ToolResult/Confirmation/
// Usage/FinishReason aren't deltas the way Text is, each carries a whole
// event. Usage and FinishReason can each arrive more than once per turn,
// since a single turn may invoke the model more than once (e.g. once to
// decide on a tool call, again after the result comes back) — the caller
// is expected to accumulate/track-latest across a turn, not treat either
// as a one-shot final value.
type StreamChunk struct {
	Text         string
	ToolCall     *ToolCall
	ToolResult   *ToolResult
	Confirmation *ToolConfirmationRequest
	Usage        *TokenUsage
	FinishReason string // non-empty only for a notable non-"stop" reason
	// Reasoning is a delta of the model's reasoning/thinking output, kept
	// distinct from Text specifically so the UI never has to guess
	// whether a given Text delta is "real" reply content or the model's
	// internal reasoning — a provider that supports it (Gemini's Thought
	// parts, an OpenRouter reasoning model's reasoning/reasoning_content
	// field) tags it at the source; see internal/adk/eventstream.go and
	// internal/adk/openrouter's aggregator for where each is set.
	Reasoning string
	Err       error
}

// TokenUsage reports the token cost of one underlying model call.
type TokenUsage struct {
	Prompt int
	Output int
	Total  int
}

// ToolCall is the agent invoking a registered tool. ID ties it to the
// ToolResult (and, if applicable, the Confirmation) that follow for the
// same invocation, so the UI can update one transcript entry across a
// call's whole lifecycle instead of appending a new one per event.
type ToolCall struct {
	ID   string
	Name string
	Args map[string]any
}

// ToolResult is a tool's result being fed back to the agent. ID matches
// the ToolCall it's answering.
type ToolResult struct {
	ID     string
	Name   string
	Result map[string]any
}

// ToolConfirmationRequest is a tool call paused pending human approval.
// ID is what RespondToConfirmation needs back to unblock it — ADK's
// internal wrapper call's own ID, distinct from OriginalID, which is the
// ID of the actual tool call this confirmation is gating (the same ID
// that call's ToolCall.ID carried) and is what the UI uses to find and
// update that call's existing transcript entry rather than appending a
// duplicate one for the confirmation.
//
// More than one of these can arrive for the same turn — parallel tool
// calls (ADK runs a turn's function calls in unordered goroutines, see
// internal/adk/tools/gate.go's package doc comment) that each require
// confirmation all get requested at once. See RespondToConfirmation for
// why every one from the same batch has to be answered together.
type ToolConfirmationRequest struct {
	ID         string
	OriginalID string
	Tool       string
	Args       map[string]any
	Hint       string // human-readable explanation, if the tool provided one; may be ""
}

// ConfirmationDecision is one answered ToolConfirmationRequest — ID
// matches the request's own ID, Approved is the user's decision.
type ConfirmationDecision struct {
	ID       string
	Approved bool
}

// BackendFactory builds a Backend from a user-supplied API key for the
// given provider — how the /key popup connects (or reconnects) without
// restarting the app. main wires the concrete constructor (adk.New) in;
// ui only ever sees this shape. Both provider and apiKey left "" means
// "rebuild from whatever's already saved on disk for each agent's own
// configured provider, no fresh key" — how /agents reloads the backend
// after a config edit without needing a key of its own (see
// App.reloadBackend).
type BackendFactory func(ctx context.Context, provider, apiKey string) (Backend, error)

// AgentConfigSummary is one agent's identity plus model selection, as
// read from its config file — the /agents menu's-eye view of
// adk.AgentSummary (kept as a distinct, ui-owned type for the same
// reason SessionSummary is: this package never imports adk directly).
// ID is "" for the root agent, otherwise a sub-agent's own identifier —
// opaque here, just round-tripped back into SetAgentProvider/
// SetAgentModel to say which agent an edit is for. Tools is exactly
// what's currently granted — nil/empty means none, not "defaults."
type AgentConfigSummary struct {
	ID       string
	Name     string
	Provider string
	Model    string
	Tools    []string
	IsRoot   bool
}

// ToolSummary is one tool the /agents tools picker can offer a checkbox
// for — the ui's-eye view of adk.ToolSummary, same reasoning as
// AgentConfigSummary. Description is shown alongside Name so a user can
// tell what a tool does without already knowing the codebase.
type ToolSummary struct {
	Name        string
	Description string
}
