package ui

import (
	"time"

	"github.com/google/uuid"
)

// Role identifies who authored a ChatMessage.
type Role int

const (
	RoleUser Role = iota
	RoleAgent
	RoleSystem
	// RoleTool covers a tool's entire lifecycle — call, optional approval,
	// eventual result — as one transcript entry that gets updated in
	// place rather than a fresh entry per event. See App.upsertToolMessage.
	RoleTool
)

// ChatMessage is one entry in the transcript. Mock/local for now — this is
// where a real backend would eventually feed messages in.
//
// Content is used by every role except RoleTool, which uses ToolName,
// ToolArgs, ToolStatus, and ToolResult instead — structured data rather
// than a pre-formatted string, so renderMessage can lay it out (and
// style the name, args, and status differently), and so a completed
// call's result can be reformatted live when the verbose-tools setting
// changes instead of the summary being baked in once at event time.
// ToolStatus only ever holds a lifecycle sentinel now ("running…", a
// pending-approval prompt, "approved"/"denied") — once a real result
// arrives, ToolResult is what's set (see App.completeToolMessage) and
// ToolStatus stops being read for that message. ToolResult stays nil
// the whole time a call is in flight, including while paused on a HITL
// confirmation — that's exactly what distinguishes "no result yet" from
// "the tool legitimately returned an empty map".
type ChatMessage struct {
	Role        Role
	Content     string
	ToolName    string
	ToolArgs    map[string]any
	ToolStatus  string         // lifecycle sentinel only — see the type doc comment
	ToolResult  map[string]any // nil until the call completes; see App.completeToolMessage
	ToolPending bool           // true only while ToolStatus is an approval request awaiting a decision
	At          time.Time

	// FinishReason is RoleAgent-only: a note about why the model's last
	// call in the turn ended on something other than a plain "stop".
	// Token usage isn't tracked per-message — see App.contextUsed and
	// header.go's renderContextBar for where it's shown instead.
	FinishReason string

	// ReasoningActive/ReasoningDuration/ReasoningText are RoleAgent-only —
	// whether this message's reasoning phase (see App.reasoning) is in
	// progress right now, how long it took once it isn't, and the actual
	// reasoning content received so far. ReasoningDuration is live-updated
	// while ReasoningActive (App.stopwatch) and then frozen — it's
	// deliberately never cleared back to zero once set, so "thought for
	// Xs" stays visible under the reply as a permanent record instead of
	// disappearing the moment reasoning ends. ReasoningText likewise stays
	// once set — it's the actual proof reasoning happened, not just a
	// duration a user has to take on faith.
	ReasoningActive   bool
	ReasoningDuration time.Duration
	ReasoningText     string
}

// newSessionID generates a fresh conversation identifier for App to use
// as App.sessionID — called once per launch (see NewApp), so every run
// starts its own conversation rather than silently resuming whatever was
// last talked to, now that sessions persist across restarts (see
// internal/adk's sqlite-backed session store). google/uuid is already
// pulled in as an ADK transitive dependency (its own platform/uuid.go id
// provider), so this doesn't add anything new to the binary.
func newSessionID() string {
	return uuid.NewString()
}
