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
// Stream, RespondToConfirmation). agents.go builds the agent tree,
// tools.go defines what it can call, eventstream.go translates ADK's
// event model into ui.StreamChunk, and store.go owns persistence — each
// a distinct concern kept out of this file on purpose, since this one
// used to hold all of them and had become hard to scan.
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

	"tui-testing/internal/settings"
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

	// modelName is the root agent's resolved model name — whatever
	// settings.json's agent.model says, or DefaultModelName if it
	// doesn't specify one. Exposed via ModelName for the boot banner,
	// same reasoning as specialists.
	modelName string
}

// New builds the root agent's model (provider/model chosen by
// settings.json's agent section — see models.go), the agent tree (see
// agents.go), and the runner backing it, from the given API key.
// Sourcing the key (env var at startup, a value typed into the /key
// popup, ...) is entirely the caller's concern; this just validates and
// uses whatever it's handed. Returns an error rather than panicking so
// the caller can decide how to degrade.
func New(ctx context.Context, apiKey string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("no API key given")
	}

	cfg := settings.Load()

	rootModel, err := buildModel(ctx, cfg.Agent.Provider, cfg.Agent.Model, apiKey)
	if err != nil {
		return nil, fmt.Errorf("create root model: %w", err)
	}
	modelName := cfg.Agent.Model
	if modelName == "" {
		modelName = DefaultModelName
	}

	sessSvc, err := openSessionStore()
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}

	root, specialists, err := buildRootAgent(ctx, apiKey, rootModel)
	if err != nil {
		return nil, err
	}

	r, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             root,
		SessionService:    sessSvc,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create runner: %w", err)
	}

	return &Client{runner: r, sessions: sessSvc, specialists: specialists, modelName: modelName}, nil
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

// RespondToConfirmation answers a pending tool-approval request and
// resumes the run. Per toolconfirmation's own doc comment, this means
// sending a FunctionResponse with the *same ID* as the confirmation
// request, named toolconfirmation.FunctionCallName, with a
// {"confirmed": bool} response payload — ADK then either runs the
// original tool call or reports it declined, either way producing more
// events on the returned channel exactly like a fresh Stream call would.
func (c *Client) RespondToConfirmation(ctx context.Context, sessionID, requestID string, approved bool) (<-chan ui.StreamChunk, error) {
	content := &genai.Content{
		Role: string(genai.RoleUser),
		Parts: []*genai.Part{{
			FunctionResponse: &genai.FunctionResponse{
				ID:       requestID,
				Name:     toolconfirmation.FunctionCallName,
				Response: map[string]any{"confirmed": approved},
			},
		}},
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
