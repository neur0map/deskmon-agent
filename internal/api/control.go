package api

import (
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
	// Respond before restarting so the client gets a response
	writeJSON(w, controlResponse{Message: "restarting"})

	// Flush the response
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Restart asynchronously — the process will die and systemd will bring it back
	go func() {
		_ = systemctl.Restart()
	}()
}

func (s *Server) handleAgentStop(w http.ResponseWriter, r *http.Request) {
	// Respond before stopping so the client gets a response
	writeJSON(w, controlResponse{Message: "stopping"})

	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	go func() {
		_ = systemctl.Stop()
	}()
}

func (s *Server) handleAgentStatus(w http.ResponseWriter, r *http.Request) {
	status, _ := systemctl.Status()
	writeJSON(w, agentStatusResponse{
		Version: s.version,
		Status:  strings.TrimSpace(status),
	})
}
