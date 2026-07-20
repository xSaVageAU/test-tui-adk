package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"tui-testing/internal/ui"
)

// beginTurn cancels any in-flight stream, adopts sessionID as the active
// session when non-empty (falling back to the current one otherwise), and
// installs a fresh cancellable context as the active turn. It returns the
// turn context, the resolved session ID, and a cleanup func the caller
// must defer.
func (s *Server) beginTurn(sessionID string) (context.Context, string, func()) {
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

	end := func() {
		s.mu.Lock()
		s.activeCancel = nil
		s.mu.Unlock()
	}
	return ctx, sessionID, end
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		http.Error(w, "Backend not connected. Use /api/key first.", http.StatusBadRequest)
		return
	}

	message := r.URL.Query().Get("message")
	if message == "" {
		http.Error(w, "Message parameter required", http.StatusBadRequest)
		return
	}

	ctx, sessionID, end := s.beginTurn(r.URL.Query().Get("sessionId"))
	defer end()

	stream, err := s.backend.Stream(ctx, sessionID, message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeSSEStream(w, stream)
}

func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	if s.backend == nil {
		http.Error(w, "Backend not connected", http.StatusBadRequest)
		return
	}

	var req struct {
		SessionID string                    `json:"sessionId"`
		Decisions []ui.ConfirmationDecision `json:"decisions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, sessionID, end := s.beginTurn(req.SessionID)
	defer end()

	stream, err := s.backend.RespondToConfirmation(ctx, sessionID, req.Decisions)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.writeSSEStream(w, stream)
}

func (s *Server) handleInterrupt(w http.ResponseWriter, r *http.Request) {
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
