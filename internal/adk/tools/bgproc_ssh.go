package tools

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// This file gives run_shell's background lifecycle a remote (SSH)
// implementation — the counterpart to bgproc.go's local bgProc.
//
// A background command is launched detached on the remote host via a
// small runner script written over SFTP (so the command needs no shell-
// quoting — it's file content, not an argument): setsid puts it in its
// own session, so it survives the launching SSH session closing and is
// its own process-group leader; its output is redirected to a log file
// and its exit code to an exit file, and the launcher echoes the PID.
// shell_output tails the log over SFTP; stop_shell kills the group by PID.
//
// Unlike a local background process, a remote one is independent of this
// app — it keeps running if the TUI exits (stopOnAppExit is false), the
// same as nohup; stop_shell is how you end it. A remote process from a
// previous run isn't recoverable after a restart (its id lives only in
// this process's registry).

// sshBgDir is where the per-process runner script, log, and exit-code
// files live on the remote host.
const sshBgDir = "/tmp/tui-agent-bg"

type sshBgProc struct {
	t         *sshTarget
	pid       int
	logPath   string
	exitPath  string
	startedAt time.Time

	mu     sync.Mutex
	cursor int64
}

func (t *sshTarget) StartBackground(command, workingDir string) (string, error) {
	id := newBgID()
	logPath := path.Join(sshBgDir, id+".log")
	exitPath := path.Join(sshBgDir, id+".exit")
	scriptPath := path.Join(sshBgDir, id+".sh")

	var cd string
	if workingDir != "" {
		cd = "cd " + shellQuote(workingDir) + " || exit 127\n"
	}
	// The command is written into the script verbatim (file content, not a
	// shell argument), so it needs no escaping. An EXIT trap records the
	// exit status for running() to read back — a trap (not a trailing
	// line) so it still fires when the command ends with an explicit
	// `exit N`, which would otherwise skip a trailing statement. exitPath
	// is under sshBgDir with an id-based name (safe characters only), so
	// it needs no quoting inside the trap's own single-quoted string.
	script := "#!/bin/sh\n" +
		"trap 'echo $? > " + exitPath + "' EXIT\n" +
		cd + command + "\n"

	if err := t.sftp.MkdirAll(sshBgDir); err != nil {
		return "", fmt.Errorf("prepare background dir: %w", err)
	}
	if err := t.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("write launcher script: %w", err)
	}

	// setsid detaches into a new session: the process survives the
	// launching SSH session closing and becomes its own process-group
	// leader (so stop can kill the whole group by -pid). Redirecting all
	// fds to the log / from /dev/null frees the SSH channel, so this Run
	// returns immediately instead of hanging until the child exits — the
	// exact failure the report hit with a bare `nohup ... &`.
	launch := "setsid sh " + shellQuote(scriptPath) + " </dev/null >" + shellQuote(logPath) + " 2>&1 & echo $!"
	res, err := t.Run(context.Background(), launch, "", 15)
	if err != nil {
		return "", fmt.Errorf("launch background process: %w", err)
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(res.Output))
	if perr != nil {
		return "", fmt.Errorf("launch background process: unexpected launcher output %q", strings.TrimSpace(res.Output))
	}

	putBg(id, &sshBgProc{t: t, pid: pid, logPath: logPath, exitPath: exitPath, startedAt: time.Now()})
	return id, nil
}

func (p *sshBgProc) readNew() (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f, err := p.t.sftp.Open(p.logPath)
	if err != nil {
		return "", nil // log not created yet (or already gone) — no new output, not fatal
	}
	defer f.Close()
	if _, err := f.Seek(p.cursor, io.SeekStart); err != nil {
		return "", nil
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("read remote log: %w", err)
	}
	p.cursor += int64(len(data))
	return string(data), nil
}

func (p *sshBgProc) running() (bool, int, error) {
	// A written exit file means the command finished on its own; its
	// content is the exit code.
	if code, ok := p.readExit(); ok {
		return false, code, nil
	}
	// No exit file: either still running, or killed before it could write
	// one. Ask the remote whether the pid is still alive.
	res, err := p.t.Run(context.Background(), fmt.Sprintf("kill -0 %d 2>/dev/null && echo A || echo D", p.pid), "", 10)
	if err != nil {
		return true, 0, err
	}
	if strings.TrimSpace(res.Output) == "A" {
		return true, 0, nil
	}
	return false, -1, nil // gone without an exit file (killed)
}

func (p *sshBgProc) readExit() (int, bool) {
	f, err := p.t.sftp.Open(p.exitPath)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, false // being written this instant
	}
	code, err := strconv.Atoi(s)
	if err != nil {
		return -1, true
	}
	return code, true
}

func (p *sshBgProc) stop() error {
	// Kill the whole process group: the script shell is the session/group
	// leader (setsid), so -pid targets it and every child. TERM first for a
	// clean shutdown, then KILL for anything that ignores it.
	cmd := fmt.Sprintf("kill -TERM -%d 2>/dev/null; sleep 0.3; kill -KILL -%d 2>/dev/null; true", p.pid, p.pid)
	_, err := p.t.Run(context.Background(), cmd, "", 15)
	return err
}

func (p *sshBgProc) startTime() time.Time { return p.startedAt }
func (p *sshBgProc) stopOnAppExit() bool  { return false }
