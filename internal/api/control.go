package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/neur0map/deskmon-agent/internal/systemctl"
)

type agentStatusResponse struct {
	Version string `json:"version"`
	Status  string `json:"status"`
}

type controlResponse struct {
	Message string `json:"message"`
}

func (s *Server) handleAgentRestart(w http.ResponseWriter, r *http.Request) {
	if err := systemctl.Restart(); err != nil {
		if errors.Is(err, systemctl.ErrDockerMode) {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
	}

	// Respond before restarting so the client gets a response
	writeJSON(w, controlResponse{Message: "restarting"})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	if err := systemctl.Stop(); err != nil {
		if errors.Is(err, systemctl.ErrDockerMode) {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
	}

	// Respond before stopping so the client gets a response
	writeJSON(w, controlResponse{Message: "stopping"})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) handleAgentStatus(w http.ResponseWriter, r *http.Request) {
	status, _ := systemctl.Status()
	writeJSON(w, agentStatusResponse{
		Version: s.version,
		Status:  strings.TrimSpace(status),
	})
}
