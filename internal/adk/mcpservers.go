package adk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pelletier/go-toml/v2"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"

	"tui-testing/internal/appdir"
	"tui-testing/internal/settings"
)

// mcpServerConfig is one MCP server's connection definition, discovered
// under mcpServersDir() — one flat TOML file per server, named for it
// (the filename minus ".toml" is the server's identity, same convention
// subagents.go's directory names use — no redundant name field inside
// the file itself). Meant to be hand-authored: copy the command/args a
// server's own README gives you straight in, which is the whole reason
// this is TOML and not JSON (see settings.go's package doc comment for
// the same reasoning applied there — this is the other config surface
// meant to be comfortable to hand-edit, not agent/interface-written).
type mcpServerConfig struct {
	Command string            `toml:"command"`
	Args    []string          `toml:"args,omitempty"`
	Env     map[string]string `toml:"env,omitempty"` // merged onto os.Environ() — many servers need an API-token env var even for local stdio use
}

const mcpServerFileExt = ".toml"

// mcpServersDir returns (creating it if missing) the directory
// config-discovered MCP servers live under — same "empty by default,
// nothing seeded" reasoning as subAgentsDir.
func mcpServersDir() (string, error) {
	dir, err := appdir.Path("mcpservers")
	if err != nil {
		return "", fmt.Errorf("resolve mcpservers dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create mcpservers dir: %w", err)
	}
	return dir, nil
}

// loadMCPServerConfigs discovers every *.toml file directly under
// mcpServersDir(), keyed by filename minus extension. A malformed file
// is a hard error, same severity loadSubAgentConfigs uses for a broken
// sub-agent directory — this is a config-authoring mistake, not
// something to silently skip. A server that's well-formed but fails to
// actually launch/connect is a different, later concern handled by
// buildMCPToolsets' resilient wrapper, since ADK only resolves a
// toolset's tools lazily, per turn — there's nothing to actually connect
// to yet at load time.
func loadMCPServerConfigs() (map[string]mcpServerConfig, error) {
	dir, err := mcpServersDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read mcpservers dir: %w", err)
	}

	configs := map[string]mcpServerConfig{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), mcpServerFileExt) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), mcpServerFileExt)
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", name, err)
		}
		var cfg mcpServerConfig
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("mcp server %q: parse %s: %w", name, path, err)
		}
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp server %q: %s: missing required \"command\"", name, path)
		}
		configs[name] = cfg
	}
	return configs, nil
}

// buildMCPToolsets resolves names (an agent's agent.json "mcpServers"
// list — root only, see agentFileConfig.MCPServers) against configs, in
// order, erroring on an unknown name the same way buildRootAgent already
// does for an unknown tool name. Each resolved server becomes an ADK
// tool.Toolset via mcptoolset.New, wrapped in resilientToolset (below).
func buildMCPToolsets(configs map[string]mcpServerConfig, names []string, rootName string) ([]tool.Toolset, error) {
	toolsets := make([]tool.Toolset, 0, len(names))
	for _, name := range names {
		cfg, ok := configs[name]
		if !ok {
			return nil, fmt.Errorf("root agent: unknown mcp server %q", name)
		}

		cmd := exec.Command(cfg.Command, cfg.Args...)
		if len(cfg.Env) > 0 {
			env := os.Environ()
			for k, v := range cfg.Env {
				env = append(env, k+"="+v)
			}
			cmd.Env = env
		}

		ts, err := mcptoolset.New(mcptoolset.Config{
			Transport: &mcp.CommandTransport{Command: cmd},
			// Every MCP tool requires confirmation outside full-auto —
			// unlike our own hand-written tools, there's no way to know
			// ahead of time which of a third-party server's tools are
			// safe reads versus real writes, so the conservative default
			// applies uniformly. No per-agent exception here the way
			// gate.go's confirmGated has for sub-agents — this only ever
			// wires onto root (see agentFileConfig.MCPServers' doc
			// comment), and ADK's ConfirmationProvider has no
			// agent-identity to check even if it needed to.
			RequireConfirmation: true,
			RequireConfirmationProvider: func(_ string, _ any) bool {
				return settings.Load().Agent.PermissionMode != settings.ModeFullAuto
			},
		})
		if err != nil {
			return nil, fmt.Errorf("mcp server %q: %w", name, err)
		}
		toolsets = append(toolsets, &resilientToolset{inner: ts})
	}
	return toolsets, nil
}

// resilientToolset wraps a tool.Toolset so a connection failure degrades
// to "no tools from this server this turn" instead of failing the whole
// turn. This matters because ADK resolves every Toolset.Tools(ctx) fresh
// on *every* turn (confirmed in ADK's own
// internal/llminternal/tools_processor.go) and propagates any error as a
// hard failure of the entire flow — without this wrapper, one
// unreachable or misconfigured MCP server would permanently break the
// root agent's ability to respond to anything at all, not just make
// that one server's own tools unavailable.
//
// The failure is swallowed silently rather than logged — this app's TUI
// owns the terminal's alt-screen the whole time it's running, so writing
// to stderr here would corrupt the rendered frame (same reasoning
// main.go's newBackend already documents for SaveAPIKey's own
// best-effort error handling).
type resilientToolset struct {
	inner tool.Toolset
}

func (r *resilientToolset) Name() string { return r.inner.Name() }

func (r *resilientToolset) Tools(ctx agent.ReadonlyContext) ([]tool.Tool, error) {
	tools, err := r.inner.Tools(ctx)
	if err != nil {
		return nil, nil
	}
	return tools, nil
}
