package tools

import (
	"fmt"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// stop_shell terminates a background process started by run_shell,
// killing its whole process tree (see killTree) so children don't
// orphan. It is deliberately NOT marked destructive: the launch that
// created the process was already confirmed (run_shell is destructive),
// and stopping only makes the system do less, so gating each stop behind
// another confirmation would be friction without a safety benefit.
// Touches no filesystem path, so resources is nil.
func init() {
	register(spec{
		destructive: false,
		resources:   nil,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "stop_shell",
				Description: "Stops a background process started with run_shell, terminating it and any child processes. Pass the shell_id that run_shell returned.",
			}, stopShell)
		},
	})
}

type stopShellArgs struct {
	ShellID string `json:"shell_id" jsonschema:"The id returned by run_shell when the process was started in the background."`
}

type stopShellResult struct {
	Status string `json:"status" jsonschema:"Result of the stop request, e.g. 'stopped' or 'already exited'."`
}

func stopShell(_ agent.Context, args stopShellArgs) (stopShellResult, error) {
	h := lookupBg(args.ShellID)
	if h == nil {
		return stopShellResult{}, fmt.Errorf("stop_shell: unknown shell id %q — it was never started, or belongs to a previous run", args.ShellID)
	}
	running, _, rerr := h.running()
	if rerr != nil {
		return stopShellResult{}, fmt.Errorf("stop_shell: %w", rerr)
	}
	if !running {
		return stopShellResult{Status: "already exited"}, nil
	}
	if err := h.stop(); err != nil {
		return stopShellResult{}, fmt.Errorf("stop_shell: %w", err)
	}
	return stopShellResult{Status: "stopped"}, nil
}
