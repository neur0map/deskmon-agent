package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func (s *Server) handleContainerStart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing container id"})
		return
	}

	log.Printf("container start requested for %s", id)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+s.dockerSocket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Printf("container start %s: docker client error: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer cli.Close()

	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		log.Printf("container start %s: error: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	log.Printf("container start %s: success", id)
	writeJSON(w, controlResponse{Message: "started"})
}

func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing container id"})
		return
	}

	log.Printf("container stop requested for %s", id)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+s.dockerSocket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Printf("container stop %s: docker client error: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer cli.Close()

	timeout := 10
	if err := cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		log.Printf("container stop %s: error: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	log.Printf("container stop %s: success", id)
	writeJSON(w, controlResponse{Message: "stopped"})
}

func (s *Server) handleContainerRestart(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "missing container id"})
		return
	}

	log.Printf("container restart requested for %s", id)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+s.dockerSocket),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		log.Printf("container restart %s: docker client error: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer cli.Close()

	timeout := 10
	if err := cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout}); err != nil {
		log.Printf("container restart %s: error: %v", id, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	log.Printf("container restart %s: success", id)
	writeJSON(w, controlResponse{Message: "restarted"})
}
