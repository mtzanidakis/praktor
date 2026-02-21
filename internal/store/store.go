package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

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

	return nil
}
