package tools

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// localTarget runs every operation on the local host — the default
// target, and exactly the behavior the tools had before the Target
// abstraction existed (the same os/exec/filepath calls, moved here
// verbatim). Stateless, so the zero value is ready to use and Close is a
// no-op.
type localTarget struct{}

// Run executes command in the platform shell (see shellCmd in
// run_shell_*.go), enforcing a timeout when one is given. A non-zero exit
// is returned via RunResult.ExitCode with no error — it's normal command
// behavior, not a tool failure; only a timeout or a failure to start the
// process is an error.
func (localTarget) Run(ctx context.Context, command, workingDir string, timeoutSec int) (RunResult, error) {
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}
	cmd := shellCmd(ctx, command)
	if workingDir != "" {
		cmd.Dir = workingDir
	}
	out, err := cmd.CombinedOutput()
	res := RunResult{Output: string(out)}

	if ctx.Err() == context.DeadlineExceeded {
		res.ExitCode = -1
		return res, fmt.Errorf("command timed out after %ds", timeoutSec)
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		res.ExitCode = -1
		return res, err
	}
	return res, nil
}

func (localTarget) StartBackground(command, workingDir string) (string, error) {
	return startBackground(command, workingDir)
}

func (localTarget) ReadFile(path string) ([]byte, error)                 { return os.ReadFile(path) }
func (localTarget) WriteFile(p string, d []byte, m fs.FileMode) error    { return os.WriteFile(p, d, m) }
func (localTarget) Stat(path string) (fs.FileInfo, error)                { return os.Stat(path) }
func (localTarget) ReadDir(path string) ([]fs.DirEntry, error)           { return os.ReadDir(path) }
func (localTarget) Open(path string) (io.ReadCloser, error)              { return os.Open(path) }
func (localTarget) Getwd() (string, error)                               { return os.Getwd() }
func (localTarget) Close() error                                         { return nil }

func (localTarget) Walk(root string, fn WalkFunc) error {
	return filepath.Walk(root, func(path string, info fs.FileInfo, err error) error {
		return fn(path, info, err)
	})
}
