package ui

import "tui-testing/internal/settings"

// permissionMode governs whether a confirmation-gated tool call (write_file
// today) actually asks for approval — separate from hitlMode, which only
// controls how a pending confirmation is *presented*, not whether one
// happens at all. The actual policy decision lives in internal/adk's
// confirmGated; this is just the UI-side toggle/display for the same
// settings.json value it reads.
//
// Only two pre-defined modes for now — normal (each tool's own sensible
// default) and full-auto (nothing ever confirms) — not yet
// user-customizable per-tool. See the config-discovery-pattern memory
// for the fuller design intent this is a first slice of.
type permissionMode int

const (
	permissionNormal permissionMode = iota
	permissionFullAuto
)

func (m permissionMode) String() string {
	if m == permissionFullAuto {
		return settings.ModeFullAuto
	}
	return settings.ModeNormal
}

func (m permissionMode) next() permissionMode {
	return (m + 1) % 2
}

// parsePermissionMode is String's inverse, used when restoring
// permissionMode from settings.json — anything other than exactly
// settings.ModeFullAuto (including "", an older file written before
// this setting existed) falls back to normal, matching
// confirmGated's own fail-safe treatment of the same value.
func parsePermissionMode(s string) permissionMode {
	if s == settings.ModeFullAuto {
		return permissionFullAuto
	}
	return permissionNormal
}

// toggleAutoAccept is the shift+tab hotkey's handler — same effect as
// picking "Tool approval mode" in /settings (toggleSetting("permission")
// does the identical flip+persist), just reachable instantly from
// anywhere instead of needing to open the menu first. No system message
// on top of that — the help-footer's own shift+tab badge (see footer.go's
// renderHelpFooter) already recolors to reflect the new state; a
// chat-log entry on every toggle was noisy.
func (a *App) toggleAutoAccept() {
	a.toggleSetting("permission")
}
