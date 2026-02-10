package api

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"syscall"
)

func (s *Server) handleProcessKill(w http.ResponseWriter, r *http.Request) {
	pidStr := r.PathValue("pid")
	if pidStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "missing pid"})
		return
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": fmt.Sprintf("invalid pid: %s", pidStr)})
		return
	}

	ip := clientIP(r)
	log.Printf("process kill requested for pid %d from %s", pid, ip)

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		log.Printf("process kill pid %d: error: %v", pid, err)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	log.Printf("process kill pid %d: success (from %s)", pid, ip)
	writeJSON(w, controlResponse{Message: "killed"})
}
