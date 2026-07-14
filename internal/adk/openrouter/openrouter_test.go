package openrouter

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

// withServer points chatCompletionsURL at an httptest.Server for the
// duration of one test, restoring the real URL after — this package
// hand-rolls the whole OpenAI-compatible wire protocol (request
// translation, SSE streaming, tool-call round-tripping) with no ADK or
// third-party framework backing it, so unlike most wiring changes in
// this codebase, that specific new-and-unverified-by-anything-else
// surface is worth a real test rather than just build+vet.
func withServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	prev := chatCompletionsURL
	chatCompletionsURL = srv.URL
	t.Cleanup(func() { chatCompletionsURL = prev })
}

func collect(t *testing.T, seq func(func(*model.LLMResponse, error) bool)) []*model.LLMResponse {
	t.Helper()
	var out []*model.LLMResponse
	for resp, err := range seq {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out = append(out, resp)
	}
	return out
}

func TestNonStreamingText(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization header = %q, want Bearer test-key", got)
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Stream {
			t.Errorf("Stream = true, want false")
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Fatalf("unexpected messages: %+v", req.Messages)
		}
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{{
				Message:      &chatMessage{Role: "assistant", Content: "hello there"},
				FinishReason: "stop",
			}},
			Usage: &chatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		})
	})

	m := NewModel("openai/gpt-4o-mini", "test-key")
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)},
		Config:   &genai.GenerateContentConfig{SystemInstruction: genai.NewContentFromText("be nice", "")},
	}

	responses := collect(t, m.GenerateContent(context.Background(), req, false))
	if len(responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(responses))
	}
	resp := responses[0]
	if resp.Partial {
		t.Error("Partial = true, want false for non-streaming")
	}
	if len(resp.Content.Parts) != 1 || resp.Content.Parts[0].Text != "hello there" {
		t.Fatalf("unexpected content: %+v", resp.Content.Parts)
	}
	if resp.FinishReason != genai.FinishReasonStop {
		t.Errorf("FinishReason = %q, want STOP", resp.FinishReason)
	}
	if resp.UsageMetadata == nil || resp.UsageMetadata.TotalTokenCount != 15 {
		t.Errorf("unexpected usage: %+v", resp.UsageMetadata)
	}
}

func TestNonStreamingToolCall(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		json.NewDecoder(r.Body).Decode(&req)
		if len(req.Tools) != 1 || req.Tools[0].Function.Name != "read_file" {
			t.Fatalf("unexpected tools: %+v", req.Tools)
		}
		if params := req.Tools[0].Function.Parameters; params["type"] != "object" {
			t.Errorf("schema type = %v, want lowercase object", params["type"])
		}
		json.NewEncoder(w).Encode(chatResponse{
			Choices: []chatChoice{{
				Message: &chatMessage{
					Role: "assistant",
					ToolCalls: []chatToolCall{{
						ID:       "call_1",
						Type:     "function",
						Function: chatToolCallFunc{Name: "read_file", Arguments: `{"path":"foo.txt"}`},
					}},
				},
				FinishReason: "tool_calls",
			}},
		})
	})

	m := NewModel("openai/gpt-4o-mini", "test-key")
	req := &model.LLMRequest{
		Contents: []*genai.Content{genai.NewContentFromText("read foo.txt", genai.RoleUser)},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{{FunctionDeclarations: []*genai.FunctionDeclaration{{
				Name: "read_file",
				Parameters: &genai.Schema{
					Type:       "OBJECT",
					Properties: map[string]*genai.Schema{"path": {Type: "STRING"}},
					Required:   []string{"path"},
				},
			}}}},
		},
	}

	responses := collect(t, m.GenerateContent(context.Background(), req, false))
	if len(responses) != 1 {
		t.Fatalf("got %d responses, want 1", len(responses))
	}
	parts := responses[0].Content.Parts
	if len(parts) != 1 || parts[0].FunctionCall == nil {
		t.Fatalf("expected one function call part, got %+v", parts)
	}
	fc := parts[0].FunctionCall
	if fc.Name != "read_file" || fc.ID != "call_1" || fc.Args["path"] != "foo.txt" {
		t.Errorf("unexpected function call: %+v", fc)
	}
	if responses[0].FinishReason != genai.FinishReasonStop {
		t.Errorf("FinishReason = %q, want STOP (tool_calls maps to STOP)", responses[0].FinishReason)
	}
}

// TestRequestTranslatesToolRoundTrip checks that a prior turn's function
// call + function response (as ADK would replay it from session
// history) become a correctly-paired assistant/tool_calls message and a
// tool message sharing the same tool_call_id — the actual correctness
// risk in contentsToMessages, since OpenRouter rejects a tool message
// whose tool_call_id doesn't match a preceding assistant tool call.
func TestRequestTranslatesToolRoundTrip(t *testing.T) {
	var seen chatRequest
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&seen)
		json.NewEncoder(w).Encode(chatResponse{Choices: []chatChoice{{Message: &chatMessage{Content: "ok"}, FinishReason: "stop"}}})
	})

	m := NewModel("openai/gpt-4o-mini", "test-key")
	req := &model.LLMRequest{
		Contents: []*genai.Content{
			genai.NewContentFromText("read foo.txt", genai.RoleUser),
			genai.NewContentFromParts([]*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "read_file", Args: map[string]any{"path": "foo.txt"}}}}, genai.RoleModel),
			genai.NewContentFromParts([]*genai.Part{{FunctionResponse: &genai.FunctionResponse{ID: "call_1", Name: "read_file", Response: map[string]any{"content": "hi"}}}}, genai.RoleUser),
		},
	}
	collect(t, m.GenerateContent(context.Background(), req, false))

	if len(seen.Messages) != 3 {
		t.Fatalf("got %d messages, want 3: %+v", len(seen.Messages), seen.Messages)
	}
	call := seen.Messages[1]
	if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].ID != "call_1" {
		t.Fatalf("unexpected assistant tool-call message: %+v", call)
	}
	result := seen.Messages[2]
	if result.Role != "tool" || result.ToolCallID != "call_1" {
		t.Fatalf("unexpected tool-result message: %+v", result)
	}
}

func TestStreamingTextAndToolCall(t *testing.T) {
	withServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		wr := bufio.NewWriter(w)

		write := func(chunk chatStreamChunk) {
			data, _ := json.Marshal(chunk)
			wr.WriteString("data: ")
			wr.Write(data)
			wr.WriteString("\n\n")
			wr.Flush()
			flusher.Flush()
		}

		write(chatStreamChunk{Choices: []chatStreamChoice{{Delta: &chatMessage{Role: "assistant"}}}})
		write(chatStreamChunk{Choices: []chatStreamChoice{{Delta: &chatMessage{Content: "Hello"}}}})
		write(chatStreamChunk{Choices: []chatStreamChoice{{Delta: &chatMessage{Content: ", world"}}}})
		write(chatStreamChunk{Choices: []chatStreamChoice{{Delta: &chatMessage{ToolCalls: []chatToolCall{
			{Index: 0, ID: "call_9", Type: "function", Function: chatToolCallFunc{Name: "read_file", Arguments: `{"pa`}},
		}}}}})
		write(chatStreamChunk{Choices: []chatStreamChoice{{Delta: &chatMessage{ToolCalls: []chatToolCall{
			{Index: 0, Function: chatToolCallFunc{Arguments: `th":"foo.txt"}`}},
		}}}}})
		write(chatStreamChunk{
			Choices: []chatStreamChoice{{FinishReason: "tool_calls"}},
			Usage:   &chatUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
		})
		wr.WriteString("data: [DONE]\n\n")
		wr.Flush()
		flusher.Flush()
	})

	m := NewModel("openai/gpt-4o-mini", "test-key")
	req := &model.LLMRequest{Contents: []*genai.Content{genai.NewContentFromText("hi", genai.RoleUser)}}

	responses := collect(t, m.GenerateContent(context.Background(), req, true))
	if len(responses) == 0 {
		t.Fatal("got no responses")
	}

	final := responses[len(responses)-1]
	for _, r := range responses[:len(responses)-1] {
		if !r.Partial {
			t.Errorf("expected only the last response to be non-partial, got non-partial early: %+v", r)
		}
	}
	if final.Partial {
		t.Error("final response has Partial = true, want false")
	}

	var text string
	var fc *genai.FunctionCall
	for _, p := range final.Content.Parts {
		if p.Text != "" {
			text += p.Text
		}
		if p.FunctionCall != nil {
			fc = p.FunctionCall
		}
	}
	if text != "Hello, world" {
		t.Errorf("aggregated text = %q, want %q", text, "Hello, world")
	}
	if fc == nil || fc.Name != "read_file" || fc.ID != "call_9" || fc.Args["path"] != "foo.txt" {
		t.Errorf("unexpected aggregated function call: %+v", fc)
	}
	if final.FinishReason != genai.FinishReasonStop {
		t.Errorf("FinishReason = %q, want STOP", final.FinishReason)
	}
	if final.UsageMetadata == nil || final.UsageMetadata.TotalTokenCount != 7 {
		t.Errorf("unexpected usage: %+v", final.UsageMetadata)
	}
}
