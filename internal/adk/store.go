package adk

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"

	"tui-testing/internal/appdir"
)

// silentGormConfig is passed to every gorm.Open in this package. GORM's
// default logger writes query/warning lines straight to stdout via the
// standard log package — invisible in a normal CLI tool, but in a
// full-screen Bubble Tea app that corrupts the rendered frame, since
// nothing routes it through the TUI's own rendering. There's no log file
// or debug pane to redirect it to yet (see the plugin-based debug pane
// idea from the earlier ADK feature survey), so silencing it entirely is
// the right call for now over routing it anywhere.
func silentGormConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

// dataPath resolves a path under appdir's "data" subdirectory (creating
// it if missing) — sqlite session storage and credentials.json live
// here, kept apart from the plainly human-editable files (agent.json,
// settings.json, subagents/, themes/) directly in appdir's root.
func dataPath(elem ...string) (string, error) {
	dir, err := appdir.Path("data")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, elem...)...), nil
}

// openSessionStore builds the persistent session service, backed by a
// sqlite file in appdir's "data" subdirectory (via the pure-Go
// glebarez/sqlite driver — already an ADK transitive dependency, and
// CGO-free, which matters for staying a single portable
// cross-compilable binary). The on-disk schema is entirely ADK's own —
// database.NewSessionService/AutoMigrate define and migrate it; nothing
// here touches it.
func openSessionStore() (session.Service, error) {
	path, err := dataPath("data.db")
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}

	sessSvc, err := database.NewSessionService(sqlite.Open(path), silentGormConfig())
	if err != nil {
		return nil, fmt.Errorf("open session store: %w", err)
	}
	if err := database.AutoMigrate(sessSvc); err != nil {
		return nil, fmt.Errorf("migrate session store: %w", err)
	}

	return sessSvc, nil
}
