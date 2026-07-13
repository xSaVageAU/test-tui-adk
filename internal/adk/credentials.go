package adk

import (
	"encoding/json"
	"fmt"
	"os"
)

// providerGemini identifies Gemini in the persisted credentials file
// (and in settings.json / a sub-agent's agent.json — see models.go) —
// the only provider today, but keying by name from the start means
// adding OpenRouter/LM Studio/Ollama later is a new map entry, not a
// file-format migration.
const providerGemini = "gemini"

// credentialsFile is provider name -> arbitrary string fields, kept
// loose rather than a fixed per-provider struct: an API key is all
// Gemini needs, but a future local provider like Ollama would want a
// baseUrl instead of (or alongside) a key, and that shape isn't settled
// yet for providers this app doesn't support.
type credentialsFile struct {
	Providers map[string]map[string]string `json:"providers"`
}

// SaveAPIKey persists apiKey as the Gemini provider's credential in
// appdir's data/credentials.json, creating or updating the file. Called
// only after a key has been proven to work (New succeeded with it) —
// see main.go's newBackend — so a typo'd key never ends up on disk.
func SaveAPIKey(apiKey string) error {
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
	if cf.Providers[providerGemini] == nil {
		cf.Providers[providerGemini] = map[string]string{}
	}
	cf.Providers[providerGemini]["apiKey"] = apiKey

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

// LoadAPIKey returns the persisted Gemini provider API key, or "" if
// none has been saved yet — a fresh install (or one where /key has
// never been used) has no credentials file at all, which is not an
// error condition here.
func LoadAPIKey() (string, error) {
	path, err := dataPath("credentials.json")
	if err != nil {
		return "", fmt.Errorf("resolve credentials path: %w", err)
	}
	cf, err := readCredentialsFile(path)
	if err != nil {
		return "", err
	}
	return cf.Providers[providerGemini]["apiKey"], nil
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
