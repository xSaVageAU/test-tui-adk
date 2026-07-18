package tools

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// run_shell executes a shell command. By default it runs in the
// foreground: it waits for the command to finish and returns its combined
// output and exit code, killing it if it exceeds the timeout (a safety
// net against a hung command — there's a default, but no hard ceiling, so
// a caller that knows a command is slow can pass a larger timeout).
//
// With background:true it instead launches the command detached with no
// timeout, returns a shell_id immediately, and leaves it running — for a
// dev server, a file watcher, or anything that must outlive this call.
// The model then uses shell_output to read the process's logs and
// stop_shell to end it; ShutdownBackground kills any survivors when the
// app exits (see bgproc.go).
//
// It's the biggest risk surface in the toolset — a command can do
// anything — so it's marked destructive (always confirms in "normal"
// mode; full-auto is the only skip, an explicit already-accepted trust
// decision). A foreground call's resource is a recursive write over the
// working directory, serializing it against every other file tool on that
// tree (see gate.go's dirWriteRef); a background call's Run returns at
// once, releasing that lock immediately, so a long-lived server never
// blocks file work. Pure exec, no third-party dependency.
func init() {
	register(spec{
		destructive: true,
		resources:   runShellResources,
		build: func() (tool.Tool, error) {
			return functiontool.New(functiontool.Config{
				Name:        "run_shell",
				Description: "Runs a command line in the operating system's default shell. By default it waits for the command and returns its combined stdout+stderr and exit code. Set background:true to launch a long-running process (e.g. a dev server) detached with no timeout; it returns a shell_id — use shell_output to read its output and stop_shell to end it. Prefer the dedicated read_file/write_file/edit_file/grep/glob tools for file work.",
			}, runShell)
		},
	})
}

type runShellArgs struct {
	Command    string `json:"command" jsonschema:"The command line to run in the shell."`
	WorkingDir string `json:"working_dir,omitempty" jsonschema:"Directory to run the command in. Defaults to the current working directory."`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"For a foreground command, kill it if it runs longer than this many seconds. Defaults to 120. Ignored when background is true."`
	Background bool   `json:"background,omitempty" jsonschema:"Launch the command detached with no timeout and return a shell_id instead of waiting. Use for long-running processes like dev servers."`
}

type runShellResult struct {
	Output   string `json:"output" jsonschema:"For a foreground command, its combined stdout and stderr. For a background launch, a short note naming the shell_id."`
	ExitCode int    `json:"exit_code" jsonschema:"For a foreground command, its exit code (0 = success, -1 if killed by the timeout or failed to start). Not meaningful for a background launch, which has not finished — check shell_output instead."`
	ShellID  string `json:"shell_id,omitempty" jsonschema:"For a background launch, the id to pass to shell_output and stop_shell. Empty for a foreground command."`
}

const (
	runShellDefaultTimeout = 120
	runShellMaxOutput      = 100 << 10 // 100 KiB of captured foreground output
)

func runShell(_ agent.Context, args runShellArgs) (runShellResult, error) {
	if strings.TrimSpace(args.Command) == "" {
		return runShellResult{}, fmt.Errorf("run_shell: empty command")
	}

	if args.Background {
		id, err := target().StartBackground(args.Command, args.WorkingDir)
		if err != nil {
			return runShellResult{}, fmt.Errorf("run_shell: %w", err)
		}
		return runShellResult{
			ShellID: id,
			Output:  fmt.Sprintf("Started in the background as %s. Use shell_output(%q) to read its output and stop_shell(%q) to end it.", id, id, id),
		}, nil
	}

	timeout := args.TimeoutSec
	if timeout <= 0 {
		timeout = runShellDefaultTimeout
	}

	// target() routes to the local host or a remote SSH machine — see
	// target.go. The non-zero-exit vs error distinction is the target's
	// contract: err is only a timeout or a failure to start the command,
	// never just a non-zero exit (which comes back in res.ExitCode).
	res, err := target().Run(context.Background(), args.Command, args.WorkingDir, timeout)
	output := truncateOutput(res.Output, runShellMaxOutput)
	if err != nil {
		return runShellResult{Output: output, ExitCode: res.ExitCode}, fmt.Errorf("run_shell: %w", err)
	}
	return runShellResult{Output: output, ExitCode: res.ExitCode}, nil
}

// truncateOutput caps captured output so a chatty command can't flood the
// model's context, keeping the head and noting how much was dropped.
func truncateOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("\n… (%d more bytes truncated)", len(s)-max)
}

func runShellResources(args map[string]any) []resourceRef {
	dir, _ := args["working_dir"].(string)
	return []resourceRef{dirWriteRef(dir)}
}
