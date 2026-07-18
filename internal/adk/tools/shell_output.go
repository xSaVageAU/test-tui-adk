package tools

import (
	"fmt"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// shell_output reads new output from a background process started by
// run_shell (background:true), plus whether it's still running. Read-only
// (destructive:false) and touches no filesystem path (resources nil) — it
// only inspects an in-process buffer. Each call returns just the output
// produced since the previous call for the same shell_id, so the model
// can poll a long-running process's logs without re-reading everything.
func init() {
	register(spec{
		destructive: false,
		resources:   nil,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "shell_output",
				Description: "Returns output produced since the last check by a background process started with run_shell, and whether it is still running or has exited. Pass the shell_id that run_shell returned.",
			}, shellOutput)
		},
	})
}

type shellOutputArgs struct {
	ShellID string `json:"shell_id" jsonschema:"The id returned by run_shell when the process was started in the background."`
}

type shellOutputResult struct {
	Output  string `json:"output" jsonschema:"Output (stdout and stderr combined) produced since the previous shell_output call for this process."`
	Running bool   `json:"running" jsonschema:"True while the process is still running; false once it has exited."`
	Status  string `json:"status" jsonschema:"Human-readable status, e.g. 'running for 30s' or 'exited (code 0)'."`
}

func shellOutput(_ agent.Context, args shellOutputArgs) (shellOutputResult, error) {
	h := lookupBg(args.ShellID)
	if h == nil {
		return shellOutputResult{}, fmt.Errorf("shell_output: unknown shell id %q — it was never started, or belongs to a previous run", args.ShellID)
	}
	out, err := h.readNew()
	if err != nil {
		return shellOutputResult{}, fmt.Errorf("shell_output: %w", err)
	}
	running, code, rerr := h.running()
	if rerr != nil {
		return shellOutputResult{}, fmt.Errorf("shell_output: %w", rerr)
	}
	if running {
		return shellOutputResult{Output: out, Running: true, Status: "running for " + time.Since(h.startTime()).Round(time.Second).String()}, nil
	}
	return shellOutputResult{Output: out, Running: false, Status: fmt.Sprintf("exited (code %d)", code)}, nil
}
