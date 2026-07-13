package adk

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
)

const (
	// AgentName is the root agent's name — the only agent that ever
	// speaks; session.Event.Author is always this, since specialists are
	// consulted via agent-as-tool (agenttool.New), not transfer
	// (agent.Config.SubAgents) — the root calls them like a function and
	// stays in control, rather than handing the conversation off to a
	// different visible identity. Exported so callers (the header, via
	// ui.AppConfig.AgentName) can display it without duplicating the
	// string.
	AgentName = "assistant"

	// rootInstructionBase is the root agent's instruction — the only
	// agent still defined in code; every specialist is entirely
	// config-discovered (see subagents.go), with no hardcoded defaults.
	// rootInstructionFor appends whatever specialists actually exist to
	// this base text, so the root always knows what it can currently
	// consult even though that set can grow, shrink, or start out empty
	// with no code change.
	rootInstructionBase = "You are the front-line assistant embedded in a terminal chat UI test harness. " +
		"Keep replies short — this is a test harness for the UI, not a place for long essays. " +
		"You have a list_files tool for browsing the working directory; use it whenever it's relevant. " +
		"You can consult specialists via tool calls when a request clearly fits their focus. Incorporate " +
		"what they tell you into your own reply — you're still the one answering the user. Handle general " +
		"requests yourself rather than consulting a specialist unnecessarily."
)

// buildRootAgent assembles the root "assistant" agent (defined here in
// code, the only agent that is) plus whatever specialists are discovered
// under appdir's "subagents" directory (see subagents.go) — one
// subdirectory per agent, holding an agent.json and an instruction.md. A
// fresh install starts with zero specialists, not a hardcoded default
// set; the root works fine on its own and gains whatever's added there.
// Every discovered specialist is wrapped via agenttool.New so the root
// consults it like a function call rather than transferring the
// conversation to it — see AgentName's doc comment for why.
//
// Every tool a sub-agent config can reference by name is built here and
// put in toolRegistry — currently list_files, read_file, and
// write_file. The root itself only gets list_files directly, same as
// before; read_file/write_file exist purely for a sub-agent's own
// agent.json to opt into, so a fresh install (zero specialists) has no
// agent capable of touching file contents at all until a user
// deliberately configures one that can.
//
// rootModel is what New already resolved for the root from settings.json's
// agent section; ctx and apiKey are threaded through so a sub-agent whose
// own agent.json specifies a different provider/model can resolve its
// own model instead of inheriting rootModel — see buildSubAgents.
//
// Also returns the discovered specialists' names, in load order —
// New uses this to populate Client.Specialists so callers (the boot
// banner, via ui.AppConfig) can show what's actually loaded without
// needing to know anything about how agents are built.
func buildRootAgent(ctx context.Context, apiKey string, rootModel model.LLM) (agent.Agent, []string, error) {
	listFilesTool, err := newListFilesTool()
	if err != nil {
		return nil, nil, fmt.Errorf("create list_files tool: %w", err)
	}
	readFileTool, err := newReadFileTool()
	if err != nil {
		return nil, nil, fmt.Errorf("create read_file tool: %w", err)
	}
	writeFileTool, err := newWriteFileTool()
	if err != nil {
		return nil, nil, fmt.Errorf("create write_file tool: %w", err)
	}
	toolRegistry := map[string]tool.Tool{
		"list_files": listFilesTool,
		"read_file":  readFileTool,
		"write_file": writeFileTool,
	}

	configs, err := loadSubAgentConfigs()
	if err != nil {
		return nil, nil, fmt.Errorf("load sub-agent configs: %w", err)
	}

	subAgents, err := buildSubAgents(ctx, apiKey, rootModel, toolRegistry, configs)
	if err != nil {
		return nil, nil, err
	}

	tools := make([]tool.Tool, 0, len(subAgents)+1)
	tools = append(tools, listFilesTool)
	names := make([]string, len(subAgents))
	for i, sa := range subAgents {
		tools = append(tools, agenttool.New(sa, nil))
		names[i] = sa.Name()
	}

	root, err := llmagent.New(llmagent.Config{
		Name:        AgentName,
		Model:       rootModel,
		Description: "A general-purpose assistant for testing the TUI against a real LLM.",
		Instruction: rootInstructionFor(subAgents),
		Tools:       tools,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("create agent: %w", err)
	}

	return root, names, nil
}

// rootInstructionFor appends a generated "Available specialists" list
// (name plus description, in loadSubAgentConfigs' order) to
// rootInstructionBase, so the root's instruction always reflects
// whatever's actually in the subagents directory instead of naming
// specialists that may not exist.
func rootInstructionFor(subAgents []agent.Agent) string {
	if len(subAgents) == 0 {
		return rootInstructionBase
	}
	var b strings.Builder
	b.WriteString(rootInstructionBase)
	b.WriteString(" Available specialists:")
	for _, sa := range subAgents {
		fmt.Fprintf(&b, "\n- %s: %s", sa.Name(), sa.Description())
	}
	return b.String()
}
