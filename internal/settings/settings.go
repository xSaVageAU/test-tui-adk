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
	// VerboseTools governs how much detail a tool call/result shows in
	// the transcript — false (the default) is a one-line lean summary
	// per call (see internal/ui/chat.go's formatToolArgs/
	// formatToolResult); true is the full generic key=value/content
	// dump. Off by default: a model's own read_file/write_file/
	// list_files results are routine, not something worth reading in
	// full on every call.
	VerboseTools bool `json:"verboseTools"`
	// WorkingAnim is the active "agent is working" animation's name (see
	// internal/ui/workinganim.go's workingAnimNames) — same
	// name-as-persisted-id convention the theme picker already uses. ""
	// (an older settings.json, or one that's simply never been changed)
	// falls back to the first variant.
	WorkingAnim string `json:"workingAnim"`
	// HideReasoningText governs whether a reasoning-capable model's
	// actual thinking output (see internal/ui/chat.go's renderMessage)
	// is shown as its own block under the "agent" label. Stored inverted
	// — false (shown) is the wanted default, and phrasing the field this
	// way makes that default line up with bool's own zero value, so an
	// older settings.json that predates this field (or simply has it
	// omitted) naturally still shows the text rather than needing
	// special "field absent" handling the way a field defaulting to true
	// would. App.showReasoning (its positive form) is what the rest of
	// the UI actually reads — see NewApp/persistSettings for the
	// negation, kept at this one boundary rather than spreading
	// !hideReasoningText through render code.
	HideReasoningText bool `json:"hideReasoningText"`
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
