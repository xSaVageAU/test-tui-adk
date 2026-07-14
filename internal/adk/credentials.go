package adk

import (
	"encoding/json"
	"fmt"
	"os"
)

// ProviderGemini/ProviderOpenRouter identify providers in the persisted
// credentials file, in settings.json, and in an agent's own agent.json
// (see models.go) — keying by name from the start means adding another
// provider is a new map entry and a buildModel case, not a file-format
// migration. ProviderOpenRouter is declared ahead of having a working
// implementation (see buildModel) specifically so credentials.go and an
// agent.json can already refer to it by name; buildModel is what still
// needs a real OpenRouter model.LLM behind it.
const (
	ProviderGemini     = "gemini"
	ProviderOpenRouter = "openrouter"
)

// credentialsFile is provider name -> arbitrary string fields, kept
// loose rather than a fixed per-provider struct: an API key is all
// Gemini needs, but a future local provider like Ollama would want a
// baseUrl instead of (or alongside) a key, and that shape isn't settled
// yet for providers this app doesn't support.
type credentialsFile struct {
	Providers map[string]map[string]string `json:"providers"`
}

// SaveAPIKey persists apiKey as the given provider's credential in
// appdir's data/credentials.json, creating or updating the file. Called
// only after a key has been proven to work (New succeeded with it) —
// see main.go's newBackend — so a typo'd key never ends up on disk.
func SaveAPIKey(provider, apiKey string) error {
	path, err := dataPath("credentials.json")
	if err != nil {
		return fmt.Errorf("resolve credentials path: %w", err)
	}

	cf, err := readCredentialsFile(path)
	if err != nil {
		return err
	}
	if cf.Providers == nil {
		cf.Providers = map[string]map[string]string{}
	}
	if cf.Providers[provider] == nil {
		cf.Providers[provider] = map[string]string{}
	}
	cf.Providers[provider]["apiKey"] = apiKey

	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}
	// 0o600: this file holds live API keys, unlike everything else this
	// app writes to appdir — deliberately tighter than the 0o644 used
	// elsewhere in the codebase.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}
	return nil
}

// LoadAPIKey returns the persisted API key for the given provider, or ""
// if none has been saved yet — a fresh install (or one where /key has
// never been used for that provider) has no credentials file, or no
// entry for that provider, which is not an error condition here.
func LoadAPIKey(provider string) (string, error) {
	path, err := dataPath("credentials.json")
	if err != nil {
		return "", fmt.Errorf("resolve credentials path: %w", err)
	}
	cf, err := readCredentialsFile(path)
	if err != nil {
		return "", err
	}
	return cf.Providers[provider]["apiKey"], nil
}

func readCredentialsFile(path string) (credentialsFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return credentialsFile{}, nil
		}
		return credentialsFile{}, fmt.Errorf("read credentials: %w", err)
	}
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return credentialsFile{}, fmt.Errorf("parse credentials: %w", err)
	}
	return cf, nil
}
