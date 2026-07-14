package openrouter

import (
	"encoding/json"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

// aggregator turns a sequence of streaming chatStreamChunks into the
// same two-tier shape ADK's own gemini driver produces (see the vendored
// internal/llminternal/stream_aggregator.go this mirrors): one
// Partial:true model.LLMResponse per incoming text delta, forwarded
// immediately for the UI to render token-by-token, plus exactly one
// final response — Partial left false, the zero value, same as
// upstream's own aggregator.Close() — carrying the fully assembled
// content (concatenated text, complete tool calls) once the stream ends.
// Downstream code (base_flow's dispatch, this app's eventstream.go)
// looks for that final non-partial event to know the turn is done; it
// never gets an explicit TurnComplete either, matching gemini's own
// driver exactly rather than guessing at a field it doesn't set.
type aggregator struct {
	text      strings.Builder
	toolCalls map[int]*toolCallBuilder
	toolOrder []int

	finishReason string
	usage        *chatUsage
}

type toolCallBuilder struct {
	id, name string
	args     strings.Builder
}

func newAggregator() *aggregator {
	return &aggregator{toolCalls: map[int]*toolCallBuilder{}}
}

// processChunk folds one SSE chunk into the running aggregate and
// returns zero or one Partial model.LLMResponse to forward immediately
// — only a text delta produces one; tool-call argument fragments are
// silent until the whole call is assembled in close(), since a partial
// JSON-arguments fragment isn't meaningful to show incrementally.
func (a *aggregator) processChunk(chunk chatStreamChunk) []*model.LLMResponse {
	if chunk.Usage != nil {
		a.usage = chunk.Usage
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	choice := chunk.Choices[0]
	if choice.FinishReason != "" {
		a.finishReason = choice.FinishReason
	}
	if choice.Delta == nil {
		return nil
	}

	var out []*model.LLMResponse
	if choice.Delta.Content != "" {
		a.text.WriteString(choice.Delta.Content)
		out = append(out, &model.LLMResponse{
			Content: &genai.Content{Parts: []*genai.Part{{Text: choice.Delta.Content}}, Role: genai.RoleModel},
			Partial: true,
		})
	}
	for _, tc := range choice.Delta.ToolCalls {
		b, ok := a.toolCalls[tc.Index]
		if !ok {
			b = &toolCallBuilder{}
			a.toolCalls[tc.Index] = b
			a.toolOrder = append(a.toolOrder, tc.Index)
		}
		if tc.ID != "" {
			b.id = tc.ID
		}
		if tc.Function.Name != "" {
			b.name = tc.Function.Name
		}
		b.args.WriteString(tc.Function.Arguments)
	}
	return out
}

// close assembles the final aggregated response, or nil if the stream
// never produced any content at all (an immediate error case upstream
// already reported separately).
func (a *aggregator) close() *model.LLMResponse {
	if a.text.Len() == 0 && len(a.toolCalls) == 0 && a.finishReason == "" {
		return nil
	}

	var parts []*genai.Part
	if a.text.Len() > 0 {
		parts = append(parts, &genai.Part{Text: a.text.String()})
	}
	for _, idx := range a.toolOrder {
		b := a.toolCalls[idx]
		var args map[string]any
		_ = json.Unmarshal([]byte(b.args.String()), &args)
		parts = append(parts, &genai.Part{FunctionCall: &genai.FunctionCall{ID: b.id, Name: b.name, Args: args}})
	}

	return &model.LLMResponse{
		Content:       &genai.Content{Parts: parts, Role: genai.RoleModel},
		FinishReason:  mapFinishReason(a.finishReason),
		UsageMetadata: toUsageMetadata(a.usage),
	}
}
