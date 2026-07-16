// This file is the translation layer between genai's request/response
// types and OpenRouter's OpenAI-compatible chat-completions JSON shape
// (see wire.go for the JSON structs themselves) — openrouter.go only
// knows how to make the HTTP call; every genai<->wire conversion lives
// here.
package openrouter

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
)

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
