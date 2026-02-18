package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type SwarmRun struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	AgentID     string          `json:"agent_id"`
	LeadAgent   string          `json:"lead_agent"`
	Task        string          `json:"task"`
	Status      string          `json:"status"`
	Agents      json.RawMessage `json:"agents"`
	Synapses    json.RawMessage `json:"synapses,omitempty"`
	Results     json.RawMessage `json:"results,omitempty"`
	StartedAt   time.Time       `json:"started_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

func scanSwarmRun(scanner interface {
	Scan(dest ...any) error
}) (*SwarmRun, error) {
	r := &SwarmRun{}
	var results, synapses *string
	err := scanner.Scan(&r.ID, &r.Name, &r.AgentID, &r.LeadAgent, &r.Task, &r.Status, &r.Agents, &synapses, &results, &r.StartedAt, &r.CompletedAt)
	if err != nil {
		return nil, err
	}
	if results != nil {
		r.Results = json.RawMessage(*results)
	}
	if synapses != nil {
		r.Synapses = json.RawMessage(*synapses)
	}
	return r, nil
}

const swarmColumns = `id, name, agent_id, lead_agent, task, status, agents, synapses, results, started_at, completed_at`

func (s *Store) SaveSwarmRun(r *SwarmRun) error {
	_, err := s.db.Exec(`
		INSERT INTO swarm_runs (id, name, agent_id, lead_agent, task, status, agents, synapses, results)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status,
			results = excluded.results,
			completed_at = CASE WHEN excluded.status IN ('completed', 'failed') THEN CURRENT_TIMESTAMP ELSE completed_at END`,
		r.ID, r.Name, r.AgentID, r.LeadAgent, r.Task, r.Status, r.Agents, r.Synapses, r.Results)
	if err != nil {
		return fmt.Errorf("save swarm run: %w", err)
	}
	return nil
}

func (s *Store) GetSwarmRun(id string) (*SwarmRun, error) {
	row := s.db.QueryRow(`SELECT `+swarmColumns+` FROM swarm_runs WHERE id = ?`, id)
	r, err := scanSwarmRun(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get swarm run: %w", err)
	}
	return r, nil
}

func (s *Store) ListSwarmRuns() ([]SwarmRun, error) {
	rows, err := s.db.Query(`SELECT ` + swarmColumns + ` FROM swarm_runs ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list swarm runs: %w", err)
	}
	defer rows.Close()

	var runs []SwarmRun
	for rows.Next() {
		r, err := scanSwarmRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan swarm run: %w", err)
		}
		runs = append(runs, *r)
	}
	return runs, rows.Err()
}

func (s *Store) DeleteSwarmRun(id string) error {
	_, err := s.db.Exec(`DELETE FROM swarm_runs WHERE id = ?`, id)
	return err
}

func (s *Store) UpdateSwarmRun(id string, status string, results json.RawMessage) error {
	_, err := s.db.Exec(`
		UPDATE swarm_runs
		SET status = ?, results = ?,
		    completed_at = CASE WHEN ? IN ('completed', 'failed') THEN CURRENT_TIMESTAMP ELSE completed_at END
		WHERE id = ?`, status, results, status, id)
	return err
}
