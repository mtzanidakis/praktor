package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()

	if cfg.Defaults.Image != "praktor-agent:latest" {
		t.Errorf("expected default image praktor-agent:latest, got %s", cfg.Defaults.Image)
	}
	if cfg.Defaults.MaxRunning != 5 {
		t.Errorf("expected max_running 5, got %d", cfg.Defaults.MaxRunning)
	}
	if cfg.Defaults.IdleTimeout != 10*time.Minute {
		t.Errorf("expected idle_timeout 10m, got %v", cfg.Defaults.IdleTimeout)
	}
	if cfg.Defaults.BasePath != "data/agents" {
		t.Errorf("expected base_path data/agents, got %s", cfg.Defaults.BasePath)
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
	if cfg.Defaults.AnthropicAPIKey != "sk-test-key" {
		t.Errorf("expected anthropic key sk-test-key, got %s", cfg.Defaults.AnthropicAPIKey)
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
defaults:
  image: "custom-agent:v1"
  max_running: 10
  base_path: "/custom/agents"
web:
  port: 3000
  enabled: false
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
	if cfg.Defaults.Image != "custom-agent:v1" {
		t.Errorf("expected custom-agent:v1, got %s", cfg.Defaults.Image)
	}
	if cfg.Defaults.MaxRunning != 10 {
		t.Errorf("expected max_running 10, got %d", cfg.Defaults.MaxRunning)
	}
	if cfg.Defaults.BasePath != "/custom/agents" {
		t.Errorf("expected base_path /custom/agents, got %s", cfg.Defaults.BasePath)
	}
	if cfg.Web.Port != 3000 {
		t.Errorf("expected web port 3000, got %d", cfg.Web.Port)
	}
	if cfg.Web.Enabled {
		t.Error("expected web disabled")
	}
}

func TestLoadAgentsFromYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
agents:
  general:
    description: "General assistant"
    workspace: general
  coder:
    description: "Code specialist"
    model: "claude-opus-4-6"
    env:
      GITHUB_TOKEN: "token123"
    allowed_tools: [WebSearch, WebFetch]
router:
  default_agent: general
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PRAKTOR_CONFIG", cfgPath)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}

	general := cfg.Agents["general"]
	if general.Description != "General assistant" {
		t.Errorf("expected description 'General assistant', got %q", general.Description)
	}
	if general.Workspace != "general" {
		t.Errorf("expected workspace 'general', got %q", general.Workspace)
	}

	coder := cfg.Agents["coder"]
	if coder.Model != "claude-opus-4-6" {
		t.Errorf("expected model 'claude-opus-4-6', got %q", coder.Model)
	}
	if coder.Env["GITHUB_TOKEN"] != "token123" {
		t.Errorf("expected GITHUB_TOKEN env, got %v", coder.Env)
	}
	if len(coder.AllowedTools) != 2 {
		t.Errorf("expected 2 allowed tools, got %d", len(coder.AllowedTools))
	}

	// Workspace defaults to agent name
	if coder.Workspace != "coder" {
		t.Errorf("expected workspace default to 'coder', got %q", coder.Workspace)
	}

	if cfg.Router.DefaultAgent != "general" {
		t.Errorf("expected default_agent 'general', got %q", cfg.Router.DefaultAgent)
	}
}

func TestValidation_DefaultAgentRequired(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
agents:
  general:
    description: "General assistant"
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PRAKTOR_CONFIG", cfgPath)

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for missing default_agent")
	}
}

func TestValidation_DefaultAgentMustExist(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	yaml := `
agents:
  general:
    description: "General assistant"
router:
  default_agent: nonexistent
`
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PRAKTOR_CONFIG", cfgPath)

	_, err := Load()
	if err == nil {
		t.Fatal("expected validation error for nonexistent default_agent")
	}
}
