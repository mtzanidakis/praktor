package store

import (
	"database/sql"
	"fmt"
	"time"
)

type ScheduledTask struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agent_id"`
	Name        string     `json:"name"`
	Schedule    string     `json:"schedule"`
	Prompt      string     `json:"prompt"`
	ContextMode string     `json:"context_mode"`
	Status      string     `json:"status"`
	NextRunAt   *time.Time `json:"next_run_at,omitempty"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastStatus  string     `json:"last_status,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func scanTask(scanner interface {
	Scan(dest ...any) error
}) (*ScheduledTask, error) {
	t := &ScheduledTask{}
	var lastStatus, lastError *string
	err := scanner.Scan(&t.ID, &t.AgentID, &t.Name, &t.Schedule, &t.Prompt, &t.ContextMode, &t.Status,
		&t.NextRunAt, &t.LastRunAt, &lastStatus, &lastError, &t.CreatedAt)
	if err != nil {
		return nil, err
	}
	if lastStatus != nil {
		t.LastStatus = *lastStatus
	}
	if lastError != nil {
		t.LastError = *lastError
	}
	return t, nil
}

func (s *Store) SaveTask(t *ScheduledTask) error {
	_, err := s.db.Exec(`
		INSERT INTO scheduled_tasks (id, agent_id, name, schedule, prompt, context_mode, status, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			schedule = excluded.schedule,
			prompt = excluded.prompt,
			context_mode = excluded.context_mode,
			status = excluded.status,
			next_run_at = excluded.next_run_at`,
		t.ID, t.AgentID, t.Name, t.Schedule, t.Prompt, t.ContextMode, t.Status, t.NextRunAt)
	if err != nil {
		return fmt.Errorf("save task: %w", err)
	}
	return nil
}

func (s *Store) GetTask(id string) (*ScheduledTask, error) {
	row := s.db.QueryRow(`
		SELECT id, agent_id, name, schedule, prompt, context_mode, status,
		       next_run_at, last_run_at, last_status, last_error, created_at
		FROM scheduled_tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

func (s *Store) ListTasks() ([]ScheduledTask, error) {
	rows, err := s.db.Query(`
		SELECT id, agent_id, name, schedule, prompt, context_mode, status,
		       next_run_at, last_run_at, last_status, last_error, created_at
		FROM scheduled_tasks ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

func (s *Store) ListTasksForAgent(agentID string) ([]ScheduledTask, error) {
	rows, err := s.db.Query(`
		SELECT id, agent_id, name, schedule, prompt, context_mode, status,
		       next_run_at, last_run_at, last_status, last_error, created_at
		FROM scheduled_tasks WHERE agent_id = ? ORDER BY created_at`, agentID)
	if err != nil {
		return nil, fmt.Errorf("list tasks for agent: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

func (s *Store) GetDueTasks(now time.Time) ([]ScheduledTask, error) {
	rows, err := s.db.Query(`
		SELECT id, agent_id, name, schedule, prompt, context_mode, status,
		       next_run_at, last_run_at, last_status, last_error, created_at
		FROM scheduled_tasks
		WHERE status = 'active' AND next_run_at <= ?
		ORDER BY next_run_at`, now)
	if err != nil {
		return nil, fmt.Errorf("get due tasks: %w", err)
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

func (s *Store) UpdateTaskRun(id string, lastStatus string, lastError string, nextRunAt *time.Time) error {
	_, err := s.db.Exec(`
		UPDATE scheduled_tasks
		SET last_run_at = CURRENT_TIMESTAMP, last_status = ?, last_error = ?, next_run_at = ?
		WHERE id = ?`, lastStatus, lastError, nextRunAt, id)
	return err
}

func (s *Store) UpdateTaskStatus(id string, status string) error {
	_, err := s.db.Exec(`UPDATE scheduled_tasks SET status = ? WHERE id = ?`, status, id)
	return err
}

func (s *Store) DeleteTask(id string) error {
	_, err := s.db.Exec(`DELETE FROM scheduled_tasks WHERE id = ?`, id)
	return err
}
