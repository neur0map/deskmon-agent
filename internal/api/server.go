package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/neur0map/deskmon-agent/internal/collector"
	"github.com/neur0map/deskmon-agent/internal/config"
)

type Server struct {
	cfg       *config.Config
	system    *collector.SystemCollector
	docker    *collector.DockerCollector
	version   string
	httpSrv   *http.Server
	rateMu    sync.Mutex
	rateMap   map[string]*rateBucket
}

type rateBucket struct {
	tokens    int
	lastReset time.Time
}

const (
	rateLimit    = 60 // requests per minute
	ratePeriod   = time.Minute
	maxBodySize  = 1024 // 1KB
)

func NewServer(cfg *config.Config, system *collector.SystemCollector, docker *collector.DockerCollector, version string) *Server {
	return &Server{
		cfg:     cfg,
		system:  system,
		docker:  docker,
		version: version,
		rateMap: make(map[string]*rateBucket),
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Public endpoint
	mux.HandleFunc("GET /health", s.handleHealth)

	// Authenticated endpoints
	mux.HandleFunc("GET /stats", s.authMiddleware(s.handleStats))
	mux.HandleFunc("GET /stats/system", s.authMiddleware(s.handleSystemStats))
	mux.HandleFunc("GET /stats/docker", s.authMiddleware(s.handleDockerStats))

	// Agent control endpoints
	mux.HandleFunc("POST /agent/restart", s.authMiddleware(s.handleAgentRestart))
	mux.HandleFunc("POST /agent/stop", s.authMiddleware(s.handleAgentStop))
	mux.HandleFunc("GET /agent/status", s.authMiddleware(s.handleAgentStatus))

	handler := s.rateLimitMiddleware(s.securityHeaders(mux))

	addr := fmt.Sprintf("%s:%d", s.cfg.Bind, s.cfg.Port)
	s.httpSrv = &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 13, // 8KB
	}

	return s.httpSrv.ListenAndServe()
}

func (s *Server) Shutdown() error {
	if s.httpSrv != nil {
		return s.httpSrv.Close()
	}
	return nil
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Limit request body size
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		// Strip server fingerprinting
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")

		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}

		s.rateMu.Lock()
		bucket, exists := s.rateMap[ip]
		if !exists {
			bucket = &rateBucket{tokens: rateLimit, lastReset: time.Now()}
			s.rateMap[ip] = bucket
		}

		// Reset bucket if period has elapsed
		if time.Since(bucket.lastReset) > ratePeriod {
			bucket.tokens = rateLimit
			bucket.lastReset = time.Now()
		}

		if bucket.tokens <= 0 {
			s.rateMu.Unlock()
			w.Header().Set("Retry-After", "60")
			http.Error(w, "", http.StatusTooManyRequests)
			return
		}

		bucket.tokens--
		s.rateMu.Unlock()

		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.AuthToken == "" {
			// No token configured — reject all authenticated requests
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
			// No "Bearer " prefix found
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AuthToken)) != 1 {
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
	}
}
