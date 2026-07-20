package webui

import (
	"net/http"

	"github.com/google/uuid"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		writeJSON(w, []any{})
		return
	}
	sessions, err := s.backend.ListSessions(s.ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, sessions)
}

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.sessionID = uuid.NewString()
	s.mu.Unlock()

	writeJSON(w, map[string]string{"sessionId": s.sessionID})
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if s.backend == nil {
		http.Error(w, "Backend not connected", http.StatusBadRequest)
		return
	}

	if err := s.backend.DeleteSession(s.ctx, id); err != nil {
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
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	if s.backend == nil {
		writeJSON(w, []any{})
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

	writeJSON(w, entries)
}
