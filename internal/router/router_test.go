package router

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/store"
)

func newTestRouter(t *testing.T) *Router {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	agents := map[string]config.AgentDefinition{
		"general": {Description: "General assistant", Workspace: "general"},
		"coder":   {Description: "Code specialist", Workspace: "coder"},
	}

	reg := registry.New(s, agents, config.DefaultsConfig{}, filepath.Join(dir, "agents"))
	_ = reg.Sync()

	return New(reg, config.RouterConfig{DefaultAgent: "general"})
}

func TestRouteWithAtPrefix(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "@coder fix the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected agent 'coder', got %q", agentID)
	}
	if msg != "fix the bug" {
		t.Errorf("expected cleaned message 'fix the bug', got %q", msg)
	}
}

func TestRouteWithAtPrefixNoMessage(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "@coder")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected agent 'coder', got %q", agentID)
	}
	if msg != "" {
		t.Errorf("expected empty cleaned message, got %q", msg)
	}
}

func TestRouteWithUnknownAtPrefix(t *testing.T) {
	rtr := newTestRouter(t)

	// Unknown agent name falls back to default
	agentID, msg, err := rtr.Route(context.Background(), "@unknown hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "general" {
		t.Errorf("expected fallback to 'general', got %q", agentID)
	}
	if msg != "@unknown hello" {
		t.Errorf("expected original message preserved, got %q", msg)
	}
}

func TestRouteFallbackToDefault(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "general" {
		t.Errorf("expected default agent 'general', got %q", agentID)
	}
	if msg != "hello world" {
		t.Errorf("expected message 'hello world', got %q", msg)
	}
}
