package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// timeFormats lists datetime formats that may appear in SQLite DATETIME columns.
// Order matters: try the most common/standard formats first.
var timeFormats = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05-07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
}

// parseTimeString parses a time string stored in SQLite, handling non-standard
// timezone offsets like "+0545" that time.Time.String() may produce.
func parseTimeString(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, f := range timeFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}
	// Handle Go's time.String() format: "2006-01-02 15:04:05.999999999 +0545 NPT"
	// or "2006-01-02 15:04:05 +0545 NPT" (with optional fractional seconds and tz name)
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05 -0700 MST", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700", s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02 15:04:05 -0700", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %q", s)
}

// scanTimeString scans a nullable time column as a string and parses it.
func scanTimeString(s *string) *time.Time {
	if s == nil || *s == "" {
		return nil
	}
	t, err := parseTimeString(*s)
	if err != nil {
		return nil
	}
	return &t
}

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
	var nextRunAt, lastRunAt, createdAt *string
	err := scanner.Scan(&t.ID, &t.AgentID, &t.Name, &t.Schedule, &t.Prompt, &t.ContextMode, &t.Status,
		&nextRunAt, &lastRunAt, &lastStatus, &lastError, &createdAt)
	if err != nil {
		return nil, err
	}
	t.NextRunAt = scanTimeString(nextRunAt)
	t.LastRunAt = scanTimeString(lastRunAt)
	if createdAt != nil {
		if ct := scanTimeString(createdAt); ct != nil {
			t.CreatedAt = *ct
		}
	}
	if lastStatus != nil {
		t.LastStatus = *lastStatus
	}
	if lastError != nil {
		t.LastError = *lastError
	}
	return t, nil
}

// timeToUTC converts a *time.Time to a *string in RFC3339 UTC format for storage.
// Returns nil if the input is nil.
func timeToUTC(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func (s *Store) SaveTask(t *ScheduledTask) error {
	_, err := s.db.Exec(`
		INSERT INTO scheduled_tasks (id, agent_id, name, schedule, prompt, context_mode, status, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			agent_id = excluded.agent_id,
			name = excluded.name,
			schedule = excluded.schedule,
			prompt = excluded.prompt,
			context_mode = excluded.context_mode,
			status = excluded.status,
			next_run_at = excluded.next_run_at`,
		t.ID, t.AgentID, t.Name, t.Schedule, t.Prompt, t.ContextMode, t.Status, timeToUTC(t.NextRunAt))
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
	// Fetch all active tasks with a next_run_at and filter in Go, because
	// the DB may contain mixed timestamp formats (pre-fix vs RFC3339) that
	// break SQLite's lexicographic string comparison.
	rows, err := s.db.Query(`
		SELECT id, agent_id, name, schedule, prompt, context_mode, status,
		       next_run_at, last_run_at, last_status, last_error, created_at
		FROM scheduled_tasks
		WHERE status = 'active' AND next_run_at IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("get due tasks: %w", err)
	}
	defer rows.Close()

	nowUTC := now.UTC()
	var tasks []ScheduledTask
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		if t.NextRunAt != nil && !t.NextRunAt.After(nowUTC) {
			tasks = append(tasks, *t)
		}
	}
	return tasks, rows.Err()
}

func (s *Store) UpdateTaskRun(id string, lastStatus string, lastError string, nextRunAt *time.Time) error {
	_, err := s.db.Exec(`
		UPDATE scheduled_tasks
		SET last_run_at = CURRENT_TIMESTAMP, last_status = ?, last_error = ?, next_run_at = ?
		WHERE id = ?`, lastStatus, lastError, timeToUTC(nextRunAt), id)
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

func (s *Store) DeleteCompletedTasks() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM scheduled_tasks WHERE status = 'completed'`)
	if err != nil {
		return 0, fmt.Errorf("delete completed tasks: %w", err)
	}
	return res.RowsAffected()
}
