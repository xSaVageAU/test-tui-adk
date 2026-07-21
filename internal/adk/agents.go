package adk

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"

	"tui-testing/internal/adk/tools"
)

// builtRoot is buildRootAgent's result — bundled into one struct rather
// than a long return list since New needs all four pieces to populate
// Client.
type builtRoot struct {
	Agent         agent.Agent
	Name          string
	ModelName     string
	ContextWindow int      // 0 if unknown; see resolveContextWindow
	Specialists   []string // discovered sub-agent names, in load order
}

// buildRootAgent assembles the root agent plus whatever specialists are
// discovered under appdir's "subagents" directory (see subagents.go) —
// one subdirectory per agent, holding an agent.json and an
// instruction.md. The root agent is now config-driven too, the same
// shape, from a single agent.json/instruction.md pair directly under
// appdir's root (see rootagent.go) — unlike a sub-agent, it's seeded
// with a working default if missing rather than simply not existing,
// since the app always needs exactly one root.
//
// Every discovered specialist is wrapped via agenttool.New so the root
// consults it like a function call rather than transferring the
// conversation to it — root's own name (config-driven, not a constant
// anymore) is still the only one that ever shows up as
// session.Event.Author, since agent-as-tool never changes who's
// speaking.
func buildRootAgent(ctx context.Context, callerProvider, callerAPIKey string) (builtRoot, error) {
	rootCfg, err := loadRootAgentConfig()
	if err != nil {
		return builtRoot{}, fmt.Errorf("load root agent config: %w", err)
	}

	toolRegistry, err := tools.Registry(rootCfg.Name)
	if err != nil {
		return builtRoot{}, fmt.Errorf("build tool registry: %w", err)
	}

	rootTools := make([]tool.Tool, 0, len(rootCfg.Tools))
	for _, name := range rootCfg.Tools {
		t, ok := toolRegistry[name]
		if !ok {
			return builtRoot{}, fmt.Errorf("root agent: unknown tool %q", name)
		}
		rootTools = append(rootTools, t)
	}

	rootModel, err := buildModel(ctx, rootCfg.Provider, rootCfg.Model, keyOverride(rootCfg.Provider, callerProvider, callerAPIKey))
	if err != nil {
		return builtRoot{}, fmt.Errorf("create root model: %w", err)
	}
	modelName := rootCfg.Model
	if modelName == "" {
		modelName = DefaultModelName
	}

	// Best-effort: resolveAPIKey failing here would be surprising (the
	// same call just succeeded moments ago inside buildModel), but
	// there's no reason to fail the whole agent build over a cosmetic UI
	// number if it somehow does — contextWindow just stays 0.
	rootProvider := rootCfg.Provider
	if rootProvider == "" {
		rootProvider = ProviderGemini
	}
	contextWindow := 0
	if key, err := resolveAPIKey(rootProvider, keyOverride(rootCfg.Provider, callerProvider, callerAPIKey)); err == nil {
		contextWindow = resolveContextWindow(ctx, rootProvider, modelName, key)
	}

	subConfigs, err := loadSubAgentConfigs()
	if err != nil {
		return builtRoot{}, fmt.Errorf("load sub-agent configs: %w", err)
	}
	subAgents, err := buildSubAgents(ctx, callerProvider, callerAPIKey, rootModel, toolRegistry, subConfigs)
	if err != nil {
		return builtRoot{}, err
	}

	names := make([]string, len(subAgents))
	for i, sa := range subAgents {
		rootTools = append(rootTools, agenttool.New(sa, nil))
		names[i] = sa.Name()
	}

	mcpConfigs, err := loadMCPServerConfigs()
	if err != nil {
		return builtRoot{}, fmt.Errorf("load mcp server configs: %w", err)
	}
	mcpToolsets, err := buildMCPToolsets(mcpConfigs, rootCfg.MCPServers, rootCfg.Name)
	if err != nil {
		return builtRoot{}, err
	}

	root, err := llmagent.New(llmagent.Config{
		Name:        rootCfg.Name,
		Model:       rootModel,
		Description: rootCfg.Description,
		Instruction: rootInstructionFor(rootCfg.instruction, subAgents),
		Tools:       rootTools,
		Toolsets:    mcpToolsets,
	})
	if err != nil {
		return builtRoot{}, fmt.Errorf("create agent: %w", err)
	}

	return builtRoot{Agent: root, Name: rootCfg.Name, ModelName: modelName, ContextWindow: contextWindow, Specialists: names}, nil
}

// ShutdownBackgroundProcesses kills any processes the run_shell tool
// started in the background, so they don't outlive the TUI. It's a thin
// re-export of tools.ShutdownBackground, letting the app's shutdown path
// (main.go) trigger cleanup without importing internal/adk/tools itself —
// the same "main talks to adk, not adk's internals" seam as the rest of
// this package's exported surface.
func ShutdownBackgroundProcesses() { tools.ShutdownBackground() }

// ConfigureExecutionTarget installs the execution target (local host or a
// remote SSH machine) from settings, returning a short description of the
// active target and any error establishing it. Thin re-export of
// tools.ConfigureTarget — same seam as ShutdownBackgroundProcesses.
func ConfigureExecutionTarget() (string, error) { return tools.ConfigureTarget() }

// CloseExecutionTarget closes the active execution target (an SSH/SFTP
// connection) as the app exits.
func CloseExecutionTarget() { tools.CloseTarget() }

// rootInstructionFor appends generated content to base — the root's own
// instruction.md content — that can't be static prose: a "Available
// specialists" list (name plus description, in loadSubAgentConfigs'
// order, so the root always knows what it can currently consult without
// the user maintaining that list by hand) and the self-extend-mcp.md
// pointer (see selfExtendDocNote — it needs this machine's actual
// resolved appdir path, which no hardcoded sentence in instruction.md
// could give it).
func rootInstructionFor(base string, subAgents []agent.Agent) string {
	var b strings.Builder
	b.WriteString(base)
	if len(subAgents) > 0 {
		b.WriteString(" Available specialists:")
		for _, sa := range subAgents {
			fmt.Fprintf(&b, "\n- %s: %s", sa.Name(), sa.Description())
		}
	}
	if note := selfExtendDocNote(); note != "" {
		b.WriteString(" ")
		b.WriteString(note)
	}
	return b.String()
}
