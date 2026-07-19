package webui

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"tui-testing/internal/settings"
	"tui-testing/internal/theme"
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

	subFS, err := fs.Sub(webFS, "web")
	if err != nil {
		return fmt.Errorf("web filesystem build: %w", err)
	}
	fileServer := http.FileServer(http.FS(subFS))
	noCacheFileServer := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		fileServer.ServeHTTP(w, r)
	})

	mux := http.NewServeMux()
	mux.Handle("/", noCacheFileServer)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionDetail)
	mux.HandleFunc("/api/transcript/", s.handleTranscript)
	mux.HandleFunc("/api/stream", s.handleStream)
	mux.HandleFunc("/api/confirm", s.handleConfirm)
	mux.HandleFunc("/api/key", s.handleKey)
	mux.HandleFunc("/api/agents", s.handleAgents)
	mux.HandleFunc("/api/interrupt", s.handleInterrupt)
	mux.HandleFunc("/api/themes", s.handleThemes)
	mux.HandleFunc("/api/settings", s.handleSettings)

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

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var specialists []string
	var modelName string
	var contextWindow int
	if s.backend != nil {
		specialists = s.backend.Specialists()
		modelName = s.backend.ModelName()
		contextWindow = s.backend.ContextWindow()
	}

	desc := "local host"
	if s.cfg.ConfigureTarget != nil {
		if d, err := s.cfg.ConfigureTarget(); err == nil && d != "" {
			desc = d
		}
	}

	resp := map[string]any{
		"modelName":         modelName,
		"specialists":       specialists,
		"contextWindow":     contextWindow,
		"targetDescription": desc,
		"backendNote":       s.cfg.BackendNote,
		"activeSessionId":   s.sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Provider string `json:"provider"`
		Key      string `json:"key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.cfg.NewBackend == nil {
		http.Error(w, "API key update not supported", http.StatusNotImplemented)
		return
	}

	backend, err := s.cfg.NewBackend(s.ctx, req.Provider, req.Key)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	s.mu.Lock()
	s.backend = backend
	s.cfg.BackendNote = ""
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s.backend == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]any{})
			return
		}
		sessions, err := s.backend.ListSessions(s.ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sessions)
		return
	}

	if r.Method == http.MethodPost {
		s.mu.Lock()
		s.sessionID = uuid.NewString()
		s.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"sessionId": s.sessionID})
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if id == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if s.backend == nil {
		http.Error(w, "Backend not connected", http.StatusBadRequest)
		return
	}

	err := s.backend.DeleteSession(s.ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	if s.sessionID == id {
		s.sessionID = uuid.NewString()
	}
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleTranscript(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/transcript/")
	if id == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if s.backend == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]any{})
		return
	}

	entries, err := s.backend.GetTranscript(s.ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.sessionID = id // Sync active session
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		http.Error(w, "Backend not connected. Use /api/key first.", http.StatusBadRequest)
		return
	}

	message := r.URL.Query().Get("message")
	sessionID := r.URL.Query().Get("sessionId")

	if message == "" {
		http.Error(w, "Message parameter required", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if sessionID != "" {
		s.sessionID = sessionID
	} else {
		sessionID = s.sessionID
	}

	if s.activeCancel != nil {
		s.activeCancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.activeCancel = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.activeCancel = nil
		s.mu.Unlock()
	}()

	stream, err := s.backend.Stream(ctx, sessionID, message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeSSEStream(w, stream)
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.backend == nil {
		http.Error(w, "Backend not connected", http.StatusBadRequest)
		return
	}

	var req struct {
		SessionID string                     `json:"sessionId"`
		Decisions []ui.ConfirmationDecision `json:"decisions"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	if req.SessionID != "" {
		s.sessionID = req.SessionID
	}
	sessionID := s.sessionID

	if s.activeCancel != nil {
		s.activeCancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.activeCancel = cancel
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.activeCancel = nil
		s.mu.Unlock()
	}()

	stream, err := s.backend.RespondToConfirmation(ctx, sessionID, req.Decisions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeSSEStream(w, stream)
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeCancel != nil {
		s.activeCancel()
		s.activeCancel = nil
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusConflict) // No active turn to cancel
}

func (s *Server) handleThemes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	themes := theme.Load()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(themes)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		sett := settings.Load()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sett)
		return
	}

	if r.Method == http.MethodPost {
		var sett settings.Settings
		if err := json.NewDecoder(r.Body).Decode(&sett); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := settings.Save(sett); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.cfg.ConfigureTarget != nil {
			_, _ = s.cfg.ConfigureTarget()
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if s.cfg.ListAgents == nil || s.cfg.ListTools == nil {
			http.Error(w, "Agents configuration not supported", http.StatusNotImplemented)
			return
		}

		agents, err := s.cfg.ListAgents()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tools, err := s.cfg.ListTools()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		resp := map[string]any{
			"agents": agents,
			"tools":  tools,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			ID       string   `json:"id"`
			Provider string   `json:"provider"`
			Model    string   `json:"model"`
			Tools    []string `json:"tools"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if s.cfg.SetAgentProvider != nil && req.Provider != "" {
			if err := s.cfg.SetAgentProvider(req.ID, req.Provider); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if s.cfg.SetAgentModel != nil && req.Model != "" {
			if err := s.cfg.SetAgentModel(req.ID, req.Model); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		if s.cfg.SetAgentTools != nil {
			if err := s.cfg.SetAgentTools(req.ID, req.Tools); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}

		// Rebuild the backend dynamically from the new disk config files
		if s.cfg.NewBackend != nil {
			backend, err := s.cfg.NewBackend(s.ctx, "", "")
			if err != nil {
				http.Error(w, "rebuilding backend: "+err.Error(), http.StatusInternalServerError)
				return
			}
			s.mu.Lock()
			s.backend = backend
			s.mu.Unlock()
		}

		w.WriteHeader(http.StatusOK)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) writeSSEStream(w http.ResponseWriter, stream <-chan ui.StreamChunk) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case chunk, ok := <-stream:
			if !ok {
				// Stream finished normally
				return
			}
			data, err := json.Marshal(chunk)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-s.ctx.Done():
			return
		}
	}
}
