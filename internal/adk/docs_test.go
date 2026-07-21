package adk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tui-testing/internal/appdir"
	"tui-testing/internal/settings"
)

func TestMaterializeDocsSeedsOnFreshInstall(t *testing.T) {
	withTempAppDir(t)

	if err := materializeDocs(); err != nil {
		t.Fatalf("materializeDocs: %v", err)
	}

	dir, err := appdir.Path("docs")
	if err != nil {
		t.Fatalf("appdir.Path: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "self-extend-mcp.md"))
	if err != nil {
		t.Fatalf("expected self-extend-mcp.md to be seeded: %v", err)
	}
	want, err := docsDefaultsFS.ReadFile("docsdefaults/self-extend-mcp.md")
	if err != nil {
		t.Fatalf("read embedded default: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("seeded doc content doesn't match the embedded default")
	}
}

func TestMaterializeDocsNeverOverwritesAnExistingFile(t *testing.T) {
	withTempAppDir(t)

	dir, err := appdir.Path("docs")
	if err != nil {
		t.Fatalf("appdir.Path: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	custom := "user-edited content, must survive"
	if err := os.WriteFile(filepath.Join(dir, "self-extend-mcp.md"), []byte(custom), 0o644); err != nil {
		t.Fatalf("seed pre-existing file: %v", err)
	}

	if err := materializeDocs(); err != nil {
		t.Fatalf("materializeDocs: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "self-extend-mcp.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != custom {
		t.Errorf("materializeDocs overwrote an existing file; got %q, want %q", got, custom)
	}
}

func TestSelfExtendDocNoteUsesRealAbsolutePathOnLocalTarget(t *testing.T) {
	withTempAppDir(t)

	note := selfExtendDocNote()
	if note == "" {
		t.Fatal("expected a non-empty note for the default (local) target")
	}
	wantPath, err := appdir.Path("docs", "self-extend-mcp.md")
	if err != nil {
		t.Fatalf("appdir.Path: %v", err)
	}
	if !strings.Contains(note, wantPath) {
		t.Errorf("note = %q, want it to contain the resolved absolute path %q", note, wantPath)
	}
	if strings.Contains(note, "~") {
		t.Errorf("note = %q, must not contain a shell-style \"~\" shorthand read_file can't expand", note)
	}
}

func TestSelfExtendDocNoteEmptyOnSSHTarget(t *testing.T) {
	withTempAppDir(t)

	s := settings.DefaultAgentSettings()
	s.Target.Type = settings.TargetSSH
	if err := settings.Save(settings.Settings{UI: settings.DefaultUISettings(), Agent: s}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	if note := selfExtendDocNote(); note != "" {
		t.Errorf("note = %q, want empty when the active target is remote (SSH)", note)
	}
}
