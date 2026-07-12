package ui

import "time"

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

// Agent is a switchable persona/session the platform can route messages to.
// Mock data today; stand-in for whatever an "agent" ends up being backed by
// (a model+system-prompt combo, a running process, etc).
type Agent struct {
	ID          string
	Name        string
	Description string
}

// mockAgents seeds the command palette until real agent data exists.
func mockAgents() []Agent {
	return []Agent{
		{ID: "research", Name: "Research", Description: "Reads code and docs, answers questions"},
		{ID: "coder", Name: "Coder", Description: "Writes and edits code"},
		{ID: "planner", Name: "Planner", Description: "Breaks work into steps"},
	}
}

// sessionID identifies this run's conversation to the backend (real or
// mock) and doubles as the label shown in the top bar. Fixed for the
// process lifetime — no persistence across restarts yet.
const sessionID = "sess_7f3d2a19"
