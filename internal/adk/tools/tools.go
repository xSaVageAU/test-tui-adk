// Package tools is where every tool the agent tree can call lives — one
// file per tool (list_files.go, read_file.go, write_file.go, ...), each
// exporting a New<Name>Tool(rootName string) (tool.Tool, error)
// constructor. gate.go holds the resource-conflict/confirmation
// machinery every constructor wraps its tool in — see its own doc
// comment for why that exists.
//
// This file is the registration point: adding a new tool means writing
// its own file (copy the shape of any existing one) and adding its
// constructor to registryConstructors below. Registry() builds every
// tool once and returns them by name; internal/adk/agents.go resolves
// each agent's own agent.json "tools" list against that map, so which
// tools an agent actually has is still config, not code — this file
// only controls which tools *exist* to be granted.
package tools

import (
	"fmt"

	"google.golang.org/adk/v2/tool"
)

// registryConstructors is every tool this package makes available.
// Order doesn't matter — Registry keys the result by each tool's own
// Name(), not by position here.
var registryConstructors = []func(rootName string) (tool.Tool, error){
	NewListFilesTool,
	NewReadFileTool,
	NewWriteFileTool,
	NewReloadAgentsTool,
}

// Registry builds every tool in registryConstructors and returns them
// keyed by name — rootName is threaded through to each constructor (see
// gate.go's confirmGated) so a tool call can tell whether it's running
// as the root agent or inside a sub-agent's disposable run.
func Registry(rootName string) (map[string]tool.Tool, error) {
	reg := make(map[string]tool.Tool, len(registryConstructors))
	for _, newTool := range registryConstructors {
		t, err := newTool(rootName)
		if err != nil {
			return nil, fmt.Errorf("build tool: %w", err)
		}
		reg[t.Name()] = t
	}
	return reg, nil
}
