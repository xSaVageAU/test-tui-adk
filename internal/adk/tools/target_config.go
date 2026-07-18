package tools

import (
	"fmt"

	"tui-testing/internal/settings"
)

// ConfigureTarget reads the execution-target setting (see
// settings.AgentSettings.Target) and installs the matching Target as the
// active one for every tool. It's called at startup and whenever the
// target config changes.
//
// It returns a short human-readable description of the active target (for
// the boot note / top bar) and an error. On an SSH connection failure the
// active target is left unchanged (local by default), so tools keep
// working on the host — the caller is expected to surface the error so
// the user knows their remote target didn't take effect.
func ConfigureTarget() (string, error) {
	s := settings.Load()
	tcfg := s.Agent.Target

	if tcfg.Type != settings.TargetSSH {
		if prev := SetTarget(localTarget{}); prev != nil {
			prev.Close()
		}
		return "host", nil
	}

	t, err := newSSHTarget(tcfg.SSH)
	if err != nil {
		return "", fmt.Errorf("ssh target: %w", err)
	}
	if prev := SetTarget(t); prev != nil {
		prev.Close()
	}
	return fmt.Sprintf("ssh://%s@%s", tcfg.SSH.User, tcfg.SSH.Host), nil
}

// CloseTarget resets the active target to local and closes whatever was
// installed (closing an SSH/SFTP connection). Called as the app exits.
func CloseTarget() {
	if prev := SetTarget(localTarget{}); prev != nil {
		prev.Close()
	}
}
