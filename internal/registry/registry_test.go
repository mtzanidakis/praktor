package registry

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/store"
)

func newTestRegistry(t *testing.T) (*Registry, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	basePath := filepath.Join(dir, "agents")

	agents := map[string]config.AgentDefinition{
		"general": {
			Description: "General assistant",
			Workspace:   "general",
		},
		"coder": {
			Description: "Code specialist",
			Model:       "claude-opus-4-6",
			Workspace:   "coder",
		},
	}

	cfg := config.DefaultsConfig{
		Image: "praktor-agent:latest",
		Model: "claude-sonnet-4-5-20250929",
	}

	reg := New(s, agents, cfg, basePath)
	return reg, s
}

func TestSync(t *testing.T) {
	reg, s := newTestRegistry(t)

	if err := reg.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	agents, err := s.ListAgents()
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Verify details
	a, err := reg.Get("general")
	if err != nil {
		t.Fatalf("get general: %v", err)
	}
	if a.Description != "General assistant" {
		t.Errorf("expected description 'General assistant', got %q", a.Description)
	}
}

func TestSyncDeletesStale(t *testing.T) {
	reg, s := newTestRegistry(t)

	// Pre-seed a stale agent
	_ = s.SaveAgent(&store.Agent{ID: "stale", Name: "Stale", Workspace: "stale"})

	if err := reg.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	stale, err := s.GetAgent("stale")
	if err != nil {
		t.Fatalf("get stale: %v", err)
	}
	if stale != nil {
		t.Error("expected stale agent to be deleted")
	}
}

func TestResolveModel(t *testing.T) {
	reg, _ := newTestRegistry(t)

	// Coder has explicit model
	if m := reg.ResolveModel("coder"); m != "claude-opus-4-6" {
		t.Errorf("expected coder model 'claude-opus-4-6', got %q", m)
	}

	// General falls back to global default
	if m := reg.ResolveModel("general"); m != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected general model 'claude-sonnet-4-5-20250929', got %q", m)
	}
}

func TestResolveImage(t *testing.T) {
	reg, _ := newTestRegistry(t)

	// Both fall back to global default
	if img := reg.ResolveImage("general"); img != "praktor-agent:latest" {
		t.Errorf("expected image 'praktor-agent:latest', got %q", img)
	}
}

func TestAgentDescriptions(t *testing.T) {
	reg, _ := newTestRegistry(t)

	descs := reg.AgentDescriptions()
	if len(descs) != 2 {
		t.Fatalf("expected 2 descriptions, got %d", len(descs))
	}
	if descs["general"] != "General assistant" {
		t.Errorf("unexpected description for general: %q", descs["general"])
	}
}

func TestUserMDTemplate(t *testing.T) {
	reg, _ := newTestRegistry(t)

	if err := reg.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	content, err := reg.GetUserMD()
	if err != nil {
		t.Fatalf("get user md: %v", err)
	}
	if content == "" {
		t.Fatal("expected USER.md to be created with template")
	}
	if !strings.Contains(content, "# User Profile") {
		t.Error("expected template to contain '# User Profile'")
	}
}

func TestUserMDReadWrite(t *testing.T) {
	reg, _ := newTestRegistry(t)

	if err := reg.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	custom := "# User Profile\n\n## Name\nAlice\n"
	if err := reg.SaveUserMD(custom); err != nil {
		t.Fatalf("save user md: %v", err)
	}

	content, err := reg.GetUserMD()
	if err != nil {
		t.Fatalf("get user md: %v", err)
	}
	if content != custom {
		t.Errorf("expected %q, got %q", custom, content)
	}
}

func TestUserMDNotExist(t *testing.T) {
	reg, _ := newTestRegistry(t)

	// Before sync, USER.md doesn't exist
	content, err := reg.GetUserMD()
	if err != nil {
		t.Fatalf("get user md: %v", err)
	}
	if content != "" {
		t.Errorf("expected empty content before sync, got %q", content)
	}
}
