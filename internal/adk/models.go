package adk

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"

	"tui-testing/internal/adk/openrouter"
)

// DefaultModelName is used whenever neither an agent's own agent.json
// nor (for the root agent's Gemini key specifically) a caller-supplied
// override specifies a model.
const DefaultModelName = "gemini-3.1-flash-lite"

// buildModel resolves (provider, modelName) to a real model.LLM — an
// empty provider defaults to ProviderGemini, an empty modelName to
// DefaultModelName. apiKeyOverride lets a caller hand in a key it
// already has on hand for this exact provider (main.go's startup flow,
// or a freshly-typed-but-not-yet-saved /key submission — see
// keyOverride); left empty, the key is looked up from
// data/credentials.json via LoadAPIKey instead, which is how every
// provider besides whatever the caller's own override happens to be for
// gets its key, including a sub-agent configured for a different
// provider than root.
//
// Gemini and OpenRouter are both implemented. Any other provider value
// is a clear, immediate error rather than a silent fallback, since
// picking the wrong model without saying so would be far more confusing
// than refusing to start. DefaultModelName only applies to Gemini —
// there's no sensible cross-provider default model slug, so an
// OpenRouter config with no "model" set is its own explicit error
// rather than silently trying to run a Gemini model name against
// OpenRouter.
func buildModel(ctx context.Context, provider, modelName, apiKeyOverride string) (model.LLM, error) {
	if provider == "" {
		provider = ProviderGemini
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
		if modelName == "" {
			modelName = DefaultModelName
		}
		return gemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})
	case ProviderOpenRouter:
		if modelName == "" {
			return nil, fmt.Errorf("provider %q requires an explicit \"model\" in agent.json (e.g. \"openai/gpt-4o-mini\")", provider)
		}
		return openrouter.NewModel(modelName, apiKey), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q (only %q and %q are implemented today)", provider, ProviderGemini, ProviderOpenRouter)
	}
}

// keyOverride returns apiKey when configProvider (an agent's own
// provider, "" meaning ProviderGemini) matches callerProvider (the
// provider the caller's apiKey was actually issued for, same
// empty-means-Gemini default) — "" otherwise, letting buildModel fall
// through to its own LoadAPIKey(provider) lookup. Callers in this
// package (buildRootAgent, buildSubAgents) only ever have one freshly-
// supplied key on hand at a time — main.go's startup flow always offers
// a Gemini one; /key offers whatever provider its popup asked for — so
// this is what stops that key from being incorrectly reused for an
// agent configured for a different provider.
func keyOverride(configProvider, callerProvider, apiKey string) string {
	if configProvider == "" {
		configProvider = ProviderGemini
	}
	if callerProvider == "" {
		callerProvider = ProviderGemini
	}
	if configProvider == callerProvider {
		return apiKey
	}
	return ""
}
