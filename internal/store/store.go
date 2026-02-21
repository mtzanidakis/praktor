package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mtzanidakis/praktor/internal/extensions"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func New(path string) (*Store, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	// Enable WAL mode for concurrent read/write access and set a busy
	// timeout so writers retry instead of immediately returning SQLITE_BUSY.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("exec %s: %w", p, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS agents (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL,
			description TEXT,
			model       TEXT,
			image       TEXT,
			workspace   TEXT NOT NULL UNIQUE,
			claude_md   TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_id    TEXT NOT NULL REFERENCES agents(id),
			sender      TEXT NOT NULL,
			content     TEXT NOT NULL,
			metadata    TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_agent ON messages(agent_id, created_at)`,
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id           TEXT PRIMARY KEY,
			agent_id     TEXT NOT NULL REFERENCES agents(id),
			name         TEXT NOT NULL,
			schedule     TEXT NOT NULL,
			prompt       TEXT NOT NULL,
			context_mode TEXT DEFAULT 'isolated',
			status       TEXT DEFAULT 'active',
			next_run_at  DATETIME,
			last_run_at  DATETIME,
			last_status  TEXT,
			last_error   TEXT,
			created_at   DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_next_run ON scheduled_tasks(status, next_run_at)`,
		`CREATE TABLE IF NOT EXISTS agent_sessions (
			id           TEXT PRIMARY KEY,
			agent_id     TEXT NOT NULL REFERENCES agents(id),
			container_id TEXT,
			status       TEXT DEFAULT 'active',
			started_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_active  DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS swarm_runs (
			id           TEXT PRIMARY KEY,
			agent_id     TEXT NOT NULL REFERENCES agents(id),
			task         TEXT NOT NULL,
			status       TEXT DEFAULT 'running',
			agents       TEXT NOT NULL,
			results      TEXT,
			started_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
			completed_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS secrets (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL UNIQUE,
			description TEXT,
			kind        TEXT NOT NULL,
			filename    TEXT,
			value       BLOB NOT NULL,
			nonce       BLOB NOT NULL,
			global      INTEGER DEFAULT 0,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS agent_secrets (
			agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			secret_id  TEXT NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
			PRIMARY KEY (agent_id, secret_id)
		)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}

	// Add columns (ignore errors if column already exists)
	for _, stmt := range []string{
		`ALTER TABLE swarm_runs ADD COLUMN name TEXT DEFAULT ''`,
		`ALTER TABLE swarm_runs ADD COLUMN synapses TEXT DEFAULT '[]'`,
		`ALTER TABLE swarm_runs ADD COLUMN lead_agent TEXT DEFAULT ''`,
		`ALTER TABLE agents ADD COLUMN extensions TEXT DEFAULT '{}'`,
		`ALTER TABLE agents ADD COLUMN extension_status TEXT DEFAULT '{}'`,
	} {
		_, _ = s.db.Exec(stmt)
	}

	// Normalized extension tables
	extTables := []string{
		`CREATE TABLE IF NOT EXISTS agent_mcp_servers (
			agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			name     TEXT NOT NULL,
			config   TEXT NOT NULL,
			PRIMARY KEY (agent_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS agent_marketplaces (
			agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			source     TEXT NOT NULL,
			name       TEXT DEFAULT '',
			sort_order INTEGER DEFAULT 0,
			PRIMARY KEY (agent_id, source)
		)`,
		`CREATE TABLE IF NOT EXISTS agent_plugins (
			agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			name       TEXT NOT NULL,
			disabled   INTEGER DEFAULT 0,
			requires   TEXT DEFAULT '[]',
			sort_order INTEGER DEFAULT 0,
			PRIMARY KEY (agent_id, name)
		)`,
		`CREATE TABLE IF NOT EXISTS agent_skills (
			agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
			name        TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			content     TEXT NOT NULL DEFAULT '',
			requires    TEXT DEFAULT '[]',
			files       TEXT DEFAULT '{}',
			PRIMARY KEY (agent_id, name)
		)`,
	}
	for _, stmt := range extTables {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec extension table migration: %w", err)
		}
	}

	// One-time data migration from JSON blob to normalized tables
	if err := s.migrateExtensionsToTables(); err != nil {
		return fmt.Errorf("migrate extensions to tables: %w", err)
	}

	return nil
}

// migrateExtensionsToTables migrates extension data from the agents.extensions
// JSON blob column into the normalized extension tables. It is idempotent —
// uses INSERT OR IGNORE so it can safely run on every startup.
func (s *Store) migrateExtensionsToTables() error {
	rows, err := s.db.Query(`SELECT id, extensions FROM agents WHERE extensions IS NOT NULL AND extensions != '' AND extensions != '{}'`)
	if err != nil {
		return fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var agentID, extJSON string
		if err := rows.Scan(&agentID, &extJSON); err != nil {
			return fmt.Errorf("scan agent: %w", err)
		}

		ext, err := extensions.Parse(extJSON)
		if err != nil {
			slog.Warn("skipping malformed extensions during migration", "agent", agentID, "error", err)
			continue
		}

		if ext.IsEmpty() {
			continue
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		for name, srv := range ext.MCPServers {
			cfgJSON, err := json.Marshal(srv)
			if err != nil {
				continue
			}
			_, _ = tx.Exec(`INSERT OR IGNORE INTO agent_mcp_servers (agent_id, name, config) VALUES (?, ?, ?)`,
				agentID, name, string(cfgJSON))
		}

		for i, m := range ext.Marketplaces {
			_, _ = tx.Exec(`INSERT OR IGNORE INTO agent_marketplaces (agent_id, source, name, sort_order) VALUES (?, ?, ?, ?)`,
				agentID, m.Source, m.Name, i)
		}

		for i, p := range ext.Plugins {
			reqJSON, _ := json.Marshal(p.Requires)
			disabled := 0
			if p.Disabled {
				disabled = 1
			}
			_, _ = tx.Exec(`INSERT OR IGNORE INTO agent_plugins (agent_id, name, disabled, requires, sort_order) VALUES (?, ?, ?, ?, ?)`,
				agentID, p.Name, disabled, string(reqJSON), i)
		}

		for name, skill := range ext.Skills {
			reqJSON, _ := json.Marshal(skill.Requires)
			filesJSON, _ := json.Marshal(skill.Files)
			_, _ = tx.Exec(`INSERT OR IGNORE INTO agent_skills (agent_id, name, description, content, requires, files) VALUES (?, ?, ?, ?, ?, ?)`,
				agentID, name, skill.Description, skill.Content, string(reqJSON), string(filesJSON))
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit tx for agent %s: %w", agentID, err)
		}

		slog.Info("migrated extensions to tables", "agent", agentID)
	}

	return rows.Err()
}
