// Package adk wraps Google's Agent Development Kit for Go
// (google.golang.org/adk/v2) into the calls the TUI actually needs: send
// a message and get the reply, or stream it token-by-token. This is the
// one place in the codebase that knows ADK, genai, or Gemini exist —
// internal/ui never imports this package, only the small ui.Backend
// interface it satisfies structurally (this package imports ui right
// back, just for the StreamChunk type Backend.Stream is contracted to
// return — the implementer depending on the consumer's shape, not a
// cycle: ui itself never references adk).
package adk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"tui-testing/internal/ui"
)

const (
	appName   = "tui-testing"
	userID    = "local-user"
	agentName = "assistant"

	instruction = "You are a concise, helpful assistant embedded in a terminal chat UI. " +
		"Keep replies short — this is a test harness for the UI, not a place for long essays. " +
		"You have a list_files tool for browsing the working directory; use it whenever it's relevant."
)

// ModelName is the Gemini model this package talks to. Exported so
// callers (e.g. the boot banner) can display it without either
// duplicating the string or reaching into adk's internals for it.
const ModelName = "gemini-3.1-flash-lite"

// Client is a single, minimal ADK agent — no tools, no sub-agents, no
// persistence beyond the process lifetime. Just enough to prove the TUI
// can round-trip a message through a real LLM.
type Client struct {
	runner *runner.Runner
}

// New builds the Gemini model, the ADK agent, and the runner backing it
// from the given API key. Sourcing the key (env var at startup, a value
// typed into the /key popup, ...) is entirely the caller's concern; this
// just validates and uses whatever it's handed. Returns an error rather
// than panicking so the caller can decide how to degrade.
func New(ctx context.Context, apiKey string) (*Client, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("no API key given")
	}

	model, err := gemini.NewModel(ctx, ModelName, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return nil, fmt.Errorf("create model: %w", err)
	}

	listFilesTool, err := functiontool.New(functiontool.Config{
		Name:        "list_files",
		Description: "Lists files and directories at the given path. Defaults to the current working directory if path is omitted.",
		// RequireConfirmation hands the pause/resume orchestration to
		// ADK entirely: it emits a toolconfirmation.FunctionCallName
		// event instead of running listFiles, and either runs it or
		// reports it declined once we answer via
		// Client.RespondToConfirmation. listFiles itself needs no
		// changes for this — see the HITL handling in runStream.
		RequireConfirmation: true,
	}, listFiles)
	if err != nil {
		return nil, fmt.Errorf("create list_files tool: %w", err)
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        agentName,
		Model:       model,
		Description: "A general-purpose assistant for testing the TUI against a real LLM.",
		Instruction: instruction,
		Tools:       []tool.Tool{listFilesTool},
	})
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	r, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             a,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, fmt.Errorf("create runner: %w", err)
	}

	return &Client{runner: r}, nil
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
// SSE streaming mode. See runStream for how events get turned into
// chunks.
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

// runStream is Stream and RespondToConfirmation's shared event-relay
// loop — sending either a text message or a confirmation response is the
// same "feed this Content into the runner and turn the resulting events
// into chunks" operation from here on.
//
// With StreamingModeSSE set, the runner yields one event per incoming
// chunk with Partial=true for text — each such event's Content.Parts
// carry only the *new* delta text for that chunk (this is straight from
// reading ADK's own streamingResponseAggregator, which forwards each raw
// provider chunk downstream as its own event rather than re-emitting a
// growing snapshot). There's also one final aggregated, non-partial text
// event once the turn completes; that one's skipped since the deltas
// already cover everything it contains.
//
// Tool calls and their results are different: a tool result event is
// never marked Partial at all (confirmed by reading where ADK's flow
// constructs it — internal/llminternal/base_flow.go builds it as a plain
// session.Event with no Partial field set), and a tool call can show up
// in both a partial chunk *and* the final aggregated event. So tool parts
// are read from every event regardless of Partial, deduped by call/result
// ID (falling back to name+payload when a provider doesn't set one) so
// the same call/result presented twice by ADK's internals still only
// reaches the UI once.
//
// A confirmation request arrives as a FunctionCall part too, but named
// toolconfirmation.FunctionCallName rather than the tool it's gatekeeping
// — toolconfirmation.OriginalCallFrom unwraps it to get the real call
// that's actually waiting to run. And our own FunctionResponse *to* that
// request — the one RespondToConfirmation just sent — comes back around
// through this same loop on the resumed run; it's filtered out rather
// than shown as if it were a real tool's result.
func (c *Client) runStream(ctx context.Context, sessionID string, msg *genai.Content) <-chan ui.StreamChunk {
	cfg := agent.RunConfig{StreamingMode: agent.StreamingModeSSE}

	ch := make(chan ui.StreamChunk)
	go func() {
		defer close(ch)

		send := func(chunk ui.StreamChunk) bool {
			select {
			case ch <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}

		seenCalls := map[string]bool{}
		seenResults := map[string]bool{}
		seenConfirmations := map[string]bool{}

		for event, err := range c.runner.Run(ctx, userID, sessionID, msg, cfg) {
			if err != nil {
				send(ui.StreamChunk{Err: fmt.Errorf("run: %w", err)})
				return
			}
			if event.Content == nil {
				continue
			}

			for _, part := range event.Content.Parts {
				switch {
				case event.Partial && part.Text != "":
					if !send(ui.StreamChunk{Text: part.Text}) {
						return
					}

				case part.FunctionCall != nil && part.FunctionCall.Name == toolconfirmation.FunctionCallName:
					fc := part.FunctionCall
					if seenConfirmations[fc.ID] {
						continue
					}
					seenConfirmations[fc.ID] = true

					original, err := toolconfirmation.OriginalCallFrom(fc)
					if err != nil {
						continue
					}
					if !send(ui.StreamChunk{Confirmation: &ui.ToolConfirmationRequest{
						ID:         fc.ID,
						OriginalID: original.ID,
						Tool:       original.Name,
						Args:       original.Args,
						Hint:       confirmationHint(fc),
					}}) {
						return
					}

				case part.FunctionCall != nil:
					fc := part.FunctionCall
					key := toolEventKey(fc.ID, fc.Name, fc.Args)
					if seenCalls[key] {
						continue
					}
					seenCalls[key] = true
					if !send(ui.StreamChunk{ToolCall: &ui.ToolCall{ID: fc.ID, Name: fc.Name, Args: fc.Args}}) {
						return
					}

				case part.FunctionResponse != nil && part.FunctionResponse.Name == toolconfirmation.FunctionCallName:
					continue // our own answer to a confirmation request, not a real tool result

				case part.FunctionResponse != nil:
					fr := part.FunctionResponse
					key := toolEventKey(fr.ID, fr.Name, fr.Response)
					if seenResults[key] {
						continue
					}
					seenResults[key] = true
					if !send(ui.StreamChunk{ToolResult: &ui.ToolResult{ID: fr.ID, Name: fr.Name, Result: fr.Response}}) {
						return
					}
				}
			}
		}
	}()

	return ch
}

// confirmationHint pulls the human-readable explanation out of a
// confirmation request's "toolConfirmation" arg, if the tool provided
// one (RequireConfirmation's auto-managed flow, which listFiles uses,
// doesn't set one; a tool using the manual ctx.RequestConfirmation API
// could). Best-effort — malformed or absent just yields "".
func confirmationHint(fc *genai.FunctionCall) string {
	raw, ok := fc.Args["toolConfirmation"]
	if !ok {
		return ""
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	hint, _ := m["hint"].(string)
	return hint
}

// toolEventKey identifies a tool call or result for deduping, preferring
// the provider-assigned ID and falling back to name+payload when that's
// empty (some providers don't set one for every call).
func toolEventKey(id, name string, payload map[string]any) string {
	if id != "" {
		return id
	}
	b, _ := json.Marshal(payload)
	return name + string(b)
}

// Struct comments here are just for human readers — jsonschema-go (what
// functiontool.New uses to infer each tool's schema) only reads the
// "jsonschema" struct tag for the description the model actually sees,
// not regular Go comments.
type listFilesArgs struct {
	Path string `json:"path,omitempty" jsonschema:"Directory to list, relative or absolute. Defaults to the current working directory if omitted."`
}

type listFilesResult struct {
	Files []string `json:"files" jsonschema:"Entry names found in the directory; directories are suffixed with a trailing slash."`
}

func listFiles(_ agent.Context, args listFilesArgs) (listFilesResult, error) {
	dir := args.Path
	if dir == "" {
		dir = "."
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return listFilesResult{}, fmt.Errorf("read dir %q: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		files = append(files, name)
	}
	sort.Strings(files)

	return listFilesResult{Files: files}, nil
}
