package webui

import (
	"encoding/json"
	"net/http"

	"tui-testing/internal/settings"
	"tui-testing/internal/theme"
)

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

	writeJSON(w, map[string]any{
		"modelName":         modelName,
		"specialists":       specialists,
		"contextWindow":     contextWindow,
		"targetDescription": desc,
		"backendNote":       s.cfg.BackendNote,
		"activeSessionId":   s.sessionID,
	})
}

// handleFiles serves the file-tree sidebar: one directory's listing per
// call, VS Code-style lazy loading (?dir= is slash-separated relative to
// the target's cwd; empty lists the cwd itself — see tools.ListDir via
// the AppConfig closure). Deliberately not under s.mu — an SFTP ReadDir
// can be slow and the closure touches nothing the mutex guards; the
// sidebar polls this while open, so it must never stall streaming or
// status calls.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ListTargetDir == nil {
		http.Error(w, "File tree not supported", http.StatusNotFound)
		return
	}
	writeJSON(w, s.cfg.ListTargetDir(r.URL.Query().Get("dir")))
}

func (s *Server) handleKey(w http.ResponseWriter, r *http.Request) {
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
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	s.mu.Lock()
	s.backend = backend
	s.cfg.BackendNote = ""
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleThemes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, theme.Load())
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, settings.Load())
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
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
}

func (s *Server) handleGetAgents(w http.ResponseWriter, r *http.Request) {
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

	writeJSON(w, map[string]any{
		"agents": agents,
		"tools":  tools,
	})
}

func (s *Server) handleUpdateAgents(w http.ResponseWriter, r *http.Request) {
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
}
