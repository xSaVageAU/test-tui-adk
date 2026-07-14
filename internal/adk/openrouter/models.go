package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// modelsURL lists every model OpenRouter serves, including each one's
// context_length — confirmed against the real endpoint to be public (no
// API key required). A var for the same testing reason as
// chatCompletionsURL.
var modelsURL = "https://openrouter.ai/api/v1/models"

type modelsListResponse struct {
	Data []struct {
		ID            string `json:"id"`
		ContextLength int    `json:"context_length"`
	} `json:"data"`
}

// ContextWindow looks up modelName's context length from OpenRouter's
// model catalog — used to feed the top bar's context-usage indicator
// (see internal/adk/contextwindow.go). Returns 0 on any failure
// (network error, non-200, malformed body) or if modelName isn't found
// in the list, rather than an error: this is a nice-to-have UI number,
// not worth failing a connect over, and the caller already treats 0 as
// "unknown."
func ContextWindow(ctx context.Context, modelName string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return 0
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}
	var list modelsListResponse
	if err := json.Unmarshal(data, &list); err != nil {
		return 0
	}

	for _, m := range list.Data {
		if m.ID == modelName {
			return m.ContextLength
		}
	}
	return 0
}
