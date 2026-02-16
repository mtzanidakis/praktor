package store

import (
	"database/sql"
	"fmt"
	"time"
)

type Agent struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Model       string    `json:"model,omitempty"`
	Image       string    `json:"image,omitempty"`
	Workspace   string    `json:"workspace"`
	ClaudeMD    string    `json:"claude_md,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s *Store) SaveAgent(a *Agent) error {
	_, err := s.db.Exec(`
		INSERT INTO agents (id, name, description, model, image, workspace, claude_md, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			model = excluded.model,
			image = excluded.image,
			workspace = excluded.workspace,
			claude_md = excluded.claude_md,
			updated_at = CURRENT_TIMESTAMP`,
		a.ID, a.Name, a.Description, a.Model, a.Image, a.Workspace, a.ClaudeMD)
	if err != nil {
		return fmt.Errorf("save agent: %w", err)
	}
	return nil
}

func (s *Store) GetAgent(id string) (*Agent, error) {
	a := &Agent{}
	var description, model, image, claudeMD sql.NullString
	err := s.db.QueryRow(`SELECT id, name, description, model, image, workspace, claude_md, created_at, updated_at FROM agents WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &description, &model, &image, &a.Workspace, &claudeMD, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}
	a.Description = description.String
	a.Model = model.String
	a.Image = image.String
	a.ClaudeMD = claudeMD.String
	return a, nil
}

func (s *Store) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(`SELECT id, name, description, model, image, workspace, claude_md, created_at, updated_at FROM agents ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var a Agent
		var description, model, image, claudeMD sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &description, &model, &image, &a.Workspace, &claudeMD, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		a.Description = description.String
		a.Model = model.String
		a.Image = image.String
		a.ClaudeMD = claudeMD.String
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

func (s *Store) DeleteAgent(id string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteAgentsNotIn(ids []string) error {
	if len(ids) == 0 {
		_, err := s.db.Exec(`DELETE FROM agents`)
		return err
	}
	query := `DELETE FROM agents WHERE id NOT IN (`
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			query += ","
		}
		query += "?"
		args[i] = id
	}
	query += ")"
	_, err := s.db.Exec(query, args...)
	return err
}
