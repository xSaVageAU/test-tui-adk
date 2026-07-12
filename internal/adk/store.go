package adk

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"google.golang.org/adk/v2/memory"
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

// openStores builds the persistent session and memory services, both
// backed by one sqlite file in appdir.Dir() (via the pure-Go
// glebarez/sqlite driver — already an ADK transitive dependency, and
// CGO-free, which matters for staying a single portable cross-compilable
// binary).
//
// Session's on-disk schema is entirely ADK's own — database.
// NewSessionService/AutoMigrate define and migrate it; nothing here
// touches it. Memory's schema is hand-rolled (see memory.go) since ADK
// only ships an in-process memory.Service and a Vertex AI-backed one, no
// generic database-backed option to plug into the same way.
func openStores() (session.Service, memory.Service, error) {
	path, err := appdir.Path("data.db")
	if err != nil {
		return nil, nil, fmt.Errorf("resolve data dir: %w", err)
	}

	sessSvc, err := database.NewSessionService(sqlite.Open(path), silentGormConfig())
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
	db, err := gorm.Open(sqlite.Open(path), silentGormConfig())
	if err != nil {
		return nil, nil, fmt.Errorf("open memory store: %w", err)
	}
	memSvc, err := newMemoryService(db)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate memory store: %w", err)
	}

	return sessSvc, memSvc, nil
}
