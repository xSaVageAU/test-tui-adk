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
// id; shell_output reads its accumulated output, stop_shell ends it. The
// whole tree is killed (not just the top shell) via killTree in the
// platform files (run_shell_windows.go / run_shell_other.go), so killing
// an `sh -c "npm run dev"` doesn't orphan its node child.
//
// ShutdownBackground kills every still-running process — main.go calls it
// as the TUI exits so background work never becomes a real ghost once the
// app is gone.

// maxBgOutput caps how much of a background process's output is retained.
// Output past this drops the oldest bytes (a dev server can log forever).
const maxBgOutput = 1 << 20 // 1 MiB

// bgProc is one launched background process and its captured output.
type bgProc struct {
	id        string
	command   string
	cmd       *exec.Cmd
	startedAt time.Time

	mu       sync.Mutex
	out      bgBuffer
	done     bool
	exitCode int
}

// bgBuffer accumulates process output under bgProc.mu, capped at
// maxBgOutput (oldest bytes dropped once full), tracking a read cursor so
// each shell_output call returns only bytes not yet seen.
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
// holding the process mutex so writes and shell_output reads don't race.
type bgWriter struct{ p *bgProc }

func (w bgWriter) Write(p []byte) (int, error) {
	w.p.mu.Lock()
	w.p.out.write(p)
	w.p.mu.Unlock()
	return len(p), nil
}

var (
	bgMu    sync.Mutex
	bgProcs = map[string]*bgProc{}
	bgSeq   atomic.Uint64
)

func lookupBg(id string) *bgProc {
	bgMu.Lock()
	defer bgMu.Unlock()
	return bgProcs[id]
}

// startBackground launches command detached (no timeout), captures its
// output, registers it under a fresh id, and returns that id. A goroutine
// waits on the process so its exit status is recorded once it ends.
func startBackground(command, workingDir string) (string, error) {
	cmd := shellCmd(context.Background(), command)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	// prepareBackground (platform-specific) puts the process in its own
	// group where that's how killTree reaches the children.
	prepareBackground(cmd)

	p := &bgProc{
		id:        "shell_" + strconv.FormatUint(bgSeq.Add(1), 10),
		command:   command,
		cmd:       cmd,
		startedAt: time.Now(),
	}
	cmd.Stdout = bgWriter{p}
	cmd.Stderr = bgWriter{p}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("run_shell: start background process: %w", err)
	}

	bgMu.Lock()
	bgProcs[p.id] = p
	bgMu.Unlock()

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

	return p.id, nil
}

// ShutdownBackground kills every still-running background process. Called
// as the app exits (see main.go) so nothing launched via run_shell
// outlives the TUI. Exported because the app's shutdown path is the one
// place outside this package that has to reach this lifecycle.
func ShutdownBackground() {
	bgMu.Lock()
	procs := make([]*bgProc, 0, len(bgProcs))
	for _, p := range bgProcs {
		procs = append(procs, p)
	}
	bgMu.Unlock()

	for _, p := range procs {
		p.mu.Lock()
		done := p.done
		p.mu.Unlock()
		if !done {
			_ = killTree(p.cmd)
		}
	}
}
