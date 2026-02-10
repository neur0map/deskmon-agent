package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/neur0map/deskmon-agent/internal/collector"
	"github.com/neur0map/deskmon-agent/internal/collector/services"
)

type healthResponse struct {
	Status string `json:"status"`
}

type statsResponse struct {
	System     collector.SystemStats      `json:"system"`
	Containers []collector.ContainerStats  `json:"containers"`
	Processes  []collector.ProcessInfo     `json:"processes"`
	Services   []services.ServiceStats     `json:"services"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, healthResponse{Status: "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	system := s.system.Collect()
	containers := s.docker.Collect()
	processes := s.system.CollectTopProcesses(10)
	svcStats := s.services.Collect()

	writeJSON(w, statsResponse{
		System:     system,
		Containers: containers,
		Processes:  processes,
		Services:   svcStats,
	})
}

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.system.Collect())
}

func (s *Server) handleDockerStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.docker.Collect())
}

func (s *Server) handleProcessStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.system.CollectTopProcesses(10))
}

func (s *Server) handleServiceStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.services.Collect())
}

func (s *Server) handleServiceDebug(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.services.DebugInfo())
}

// handleServiceConfigure sets credentials/config for a service plugin.
// POST /services/{pluginId}/configure  {"password":"..."}
func (s *Server) handleServiceConfigure(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("pluginId")
	if pluginID == "" {
		http.Error(w, `{"error":"missing pluginId"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if body.Password == "" {
		http.Error(w, `{"error":"password is required"}`, http.StatusBadRequest)
		return
	}

	// Store in the service detector
	s.services.SetServiceConfig(pluginID, "password", body.Password)

	// Persist to config file
	if s.cfg != nil {
		switch pluginID {
		case "pihole":
			s.cfg.Services.PiHole.Password = body.Password
		}
		if s.configPath != "" {
			if err := s.cfg.Save(s.configPath); err != nil {
				log.Printf("api: failed to save config: %v", err)
			}
		}
	}

	ip := clientIP(r)
	log.Printf("api: service %s configured by %s", pluginID, ip)

	writeJSON(w, controlResponse{Message: "configured"})
}
