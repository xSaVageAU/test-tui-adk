package settings

import (
	"os"
	"testing"

	"tui-testing/internal/appdir"
)

// withTempAppDir redirects appdir.Dir() (os.UserHomeDir(), which reads
// USERPROFILE on Windows) to a fresh temp directory for one test, so
// Load/Save never touch this machine's real ~/.tui-testing/settings.json
// — appdir has no dependency-injection seam, so an env var override is
// the only way to isolate this without touching real user data.
func withTempAppDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir) // portable if this ever runs on non-Windows CI
}

func TestLoadFreshInstallReturnsDefaults(t *testing.T) {
	withTempAppDir(t)

	s := Load()
	if s.UI != DefaultUISettings() {
		t.Errorf("UI = %+v, want defaults %+v", s.UI, DefaultUISettings())
	}
	if s.Agent != DefaultAgentSettings() {
		t.Errorf("Agent = %+v, want defaults %+v", s.Agent, DefaultAgentSettings())
	}

	path, err := appdir.Path("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected settings.json to be self-healed onto disk on first load, stat error: %v", err)
	}
}

// TestLoadMigratesLegacyPermissionModeFromUISection covers the real
// backward-compatibility risk from splitting UISettings/AgentSettings
// apart: a settings.json written before the split had permissionMode
// nested under "ui" — this app's own real on-disk file was exactly this
// shape at the time of the split. Without an explicit migration, a user
// who'd set full-auto would silently and silently have it reset back to
// normal on next launch; this pins down that it's recovered instead.
func TestLoadMigratesLegacyPermissionModeFromUISection(t *testing.T) {
	withTempAppDir(t)
	path, err := appdir.Path("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"ui":{"highlightUser":true,"streamReplies":true,"hitlMode":"modal","permissionMode":"full-auto","verboseTools":false}}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	s := Load()
	if s.Agent.PermissionMode != ModeFullAuto {
		t.Errorf("Agent.PermissionMode = %q, want %q (migrated from the old ui.permissionMode location)", s.Agent.PermissionMode, ModeFullAuto)
	}
	if !s.UI.HighlightUser || !s.UI.StreamReplies {
		t.Errorf("UI fields lost during migration: %+v", s.UI)
	}
}

// TestLoadDoesNotMigrateWhenAgentSectionAlreadyPresent covers the
// already-migrated case — a real "agent" key should win outright, not
// get silently overwritten by whatever (if anything) still happens to
// linger under the old "ui" location.
func TestLoadDoesNotMigrateWhenAgentSectionAlreadyPresent(t *testing.T) {
	withTempAppDir(t)
	path, err := appdir.Path("settings.json")
	if err != nil {
		t.Fatal(err)
	}
	current := `{"ui":{"highlightUser":true,"streamReplies":true,"hitlMode":"modal"},"agent":{"permissionMode":"full-auto"}}`
	if err := os.WriteFile(path, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}

	s := Load()
	if s.Agent.PermissionMode != ModeFullAuto {
		t.Errorf("Agent.PermissionMode = %q, want %q", s.Agent.PermissionMode, ModeFullAuto)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	withTempAppDir(t)
	want := Settings{
		UI: UISettings{
			HighlightUser: false,
			StreamReplies: true,
			HITLMode:      "inline",
			VerboseTools:  true,
			WorkingAnim:   "Bars",
			PopupWidth:    70,
		},
		Agent: AgentSettings{PermissionMode: ModeFullAuto},
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got != want {
		t.Errorf("round-tripped Settings = %+v, want %+v", got, want)
	}
}
