package adk

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
)

// builtRoot is buildRootAgent's result — bundled into one struct rather
// than a long return list since New needs all four pieces to populate
// Client.
type builtRoot struct {
	Agent       agent.Agent
	Name        string
	ModelName   string
	Specialists []string // discovered sub-agent names, in load order
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

	listFilesTool, err := newListFilesTool(rootCfg.Name)
	if err != nil {
		return builtRoot{}, fmt.Errorf("create list_files tool: %w", err)
	}
	readFileTool, err := newReadFileTool(rootCfg.Name)
	if err != nil {
		return builtRoot{}, fmt.Errorf("create read_file tool: %w", err)
	}
	writeFileTool, err := newWriteFileTool(rootCfg.Name)
	if err != nil {
		return builtRoot{}, fmt.Errorf("create write_file tool: %w", err)
	}
	toolRegistry := map[string]tool.Tool{
		"list_files": listFilesTool,
		"read_file":  readFileTool,
		"write_file": writeFileTool,
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

	root, err := llmagent.New(llmagent.Config{
		Name:        rootCfg.Name,
		Model:       rootModel,
		Description: rootCfg.Description,
		Instruction: rootInstructionFor(rootCfg.instruction, subAgents),
		Tools:       rootTools,
	})
	if err != nil {
		return builtRoot{}, fmt.Errorf("create agent: %w", err)
	}

	return builtRoot{Agent: root, Name: rootCfg.Name, ModelName: modelName, Specialists: names}, nil
}

// rootInstructionFor appends a generated "Available specialists" list
// (name plus description, in loadSubAgentConfigs' order) to base — the
// root's own instruction.md content — so the root always knows what it
// can currently consult even though that set can grow, shrink, or start
// out empty, without the user needing to maintain that list by hand.
func rootInstructionFor(base string, subAgents []agent.Agent) string {
	if len(subAgents) == 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString(" Available specialists:")
	for _, sa := range subAgents {
		fmt.Fprintf(&b, "\n- %s: %s", sa.Name(), sa.Description())
	}
	return b.String()
}
