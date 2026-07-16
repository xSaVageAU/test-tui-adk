// Package openrouter implements model.LLM against OpenRouter's chat-
// completions API (https://openrouter.ai/api/v1/chat/completions), which
// is OpenAI-compatible. Unlike Gemini, ADK v2 ships no adapter for this
// shape (only model/gemini and model/apigee) — everything here is a
// from-scratch translation between genai's request/response types and
// OpenAI-style JSON, including streaming (SSE) and tool-call round-
// tripping. Mirrors model/gemini's file shape (NewModel, Name,
// GenerateContent) so buildModel (internal/adk/models.go) can treat both
// providers identically.
//
// This file is just the model type and the actual HTTP calls
// (generate/generateStream/buildRequest/setHeaders); every genai<->wire
// conversion lives in convert.go, and the wire-format JSON structs
// themselves in wire.go.
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
