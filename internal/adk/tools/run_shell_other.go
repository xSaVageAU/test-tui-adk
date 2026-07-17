//go:build !windows

package tools

import (
	"context"
	"os/exec"
	"syscall"
)

// shellCmd builds the shell invocation for run_shell on non-Windows
// platforms. `sh -c <command>` passes the command as a single argv
// element, which is exactly right here — sh does its own word splitting
// and quote handling, and Go's POSIX argv passing doesn't re-escape
// anything (the Windows quote-mangling that run_shell_windows.go works
// around is specific to Windows command-line construction).
func shellCmd(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", command)
}

// prepareBackground puts the process in its own process group (Setpgid)
// so killTree can signal the whole group at once — otherwise killing the
// `sh` that ran "npm run dev" would leave its node child orphaned.
func prepareBackground(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree kills the background process's entire group. Signalling the
// negative PID targets every process in the group created by
// prepareBackground's Setpgid, so children die with the shell.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
