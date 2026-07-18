package tools

import (
	"context"
	"io"
	"io/fs"
	"sync"
)

// This file defines the execution Target abstraction: where a tool's
// filesystem and command operations actually happen. By default that's
// the local host (localTarget, target_local.go); it can be switched to a
// remote machine over SSH/SFTP (sshTarget, target_ssh.go) so the agent
// operates on a remote box without any tool knowing the difference. Every
// tool goes through target() instead of calling os/exec directly, so the
// choice is pure config (see settings.AgentSettings.Target) — the same
// config-not-code shape as the rest of the app.

// RunResult is the outcome of a Target.Run: a command's combined output
// and exit code (0 = success, -1 = killed by timeout or failed to start).
type RunResult struct {
	Output   string
	ExitCode int
}

// WalkFunc is called for each entry under a Target.Walk root, in the
// FileInfo style (rather than fs.DirEntry) because that's what SFTP's
// walker yields natively; the local target adapts filepath.Walk to match.
type WalkFunc func(path string, info fs.FileInfo, err error) error

// Target is where a tool's filesystem and command work executes — the
// local host or a remote machine. All paths are interpreted in the
// target's own filesystem (a remote path on an SSH target, a local path
// on the host). Implementations must be safe for concurrent use: tools
// run in parallel goroutines (see gate.go).
type Target interface {
	// Run executes a command, waits for it, and returns its combined
	// stdout+stderr and exit code. workingDir "" means the target's
	// default directory; timeoutSec <= 0 means no timeout.
	Run(ctx context.Context, command, workingDir string, timeoutSec int) (RunResult, error)

	// StartBackground launches a detached, long-running process and
	// returns an id for shell_output/stop_shell (see bgproc.go). Not
	// every target supports it — an SSH target returns an error for now.
	StartBackground(command, workingDir string) (string, error)

	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, mode fs.FileMode) error
	Stat(path string) (fs.FileInfo, error)
	ReadDir(path string) ([]fs.DirEntry, error)
	Open(path string) (io.ReadCloser, error)
	Walk(root string, fn WalkFunc) error

	// Close releases any resources the target holds (an SSH/SFTP
	// connection). The local target's Close is a no-op.
	Close() error
}

var (
	targetMu     sync.RWMutex
	activeTarget Target = localTarget{}
)

// target returns the active execution target — local by default, or
// whatever SetTarget last installed. Every tool routes its filesystem and
// command work through this.
func target() Target {
	targetMu.RLock()
	defer targetMu.RUnlock()
	return activeTarget
}

// SetTarget installs t as the active execution target and returns the
// previous one, so the caller can Close it. Called at startup and when
// the target configuration changes (see internal/adk's target wiring).
func SetTarget(t Target) Target {
	targetMu.Lock()
	defer targetMu.Unlock()
	prev := activeTarget
	activeTarget = t
	return prev
}
