package adk

import (
	"context"

	"google.golang.org/genai"

	"tui-testing/internal/adk/openrouter"
)

// resolveContextWindow looks up modelName's context window (max input
// tokens) for provider — feeds Client.ContextWindow, which backs the
// top bar's context-usage indicator (see ui.App.contextWindow). Best-
// effort: any failure (network hiccup, unrecognized model, ...) just
// returns 0 ("unknown," which the UI renders as no bar at all) rather
// than erroring — this is a nice-to-have, not worth failing a connect
// over.
func resolveContextWindow(ctx context.Context, provider, modelName, apiKey string) int {
	if provider == ProviderOpenRouter {
		return openrouter.ContextWindow(ctx, modelName)
	}

	// Gemini (including provider == "", which buildModel treats the same
	// way) — a plain genai.Client here rather than reusing the ADK
	// gemini.NewModel instance built for the actual conversation, since
	// that wrapper exposes no way to reach the underlying client's
	// Models.Get.
	client, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: apiKey})
	if err != nil {
		return 0
	}
	m, err := client.Models.Get(ctx, modelName, nil)
	if err != nil {
		return 0
	}
	return int(m.InputTokenLimit)
}
