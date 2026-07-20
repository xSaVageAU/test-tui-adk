// Package webui serves the browser frontend: static assets embedded from
// web/ plus a JSON/SSE API mapped onto the same ui.Backend interface the
// TUI uses. Handlers are split by resource — stream.go (streaming turns,
// HITL confirmation, interrupt), sessions.go (session list/create/delete
// and transcripts), config.go (status, key, agents, themes, settings,
// files) — with the Server struct, routing, and shared helpers here.
// Routes use Go 1.22+ ServeMux method/wildcard patterns, so method
// dispatch and path-parameter parsing live in the route table, not the
// handlers.
package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"strconv"
	"sync"

	"tui-testing/internal/ui"

	"github.com/google/uuid"
)

//go:embed web/*
var webFS embed.FS

// Server wraps the WebUI HTTP handlers and maps them to the ADK backend
type Server struct {
	cfg          ui.AppConfig
	backend      ui.Backend
	sessionID    string
	ctx          context.Context
	activeCancel context.CancelFunc // current in-flight stream cancellation
	mu           sync.Mutex
}

// StartServer starts the WebUI HTTP server on the given port
func StartServer(ctx context.Context, cfg ui.AppConfig, port int) error {
	s := &Server{
		cfg:       cfg,
		backend:   cfg.Backend,
		sessionID: uuid.NewString(),
		ctx:       ctx,
	}

	// Windows can map .js to text/plain via the registry, which breaks ES
	// module loading — browsers enforce a JavaScript MIME type for module
	// scripts. Pin the types we embed so FileServer never depends on the
	// host's registry.
	_ = mime.AddExtensionType(".js", "text/javascript; charset=utf-8")
	_ = mime.AddExtensionType(".css", "text/css; charset=utf-8")

	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("web filesystem build: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	noCacheFileServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		fileServer.ServeHTTP(w, r)
	})

	// API routes live on their own mux: if they shared a mux with the "/"
	// static catch-all, a wrong-method API request would fall through to
	// the file server and 404 instead of getting ServeMux's automatic 405.
	api := http.NewServeMux()

	// config.go
	api.HandleFunc("GET /api/status", s.handleStatus)
	api.HandleFunc("POST /api/key", s.handleKey)
	api.HandleFunc("GET /api/agents", s.handleGetAgents)
	api.HandleFunc("POST /api/agents", s.handleUpdateAgents)
	api.HandleFunc("GET /api/themes", s.handleThemes)
	api.HandleFunc("GET /api/settings", s.handleGetSettings)
	api.HandleFunc("POST /api/settings", s.handleSaveSettings)
	api.HandleFunc("GET /api/files", s.handleFiles)

	// sessions.go
	api.HandleFunc("GET /api/sessions", s.handleListSessions)
	api.HandleFunc("POST /api/sessions", s.handleNewSession)
	api.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	api.HandleFunc("GET /api/transcript/{id}", s.handleTranscript)

	// stream.go
	api.HandleFunc("GET /api/stream", s.handleStream)
	api.HandleFunc("POST /api/confirm", s.handleConfirm)
	api.HandleFunc("POST /api/interrupt", s.handleInterrupt)

	mux := http.NewServeMux()
	mux.Handle("/", noCacheFileServer)
	mux.Handle("/api/", api)

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %d is already in use by another application.\nTo start on a different port, run: tui-testing --web --port <new_port>", port)
	}

	fmt.Printf("\n============================================\n")
	fmt.Printf("   WebUI Server Started Successfully!\n")
	fmt.Printf("   Access the UI at: http://localhost:%d\n", port)
	fmt.Printf("============================================\n\n")

	srv := &http.Server{
		Handler: mux,
	}

	// Wait for global context cancellation to clean up
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()

	return srv.Serve(listener)
}

// writeJSON sets the content type and encodes v; encode errors are not
// recoverable mid-response, so they are deliberately ignored.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
