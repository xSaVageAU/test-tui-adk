package adk

import (
	"errors"
	"os"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
)

// withTempAppDir redirects appdir.Dir() (os.UserHomeDir(), which reads
// USERPROFILE on Windows) to a fresh temp directory for one test, so
// loadMCPServerConfigs never touches this machine's real
// ~/.tui-testing/mcpservers — same technique internal/settings' own
// tests already use, since appdir has no dependency-injection seam.
func withTempAppDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
}

func TestLoadMCPServerConfigsEmptyDirReturnsEmptyMap(t *testing.T) {
	withTempAppDir(t)
	configs, err := loadMCPServerConfigs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("configs = %v, want empty", configs)
	}
}

func TestLoadMCPServerConfigsParsesByFilename(t *testing.T) {
	withTempAppDir(t)
	dir, err := mcpServersDir()
	if err != nil {
		t.Fatal(err)
	}
	toml := "command = \"npx\"\nargs = [\"-y\", \"@modelcontextprotocol/server-filesystem\", \"/tmp\"]\n\n[env]\nTOKEN = \"abc123\"\n"
	if err := os.WriteFile(dir+"/filesystem.toml", []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	configs, err := loadMCPServerConfigs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg, ok := configs["filesystem"]
	if !ok {
		t.Fatalf("configs = %v, want a \"filesystem\" entry (from the filename, not a name field)", configs)
	}
	if cfg.Command != "npx" {
		t.Errorf("Command = %q, want %q", cfg.Command, "npx")
	}
	if len(cfg.Args) != 3 || cfg.Args[0] != "-y" {
		t.Errorf("Args = %v, want [-y @modelcontextprotocol/server-filesystem /tmp]", cfg.Args)
	}
	if cfg.Env["TOKEN"] != "abc123" {
		t.Errorf("Env[TOKEN] = %q, want %q", cfg.Env["TOKEN"], "abc123")
	}
}

func TestLoadMCPServerConfigsMissingCommandIsHardError(t *testing.T) {
	withTempAppDir(t)
	dir, err := mcpServersDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/broken.toml", []byte("args = [\"foo\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadMCPServerConfigs(); err == nil {
		t.Error("expected a hard error for a config missing \"command\", got nil")
	}
}

func TestLoadMCPServerConfigsMalformedTOMLIsHardError(t *testing.T) {
	withTempAppDir(t)
	dir, err := mcpServersDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/broken.toml", []byte("this is not [ valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadMCPServerConfigs(); err == nil {
		t.Error("expected a hard error for malformed TOML, got nil")
	}
}

func TestBuildMCPToolsetsUnknownNameIsHardError(t *testing.T) {
	_, err := buildMCPToolsets(map[string]mcpServerConfig{}, []string{"nonexistent"}, "root")
	if err == nil {
		t.Error("expected an error for an unresolved mcp server name, got nil")
	}
}

// fakeToolset is a minimal tool.Toolset stub — real MCP connection
// behavior (an actual server process/handshake) is exercised via live
// verification, not a unit test; this only exercises resilientToolset's
// own error-swallowing logic, independent of what the inner toolset
// actually is.
type fakeToolset struct {
	tools []tool.Tool
	err   error
}

func (f *fakeToolset) Name() string { return "fake" }
func (f *fakeToolset) Tools(_ agent.ReadonlyContext) ([]tool.Tool, error) {
	return f.tools, f.err
}

// TestResilientToolsetSwallowsError is the single most important
// behavior in this whole feature: ADK calls every Toolset.Tools(ctx)
// fresh on *every* turn (internal/llminternal/tools_processor.go) and
// treats any error as a hard failure of the entire flow, not just that
// toolset — confirmed by reading ADK's own vendored source. Without this,
// one unreachable or misconfigured MCP server would permanently break
// the root agent's ability to respond to anything, every turn, until the
// config is fixed and reloaded.
func TestResilientToolsetSwallowsError(t *testing.T) {
	inner := &fakeToolset{err: errors.New("connection refused")}
	r := &resilientToolset{inner: inner}

	tools, err := r.Tools(nil)
	if err != nil {
		t.Errorf("Tools() error = %v, want nil — a broken server must degrade, not fail the whole turn", err)
	}
	if tools != nil {
		t.Errorf("Tools() = %v, want nil", tools)
	}
}

func TestResilientToolsetPassesThroughSuccess(t *testing.T) {
	want := []tool.Tool{}
	inner := &fakeToolset{tools: want}
	r := &resilientToolset{inner: inner}

	tools, err := r.Tools(nil)
	if err != nil {
		t.Errorf("Tools() error = %v, want nil", err)
	}
	if len(tools) != len(want) {
		t.Errorf("Tools() = %v, want %v", tools, want)
	}
}

func TestResilientToolsetForwardsName(t *testing.T) {
	r := &resilientToolset{inner: &fakeToolset{}}
	if got := r.Name(); got != "fake" {
		t.Errorf("Name() = %q, want %q", got, "fake")
	}
}
