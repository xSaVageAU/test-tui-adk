package adk

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/database"
)

// dataDir returns (creating it if needed) the directory persistent state
// lives in — a per-user config directory rather than the process's
// working directory, so where the binary happens to be launched from
// doesn't change what it remembers.
func dataDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "tui-testing")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// openStores builds the persistent session and memory services, both
// backed by one sqlite file in dataDir (via the pure-Go glebarez/sqlite
// driver — already an ADK transitive dependency, and CGO-free, which
// matters for staying a single portable cross-compilable binary).
//
// Session's on-disk schema is entirely ADK's own — database.
// NewSessionService/AutoMigrate define and migrate it; nothing here
// touches it. Memory's schema is hand-rolled (see memory.go) since ADK
// only ships an in-process memory.Service and a Vertex AI-backed one, no
// generic database-backed option to plug into the same way.
func openStores() (session.Service, memory.Service, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve data dir: %w", err)
	}
	path := filepath.Join(dir, "data.db")

	sessSvc, err := database.NewSessionService(sqlite.Open(path))
	if err != nil {
		return nil, nil, fmt.Errorf("open session store: %w", err)
	}
	if err := database.AutoMigrate(sessSvc); err != nil {
		return nil, nil, fmt.Errorf("migrate session store: %w", err)
	}

	// A second, independent connection to the same file — NewSessionService
	// takes a dialector and opens its own *gorm.DB internally rather than
	// accepting/exposing one, so there's no handle to share here even if
	// it would've been marginally tidier to.
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}
	memSvc, err := newMemoryService(db)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate memory store: %w", err)
	}

	return sessSvc, memSvc, nil
}
