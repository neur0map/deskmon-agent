package api

import (
	"net/http"
)

type healthResponse struct {
	Status string `json:"status"`
}

type statsResponse struct {
	System     interface{} `json:"system"`
	Containers interface{} `json:"containers"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, healthResponse{Status: "ok"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	system := s.system.Collect()
	containers := s.docker.Collect()

	writeJSON(w, statsResponse{
		System:     system,
		Containers: containers,
	})
}

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.system.Collect())
}

func (s *Server) handleDockerStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.docker.Collect())
}
