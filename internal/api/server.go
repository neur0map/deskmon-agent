package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/neur0map/deskmon-agent/internal/collector"
	"github.com/neur0map/deskmon-agent/internal/config"
)

type Server struct {
	cfg          *config.Config
	system       *collector.SystemCollector
	docker       *collector.DockerCollector
	version      string
	httpSrv      *http.Server
	dockerSocket string
	rateMu       sync.Mutex
	rateMap      map[string]*rateBucket
	stopCh       chan struct{}
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
		cfg:          cfg,
		system:       system,
		docker:       docker,
		version:      version,
		dockerSocket: config.DefaultDockerSock,
		rateMap:      make(map[string]*rateBucket),
		stopCh:       make(chan struct{}),
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
	mux.HandleFunc("GET /stats/processes", s.authMiddleware(s.handleProcessStats))

	// Agent control endpoints
	mux.HandleFunc("POST /agent/restart", s.authMiddleware(s.handleAgentRestart))
	mux.HandleFunc("POST /agent/stop", s.authMiddleware(s.handleAgentStop))
	mux.HandleFunc("GET /agent/status", s.authMiddleware(s.handleAgentStatus))

	// Container action endpoints
	mux.HandleFunc("POST /containers/{id}/start", s.authMiddleware(s.handleContainerStart))
	mux.HandleFunc("POST /containers/{id}/stop", s.authMiddleware(s.handleContainerStop))
	mux.HandleFunc("POST /containers/{id}/restart", s.authMiddleware(s.handleContainerRestart))

	// Process action endpoints
	mux.HandleFunc("POST /processes/{pid}/kill", s.authMiddleware(s.handleProcessKill))

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

	s.startRateCleanup()

	return s.httpSrv.ListenAndServe()
}

func (s *Server) Shutdown() error {
	close(s.stopCh)
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
			log.Printf("rate limit exceeded for %s on %s %s", ip, r.Method, r.URL.Path)
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
		ip := clientIP(r)

		if s.cfg.AuthToken == "" {
			log.Printf("auth reject %s %s from %s: no token configured on agent", r.Method, r.URL.Path, ip)
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			log.Printf("auth reject %s %s from %s: missing Authorization header", r.Method, r.URL.Path, ip)
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		if token == auth {
			log.Printf("auth reject %s %s from %s: missing Bearer prefix", r.Method, r.URL.Path, ip)
			http.Error(w, "", http.StatusUnauthorized)
			return
		}

		if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AuthToken)) != 1 {
			log.Printf("auth reject %s %s from %s: invalid token", r.Method, r.URL.Path, ip)
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

func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (s *Server) sweepRateBuckets() {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	cutoff := 2 * ratePeriod
	for ip, bucket := range s.rateMap {
		if time.Since(bucket.lastReset) > cutoff {
			delete(s.rateMap, ip)
		}
	}
}

func (s *Server) startRateCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.sweepRateBuckets()
			case <-s.stopCh:
				return
			}
		}
	}()
}
