package adk

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/toolconfirmation"

	"tui-testing/internal/ui"
)

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

			// The aggregator's Close() result — see runStream's doc comment
			// on the final aggregated event — is the one event per model
			// call carrying real UsageMetadata/FinishReason; every partial
			// chunk before it has Partial=true and, per streamingResponse-
			// aggregator.aggregateResponse, only ever forwards whatever
			// usage the raw provider chunk itself happened to carry (often
			// nothing until the last one). !event.Partial isolates exactly
			// that one event.
			if !event.Partial {
				if u := event.UsageMetadata; u != nil {
					if !send(ui.StreamChunk{Usage: &ui.TokenUsage{
						Prompt: int(u.PromptTokenCount),
						Output: int(u.CandidatesTokenCount + u.ThoughtsTokenCount),
						Total:  int(u.TotalTokenCount),
					}}) {
						return
					}
				}
				if fr := event.FinishReason; fr != "" && fr != genai.FinishReasonStop && fr != genai.FinishReasonUnspecified {
					if !send(ui.StreamChunk{FinishReason: string(fr)}) {
						return
					}
				}
			}

			if event.Content == nil {
				continue
			}

			for _, part := range event.Content.Parts {
				switch {
				// Checked before the plain-Text case below, not merged
				// into it: a thought part also has Text != "", so
				// whichever case comes first wins — Gemini sets Thought
				// on parts that represent reasoning rather than the
				// actual reply (our own OpenRouter model.LLM does the
				// same for a provider's reasoning/reasoning_content
				// field — see internal/adk/openrouter's aggregator), and
				// those should never be treated as reply text.
				case event.Partial && part.Thought && part.Text != "":
					if !send(ui.StreamChunk{Reasoning: part.Text}) {
						return
					}

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
