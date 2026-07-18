// Package settings owns ~/.tui-testing/settings.toml — the app's
// general-purpose UI/agent settings file, distinct from credentials.json
// (secrets) and an agent's own agent.json (root or sub-agent config,
// including which provider/model it runs on — see internal/adk). TOML
// rather than JSON specifically because, unlike agent.json (mostly
// written by the root agent or an interface, not hand-edited) and the
// theme files, this is the one config surface meant to be comfortable to
// edit by hand — a lighter syntax is worth a dependency here. Load
// normalizes the file to the complete current schema on every read (see
// Load), so every available option is always present as boilerplate to
// fill in, and no field is silently absent; the trade-off is that the
// file is machine-managed — hand-added comments aren't preserved (the
// same as a /settings toggle already rewriting it via Save). It's a
// neutral package, like internal/appdir and internal/theme: internal/ui
// both writes this (whenever a /settings toggle changes) and reads it at
// startup.
package settings

import (
	"bytes"
	"os"

	"tui-testing/internal/appdir"

	"github.com/pelletier/go-toml/v2"
)

const settingsFileName = "settings.toml"

// Settings is the whole shape of settings.toml. Split into a top-level
// section per concern — UI (purely visual/TUI toggles) and Agent (agent/
// tool execution policy) — rather than one flat section, so each can
// grow its own larger shape later (Agent in particular is expected to
// gain a lot more, e.g. per-tool policy) without a migration or the two
// kinds of setting getting tangled together in one struct.
type Settings struct {
	UI    UISettings    `toml:"ui"`
	Agent AgentSettings `toml:"agent"`
}

// UISettings mirrors the toggles /settings' "TUI Settings" page exposes
// — see internal/ui/commands.go's openTUISettingsMenu. Written
// automatically by the app every time one changes. Purely
// display/interaction concerns — anything that changes what the agent
// itself is allowed to do belongs in AgentSettings instead.
type UISettings struct {
	HighlightUser bool   `toml:"highlightUser"`
	StreamReplies bool   `toml:"streamReplies"`
	HITLMode      string `toml:"hitlMode"` // "modal" or "inline"
	// VerboseTools governs how much detail a tool call/result shows in
	// the transcript — false (the default) is a one-line lean summary
	// per call (see internal/ui/chat.go's formatToolArgs/
	// formatToolResult); true is the full generic key=value/content
	// dump. Off by default: a model's own read_file/write_file/
	// list_files results are routine, not something worth reading in
	// full on every call.
	VerboseTools bool `toml:"verboseTools"`
	// WorkingAnim is the active "agent is working" animation's name (see
	// internal/ui/workinganim.go's workingAnimNames) — same
	// name-as-persisted-id convention the theme picker already uses. ""
	// (an older settings file, or one that's simply never been changed)
	// falls back to the first variant.
	WorkingAnim string `toml:"workingAnim"`
	// HideReasoningText governs whether a reasoning-capable model's
	// actual thinking output (see internal/ui/chat.go's renderMessage)
	// is shown as its own block under the "agent" label. Stored inverted
	// — false (shown) is the wanted default, and phrasing the field this
	// way makes that default line up with bool's own zero value, so an
	// older settings file that predates this field (or simply has it
	// omitted) naturally still shows the text rather than needing
	// special "field absent" handling the way a field defaulting to true
	// would. App.showReasoning (its positive form) is what the rest of
	// the UI actually reads — see NewApp/persistSettings for the
	// negation, kept at this one boundary rather than spreading
	// !hideReasoningText through render code.
	HideReasoningText bool `toml:"hideReasoningText"`
	// PopupWidth/PopupHeight override the outer size every popup modal
	// renders at (the command palette, /settings, /agents, the /key and
	// /agents-model text fields — see internal/ui/app.go's
	// effectivePopupWidth/effectivePopupHeight, the only readers of these
	// two fields). 0 means "unset", falling back to popupWidthDefault/
	// popupHeightDefault there — omitempty keeps an untouched settings
	// file free of two keys nobody's ever edited.
	PopupWidth  int `toml:"popupWidth,omitempty"`
	PopupHeight int `toml:"popupHeight,omitempty"`
	// ToolPreviewMaxLines caps how many lines of a tool's content (
	// read_file's result, write_file's written content) verbose mode
	// shows before truncating — see internal/ui/toolformat.go's
	// toolPreviewMaxLinesDefault. 0 means "unset", falling back to that
	// default.
	ToolPreviewMaxLines int `toml:"toolPreviewMaxLines,omitempty"`
}

// AgentSettings mirrors /settings' "Agent Settings" page — agent/tool
// execution policy, distinct from UISettings' display concerns. Expected
// to grow (per-tool policy, execution targets, ...) as the app's config
// surface expands; kept as its own top-level settings section from the
// start specifically so that growth doesn't need a migration later.
type AgentSettings struct {
	// PermissionMode governs whether a confirmation-gated tool call
	// (write_file today) actually asks for approval — see
	// internal/adk/tools/gate.go's confirmGatedTool, the actual consumer
	// of this value. Any value other than ModeFullAuto (including "",
	// covering an older settings file written before this field
	// existed) is treated as ModeNormal — deliberately fail-safe rather
	// than needing its own explicit "missing/malformed" handling here.
	PermissionMode string `toml:"permissionMode"`

	// Target selects where the agent's tools actually run — the local
	// host (the default) or a remote machine over SSH. See TargetSettings
	// and internal/adk/tools/target.go, the consumer of this value.
	Target TargetSettings `toml:"target"`
}

// TargetSettings selects the execution target for every tool. Type
// "host" (or "", the default) runs everything locally; "ssh" runs
// commands over a persistent SSH connection and does file operations over
// SFTP, using the SSH block below. Kept as its own struct so more target
// kinds (container, remote agent, ...) can slot in later — see the
// tool-execution-targets memory for the fuller intent.
//
// No omitempty on Target/SSH or their fields: the whole block is always
// written to settings.toml (see Load's normalization) so a user switching
// to SSH finds every field already scaffolded to fill in, rather than
// having to know the schema and type it out.
type TargetSettings struct {
	Type string      `toml:"type"` // "host" (default) or "ssh"
	SSH  SSHSettings `toml:"ssh"`
}

// SSHSettings is the connection detail for an "ssh" target. Auth is by
// private key only (no passwords stored on disk): KeyPath, or the first
// of ~/.ssh/id_ed25519 / ~/.ssh/id_rsa if unset. Host keys are verified
// against known_hosts (KnownHosts, or ~/.ssh/known_hosts) unless
// InsecureSkipHostKey is set — so a first connection to an unknown host
// fails with a clear message rather than silently trusting it.
type SSHSettings struct {
	Host                string `toml:"host"`
	Port                int    `toml:"port"` // default DefaultSSHPort
	User                string `toml:"user"`
	KeyPath             string `toml:"keyPath"`
	KnownHosts          string `toml:"knownHosts"`
	InsecureSkipHostKey bool   `toml:"insecureSkipHostKey"`
}

// TargetHost/TargetSSH are TargetSettings.Type's valid values. "" is
// treated as TargetHost (fail-safe: an older settings file, or one that
// never set a target, runs locally). DefaultSSHPort is the port used when
// SSHSettings.Port is left 0.
const (
	TargetHost     = "host"
	TargetSSH      = "ssh"
	DefaultSSHPort = 22
)

// ModeNormal/ModeFullAuto are PermissionMode's only two valid values —
// pre-defined, not yet user-customizable per-tool (see the
// config-discovery-pattern memory for the fuller design intent this is
// a first slice of).
const (
	ModeNormal   = "normal"
	ModeFullAuto = "full-auto"
)

// DefaultUISettings is what a fresh install (no settings file yet, or
// one whose UI section is missing/malformed) starts from.
func DefaultUISettings() UISettings {
	return UISettings{HighlightUser: true, StreamReplies: true, HITLMode: "modal"}
}

// DefaultAgentSettings is AgentSettings' equivalent of DefaultUISettings —
// including a fully-populated (host) Target block so a fresh settings.toml
// already carries the whole [agent.target.ssh] scaffold, not just the
// fields that happen to be non-zero.
func DefaultAgentSettings() AgentSettings {
	return AgentSettings{
		PermissionMode: ModeNormal,
		Target: TargetSettings{
			Type: TargetHost,
			SSH:  SSHSettings{Port: DefaultSSHPort},
		},
	}
}

// Load reads settings.toml, falling back to DefaultUISettings()/
// DefaultAgentSettings() whenever the file is missing, unreadable, or
// malformed — always returns something usable, never an error the
// caller needs to handle specially, matching theme.Load()'s same
// best-effort shape. If the file didn't exist at all (a fresh install),
// the defaults are written out immediately — same self-heal-on-load
// behavior as the root agent's config (see internal/adk/rootagent.go) —
// so settings.toml shows up on disk from first launch, not only after
// the first toggle change.
func Load() Settings {
	fallback := Settings{UI: DefaultUISettings(), Agent: DefaultAgentSettings()}

	path, err := appdir.Path(settingsFileName)
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
	if err := toml.Unmarshal(data, &s); err != nil {
		return fallback
	}
	if s.UI.HITLMode == "" {
		s.UI = DefaultUISettings()
	}
	// Default only the empty field, not the whole Agent section — a file
	// that sets [agent.target] but omits permissionMode (e.g. hand-edited
	// to add an SSH target) must keep its Target block, not have it wiped
	// by a blanket reset to DefaultAgentSettings().
	if s.Agent.PermissionMode == "" {
		s.Agent.PermissionMode = ModeNormal
	}
	if s.Agent.Target.Type == "" {
		s.Agent.Target.Type = TargetHost
	}
	if s.Agent.Target.SSH.Port == 0 {
		s.Agent.Target.SSH.Port = DefaultSSHPort
	}

	// Normalize the file to the complete current schema: if the parsed-
	// and-defaulted settings don't already reproduce the file byte-for-
	// byte — because a config field was added since it was last written,
	// or it was hand-edited into non-canonical form — rewrite it, so every
	// available option is present as boilerplate to fill in rather than
	// silently absent. Idempotent once canonical (Marshal reproduces it
	// exactly, so nothing is rewritten). Best-effort, and hand-added
	// comments aren't preserved through it — the file is machine-managed,
	// the same as any /settings toggle already rewrites it via Save.
	if canonical, err := toml.Marshal(s); err == nil && !bytes.Equal(canonical, data) {
		_ = os.WriteFile(path, canonical, 0o644)
	}
	return s
}

// Save persists the whole Settings value to settings.toml, overwriting
// the file.
func Save(s Settings) error {
	path, err := appdir.Path(settingsFileName)
	if err != nil {
		return err
	}
	data, err := toml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
