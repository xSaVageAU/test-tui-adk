// Package settings owns ~/.tui-testing/settings.json — the app's
// general-purpose UI settings file, distinct from credentials.json
// (secrets) and an agent's own agent.json (root or sub-agent config,
// including which provider/model it runs on — see internal/adk). It's a
// neutral package, like internal/appdir and internal/theme:
// internal/ui both writes this (whenever a /settings toggle changes)
// and reads it at startup.
package settings

import (
	"encoding/json"
	"os"

	"tui-testing/internal/appdir"
)

// Settings is the whole shape of settings.json. Wrapped in a "ui" key
// rather than being flat so a future unrelated section (not agent
// config — that lives in agent.json now) has somewhere to go without a
// migration.
type Settings struct {
	UI UISettings `json:"ui"`
}

// UISettings mirrors the toggles /settings exposes in the TUI — see
// internal/ui/commands.go's openSettingsMenu. Written automatically by
// the app every time one changes.
type UISettings struct {
	HighlightUser bool   `json:"highlightUser"`
	StreamReplies bool   `json:"streamReplies"`
	HITLMode      string `json:"hitlMode"` // "modal" or "inline"
	// PermissionMode governs whether a confirmation-gated tool call
	// (write_file today) actually asks for approval — see
	// internal/adk/toolgate.go's confirmGated, the actual consumer of
	// this value. Any value other than ModeFullAuto (including "",
	// covering an older settings.json written before this field
	// existed) is treated as ModeNormal — deliberately fail-safe rather
	// than needing its own explicit "missing/malformed" handling here.
	PermissionMode string `json:"permissionMode"`
}

// ModeNormal/ModeFullAuto are PermissionMode's only two valid values —
// pre-defined, not yet user-customizable per-tool (see the
// config-discovery-pattern memory for the fuller design intent this is
// a first slice of).
const (
	ModeNormal   = "normal"
	ModeFullAuto = "full-auto"
)

// DefaultUISettings is what a fresh install (no settings.json yet, or
// one whose UI section is missing/malformed) starts from.
func DefaultUISettings() UISettings {
	return UISettings{HighlightUser: true, StreamReplies: true, HITLMode: "modal", PermissionMode: ModeNormal}
}

// Load reads settings.json, falling back to DefaultUISettings()
// whenever the file is missing, unreadable, or malformed — always
// returns something usable, never an error the caller needs to handle
// specially, matching theme.Load()'s same best-effort shape. If the
// file didn't exist at all (a fresh install), the defaults are written
// out immediately — same self-heal-on-load behavior as the root agent's
// config (see internal/adk/rootagent.go) — so settings.json shows up on
// disk from first launch, not only after the first toggle change.
func Load() Settings {
	fallback := Settings{UI: DefaultUISettings()}

	path, err := appdir.Path("settings.json")
	if err != nil {
		return fallback
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = Save(fallback) // best-effort; Load must still work even if this fails
		}
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

// Save persists the whole Settings value, overwriting the file.
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
