package adk

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"tui-testing/internal/appdir"
	"tui-testing/internal/settings"
)

// docsDefaultsFS embeds this app's built-in agent-facing reference docs
// — see docsdefaults/*.md. These exist so the agent can read up on
// something this app has opinions about (today: configuring its own MCP
// tool servers) only on the turns that's actually relevant, rather than
// paying for the content in every system prompt — defaultRootInstruction's
// one-line pointer is the only always-loaded cost; see rootagent.go.
//
//go:embed docsdefaults/*.md
var docsDefaultsFS embed.FS

// materializeDocs seeds appdir's "docs" directory with any embedded
// reference doc not already present on disk — same write-once,
// never-clobber shape seedIfMissing uses for agent.json/instruction.md,
// so a doc a user (or the agent, recording something it learned) has
// since edited survives future launches instead of reverting to the
// shipped default. Called alongside seedIfMissing, on every root-agent
// build, since it's equally cheap to no-op once the files exist.
func materializeDocs() error {
	dir, err := appdir.Path("docs")
	if err != nil {
		return fmt.Errorf("resolve docs dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create docs dir: %w", err)
	}

	entries, err := docsDefaultsFS.ReadDir("docsdefaults")
	if err != nil {
		return fmt.Errorf("read embedded docs: %w", err)
	}
	for _, e := range entries {
		dst := filepath.Join(dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue // already there — leave whatever's been edited alone
		}
		data, err := docsDefaultsFS.ReadFile("docsdefaults/" + e.Name())
		if err != nil {
			return fmt.Errorf("read embedded doc %q: %w", e.Name(), err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return fmt.Errorf("write doc %q: %w", e.Name(), err)
		}
	}
	return nil
}

// selfExtendDocNote returns a sentence pointing the agent at its local,
// materialized copy of self-extend-mcp.md — with the real, absolute,
// OS-native path baked in, never a "~/..." shorthand, since read_file
// resolves paths literally against a Target with no shell to expand
// that itself (this is exactly the bug that prompted this function to
// exist — a static "~/.tui-testing/..." sentence in instruction.md
// failed with "the system cannot find the path specified").
//
// Returns "" (nothing appended) when the active execution target is
// remote (SSH): the doc is materialized on this machine's local appdir,
// but tool calls resolve against whatever target is active, so pointing
// at a local path while running against a remote target would just hand
// the agent another unreachable path — worse than saying nothing.
func selfExtendDocNote() string {
	if settings.Load().Agent.Target.Type == settings.TargetSSH {
		return ""
	}
	path, err := appdir.Path("docs", "self-extend-mcp.md")
	if err != nil {
		return ""
	}
	return "If asked to give yourself a new tool or capability, read " + path +
		" first — it explains how this app expects a custom MCP tool server to be configured."
}
