package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/neur0map/deskmon-agent/internal/collector"
	"github.com/neur0map/deskmon-agent/internal/config"
)

func newTestServer(authToken string) *Server {
	cfg := &config.Config{
		Port:      7654,
		Bind:      "127.0.0.1",
		AuthToken: authToken,
	}
	sys := collector.NewSystemCollector()
	docker := collector.NewDockerCollector("/var/run/docker.sock")
	return NewServer(cfg, sys, docker, "test")
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer("testtoken")

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("expected status ok, got %s", resp.Status)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	srv := newTestServer("secrettoken")

	called := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer secrettoken")
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("handler was not called with valid token")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	srv := newTestServer("secrettoken")

	called := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer wrongtoken")
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called with invalid token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_MissingToken(t *testing.T) {
	srv := newTestServer("secrettoken")

	called := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called without token")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_NoBearerPrefix(t *testing.T) {
	srv := newTestServer("secrettoken")

	called := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "secrettoken")
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called without Bearer prefix")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddleware_NoTokenConfigured(t *testing.T) {
	srv := newTestServer("")

	called := false
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	req.Header.Set("Authorization", "Bearer anytoken")
	w := httptest.NewRecorder()
	handler(w, req)

	if called {
		t.Error("handler should not be called when no token is configured")
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRateLimiting(t *testing.T) {
	srv := newTestServer("tok")

	handler := srv.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust the rate limit
	for i := 0; i < rateLimit; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d should succeed, got %d", i, w.Code)
		}
	}

	// Next request should be rate limited
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}

	// Different IP should still work
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = "192.168.1.2:1234"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("different IP should not be rate limited, got %d", w2.Code)
	}
}

func TestAgentStatus(t *testing.T) {
	srv := newTestServer("tok")

	req := httptest.NewRequest(http.MethodGet, "/agent/status", nil)
	w := httptest.NewRecorder()
	srv.handleAgentStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var resp agentStatusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Version != "test" {
		t.Errorf("expected version 'test', got '%s'", resp.Version)
	}
}
