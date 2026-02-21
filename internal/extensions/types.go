package extensions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// AgentExtensions holds the full extensions configuration for an agent.
type AgentExtensions struct {
	MCPServers   map[string]MCPServerConfig `json:"mcp_servers,omitempty"`
	Marketplaces []MarketplaceConfig        `json:"marketplaces,omitempty"`
	Plugins      []PluginConfig             `json:"plugins,omitempty"`
	Skills       map[string]SkillConfig     `json:"skills,omitempty"`
	Settings     map[string]any             `json:"settings,omitempty"`
}

// MCPServerConfig defines an MCP server (stdio, http, or sse).
type MCPServerConfig struct {
	Type    string            `json:"type"`              // "stdio", "http"
	Command string            `json:"command,omitempty"` // for stdio
	Args    []string          `json:"args,omitempty"`    // for stdio
	URL     string            `json:"url,omitempty"`     // for http/sse
	Env     map[string]string `json:"env,omitempty"`     // for stdio
	Headers map[string]string `json:"headers,omitempty"` // for http/sse
}

// MarketplaceConfig defines a Claude Code plugin marketplace source.
type MarketplaceConfig struct {
	Source string `json:"source"`         // "owner/repo", git URL, or URL to marketplace.json
	Name   string `json:"name,omitempty"` // optional display name override
}

// PluginConfig defines a Claude Code plugin to install.
type PluginConfig struct {
	Name     string   `json:"name"`               // "plugin-name@marketplace"
	Disabled bool     `json:"disabled,omitempty"`  // disable without uninstalling
	Requires []string `json:"requires,omitempty"`  // nix packages needed
}

// SkillConfig defines a Claude Code skill (SKILL.md).
type SkillConfig struct {
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Requires    []string `json:"requires,omitempty"` // nix packages needed
}

// IsEmpty returns true if no extensions are configured.
func (e *AgentExtensions) IsEmpty() bool {
	return len(e.MCPServers) == 0 && len(e.Marketplaces) == 0 && len(e.Plugins) == 0 && len(e.Skills) == 0 && len(e.Settings) == 0
}

var (
	validMCPTypes        = map[string]bool{"stdio": true, "http": true}
	skillNameRegexp      = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	marketplaceNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)
)

// Validate checks the extensions for correctness.
func (e *AgentExtensions) Validate() error {
	for name, srv := range e.MCPServers {
		if !validMCPTypes[srv.Type] {
			return fmt.Errorf("mcp server %q: invalid type %q (must be stdio or http)", name, srv.Type)
		}
		if srv.Type == "stdio" && srv.Command == "" {
			return fmt.Errorf("mcp server %q: stdio type requires command", name)
		}
		if srv.Type == "http" && srv.URL == "" {
			return fmt.Errorf("mcp server %q: http type requires url", name)
		}
	}

	for i, m := range e.Marketplaces {
		if m.Source == "" {
			return fmt.Errorf("marketplace[%d]: source is required", i)
		}
		if m.Name != "" && !marketplaceNameRegex.MatchString(m.Name) {
			return fmt.Errorf("marketplace %q: name must be alphanumeric with hyphens", m.Name)
		}
	}

	for i, p := range e.Plugins {
		if p.Name == "" {
			return fmt.Errorf("plugin[%d]: name is required", i)
		}
		if !strings.Contains(p.Name, "@") {
			return fmt.Errorf("plugin %q: must be in name@marketplace format", p.Name)
		}
	}

	for name := range e.Skills {
		if !skillNameRegexp.MatchString(name) {
			return fmt.Errorf("skill %q: name must be alphanumeric with hyphens/underscores", name)
		}
	}

	return nil
}

// Parse parses a JSON string into AgentExtensions.
func Parse(data string) (*AgentExtensions, error) {
	if data == "" || data == "{}" {
		return &AgentExtensions{}, nil
	}
	var ext AgentExtensions
	if err := json.Unmarshal([]byte(data), &ext); err != nil {
		return nil, fmt.Errorf("parse extensions: %w", err)
	}
	return &ext, nil
}

// ResolveSecretRefs resolves secret:name references in MCP server env and
// headers values using the provided resolver function.
func (e *AgentExtensions) ResolveSecretRefs(resolve func(name string) (string, error)) error {
	for srvName, srv := range e.MCPServers {
		for k, v := range srv.Env {
			if secretName, ok := strings.CutPrefix(v, "secret:"); ok {
				val, err := resolve(secretName)
				if err != nil {
					return fmt.Errorf("mcp server %q env %q: %w", srvName, k, err)
				}
				srv.Env[k] = val
			}
		}
		for k, v := range srv.Headers {
			if secretName, ok := strings.CutPrefix(v, "secret:"); ok {
				val, err := resolve(secretName)
				if err != nil {
					return fmt.Errorf("mcp server %q header %q: %w", srvName, k, err)
				}
				srv.Headers[k] = val
			}
		}
		e.MCPServers[srvName] = srv
	}
	return nil
}
