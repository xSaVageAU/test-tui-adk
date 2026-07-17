package settings

import (
	"os"
	"testing"

	"tui-testing/internal/appdir"
)

// withTempAppDir redirects appdir.Dir() (os.UserHomeDir(), which reads
// USERPROFILE on Windows) to a fresh temp directory for one test, so
// Load/Save never touch this machine's real ~/.tui-testing/settings.toml
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

	path, err := appdir.Path(settingsFileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected %s to be self-healed onto disk on first load, stat error: %v", settingsFileName, err)
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
