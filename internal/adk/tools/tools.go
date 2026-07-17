// Package tools is where every tool the agent tree can call lives — one
// file per tool (list_files.go, read_file.go, write_file.go, ...). Each
// file self-registers its tool by calling register(spec{...}) from an
// init(), so adding a tool means writing one new file and editing
// nothing else — this file never grows. (Same driver-registration shape
// as database/sql: registration order doesn't matter, since Registry
// keys the result by each tool's own Name().)
//
// gate.go holds the resource-conflict/confirmation machinery that
// Registry wraps every tool in — see its own doc comment for why that
// exists and why the wrapping order is load-bearing. internal/adk/
// agents.go resolves each agent's own agent.json "tools" list against
// the map Registry returns, so which tools an agent actually has is
// still config, not code — this package only controls which tools
// *exist* to be granted.
package tools

import (
	"fmt"

	"google.golang.org/adk/v2/tool"
)

// spec declares one tool for the registry — the small amount of
// per-tool metadata Registry needs to wrap it correctly, plus a closure
// that builds the raw tool itself.
type spec struct {
	// destructive marks a tool that creates, overwrites, or executes
	// something — it requires human confirmation in "normal" mode (see
	// confirmGated in gate.go). false for read-only tools, which have
	// nothing to approve.
	destructive bool
	// resources maps a call's raw args to the filesystem resources it
	// touches, so calls that overlap serialize instead of racing (see
	// gate.go). nil means the tool needs no conflict gating at all —
	// e.g. reload_agents touches no path.
	resources func(args map[string]any) []resourceRef
	// build constructs the raw, unwrapped functiontool. It's a closure
	// rather than plain Name/Description/Handler fields because
	// functiontool.New is generic over the handler's argument/result
	// types, and those types are only known at the concrete call site in
	// each tool's own file — so construction has to happen there, and
	// this carries the result back.
	build func() (tool.Tool, error)
}

// specs is every registered tool. It's appended to only by register()
// from each tool file's init(), i.e. only during package
// initialization, before any Registry() call — so it needs no locking.
var specs []spec

// register adds one tool to the registry. Each tool file calls this once
// from its init(); see any existing tool file for the shape.
func register(s spec) { specs = append(specs, s) }

// Registry builds every registered tool and returns them keyed by name.
// The shared wrapping lives here, in one place: confirmGated (per-call,
// context-aware human confirmation) innermost, then gated (resource-
// conflict serialization) around it when the tool declares resources.
// That composition order is load-bearing — see confirmGated's doc
// comment in gate.go. rootName is threaded to confirmGated so a call can
// tell whether it's running as the root agent or inside a sub-agent's
// disposable run.
func Registry(rootName string) (map[string]tool.Tool, error) {
	reg := make(map[string]tool.Tool, len(specs))
	for _, s := range specs {
		raw, err := s.build()
		if err != nil {
			return nil, fmt.Errorf("build tool: %w", err)
		}
		t := confirmGated(raw, s.destructive, rootName)
		if s.resources != nil {
			t = gated(t, s.resources)
		}
		reg[t.Name()] = t
	}
	return reg, nil
}
