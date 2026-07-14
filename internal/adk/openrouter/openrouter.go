// Package openrouter implements model.LLM against OpenRouter's chat-
// completions API (https://openrouter.ai/api/v1/chat/completions), which
// is OpenAI-compatible. Unlike Gemini, ADK v2 ships no adapter for this
// shape (only model/gemini and model/apigee) — everything here is a
// from-scratch translation between genai's request/response types and
// OpenAI-style JSON, including streaming (SSE) and tool-call round-
// tripping. Mirrors model/gemini's file shape (NewModel, Name,
// GenerateContent) so buildModel (internal/adk/models.go) can treat both
// providers identically.
package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

// chatCompletionsURL is OpenRouter's one and only endpoint this package
// needs — no separate base URL/model-listing support, since agent.json
// is expected to name a full OpenRouter model slug (e.g.
// "openai/gpt-4o-mini") directly. A var, not a const, purely so tests
// can point it at an httptest.Server instead of the real API.
var chatCompletionsURL = "https://openrouter.ai/api/v1/chat/completions"

type openRouterModel struct {
	name       string
	apiKey     string
	httpClient *http.Client
}

// NewModel returns a model.LLM backed by OpenRouter. Unlike gemini.NewModel,
// this never fails at construction — there's no client SDK to initialize,
// just an HTTP client — so any problem (bad key, unknown model slug,
// network failure) only ever surfaces from a real GenerateContent call.
func NewModel(modelName, apiKey string) model.LLM {
	return &openRouterModel{name: modelName, apiKey: apiKey, httpClient: &http.Client{}}
}

func (m *openRouterModel) Name() string { return m.name }

func (m *openRouterModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	if stream {
		return m.generateStream(ctx, req)
	}
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.generate(ctx, req)
		yield(resp, err)
	}
}

func (m *openRouterModel) modelName(req *model.LLMRequest) string {
	if req.Model != "" {
		return req.Model
	}
	return m.name
}

func (m *openRouterModel) generate(ctx context.Context, req *model.LLMRequest) (*model.LLMResponse, error) {
	chatReq := m.buildRequest(req, false)
	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openrouter: build request: %w", err)
	}
	m.setHeaders(httpReq)

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openrouter: read response: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("openrouter: parse response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("openrouter: %s", parsed.Error.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter: unexpected status %d: %s", resp.StatusCode, string(data))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openrouter: empty response")
	}

	return choiceToLLMResponse(parsed.Choices[0], parsed.Usage), nil
}

func (m *openRouterModel) generateStream(ctx context.Context, req *model.LLMRequest) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		chatReq := m.buildRequest(req, true)
		body, err := json.Marshal(chatReq)
		if err != nil {
			yield(nil, fmt.Errorf("openrouter: encode request: %w", err))
			return
		}

		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, chatCompletionsURL, bytes.NewReader(body))
		if err != nil {
			yield(nil, fmt.Errorf("openrouter: build request: %w", err))
			return
		}
		m.setHeaders(httpReq)
		httpReq.Header.Set("Accept", "text/event-stream")

		resp, err := m.httpClient.Do(httpReq)
		if err != nil {
			yield(nil, fmt.Errorf("openrouter: request failed: %w", err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			data, _ := io.ReadAll(resp.Body)
			yield(nil, fmt.Errorf("openrouter: unexpected status %d: %s", resp.StatusCode, string(data)))
			return
		}

		agg := newAggregator()
		scanner := bufio.NewScanner(resp.Body)
		// SSE lines can run long once tool-call arguments or a big JSON
		// blob stream in — default 64KiB scanner buffer is the initial
		// size, not the cap; grow it to 1MiB rather than risk
		// bufio.ErrTooLong on a legitimately large single line.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		for scanner.Scan() {
			data, ok := strings.CutPrefix(scanner.Text(), "data: ")
			if !ok || data == "" {
				continue
			}
			if data == "[DONE]" {
				break
			}

			var chunk chatStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				yield(nil, fmt.Errorf("openrouter: parse stream chunk: %w", err))
				return
			}
			if chunk.Error != nil {
				yield(nil, fmt.Errorf("openrouter: %s", chunk.Error.Message))
				return
			}
			for _, partial := range agg.processChunk(chunk) {
				if !yield(partial, nil) {
					return
				}
			}
		}
		if err := scanner.Err(); err != nil {
			yield(nil, fmt.Errorf("openrouter: read stream: %w", err))
			return
		}

		if final := agg.close(); final != nil {
			yield(final, nil)
		}
	}
}

func (m *openRouterModel) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// Not required for the API to function — OpenRouter just uses it to
	// label requests on its own dashboard/leaderboards.
	req.Header.Set("X-Title", "tui-testing")
}

// buildRequest translates an ADK model.LLMRequest into an OpenRouter
// chat-completions request. Never errors: an empty/nil req.Config just
// means "no extra sampling params, no tools" — buildModel is what
// rejects a genuinely missing model name before this is ever reached.
func (m *openRouterModel) buildRequest(req *model.LLMRequest, stream bool) *chatRequest {
	var messages []chatMessage
	if req.Config != nil && req.Config.SystemInstruction != nil {
		if text := textOf(req.Config.SystemInstruction); text != "" {
			messages = append(messages, chatMessage{Role: "system", Content: text})
		}
	}
	messages = append(messages, contentsToMessages(req.Contents)...)

	cr := &chatRequest{
		Model:    m.modelName(req),
		Messages: messages,
		Stream:   stream,
	}
	if stream {
		cr.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
	if req.Config == nil {
		return cr
	}

	if len(req.Config.Tools) > 0 {
		cr.Tools = toolsToChatTools(req.Config.Tools)
	}
	if choice := toolConfigToChoice(req.Config.ToolConfig); choice != nil {
		cr.ToolChoice = choice
	}
	cr.Temperature = req.Config.Temperature
	cr.TopP = req.Config.TopP
	if req.Config.MaxOutputTokens > 0 {
		cr.MaxTokens = req.Config.MaxOutputTokens
	}
	if len(req.Config.StopSequences) > 0 {
		cr.Stop = req.Config.StopSequences
	}
	return cr
}

// textOf concatenates every text part of c — used only for the system
// instruction, which this app (and ADK generally) always populates as
// plain text, never a mix of text and other part types.
func textOf(c *genai.Content) string {
	var b strings.Builder
	for _, p := range c.Parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// contentsToMessages flattens genai's turn-based Contents into OpenAI's
// flat message list. Routing is driven by each Part's own type rather
// than Content.Role: a FunctionCall part always becomes part of an
// assistant message's tool_calls (only the model ever emits one),  and a
// FunctionResponse part always becomes its own role:"tool" message
// (regardless of what Role genai's own bookkeeping put on the
// surrounding Content) — that's what OpenAI's wire format actually
// requires, and it means this doesn't need to special-case whichever
// role genai happens to tag a tool-result Content with.
func contentsToMessages(contents []*genai.Content) []chatMessage {
	var out []chatMessage
	for _, c := range contents {
		role := "user"
		if c.Role == genai.RoleModel {
			role = "assistant"
		}

		var text strings.Builder
		var toolCalls []chatToolCall
		var toolMessages []chatMessage

		for _, p := range c.Parts {
			switch {
			case p.FunctionCall != nil:
				args, _ := json.Marshal(p.FunctionCall.Args)
				toolCalls = append(toolCalls, chatToolCall{
					ID:       p.FunctionCall.ID,
					Type:     "function",
					Function: chatToolCallFunc{Name: p.FunctionCall.Name, Arguments: string(args)},
				})
			case p.FunctionResponse != nil:
				toolMessages = append(toolMessages, chatMessage{
					Role:       "tool",
					ToolCallID: p.FunctionResponse.ID,
					Content:    functionResponseContent(p.FunctionResponse),
				})
			case p.Text != "":
				text.WriteString(p.Text)
			}
		}

		if text.Len() > 0 || len(toolCalls) > 0 {
			out = append(out, chatMessage{Role: role, Content: text.String(), ToolCalls: toolCalls})
		}
		out = append(out, toolMessages...)
	}
	return out
}

func functionResponseContent(fr *genai.FunctionResponse) string {
	if fr.Response == nil {
		return "{}"
	}
	b, err := json.Marshal(fr.Response)
	if err != nil {
		return fmt.Sprintf("%v", fr.Response)
	}
	return string(b)
}

// reasoningText picks whichever of a message/delta's two possible
// reasoning-output shapes (see chatMessage's doc comment) is actually
// populated — Reasoning if non-empty, ReasoningDetails otherwise. These
// are two *representations of the same content*, not two additive
// pieces of it: a provider that populates both puts the identical text
// in each (confirmed live — reading both unconditionally, as an earlier
// version of this function did, produced visibly duplicated reasoning
// output, every fragment doubled). ReasoningDetails entries: only
// reasoning.text/reasoning.summary carry human-readable content;
// reasoning.encrypted has nothing displayable and is skipped.
func reasoningText(m *chatMessage) string {
	if m == nil {
		return ""
	}
	if m.Reasoning != "" {
		return m.Reasoning
	}
	var b strings.Builder
	for _, d := range m.ReasoningDetails {
		switch d.Type {
		case "reasoning.text":
			b.WriteString(d.Text)
		case "reasoning.summary":
			b.WriteString(d.Summary)
		}
	}
	return b.String()
}

// toolsToChatTools flattens every FunctionDeclaration across all tools
// into OpenAI's flat tools list — genai groups declarations under a
// Tool wrapper that also carries unrelated built-in tool types
// (GoogleSearch, CodeExecution, ...) this app's own tools never use, so
// only FunctionDeclarations is read.
func toolsToChatTools(tools []*genai.Tool) []chatTool {
	var out []chatTool
	for _, t := range tools {
		for _, fd := range t.FunctionDeclarations {
			out = append(out, chatTool{
				Type: "function",
				Function: chatFunction{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  parametersSchema(fd),
				},
			})
		}
	}
	return out
}

// parametersSchema prefers fd.ParametersJsonSchema over fd.Parameters —
// ADK's own functiontool package (github.com/google/jsonschema-go under
// the hood) populates ParametersJsonSchema, not the genai.Schema-typed
// Parameters field, for every tool this app builds. Using only
// schemaToJSONSchema(fd.Parameters) meant every tool's schema silently
// resolved to the "no parameters" default below — the tool's name and
// description came through fine, but path/content/etc. never did,
// which is exactly the bug a model hit live: it had no declared
// parameters to work from and had to guess "path" from the prose
// description, then learn the real shape from a validation error.
// ParametersJsonSchema is returned as-is (an `any`, concretely a
// *jsonschema.Schema) rather than converted into a map — it already
// marshals to correct, OpenAI-compatible JSON Schema on its own.
func parametersSchema(fd *genai.FunctionDeclaration) any {
	if fd.ParametersJsonSchema != nil {
		return fd.ParametersJsonSchema
	}
	return schemaToJSONSchema(fd.Parameters)
}

// schemaToJSONSchema converts a genai.Schema to a plain JSON Schema map
// — genai's Type is the same OpenAPI-3-subset shape as JSON Schema,
// just spelled in uppercase ("OBJECT", "STRING", ...) where JSON Schema
// wants lowercase. Covers the subset this app's own tools actually use
// (object/string/array with properties/items/required/description) —
// not the full Schema struct (anyOf, min/max bounds, patterns, ...),
// since nothing in this codebase emits those today.
func schemaToJSONSchema(s *genai.Schema) map[string]any {
	if s == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	m := map[string]any{}
	if s.Type != "" {
		m["type"] = strings.ToLower(string(s.Type))
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Items != nil {
		m["items"] = schemaToJSONSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for k, v := range s.Properties {
			props[k] = schemaToJSONSchema(v)
		}
		m["properties"] = props
	}
	if len(s.Required) > 0 {
		m["required"] = s.Required
	}
	return m
}

// toolConfigToChoice maps genai's tool-calling mode to OpenAI's
// tool_choice — nil means "omit the field", which OpenRouter treats the
// same as "auto" whenever tools are present.
func toolConfigToChoice(tc *genai.ToolConfig) any {
	if tc == nil || tc.FunctionCallingConfig == nil {
		return nil
	}
	switch tc.FunctionCallingConfig.Mode {
	case genai.FunctionCallingConfigModeNone:
		return "none"
	case genai.FunctionCallingConfigModeAny:
		if len(tc.FunctionCallingConfig.AllowedFunctionNames) == 1 {
			return map[string]any{
				"type":     "function",
				"function": map[string]any{"name": tc.FunctionCallingConfig.AllowedFunctionNames[0]},
			}
		}
		return "required"
	default:
		return "auto"
	}
}

// choiceToLLMResponse converts one non-streaming choice into the
// model.LLMResponse shape ADK expects back from a non-streaming call —
// same output shape generateStream's aggregator produces for its single
// final event, so downstream code (base_flow's function-call dispatch,
// this app's own eventstream.go) doesn't need to know which path built it.
func choiceToLLMResponse(choice chatChoice, usage *chatUsage) *model.LLMResponse {
	var parts []*genai.Part
	if choice.Message != nil {
		if reasoning := reasoningText(choice.Message); reasoning != "" {
			parts = append(parts, &genai.Part{Text: reasoning, Thought: true})
		}
		if choice.Message.Content != "" {
			parts = append(parts, &genai.Part{Text: choice.Message.Content})
		}
		for _, tc := range choice.Message.ToolCalls {
			parts = append(parts, &genai.Part{FunctionCall: toolCallToFunctionCall(tc)})
		}
	}
	return &model.LLMResponse{
		Content:       &genai.Content{Parts: parts, Role: genai.RoleModel},
		FinishReason:  mapFinishReason(choice.FinishReason),
		UsageMetadata: toUsageMetadata(usage),
	}
}

func toolCallToFunctionCall(tc chatToolCall) *genai.FunctionCall {
	var args map[string]any
	_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
	return &genai.FunctionCall{ID: tc.ID, Name: tc.Function.Name, Args: args}
}

func mapFinishReason(reason string) genai.FinishReason {
	switch reason {
	case "":
		return ""
	case "stop", "tool_calls", "eos":
		return genai.FinishReasonStop
	case "length":
		return genai.FinishReasonMaxTokens
	case "content_filter":
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonOther
	}
}

func toUsageMetadata(u *chatUsage) *genai.GenerateContentResponseUsageMetadata {
	if u == nil {
		return nil
	}
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     u.PromptTokens,
		CandidatesTokenCount: u.CompletionTokens,
		TotalTokenCount:      u.TotalTokens,
	}
}
