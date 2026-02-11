package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

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

// handleServiceAction performs an action on a detected service plugin.
// POST /services/{pluginId}/action  {"action":"setBlocking","params":{"enabled":true}}
func (s *Server) handleServiceAction(w http.ResponseWriter, r *http.Request) {
	pluginID := r.PathValue("pluginId")
	if pluginID == "" {
		http.Error(w, `{"error":"missing pluginId"}`, http.StatusBadRequest)
		return
	}

	var body struct {
		Action string                 `json:"action"`
		Params map[string]interface{} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	if body.Action == "" {
		http.Error(w, `{"error":"action is required"}`, http.StatusBadRequest)
		return
	}

	ip := clientIP(r)
	log.Printf("api: service action %s/%s requested by %s", pluginID, body.Action, ip)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := s.services.PerformAction(ctx, pluginID, body.Action, body.Params)
	if err != nil {
		log.Printf("api: service action %s/%s failed: %v", pluginID, body.Action, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	log.Printf("api: service action %s/%s: %s", pluginID, body.Action, result)
	writeJSON(w, controlResponse{Message: result})
}
