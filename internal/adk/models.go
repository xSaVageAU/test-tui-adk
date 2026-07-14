package adk

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
)

// DefaultModelName is used whenever neither an agent's own agent.json
// nor (for the root agent's Gemini key specifically) a caller-supplied
// override specifies a model.
const DefaultModelName = "gemini-3.1-flash-lite"

// buildModel resolves (provider, modelName) to a real model.LLM — an
// empty provider defaults to ProviderGemini, an empty modelName to
// DefaultModelName. apiKeyOverride lets a caller hand in a key it
// already has on hand for this exact provider (main.go only ever
// sources a Gemini key, from GOOGLE_API_KEY or /key — see
// geminiOverride); left empty, the key is looked up from
// data/credentials.json via LoadAPIKey instead, which is how every
// provider besides "whatever main.go started with" gets its key,
// including a sub-agent configured for a different provider than root.
//
// Only Gemini is actually implemented today. ProviderOpenRouter is
// recognized (so agent.json/credentials.json can already name it) but
// deliberately stubbed — it needs its own model.LLM implementation
// (OpenRouter speaks an OpenAI-compatible chat-completions API; ADK
// only ships Gemini and Apigee model packages, so this would be a
// from-scratch adapter, not a config flip) — that's future work, this
// is just the seam it'll plug into. Any other provider value is a
// clear, immediate error rather than a silent fallback, since picking
// the wrong model without saying so would be far more confusing than
// refusing to start.
func buildModel(ctx context.Context, provider, modelName, apiKeyOverride string) (model.LLM, error) {
	if provider == "" {
		provider = ProviderGemini
	}
	if modelName == "" {
		modelName = DefaultModelName
	}

	apiKey := apiKeyOverride
	if apiKey == "" {
		var err error
		apiKey, err = LoadAPIKey(provider)
		if err != nil {
			return nil, err
		}
	}
	if apiKey == "" {
		return nil, fmt.Errorf("no API key saved for provider %q — use /key to set one", provider)
	}

	switch provider {
	case ProviderGemini:
		return gemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})
	case ProviderOpenRouter:
		return nil, fmt.Errorf("provider %q is not implemented yet — OpenRouter support is planned but not built", provider)
	default:
		return nil, fmt.Errorf("unsupported provider %q (only %q is implemented today)", provider, ProviderGemini)
	}
}

// geminiOverride returns apiKey when provider resolves to Gemini (empty
// counts as Gemini, same default buildModel applies), "" otherwise.
// Callers in this package (buildRootAgent, buildSubAgents) only ever
// have a Gemini key on hand to offer as an override — main.go sources
// it from GOOGLE_API_KEY or the /key popup, both Gemini-specific — so a
// non-Gemini provider always falls through to buildModel's own
// LoadAPIKey lookup instead of incorrectly reusing this one.
func geminiOverride(provider, apiKey string) string {
	if provider == "" || provider == ProviderGemini {
		return apiKey
	}
	return ""
}
