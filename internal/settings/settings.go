// Package settings owns ~/.tui-testing/settings.json — the app's one
// general-purpose settings file, distinct from credentials.json
// (secrets) and a sub-agent's own agent.json (per-specialist config).
// It's a neutral package, like internal/appdir and internal/theme:
// internal/ui (writes the UI section whenever a /settings toggle
// changes, reads it at startup) and internal/adk (only ever reads the
// Agent section, hand-edited by the user, to pick the root agent's
// provider/model) both import this directly rather than one importing
// the other.
package settings

import (
	"encoding/json"
	"os"

	"tui-testing/internal/appdir"
)

// Settings is the whole shape of settings.json.
type Settings struct {
	UI    UISettings    `json:"ui"`
	Agent AgentSettings `json:"agent"`
}

// UISettings mirrors the toggles /settings exposes in the TUI — see
// internal/ui/commands.go's openSettingsMenu. Written automatically by
// the app every time one changes.
type UISettings struct {
	HighlightUser bool   `json:"highlightUser"`
	StreamReplies bool   `json:"streamReplies"`
	HITLMode      string `json:"hitlMode"` // "modal" or "inline"
}

// AgentSettings picks the root agent's provider/model. Hand-edited by
// the user, the same way a sub-agent's own agent.json is — this app
// never writes this section itself. Empty fields mean "use
// internal/adk's built-in default," resolved there rather than here,
// since what that default actually is is an adk-domain concern.
type AgentSettings struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

// DefaultUISettings is what a fresh install (no settings.json yet, or
// one whose UI section is missing/malformed) starts from.
func DefaultUISettings() UISettings {
	return UISettings{HighlightUser: true, StreamReplies: true, HITLMode: "modal"}
}

// Load reads settings.json, falling back to DefaultUISettings() for the
// UI section (and a zero AgentSettings, meaning "use the built-in
// default") whenever the file is missing, unreadable, or malformed —
// always returns something usable, never an error the caller needs to
// handle specially, matching theme.Load()'s same best-effort shape.
func Load() Settings {
	fallback := Settings{UI: DefaultUISettings()}

	path, err := appdir.Path("settings.json")
	if err != nil {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return fallback
	}
	if s.UI.HITLMode == "" {
		s.UI = DefaultUISettings()
	}
	return s
}

// Save persists the whole Settings value, overwriting the file. A
// caller that only means to change the UI section should Load first and
// mutate that field in place, so the Agent section — hand-edited by the
// user — round-trips untouched.
func Save(s Settings) error {
	path, err := appdir.Path("settings.json")
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
