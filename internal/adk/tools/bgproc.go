package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// This file holds the lifecycle machinery for background processes
// launched by run_shell (background:true) — a dev server, a file watcher,
// anything that must outlive the tool call that starts it. Each gets an
// id; shell_output reads its accumulated output, stop_shell ends it.
//
// A process may be local (bgProc, here) or remote over SSH (sshBgProc, in
// bgproc_ssh.go); both satisfy bgHandle, so shell_output and stop_shell
// operate on either without caring which target it lives on. The registry
// (bgProcs) holds them all by id.
//
// ShutdownBackground kills every still-running *local* process as the TUI
// exits so they don't become ghosts; remote processes are independent of
// this app and left running (see bgHandle.stopOnAppExit).

// maxBgOutput caps how much of a local background process's output is
// retained. Output past this drops the oldest bytes (a dev server can log
// forever).
const maxBgOutput = 1 << 20 // 1 MiB

// bgHandle is one launched background process, local or remote.
type bgHandle interface {
	// readNew returns output produced since the previous readNew call.
	readNew() (string, error)
	// running reports whether the process is still running, and its exit
	// code once it isn't.
	running() (isRunning bool, exitCode int, err error)
	// stop terminates the process and its children.
	stop() error
	// startTime is when it was launched, for a "running for N" status note.
	startTime() time.Time
	// stopOnAppExit reports whether ShutdownBackground should kill this on
	// TUI exit: true for local processes (children of this process that
	// would otherwise orphan), false for remote ones (independent
	// processes on another machine, left running like nohup — stop_shell
	// ends them).
	stopOnAppExit() bool
}

var (
	bgMu    sync.Mutex
	bgProcs = map[string]bgHandle{}
	bgSeq   atomic.Uint64
)

func newBgID() string { return "shell_" + strconv.FormatUint(bgSeq.Add(1), 10) }

func putBg(id string, h bgHandle) {
	bgMu.Lock()
	bgProcs[id] = h
	bgMu.Unlock()
}

func lookupBg(id string) bgHandle {
	bgMu.Lock()
	defer bgMu.Unlock()
	return bgProcs[id]
}

// ShutdownBackground kills every still-running local background process.
// Called as the app exits (see main.go) so nothing local outlives the
// TUI. Remote processes (stopOnAppExit false) are deliberately left
// running. Exported because the app's shutdown path is the one place
// outside this package that reaches this lifecycle.
func ShutdownBackground() {
	bgMu.Lock()
	handles := make([]bgHandle, 0, len(bgProcs))
	for _, h := range bgProcs {
		handles = append(handles, h)
	}
	bgMu.Unlock()

	for _, h := range handles {
		if !h.stopOnAppExit() {
			continue
		}
		if run, _, _ := h.running(); run {
			_ = h.stop()
		}
	}
}

// --- local process ---

// bgProc is one background process running on the local host.
type bgProc struct {
	command   string
	cmd       *exec.Cmd
	startedAt time.Time

	mu       sync.Mutex
	out      bgBuffer
	done     bool
	exitCode int
}

// startBackground launches command detached on the local host (no
// timeout), captures its output, registers it under a fresh id, and
// returns that id. A goroutine waits on the process so its exit status is
// recorded once it ends.
func startBackground(command, workingDir string) (string, error) {
	cmd := shellCmd(context.Background(), command)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	// prepareBackground (platform-specific) puts the process in its own
	// group where that's how killTree reaches the children.
	prepareBackground(cmd)

	p := &bgProc{command: command, cmd: cmd, startedAt: time.Now()}
	cmd.Stdout = bgWriter{p}
	cmd.Stderr = bgWriter{p}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("run_shell: start background process: %w", err)
	}

	id := newBgID()
	putBg(id, p)

	go func() {
		err := cmd.Wait()
		p.mu.Lock()
		p.done = true
		if ee, ok := err.(*exec.ExitError); ok {
			p.exitCode = ee.ExitCode()
		} else if err != nil {
			p.exitCode = -1
		}
		p.mu.Unlock()
	}()

	return id, nil
}

func (p *bgProc) readNew() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.readNew(), nil
}

func (p *bgProc) running() (bool, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.done, p.exitCode, nil
}

func (p *bgProc) stop() error            { return killTree(p.cmd) }
func (p *bgProc) startTime() time.Time   { return p.startedAt }
func (p *bgProc) stopOnAppExit() bool    { return true }

// bgBuffer accumulates process output under bgProc.mu, capped at
// maxBgOutput (oldest bytes dropped once full), tracking a read cursor so
// each readNew returns only bytes not yet seen.
type bgBuffer struct {
	data   []byte
	cursor int
}

func (b *bgBuffer) write(p []byte) {
	b.data = append(b.data, p...)
	if over := len(b.data) - maxBgOutput; over > 0 {
		b.data = b.data[over:]
		if b.cursor -= over; b.cursor < 0 {
			b.cursor = 0
		}
	}
}

func (b *bgBuffer) readNew() string {
	out := string(b.data[b.cursor:])
	b.cursor = len(b.data)
	return out
}

// bgWriter funnels a process's stdout/stderr into its bgProc's buffer,
// holding the process mutex so writes and readNew reads don't race.
type bgWriter struct{ p *bgProc }

func (w bgWriter) Write(p []byte) (int, error) {
	w.p.mu.Lock()
	w.p.out.write(p)
	w.p.mu.Unlock()
	return len(p), nil
}
