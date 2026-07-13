package adk

import (
	"context"
	"fmt"

	"google.golang.org/genai"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
)

// DefaultModelName is used whenever neither settings.json's agent
// section nor a sub-agent's own config specifies a model.
const DefaultModelName = "gemini-3.1-flash-lite"

// buildModel resolves (provider, modelName) to a real model.LLM — an
// empty provider defaults to providerGemini, an empty modelName to
// DefaultModelName. Only Gemini is actually implemented today; any
// other provider value is a clear, immediate error rather than a silent
// fallback, since picking the wrong model without saying so would be
// far more confusing than refusing to start.
func buildModel(ctx context.Context, provider, modelName, apiKey string) (model.LLM, error) {
	if provider == "" {
		provider = providerGemini
	}
	if modelName == "" {
		modelName = DefaultModelName
	}

	switch provider {
	case providerGemini:
		return gemini.NewModel(ctx, modelName, &genai.ClientConfig{APIKey: apiKey})
	default:
		return nil, fmt.Errorf("unsupported provider %q (only %q is implemented today)", provider, providerGemini)
	}
}
