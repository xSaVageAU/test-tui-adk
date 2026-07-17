package tools

import (
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// ReloadAgentsToolName is exported so internal/adk/eventstream.go can
// recognize a completed call to this specific tool and translate it into
// a ui.StreamChunk.ReloadRequested signal — see that file for why the
// bridge has to work this way (tools have no other way to reach back
// into the running backend/UI).
const ReloadAgentsToolName = "reload_agents"

// NewReloadAgentsTool builds a tool that does nothing itself beyond
// returning a fixed result — it exists purely as a signal a running turn
// can emit to ask for the whole agent tree (tools, sub-agents, MCP
// servers) to be rebuilt from whatever's currently on disk, same as
// picking /reload-agents by hand. Destructive in effect (it changes what
// the agent itself is capable of), so it requires confirmation in
// "normal" mode same as write_file — an agent only gets to skip that in
// full-auto, which is an explicit, already-accepted trust decision, not
// something this tool needs its own separate safeguard for.
func NewReloadAgentsTool(rootName string) (tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name:        ReloadAgentsToolName,
		Description: "Reloads agents, tools, and MCP servers from disk, picking up any configuration changes made since the app started — including a newly self-registered MCP server.",
	}, reloadAgents)
	if err != nil {
		return nil, err
	}
	return gated(confirmGated(t, true, rootName), func(map[string]any) []resourceRef { return nil }), nil
}

type reloadAgentsArgs struct{}

type reloadAgentsResult struct {
	Status string `json:"status"`
}

func reloadAgents(_ agent.Context, _ reloadAgentsArgs) (reloadAgentsResult, error) {
	return reloadAgentsResult{Status: "reload requested"}, nil
}
