package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mtzanidakis/praktor/internal/extensions"
)

// GetAgentExtensions queries the normalized extension tables and returns a
// JSON string matching the AgentExtensions structure.
func (s *Store) GetAgentExtensions(agentID string) (string, error) {
	var ext extensions.AgentExtensions

	// MCP Servers
	mcpRows, err := s.db.Query(`SELECT name, config FROM agent_mcp_servers WHERE agent_id = ?`, agentID)
	if err != nil {
		return "", fmt.Errorf("query mcp servers: %w", err)
	}
	defer mcpRows.Close()

	for mcpRows.Next() {
		var name, cfgJSON string
		if err := mcpRows.Scan(&name, &cfgJSON); err != nil {
			return "", fmt.Errorf("scan mcp server: %w", err)
		}
		var cfg extensions.MCPServerConfig
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
			return "", fmt.Errorf("unmarshal mcp server %q: %w", name, err)
		}
		if ext.MCPServers == nil {
			ext.MCPServers = make(map[string]extensions.MCPServerConfig)
		}
		ext.MCPServers[name] = cfg
	}
	if err := mcpRows.Err(); err != nil {
		return "", fmt.Errorf("mcp server rows: %w", err)
	}

	// Marketplaces
	mpRows, err := s.db.Query(`SELECT source, name FROM agent_marketplaces WHERE agent_id = ? ORDER BY sort_order`, agentID)
	if err != nil {
		return "", fmt.Errorf("query marketplaces: %w", err)
	}
	defer mpRows.Close()

	for mpRows.Next() {
		var m extensions.MarketplaceConfig
		if err := mpRows.Scan(&m.Source, &m.Name); err != nil {
			return "", fmt.Errorf("scan marketplace: %w", err)
		}
		ext.Marketplaces = append(ext.Marketplaces, m)
	}
	if err := mpRows.Err(); err != nil {
		return "", fmt.Errorf("marketplace rows: %w", err)
	}

	// Plugins
	plRows, err := s.db.Query(`SELECT name, disabled, requires FROM agent_plugins WHERE agent_id = ? ORDER BY sort_order`, agentID)
	if err != nil {
		return "", fmt.Errorf("query plugins: %w", err)
	}
	defer plRows.Close()

	for plRows.Next() {
		var p extensions.PluginConfig
		var disabled int
		var reqJSON string
		if err := plRows.Scan(&p.Name, &disabled, &reqJSON); err != nil {
			return "", fmt.Errorf("scan plugin: %w", err)
		}
		p.Disabled = disabled != 0
		if reqJSON != "" && reqJSON != "[]" && reqJSON != "null" {
			_ = json.Unmarshal([]byte(reqJSON), &p.Requires)
		}
		ext.Plugins = append(ext.Plugins, p)
	}
	if err := plRows.Err(); err != nil {
		return "", fmt.Errorf("plugin rows: %w", err)
	}

	// Skills
	skRows, err := s.db.Query(`SELECT name, description, content, requires, files FROM agent_skills WHERE agent_id = ?`, agentID)
	if err != nil {
		return "", fmt.Errorf("query skills: %w", err)
	}
	defer skRows.Close()

	for skRows.Next() {
		var name, desc, content, reqJSON, filesJSON string
		if err := skRows.Scan(&name, &desc, &content, &reqJSON, &filesJSON); err != nil {
			return "", fmt.Errorf("scan skill: %w", err)
		}
		skill := extensions.SkillConfig{
			Description: desc,
			Content:     content,
		}
		if reqJSON != "" && reqJSON != "[]" && reqJSON != "null" {
			_ = json.Unmarshal([]byte(reqJSON), &skill.Requires)
		}
		if filesJSON != "" && filesJSON != "{}" && filesJSON != "null" {
			_ = json.Unmarshal([]byte(filesJSON), &skill.Files)
		}
		if ext.Skills == nil {
			ext.Skills = make(map[string]extensions.SkillConfig)
		}
		ext.Skills[name] = skill
	}
	if err := skRows.Err(); err != nil {
		return "", fmt.Errorf("skill rows: %w", err)
	}

	data, err := json.Marshal(ext)
	if err != nil {
		return "", fmt.Errorf("marshal extensions: %w", err)
	}
	return string(data), nil
}

// SetAgentExtensions parses a JSON string and writes the extension data into
// the normalized tables. All existing rows for the agent are replaced within
// a transaction.
func (s *Store) SetAgentExtensions(agentID, extensionsJSON string) error {
	ext, err := extensions.Parse(extensionsJSON)
	if err != nil {
		return fmt.Errorf("parse extensions: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Delete existing rows
	for _, table := range []string{"agent_mcp_servers", "agent_marketplaces", "agent_plugins", "agent_skills"} {
		if _, err := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE agent_id = ?`, table), agentID); err != nil {
			return fmt.Errorf("delete from %s: %w", table, err)
		}
	}

	// Insert MCP servers
	for name, srv := range ext.MCPServers {
		cfgJSON, err := json.Marshal(srv)
		if err != nil {
			return fmt.Errorf("marshal mcp server %q: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO agent_mcp_servers (agent_id, name, config) VALUES (?, ?, ?)`,
			agentID, name, string(cfgJSON)); err != nil {
			return fmt.Errorf("insert mcp server %q: %w", name, err)
		}
	}

	// Insert marketplaces
	for i, m := range ext.Marketplaces {
		if _, err := tx.Exec(`INSERT INTO agent_marketplaces (agent_id, source, name, sort_order) VALUES (?, ?, ?, ?)`,
			agentID, m.Source, m.Name, i); err != nil {
			return fmt.Errorf("insert marketplace %q: %w", m.Source, err)
		}
	}

	// Insert plugins
	for i, p := range ext.Plugins {
		reqJSON, _ := json.Marshal(p.Requires)
		disabled := 0
		if p.Disabled {
			disabled = 1
		}
		if _, err := tx.Exec(`INSERT INTO agent_plugins (agent_id, name, disabled, requires, sort_order) VALUES (?, ?, ?, ?, ?)`,
			agentID, p.Name, disabled, string(reqJSON), i); err != nil {
			return fmt.Errorf("insert plugin %q: %w", p.Name, err)
		}
	}

	// Insert skills
	for name, skill := range ext.Skills {
		reqJSON, _ := json.Marshal(skill.Requires)
		filesJSON, _ := json.Marshal(skill.Files)
		if _, err := tx.Exec(`INSERT INTO agent_skills (agent_id, name, description, content, requires, files) VALUES (?, ?, ?, ?, ?, ?)`,
			agentID, name, skill.Description, skill.Content, string(reqJSON), string(filesJSON)); err != nil {
			return fmt.Errorf("insert skill %q: %w", name, err)
		}
	}

	// Touch updated_at on agent
	if _, err := tx.Exec(`UPDATE agents SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, agentID); err != nil {
		return fmt.Errorf("update agent timestamp: %w", err)
	}

	return tx.Commit()
}

// GetExtensionStatus returns the raw JSON extension status string for an agent.
func (s *Store) GetExtensionStatus(agentID string) (string, error) {
	var status sql.NullString
	err := s.db.QueryRow(`SELECT extension_status FROM agents WHERE id = ?`, agentID).Scan(&status)
	if err == sql.ErrNoRows {
		return "{}", nil
	}
	if err != nil {
		return "", fmt.Errorf("get extension status: %w", err)
	}
	if !status.Valid || status.String == "" {
		return "{}", nil
	}
	return status.String, nil
}

// SetExtensionStatus updates the extension status JSON for an agent.
func (s *Store) SetExtensionStatus(agentID, statusJSON string) error {
	result, err := s.db.Exec(
		`UPDATE agents SET extension_status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		statusJSON, agentID)
	if err != nil {
		return fmt.Errorf("set extension status: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agentID)
	}
	return nil
}
