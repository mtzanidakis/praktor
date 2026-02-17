package store

import (
	"database/sql"
	"fmt"
	"time"
)

type Secret struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Kind        string    `json:"kind"`
	Filename    string    `json:"filename,omitempty"`
	Value       []byte    `json:"-"`
	Nonce       []byte    `json:"-"`
	Global      bool      `json:"global"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Store) SaveSecret(sec *Secret) error {
	_, err := s.db.Exec(`
		INSERT INTO secrets (id, name, description, kind, filename, value, nonce, global)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, description=excluded.description,
			kind=excluded.kind, filename=excluded.filename,
			value=excluded.value, nonce=excluded.nonce,
			global=excluded.global, updated_at=CURRENT_TIMESTAMP`,
		sec.ID, sec.Name, sec.Description, sec.Kind, sec.Filename,
		sec.Value, sec.Nonce, boolToInt(sec.Global))
	if err != nil {
		return fmt.Errorf("save secret: %w", err)
	}
	return nil
}

func (s *Store) GetSecret(id string) (*Secret, error) {
	row := s.db.QueryRow(`
		SELECT id, name, description, kind, filename, value, nonce, global, created_at, updated_at
		FROM secrets WHERE id = ?`, id)
	sec, err := scanSecret(row, true)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get secret: %w", err)
	}
	return sec, nil
}

func (s *Store) ListSecrets() ([]Secret, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, kind, filename, global, created_at, updated_at
		FROM secrets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list secrets: %w", err)
	}
	defer rows.Close()

	var secrets []Secret
	for rows.Next() {
		sec, err := scanSecretMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scan secret: %w", err)
		}
		secrets = append(secrets, *sec)
	}
	return secrets, rows.Err()
}

func (s *Store) DeleteSecret(id string) error {
	_, err := s.db.Exec(`DELETE FROM secrets WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}
	return nil
}

func (s *Store) GetAgentSecrets(agentID string) ([]Secret, error) {
	rows, err := s.db.Query(`
		SELECT s.id, s.name, s.description, s.kind, s.filename, s.global, s.created_at, s.updated_at
		FROM secrets s
		WHERE s.global = 1
		   OR s.id IN (SELECT secret_id FROM agent_secrets WHERE agent_id = ?)
		ORDER BY s.name`, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent secrets: %w", err)
	}
	defer rows.Close()

	var secrets []Secret
	for rows.Next() {
		sec, err := scanSecretMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scan agent secret: %w", err)
		}
		secrets = append(secrets, *sec)
	}
	return secrets, rows.Err()
}

func (s *Store) GetAgentSecret(agentID, secretID string) (*Secret, error) {
	row := s.db.QueryRow(`
		SELECT s.id, s.name, s.description, s.kind, s.filename, s.value, s.nonce, s.global, s.created_at, s.updated_at
		FROM secrets s
		WHERE s.id = ? AND (s.global = 1 OR s.id IN (SELECT secret_id FROM agent_secrets WHERE agent_id = ?))`,
		secretID, agentID)
	sec, err := scanSecret(row, true)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent secret: %w", err)
	}
	return sec, nil
}

func (s *Store) AddAgentSecret(agentID, secretID string) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO agent_secrets (agent_id, secret_id) VALUES (?, ?)`,
		agentID, secretID)
	if err != nil {
		return fmt.Errorf("add agent secret: %w", err)
	}
	return nil
}

func (s *Store) RemoveAgentSecret(agentID, secretID string) error {
	_, err := s.db.Exec(`DELETE FROM agent_secrets WHERE agent_id = ? AND secret_id = ?`,
		agentID, secretID)
	if err != nil {
		return fmt.Errorf("remove agent secret: %w", err)
	}
	return nil
}

func (s *Store) SetAgentSecrets(agentID string, secretIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM agent_secrets WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("clear agent secrets: %w", err)
	}

	for _, sid := range secretIDs {
		if _, err := tx.Exec(`INSERT INTO agent_secrets (agent_id, secret_id) VALUES (?, ?)`,
			agentID, sid); err != nil {
			return fmt.Errorf("insert agent secret: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) SetSecretAgents(secretID string, agentIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM agent_secrets WHERE secret_id = ?`, secretID); err != nil {
		return fmt.Errorf("clear secret agents: %w", err)
	}

	for _, aid := range agentIDs {
		if _, err := tx.Exec(`INSERT INTO agent_secrets (agent_id, secret_id) VALUES (?, ?)`,
			aid, secretID); err != nil {
			return fmt.Errorf("insert secret agent: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) GetSecretAgentIDs(secretID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT agent_id FROM agent_secrets WHERE secret_id = ?`, secretID)
	if err != nil {
		return nil, fmt.Errorf("get secret agent ids: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSecret(s scanner, withValue bool) (*Secret, error) {
	sec := &Secret{}
	var global int
	var desc, filename sql.NullString
	if withValue {
		err := s.Scan(&sec.ID, &sec.Name, &desc, &sec.Kind, &filename,
			&sec.Value, &sec.Nonce, &global, &sec.CreatedAt, &sec.UpdatedAt)
		if err != nil {
			return nil, err
		}
	}
	sec.Global = global == 1
	sec.Description = desc.String
	sec.Filename = filename.String
	return sec, nil
}

func scanSecretMeta(s scanner) (*Secret, error) {
	sec := &Secret{}
	var global int
	var desc, filename sql.NullString
	err := s.Scan(&sec.ID, &sec.Name, &desc, &sec.Kind, &filename,
		&global, &sec.CreatedAt, &sec.UpdatedAt)
	if err != nil {
		return nil, err
	}
	sec.Global = global == 1
	sec.Description = desc.String
	sec.Filename = filename.String
	return sec, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
