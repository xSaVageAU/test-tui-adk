package openrouter

// This file is the plain-JSON wire shape of OpenRouter's (OpenAI-
// compatible) chat-completions API — no client SDK exists to import,
// so these are hand-declared against the documented format
// (https://openrouter.ai/docs/api-reference/chat-completion). Kept
// separate from openrouter.go's translation logic so the two concerns
// (what the wire looks like vs. how genai maps onto it) stay easy to
// tell apart.

type chatRequest struct {
	Model         string             `json:"model"`
	Messages      []chatMessage      `json:"messages"`
	Tools         []chatTool         `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	Temperature   *float32           `json:"temperature,omitempty"`
	TopP          *float32           `json:"top_p,omitempty"`
	MaxTokens     int32              `json:"max_tokens,omitempty"`
	Stop          []string           `json:"stop,omitempty"`
	Stream        bool               `json:"stream,omitempty"`
	StreamOptions *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// chatMessage doubles as both an outbound request message and (via
// chatStreamChunk.Delta) an inbound streaming delta fragment — the
// fields OpenRouter actually sends for a delta (role/content/tool_calls)
// are a subset of what a full message can hold, so one struct covers
// both without needing a second near-identical type.
type chatMessage struct {
	Role       string         `json:"role,omitempty"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	// Index only carries meaning in a streaming delta (which of
	// possibly several parallel tool calls this fragment belongs to);
	// zero-valued and unused everywhere else.
	Index    int              `json:"index,omitempty"`
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function chatToolCallFunc `json:"function"`
}

type chatToolCallFunc struct {
	Name string `json:"name,omitempty"`
	// Arguments is a JSON object encoded as a string (OpenAI's
	// convention, not nested JSON) — in a streaming delta this is a
	// fragment to be concatenated with prior fragments for the same
	// tool call index, only valid to parse once the call is complete.
	Arguments string `json:"arguments,omitempty"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Parameters is `any`, not map[string]any: it usually holds a
	// *jsonschema.Schema straight from ADK's own functiontool package
	// (see parametersSchema in openrouter.go) — that type marshals
	// itself correctly via its own MarshalJSON, and round-tripping it
	// through a map first would be pure overhead. The map[string]any
	// fallback path (schemaToJSONSchema) marshals fine here too.
	Parameters any `json:"parameters,omitempty"`
}

type chatUsage struct {
	PromptTokens     int32 `json:"prompt_tokens"`
	CompletionTokens int32 `json:"completion_tokens"`
	TotalTokens      int32 `json:"total_tokens"`
}

type chatError struct {
	Message string `json:"message"`
}

// chatResponse is a non-streaming POST's whole body.
type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage,omitempty"`
	Error   *chatError   `json:"error,omitempty"`
}

type chatChoice struct {
	Message      *chatMessage `json:"message,omitempty"`
	FinishReason string       `json:"finish_reason"`
}

// chatStreamChunk is one `data: {...}` SSE event's decoded JSON.
type chatStreamChunk struct {
	Choices []chatStreamChoice `json:"choices"`
	Usage   *chatUsage         `json:"usage,omitempty"`
	Error   *chatError         `json:"error,omitempty"`
}

type chatStreamChoice struct {
	Delta        *chatMessage `json:"delta,omitempty"`
	FinishReason string       `json:"finish_reason"`
}
