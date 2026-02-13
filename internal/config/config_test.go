package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()

	if cfg.Agent.Image != "praktor-agent:latest" {
		t.Errorf("expected default image praktor-agent:latest, got %s", cfg.Agent.Image)
	}
	if cfg.Agent.MaxContainers != 5 {
		t.Errorf("expected max_containers 5, got %d", cfg.Agent.MaxContainers)
	}
	if cfg.Agent.IdleTimeout != 30*time.Minute {
		t.Errorf("expected idle_timeout 30m, got %v", cfg.Agent.IdleTimeout)
	}
	if cfg.NATS.Port != 4222 {
		t.Errorf("expected nats port 4222, got %d", cfg.NATS.Port)
	}
	if cfg.Web.Port != 8080 {
		t.Errorf("expected web port 8080, got %d", cfg.Web.Port)
	}
	if !cfg.Web.Enabled {
		t.Error("expected web enabled by default")
	}
	if cfg.Store.Path != "data/praktor.db" {
		t.Errorf("expected store path data/praktor.db, got %s", cfg.Store.Path)
	}
}

func TestLoadWithEnvOverrides(t *testing.T) {
	// Point config to a non-existent file so we use defaults
	t.Setenv("PRAKTOR_CONFIG", "/nonexistent/config.yaml")
	t.Setenv("PRAKTOR_TELEGRAM_TOKEN", "test-token-123")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	t.Setenv("PRAKTOR_WEB_PASSWORD", "secret")
	t.Setenv("PRAKTOR_WEB_PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Telegram.Token != "test-token-123" {
		t.Errorf("expected telegram token test-token-123, got %s", cfg.Telegram.Token)
	}
	if cfg.Agent.AnthropicAPIKey != "sk-test-key" {
		t.Errorf("expected anthropic key sk-test-key, got %s", cfg.Agent.AnthropicAPIKey)
	}
	if cfg.Web.Auth != "secret" {
		t.Errorf("expected web auth secret, got %s", cfg.Web.Auth)
	}
	if cfg.Web.Port != 9090 {
		t.Errorf("expected web port 9090, got %d", cfg.Web.Port)
	}
}

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
telegram:
  token: "yaml-token"
  allow_from: [123, 456]
agent:
  image: "custom-agent:v1"
  max_containers: 10
web:
  port: 3000
  enabled: false
groups:
  base_path: "/custom/groups"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PRAKTOR_CONFIG", cfgPath)
	// Clear any env overrides
	t.Setenv("PRAKTOR_TELEGRAM_TOKEN", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Telegram.Token != "yaml-token" {
		t.Errorf("expected yaml-token, got %s", cfg.Telegram.Token)
	}
	if len(cfg.Telegram.AllowFrom) != 2 {
		t.Errorf("expected 2 allow_from entries, got %d", len(cfg.Telegram.AllowFrom))
	}
	if cfg.Agent.Image != "custom-agent:v1" {
		t.Errorf("expected custom-agent:v1, got %s", cfg.Agent.Image)
	}
	if cfg.Agent.MaxContainers != 10 {
		t.Errorf("expected max_containers 10, got %d", cfg.Agent.MaxContainers)
	}
	if cfg.Web.Port != 3000 {
		t.Errorf("expected web port 3000, got %d", cfg.Web.Port)
	}
	if cfg.Web.Enabled {
		t.Error("expected web disabled")
	}
}
