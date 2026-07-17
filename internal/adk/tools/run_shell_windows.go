//go:build windows

package tools

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
)

// shellCmd builds the cmd.exe invocation for run_shell on Windows.
//
// It sets the raw command line via SysProcAttr.CmdLine instead of
// letting exec build it from argv, because Go's Windows argv→command-
// line builder (internal/syscall/windows.EscapeArg) backslash-escapes
// embedded double quotes ('"' → '\"', the Unix convention) — and
// cmd.exe does not understand '\"', so it passes the backslashes through
// literally. That turned `echo "hi"` into `echo \"hi\"` and broke every
// command containing a quote. CmdLine is passed verbatim, so the quotes
// reach cmd.exe intact.
//
// Form: `cmd /S /C "<command>"`. The leading `cmd` is argv[0] (the child
// parses the program name off the front of the command line; the actual
// executable comes from CommandContext's resolved path). /C runs the
// command; /S makes cmd strip exactly the first and last quote of the
// remainder — deterministic — so wrapping <command> in one outer pair
// hands cmd the command verbatim, quotes and all, even when it itself
// begins with a quoted path like `"C:\Program Files\x.exe" arg`.
//
// CommandContext (not Command) is kept so the timeout in run_shell still
// kills the process on ctx cancellation; SysProcAttr is set afterward.
func shellCmd(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + command + `"`}
	return cmd
}

// prepareBackground is a no-op on Windows: killTree uses taskkill /T,
// which walks the child tree from the PID itself, so the process needs no
// special group setup — and we must not touch SysProcAttr here anyway,
// since shellCmd already set its CmdLine and overwriting it would break
// the quote handling above.
func prepareBackground(cmd *exec.Cmd) {}

// killTree terminates a background process and all of its descendants via
// `taskkill /T /F /PID`. /T kills the whole tree (so a dev server's child
// node/python processes go too, not just the top cmd.exe), /F forces it.
// Shelling out to taskkill keeps this dependency-free — it ships with
// Windows — rather than pulling in Job Objects from golang.org/x/sys.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
