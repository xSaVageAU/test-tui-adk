// Package appdir resolves the one standard, per-user directory this
// app's persistent state lives in — every package that needs to write
// something to disk (sqlite stores today, maybe a settings file or logs
// later) should ask this package for a path rather than deciding its own
// spot, so everything the app ever persists ends up findable in one
// place instead of scattered.
package appdir

import (
	"os"
	"path/filepath"
)

// Name is the directory this app's data lives under, within the OS's
// standard per-user config location. A placeholder tied to this repo's
// module name — rename it here, once, whenever this stops being a
// prototype and gets its real product name.
const Name = "tui-testing"

// Dir returns (creating it if needed) this app's standard data
// directory — a dotfile-style folder directly under the user's home
// directory ("~/.tui-testing", i.e. "/root/.tui-testing" for a Linux
// machine with only a root user, "C:\Users\<user>\.tui-testing" on
// Windows), rather than the process's working directory, so where the
// binary happens to be launched from never changes what it remembers.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "."+Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// Path joins Dir() with the given path elements — e.g. appdir.Path("data.db").
func Path(elem ...string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, elem...)...), nil
}
