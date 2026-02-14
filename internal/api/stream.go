package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// handleStatsStream serves an SSE stream of live stats updates.
// Events: "system" (1s), "docker" (5s), keepalive (15s).
func (s *Server) handleStatsStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ip := clientIP(r)
	log.Printf("SSE stream opened from %s", ip)

	// Ensure no write deadline for this long-lived connection
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("SSE: SetWriteDeadline failed (non-fatal): %v", err)
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx/proxy buffering
	flusher.Flush()

	// Send initial comment to bust through client-side buffers and confirm connection
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	// Subscribe to all broadcasters
	sysCh, sysCleanup := s.system.Broadcast.Subscribe(2)
	defer sysCleanup()

	dockerCh, dockerCleanup := s.docker.Broadcast.Subscribe(2)
	defer dockerCleanup()

	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			log.Printf("SSE stream closed from %s", ip)
			return

		case ev := <-sysCh:
			writeSSE(w, flusher, "system", ev)

		case ev := <-dockerCh:
			writeSSE(w, flusher, "docker", ev)

		case <-keepalive.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// writeSSE marshals data as JSON and writes it as an SSE event.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonBytes)
	flusher.Flush()
}
