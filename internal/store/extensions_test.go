package store

import (
	"encoding/json"
	"testing"

	"github.com/mtzanidakis/praktor/internal/extensions"
)

func TestExtensionsCRUD(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Empty extensions by default
	data, err := s.GetAgentExtensions("a1")
	if err != nil {
		t.Fatalf("get empty extensions: %v", err)
	}
	var ext extensions.AgentExtensions
	if err := json.Unmarshal([]byte(data), &ext); err != nil {
		t.Fatalf("unmarshal empty extensions: %v", err)
	}
	if !ext.IsEmpty() {
		t.Error("expected empty extensions for new agent")
	}

	// Set extensions with all types
	input := extensions.AgentExtensions{
		MCPServers: map[string]extensions.MCPServerConfig{
			"context7": {
				Type: "http",
				URL:  "https://mcp.context7.com/mcp",
				Headers: map[string]string{
					"Authorization": "Bearer test-key",
				},
			},
			"local-tool": {
				Type:    "stdio",
				Command: "mytool",
				Args:    []string{"--serve"},
				Env:     map[string]string{"DEBUG": "1"},
			},
		},
		Marketplaces: []extensions.MarketplaceConfig{
			{Source: "owner/repo", Name: "my-marketplace"},
			{Source: "https://example.com/marketplace.json"},
		},
		Plugins: []extensions.PluginConfig{
			{Name: "tool@my-marketplace", Requires: []string{"nodejs"}},
			{Name: "disabled-tool@official", Disabled: true},
		},
		Skills: map[string]extensions.SkillConfig{
			"code-review": {
				Description: "Code review skill",
				Content:     "Review code carefully",
				Requires:    []string{"shellcheck"},
				Files:       map[string]string{"scripts/lint.sh": "IyEvYmluL3No"},
			},
		},
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	if err := s.SetAgentExtensions("a1", string(inputJSON)); err != nil {
		t.Fatalf("set extensions: %v", err)
	}

	// Read back
	data, err = s.GetAgentExtensions("a1")
	if err != nil {
		t.Fatalf("get extensions: %v", err)
	}

	var got extensions.AgentExtensions
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("unmarshal extensions: %v", err)
	}

	// MCP Servers
	if len(got.MCPServers) != 2 {
		t.Errorf("expected 2 mcp servers, got %d", len(got.MCPServers))
	}
	if srv, ok := got.MCPServers["context7"]; !ok {
		t.Error("missing mcp server 'context7'")
	} else {
		if srv.Type != "http" {
			t.Errorf("expected type 'http', got '%s'", srv.Type)
		}
		if srv.URL != "https://mcp.context7.com/mcp" {
			t.Errorf("unexpected url: %s", srv.URL)
		}
		if srv.Headers["Authorization"] != "Bearer test-key" {
			t.Errorf("unexpected header: %s", srv.Headers["Authorization"])
		}
	}
	if srv, ok := got.MCPServers["local-tool"]; !ok {
		t.Error("missing mcp server 'local-tool'")
	} else {
		if srv.Command != "mytool" {
			t.Errorf("expected command 'mytool', got '%s'", srv.Command)
		}
		if len(srv.Args) != 1 || srv.Args[0] != "--serve" {
			t.Errorf("unexpected args: %v", srv.Args)
		}
		if srv.Env["DEBUG"] != "1" {
			t.Errorf("unexpected env: %v", srv.Env)
		}
	}

	// Marketplaces (order preserved)
	if len(got.Marketplaces) != 2 {
		t.Errorf("expected 2 marketplaces, got %d", len(got.Marketplaces))
	} else {
		if got.Marketplaces[0].Source != "owner/repo" {
			t.Errorf("unexpected marketplace[0] source: %s", got.Marketplaces[0].Source)
		}
		if got.Marketplaces[0].Name != "my-marketplace" {
			t.Errorf("unexpected marketplace[0] name: %s", got.Marketplaces[0].Name)
		}
		if got.Marketplaces[1].Source != "https://example.com/marketplace.json" {
			t.Errorf("unexpected marketplace[1] source: %s", got.Marketplaces[1].Source)
		}
	}

	// Plugins (order preserved)
	if len(got.Plugins) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(got.Plugins))
	} else {
		if got.Plugins[0].Name != "tool@my-marketplace" {
			t.Errorf("unexpected plugin[0] name: %s", got.Plugins[0].Name)
		}
		if got.Plugins[0].Disabled {
			t.Error("plugin[0] should not be disabled")
		}
		if len(got.Plugins[0].Requires) != 1 || got.Plugins[0].Requires[0] != "nodejs" {
			t.Errorf("unexpected plugin[0] requires: %v", got.Plugins[0].Requires)
		}
		if got.Plugins[1].Name != "disabled-tool@official" {
			t.Errorf("unexpected plugin[1] name: %s", got.Plugins[1].Name)
		}
		if !got.Plugins[1].Disabled {
			t.Error("plugin[1] should be disabled")
		}
	}

	// Skills
	if len(got.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(got.Skills))
	}
	if skill, ok := got.Skills["code-review"]; !ok {
		t.Error("missing skill 'code-review'")
	} else {
		if skill.Description != "Code review skill" {
			t.Errorf("unexpected skill description: %s", skill.Description)
		}
		if skill.Content != "Review code carefully" {
			t.Errorf("unexpected skill content: %s", skill.Content)
		}
		if len(skill.Requires) != 1 || skill.Requires[0] != "shellcheck" {
			t.Errorf("unexpected skill requires: %v", skill.Requires)
		}
		if skill.Files["scripts/lint.sh"] != "IyEvYmluL3No" {
			t.Errorf("unexpected skill file: %v", skill.Files)
		}
	}
}

func TestExtensionsOverwrite(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Set initial extensions
	initial := `{"mcp_servers":{"srv1":{"type":"http","url":"https://example.com"}},"plugins":[{"name":"p1@mp"}]}`
	if err := s.SetAgentExtensions("a1", initial); err != nil {
		t.Fatalf("set initial: %v", err)
	}

	// Overwrite with different extensions
	updated := `{"mcp_servers":{"srv2":{"type":"stdio","command":"tool"}},"skills":{"sk1":{"description":"d","content":"c"}}}`
	if err := s.SetAgentExtensions("a1", updated); err != nil {
		t.Fatalf("set updated: %v", err)
	}

	data, err := s.GetAgentExtensions("a1")
	if err != nil {
		t.Fatalf("get extensions: %v", err)
	}

	var got extensions.AgentExtensions
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Old data should be gone
	if _, ok := got.MCPServers["srv1"]; ok {
		t.Error("srv1 should have been removed")
	}
	if len(got.Plugins) != 0 {
		t.Errorf("expected 0 plugins, got %d", len(got.Plugins))
	}

	// New data should be present
	if _, ok := got.MCPServers["srv2"]; !ok {
		t.Error("srv2 should be present")
	}
	if _, ok := got.Skills["sk1"]; !ok {
		t.Error("sk1 should be present")
	}
}

func TestExtensionsClearAll(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Set some extensions
	ext := `{"mcp_servers":{"s":{"type":"http","url":"u"}},"plugins":[{"name":"p@m"}]}`
	_ = s.SetAgentExtensions("a1", ext)

	// Clear all
	if err := s.SetAgentExtensions("a1", "{}"); err != nil {
		t.Fatalf("clear extensions: %v", err)
	}

	data, err := s.GetAgentExtensions("a1")
	if err != nil {
		t.Fatalf("get extensions: %v", err)
	}

	var got extensions.AgentExtensions
	_ = json.Unmarshal([]byte(data), &got)
	if !got.IsEmpty() {
		t.Error("expected empty extensions after clear")
	}
}

func TestExtensionsCascadeDelete(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	ext := `{"mcp_servers":{"s":{"type":"http","url":"u"}},"skills":{"sk":{"description":"d","content":"c"}}}`
	_ = s.SetAgentExtensions("a1", ext)

	// Delete agent — cascading should clean up extension rows
	if err := s.DeleteAgent("a1"); err != nil {
		t.Fatalf("delete agent: %v", err)
	}

	// Re-create agent, extensions should be empty
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})
	data, err := s.GetAgentExtensions("a1")
	if err != nil {
		t.Fatalf("get extensions: %v", err)
	}

	var got extensions.AgentExtensions
	_ = json.Unmarshal([]byte(data), &got)
	if !got.IsEmpty() {
		t.Errorf("expected empty extensions after cascade delete, got: %s", data)
	}
}

func TestExtensionStatus(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Default empty
	status, err := s.GetExtensionStatus("a1")
	if err != nil {
		t.Fatalf("get status: %v", err)
	}
	if status != "{}" {
		t.Errorf("expected '{}', got '%s'", status)
	}

	// Set status
	statusJSON := `{"marketplaces":["official"],"plugins":[{"name":"p","enabled":true}]}`
	if err := s.SetExtensionStatus("a1", statusJSON); err != nil {
		t.Fatalf("set status: %v", err)
	}

	got, err := s.GetExtensionStatus("a1")
	if err != nil {
		t.Fatalf("get status after set: %v", err)
	}
	if got != statusJSON {
		t.Errorf("expected '%s', got '%s'", statusJSON, got)
	}

	// Not found
	if err := s.SetExtensionStatus("nonexistent", "{}"); err == nil {
		t.Error("expected error for nonexistent agent")
	}
}

func TestExtensionsMigrateFromBlob(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Simulate legacy data: write directly to the blob column
	blob := `{"mcp_servers":{"ctx":{"type":"http","url":"https://example.com"}},"marketplaces":[{"source":"owner/repo"}],"plugins":[{"name":"p@mp","disabled":true,"requires":["node"]}],"skills":{"review":{"description":"review","content":"do review","files":{"run.sh":"IyE="}}}}`
	_, err := s.db.Exec(`UPDATE agents SET extensions = ? WHERE id = ?`, blob, "a1")
	if err != nil {
		t.Fatalf("write blob: %v", err)
	}

	// Clear the normalized tables to simulate pre-migration state
	for _, table := range []string{"agent_mcp_servers", "agent_marketplaces", "agent_plugins", "agent_skills"} {
		s.db.Exec("DELETE FROM " + table + " WHERE agent_id = 'a1'")
	}

	// Run migration
	if err := s.migrateExtensionsToTables(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Read back via normalized path
	data, err := s.GetAgentExtensions("a1")
	if err != nil {
		t.Fatalf("get extensions: %v", err)
	}

	var got extensions.AgentExtensions
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.MCPServers) != 1 {
		t.Errorf("expected 1 mcp server, got %d", len(got.MCPServers))
	}
	if srv, ok := got.MCPServers["ctx"]; !ok || srv.URL != "https://example.com" {
		t.Errorf("unexpected mcp server: %+v", got.MCPServers)
	}
	if len(got.Marketplaces) != 1 || got.Marketplaces[0].Source != "owner/repo" {
		t.Errorf("unexpected marketplaces: %+v", got.Marketplaces)
	}
	if len(got.Plugins) != 1 || got.Plugins[0].Name != "p@mp" || !got.Plugins[0].Disabled {
		t.Errorf("unexpected plugins: %+v", got.Plugins)
	}
	if len(got.Plugins[0].Requires) != 1 || got.Plugins[0].Requires[0] != "node" {
		t.Errorf("unexpected plugin requires: %v", got.Plugins[0].Requires)
	}
	if len(got.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(got.Skills))
	}
	if sk, ok := got.Skills["review"]; !ok || sk.Content != "do review" || sk.Files["run.sh"] != "IyE=" {
		t.Errorf("unexpected skill: %+v", got.Skills)
	}

	// Migration is idempotent — running again should not error
	if err := s.migrateExtensionsToTables(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestExtensionsNonexistentAgent(t *testing.T) {
	s := newTestStore(t)

	// GetAgentExtensions for nonexistent agent returns empty
	data, err := s.GetAgentExtensions("nonexistent")
	if err != nil {
		t.Fatalf("get extensions: %v", err)
	}
	var ext extensions.AgentExtensions
	_ = json.Unmarshal([]byte(data), &ext)
	if !ext.IsEmpty() {
		t.Error("expected empty extensions for nonexistent agent")
	}
}
