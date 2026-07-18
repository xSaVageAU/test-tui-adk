package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"tui-testing/internal/settings"
)

// sshTarget runs commands over a persistent SSH connection and does file
// operations over SFTP on that same connection — so the agent operates on
// a remote machine while every tool stays unchanged (see target.go).
//
// The connection persists for the target's lifetime (established once by
// newSSHTarget, closed by Close); each Run opens a fresh exec session on
// it, which keeps output/exit-code capture clean at the cost of not
// carrying cwd/env between commands — a deliberate choice over a single
// stateful shell (see the tool-execution-targets memory). File ops pull a
// file over SFTP, and write_file/edit_file push the modified bytes back.
// Background processes (run_shell background:true) aren't supported over
// SSH yet — StartBackground returns an error.
type sshTarget struct {
	client *ssh.Client
	sftp   *sftp.Client
}

func newSSHTarget(cfg settings.SSHSettings) (*sshTarget, error) {
	if cfg.Host == "" || cfg.User == "" {
		return nil, fmt.Errorf("ssh target needs at least agent.target.ssh.host and .user set")
	}
	auth, err := sshAuth(cfg)
	if err != nil {
		return nil, err
	}
	hostKey, err := hostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auth,
		HostKeyCallback: hostKey,
		Timeout:         15 * time.Second,
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	sc, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("start sftp: %w", err)
	}
	return &sshTarget{client: client, sftp: sc}, nil
}

func (t *sshTarget) Run(ctx context.Context, command, workingDir string, timeoutSec int) (RunResult, error) {
	sess, err := t.client.NewSession()
	if err != nil {
		return RunResult{ExitCode: -1}, fmt.Errorf("ssh session: %w", err)
	}
	defer sess.Close()

	// A single locked buffer for stdout+stderr — the SSH library writes to
	// it from its own goroutines, and the timeout path reads it, so the
	// access has to be synchronized.
	buf := &lockedBuffer{}
	sess.Stdout = buf
	sess.Stderr = buf

	full := command
	if workingDir != "" {
		full = "cd " + shellQuote(workingDir) + " && " + command
	}

	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
	}

	if err := sess.Start(full); err != nil {
		return RunResult{ExitCode: -1}, fmt.Errorf("ssh start: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGKILL)
		_ = sess.Close()
		return RunResult{Output: buf.String(), ExitCode: -1}, fmt.Errorf("command timed out after %ds", timeoutSec)
	case werr := <-done:
		res := RunResult{Output: buf.String()}
		if werr == nil {
			return res, nil
		}
		// A non-zero remote exit comes back as *ssh.ExitError — report it
		// via ExitCode with no error, same contract as localTarget.Run.
		var ee *ssh.ExitError
		if errors.As(werr, &ee) {
			res.ExitCode = ee.ExitStatus()
			return res, nil
		}
		res.ExitCode = -1
		return res, werr
	}
}

func (t *sshTarget) ReadFile(path string) ([]byte, error) {
	f, err := t.sftp.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (t *sshTarget) WriteFile(path string, data []byte, mode fs.FileMode) error {
	f, err := t.sftp.Create(path) // truncates or creates
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Best-effort chmod: a create over SFTP doesn't take a mode, so set it
	// after. A remote filesystem may not support it; don't fail the write.
	_ = t.sftp.Chmod(path, mode)
	return nil
}

func (t *sshTarget) Stat(path string) (fs.FileInfo, error) { return t.sftp.Stat(path) }

func (t *sshTarget) ReadDir(path string) ([]fs.DirEntry, error) {
	infos, err := t.sftp.ReadDir(path)
	if err != nil {
		return nil, err
	}
	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	return entries, nil
}

func (t *sshTarget) Open(path string) (io.ReadCloser, error) { return t.sftp.Open(path) }

func (t *sshTarget) Walk(root string, fn WalkFunc) error {
	w := t.sftp.Walk(root)
	for w.Step() {
		if err := w.Err(); err != nil {
			if ferr := fn(w.Path(), nil, err); ferr != nil {
				return ferr
			}
			continue
		}
		ferr := fn(w.Path(), w.Stat(), nil)
		switch {
		case ferr == nil:
		case errors.Is(ferr, fs.SkipDir):
			w.SkipDir()
		case errors.Is(ferr, fs.SkipAll):
			return nil
		default:
			return ferr
		}
	}
	return nil
}

func (t *sshTarget) Close() error {
	// Close SFTP first (it rides on the SSH connection), then the client.
	if t.sftp != nil {
		t.sftp.Close()
	}
	if t.client != nil {
		return t.client.Close()
	}
	return nil
}

// lockedBuffer is a bytes.Buffer safe for the concurrent Write (from the
// SSH library) and String (from Run's timeout path) that sshTarget.Run
// needs.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// sshAuth builds public-key auth from cfg.KeyPath, or the first of the
// usual default key files when it's unset. Passphrase-protected keys and
// ssh-agent aren't supported yet — a clear error rather than a silent
// failure (both are noted as follow-ups in the tool-execution-targets
// memory).
func sshAuth(cfg settings.SSHSettings) ([]ssh.AuthMethod, error) {
	keyPath := expandHome(cfg.KeyPath)
	if keyPath == "" {
		for _, name := range []string{"id_ed25519", "id_rsa"} {
			p := filepath.Join(homeDir(), ".ssh", name)
			if _, err := os.Stat(p); err == nil {
				keyPath = p
				break
			}
		}
	}
	if keyPath == "" {
		return nil, fmt.Errorf("no SSH private key found — set agent.target.ssh.keyPath")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read ssh key %q: %w", keyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh key %q (passphrase-protected keys aren't supported yet): %w", keyPath, err)
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}

// hostKeyCallback verifies the remote host key against known_hosts unless
// InsecureSkipHostKey is set. A missing/unknown host is a clear error
// telling the user to ssh in once first, rather than trust-on-first-use.
func hostKeyCallback(cfg settings.SSHSettings) (ssh.HostKeyCallback, error) {
	if cfg.InsecureSkipHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	khPath := expandHome(cfg.KnownHosts)
	if khPath == "" {
		khPath = filepath.Join(homeDir(), ".ssh", "known_hosts")
	}
	cb, err := knownhosts.New(khPath)
	if err != nil {
		return nil, fmt.Errorf("load known_hosts %q: %w — ssh into the host once to add its key, or set agent.target.ssh.insecureSkipHostKey", khPath, err)
	}
	return cb, nil
}

// shellQuote wraps s in single quotes for a POSIX remote shell, escaping
// any embedded single quotes — used to prepend `cd <dir> &&` safely.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// expandHome resolves a leading ~ to the user's home directory; other
// paths pass through unchanged.
func expandHome(p string) string {
	if p == "~" {
		return homeDir()
	}
	if strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return filepath.Join(homeDir(), p[2:])
	}
	return p
}
