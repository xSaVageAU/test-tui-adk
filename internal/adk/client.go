// Package adk wraps Google's Agent Development Kit for Go
// (google.golang.org/adk/v2) into the calls the TUI actually needs: send
// a message and get the reply, or stream it token-by-token. This is the
// one place in the codebase that knows ADK, genai, or Gemini exist —
// internal/ui never imports this package, only the small ui.Backend
// interface it satisfies structurally (this package imports ui right
// back, just for the StreamChunk type Backend.Stream is contracted to
// return — the implementer depending on the consumer's shape, not a
// cycle: ui itself never references adk).
//
// File layout: client.go is just the public API (Client, New, Send,
// Stream, RespondToConfirmation). agents.go builds the agent tree, the
// tools subpackage (internal/adk/tools) defines what it can call — one
// file per tool, tools.go there is the registration point, see its own
// package doc comment — eventstream.go translates ADK's event model
// into ui.StreamChunk, and store.go owns persistence — each a distinct
// concern kept out of this file on purpose, since this one used to hold
// all of them and had become hard to scan.
package adk

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"tui-testing/internal/ui"
)

const (
	appName = "tui-testing"
	userID  = "local-user"
)

// Client is a single ADK agent — a root agent with specialists available
// as agent-as-tool calls, never as transfer targets, so there's exactly
// one voice in the conversation no matter what the root ends up
// consulting — backed by a sqlite-persisted session store (conversation
// history survives a restart; see store.go). No long-term memory: an
// earlier pass wired one up via ADK's memory.Service, but it turned out
// to just be keyword search over raw stored transcript, not anything
// resembling durable remembered facts — pulled back out rather than
// keep something that didn't match what was actually wanted. Revisit
// later as a deliberate, from-scratch feature.
type Client struct {
	runner   *runner.Runner
	sessions session.Service

	// specialists is the name of every sub-agent discovered at startup
	// (see agents.go's buildRootAgent), in load order. Exposed via
	// Specialists so callers (the boot banner) can show what's actually
	// loaded without reaching into agent-building internals.
	specialists []string

	// modelName is the root agent's resolved model name — whatever its
	// own agent.json says, or DefaultModelName if it doesn't specify
	// one. Exposed via ModelName for the boot banner, same reasoning as
	// specialists.
	modelName string

	// contextWindow is the root model's max input tokens, resolved once
	// at build time (see agents.go's buildRootAgent/resolveContextWindow)
	// — 0 if it couldn't be determined. Exposed via ContextWindow for the
	// top bar's context-usage indicator.
	contextWindow int
}

// New builds the root agent (config-driven — see agents.go and
// rootagent.go) and the runner backing it. apiKey, if non-empty, is a
// key the caller already has on hand for provider specifically (env var
// at startup, a value just typed into the /key popup, ...) — used only
// if the root agent (or a sub-agent) actually turns out to be
// configured for that same provider; every other provider resolves its
// own key from data/credentials.json instead (see models.go's
// buildModel/keyOverride). Both empty is not an immediate error — it
// only becomes one once buildModel actually needs a key it can't find,
// which lets a plain reload (rebuild everything from whatever's already
// saved on disk, no fresh key) reuse this same entry point — see
// internal/ui's /agents-triggered reload.
func New(ctx context.Context, provider, apiKey string) (*Client, error) {
	sessSvc, err := openSessionStore()
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}

	built, err := buildRootAgent(ctx, provider, apiKey)
	if err != nil {
		return nil, err
	}

	r, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             built.Agent,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create runner: %w", err)
	}

	return &Client{
		runner:        r,
		sessions:      sessSvc,
		specialists:   built.Specialists,
		modelName:     built.ModelName,
		contextWindow: built.ContextWindow,
	}, nil
}

// Specialists returns the name of every sub-agent that was discovered
// and loaded at startup, in load order — empty if none were configured
// under ~/.tui-testing/subagents.
func (c *Client) Specialists() []string {
	return c.specialists
}

// ModelName returns the root agent's resolved model name.
func (c *Client) ModelName() string {
	return c.modelName
}

// ContextWindow returns the root model's max input tokens, or 0 if it
// couldn't be determined (see resolveContextWindow).
func (c *Client) ContextWindow() int {
	return c.contextWindow
}

// RootAgentName reads just the root agent's configured name, without
// building a model or needing an API key — main.go calls this at
// startup purely to fail fast on a broken root agent.json even before
// (or without) a working API key or connection. Seeds the default
// config the same way buildRootAgent's own load does, so calling this
// before New is always safe and never conjures a different name than
// New would use.
func RootAgentName() (string, error) {
	cfg, err := loadRootAgentConfig()
	if err != nil {
		return "", err
	}
	return cfg.Name, nil
}

// Send sends a single user message in the given session and returns the
// agent's reply text, blocking until the run completes. sessionID is
// caller-chosen and reused across calls to keep conversation history —
// AutoCreateSession means the first call for a given ID creates it.
func (c *Client) Send(ctx context.Context, sessionID, message string) (string, error) {
	msg := genai.NewContentFromText(message, genai.RoleUser)

	var reply strings.Builder
	for event, err := range c.runner.Run(ctx, userID, sessionID, msg, agent.RunConfig{}) {
		if err != nil {
			return "", fmt.Errorf("run: %w", err)
		}
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			reply.WriteString(part.Text)
		}
	}

	if reply.Len() == 0 {
		return "", fmt.Errorf("empty response from model")
	}
	return reply.String(), nil
}

// Stream is Send's token-by-token counterpart, backed by ADK's built-in
// SSE streaming mode. See eventstream.go's runStream for how events get
// turned into chunks.
func (c *Client) Stream(ctx context.Context, sessionID, message string) (<-chan ui.StreamChunk, error) {
	msg := genai.NewContentFromText(message, genai.RoleUser)
	return c.runStream(ctx, sessionID, msg), nil
}

// RespondToConfirmation answers one or more pending tool-approval
// requests and resumes the run. Per toolconfirmation's own doc comment,
// each answer means sending a FunctionResponse with the *same ID* as its
// confirmation request, named toolconfirmation.FunctionCallName, with a
// {"confirmed": bool} response payload — ADK then either runs the
// original tool call or reports it declined, either way producing more
// events on the returned channel exactly like a fresh Stream call would.
//
// Every decision is sent as its own Part within a single Content/message
// rather than one call each: ADK's RequestConfirmationRequestProcessor
// (internal to the ADK module) matches answers back to their original
// calls by scanning only the single most recent message for
// FunctionResponses, so parallel tool calls that all requested
// confirmation in the same turn (see internal/adk/tools/gate.go's
// package doc comment on why those race) have to be answered together
// here — a decision sent in its own separate call would leave any others
// from the same batch unresolved indefinitely rather than getting picked
// up later.
func (c *Client) RespondToConfirmation(ctx context.Context, sessionID string, decisions []ui.ConfirmationDecision) (<-chan ui.StreamChunk, error) {
	parts := make([]*genai.Part, len(decisions))
	for i, d := range decisions {
		parts[i] = &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				ID:       d.ID,
				Name:     toolconfirmation.FunctionCallName,
				Response: map[string]any{"confirmed": d.Approved},
			},
		}
	}
	content := &genai.Content{
		Role:  string(genai.RoleUser),
		Parts: parts,
	}
	return c.runStream(ctx, sessionID, content), nil
}

// ListSessions returns every session for this app/user, most-recently-
// updated first. Metadata-only — session.Service.List doesn't populate a
// session's events (confirmed by reading session/database/service.go's
// List, which queries only the sessions table), so there's no cheap way
// to include a content preview here; ui.SessionSummary reflects that.
func (c *Client) ListSessions(ctx context.Context) ([]ui.SessionSummary, error) {
	resp, err := c.sessions.List(ctx, &session.ListRequest{AppName: appName, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	summaries := make([]ui.SessionSummary, len(resp.Sessions))
	for i, sess := range resp.Sessions {
		summaries[i] = ui.SessionSummary{ID: sess.ID(), UpdatedAt: sess.LastUpdateTime()}
	}
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
	})
	return summaries, nil
}
