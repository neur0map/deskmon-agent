package api

import (
	"net/http"

	"github.com/neur0map/deskmon-agent/internal/collector"
)

type healthResponse struct {
	Status string `json:"status"`
}

type statsResponse struct {
	System     collector.SystemStats     `json:"system"`
	Containers []collector.ContainerStats `json:"containers"`
	Processes  []collector.ProcessInfo    `json:"processes"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, healthResponse{Status: "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	system := s.system.Collect()
	containers := s.docker.Collect()
	processes := s.system.CollectTopProcesses(10)

	writeJSON(w, statsResponse{
		System:     system,
		Containers: containers,
		Processes:  processes,
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
