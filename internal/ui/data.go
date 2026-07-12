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
// ToolArgs, and ToolStatus instead — structured data rather than a
// pre-formatted string, so renderMessage can lay it out (and style the
// name, args, and status differently) instead of just printing whatever
// string it was handed. ToolStatus is the one field that gets mutated in
// place as a call progresses — see App.upsertToolMessage.
type ChatMessage struct {
	Role        Role
	Content     string
	ToolName    string
	ToolArgs    map[string]any
	ToolStatus  string // e.g. "running…", the pending-approval text, or a result summary
	ToolPending bool   // true only while ToolStatus is an approval request awaiting a decision
	At          time.Time

	// Usage and FinishReason are meaningful only on RoleAgent messages —
	// the turn's accumulated token cost and, if the model's last call
	// ended on something other than a plain "stop", a note about why. See
	// App.attachTurnUsage.
	Usage        *TokenUsage
	FinishReason string
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
