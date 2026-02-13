package store

import (
	"database/sql"
	"fmt"
	"time"
)

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Folder    string    `json:"folder"`
	IsMain    bool      `json:"is_main"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) SaveGroup(g *Group) error {
	_, err := s.db.Exec(`
		INSERT INTO groups (id, name, folder, is_main, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			folder = excluded.folder,
			is_main = excluded.is_main,
			updated_at = CURRENT_TIMESTAMP`,
		g.ID, g.Name, g.Folder, g.IsMain)
	if err != nil {
		return fmt.Errorf("save group: %w", err)
	}
	return nil
}

func (s *Store) GetGroup(id string) (*Group, error) {
	g := &Group{}
	err := s.db.QueryRow(`SELECT id, name, folder, is_main, created_at, updated_at FROM groups WHERE id = ?`, id).
		Scan(&g.ID, &g.Name, &g.Folder, &g.IsMain, &g.CreatedAt, &g.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get group: %w", err)
	}
	return g, nil
}

func (s *Store) ListGroups() ([]Group, error) {
	rows, err := s.db.Query(`SELECT id, name, folder, is_main, created_at, updated_at FROM groups ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list groups: %w", err)
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		var g Group
		if err := rows.Scan(&g.ID, &g.Name, &g.Folder, &g.IsMain, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan group: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

func (s *Store) DeleteGroup(id string) error {
	_, err := s.db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	return err
}
