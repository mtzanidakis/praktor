package router

import (
	"context"
	"path/filepath"
	"sync"
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
	t.Cleanup(func() { _ = s.Close() })

	agents := map[string]config.AgentDefinition{
		"general": {Description: "General assistant", Workspace: "general"},
		"coder":   {Description: "Code specialist", Workspace: "coder"},
	}

	reg := registry.New(s, agents, config.DefaultsConfig{}, filepath.Join(dir, "agents"))
	_ = reg.Sync()

	return New(reg, config.RouterConfig{DefaultAgent: "general"})
}

// fakeOrch records route queries and answers with a fixed agent name.
type fakeOrch struct {
	mu     sync.Mutex
	answer string
	calls  []routeCall
}

type routeCall struct {
	agentID, model string
}

func (f *fakeOrch) RouteQuery(_ context.Context, agentID, _, model string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, routeCall{agentID: agentID, model: model})
	return f.answer, nil
}

func TestRoutePassesModelToRouteQuery(t *testing.T) {
	rtr := newTestRouter(t)
	rtr.UpdateConfig(config.RouterConfig{DefaultAgent: "general", Model: "claude-haiku-5-5"})
	orch := &fakeOrch{answer: " coder\n"}
	rtr.SetOrchestrator(orch)

	agentID, msg, err := rtr.Route(context.Background(), "fix the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected smart-routed agent 'coder', got %q", agentID)
	}
	if msg != "fix the bug" {
		t.Errorf("expected message preserved, got %q", msg)
	}
	if len(orch.calls) != 1 {
		t.Fatalf("expected 1 route query, got %d", len(orch.calls))
	}
	if orch.calls[0].agentID != "general" {
		t.Errorf("expected route query to default agent 'general', got %q", orch.calls[0].agentID)
	}
	if orch.calls[0].model != "claude-haiku-5-5" {
		t.Errorf("expected routing model claude-haiku-5-5, got %q", orch.calls[0].model)
	}
}

func TestUpdateConfigChangesRoutingModel(t *testing.T) {
	rtr := newTestRouter(t)
	orch := &fakeOrch{answer: "general"}
	rtr.SetOrchestrator(orch)

	rtr.UpdateConfig(config.RouterConfig{DefaultAgent: "coder", Model: "claude-sonnet-5-5"})
	if rtr.DefaultAgent() != "coder" || rtr.Model() != "claude-sonnet-5-5" {
		t.Fatalf("UpdateConfig not applied: default=%q model=%q", rtr.DefaultAgent(), rtr.Model())
	}

	if _, _, err := rtr.Route(context.Background(), "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orch.calls) != 1 {
		t.Fatalf("expected 1 route query, got %d", len(orch.calls))
	}
	if got := orch.calls[0]; got.agentID != "coder" || got.model != "claude-sonnet-5-5" {
		t.Errorf("expected query to coder with claude-sonnet-5-5, got %+v", got)
	}
}

func TestRouteSkipsRouteQueryForAtPrefix(t *testing.T) {
	rtr := newTestRouter(t)
	orch := &fakeOrch{answer: "general"}
	rtr.SetOrchestrator(orch)

	if _, _, err := rtr.Route(context.Background(), "@coder hi"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orch.calls) != 0 {
		t.Errorf("expected no route query for @prefix, got %d", len(orch.calls))
	}
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

func TestRouteSwarmPrefix(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "@swarm agent1,agent2: do stuff")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "swarm" {
		t.Errorf("expected swarm, got %q", agentID)
	}
	if msg != "agent1,agent2: do stuff" {
		t.Errorf("expected cleaned swarm message, got %q", msg)
	}
}

func TestRouteAtPrefixBeatsDefault(t *testing.T) {
	rtr := newTestRouter(t)

	agentID, msg, err := rtr.Route(context.Background(), "@coder fix this")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if agentID != "coder" {
		t.Errorf("expected @prefix to win, got %q", agentID)
	}
	if msg != "fix this" {
		t.Errorf("expected cleaned message, got %q", msg)
	}
}
