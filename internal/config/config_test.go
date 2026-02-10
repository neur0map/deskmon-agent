package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("expected port %d, got %d", DefaultPort, cfg.Port)
	}
	if cfg.Bind != DefaultBind {
		t.Errorf("expected bind %s, got %s", DefaultBind, cfg.Bind)
	}
	if cfg.AuthToken != "" {
		t.Errorf("expected empty auth token, got %s", cfg.AuthToken)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `port: 9090
auth_token: "testtoken123"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.AuthToken != "testtoken123" {
		t.Errorf("expected auth token testtoken123, got %s", cfg.AuthToken)
	}
	if cfg.Bind != DefaultBind {
		t.Errorf("expected default bind %s, got %s", DefaultBind, cfg.Bind)
	}
}

func TestLoadZeroPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	content := `port: 0
auth_token: "tok"
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != DefaultPort {
		t.Errorf("expected default port %d for zero value, got %d", DefaultPort, cfg.Port)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(path, []byte(":::invalid"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}
