package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestAgentCRUD(t *testing.T) {
	s := newTestStore(t)

	a := &Agent{ID: "general", Name: "General", Workspace: "general", Description: "General assistant"}
	if err := s.SaveAgent(a); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	got, err := s.GetAgent("general")
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	if got == nil {
		t.Fatal("expected agent, got nil")
	}
	if got.Name != "General" {
		t.Errorf("expected name 'General', got '%s'", got.Name)
	}
	if got.Description != "General assistant" {
		t.Errorf("expected description 'General assistant', got '%s'", got.Description)
	}

	// List
	agents, err := s.ListAgents()
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}

	// Update
	a.Name = "Updated Agent"
	if err := s.SaveAgent(a); err != nil {
		t.Fatalf("update agent: %v", err)
	}
	got, _ = s.GetAgent("general")
	if got.Name != "Updated Agent" {
		t.Errorf("expected 'Updated Agent', got '%s'", got.Name)
	}

	// Not found
	got, err = s.GetAgent("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent agent")
	}

	// DeleteAgentsNotIn
	_ = s.SaveAgent(&Agent{ID: "coder", Name: "Coder", Workspace: "coder"})
	_ = s.SaveAgent(&Agent{ID: "researcher", Name: "Researcher", Workspace: "researcher"})
	if err := s.DeleteAgentsNotIn([]string{"general", "coder"}); err != nil {
		t.Fatalf("delete agents not in: %v", err)
	}
	agents, _ = s.ListAgents()
	if len(agents) != 2 {
		t.Errorf("expected 2 agents after delete, got %d", len(agents))
	}
}

func TestMessageCRUD(t *testing.T) {
	s := newTestStore(t)

	// Create agent first
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Save messages
	for i := 0; i < 5; i++ {
		_ = s.SaveMessage(&Message{
			AgentID: "a1",
			Sender:  "user:1",
			Content: "message " + string(rune('A'+i)),
		})
	}

	messages, err := s.GetMessages("a1", 10)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(messages))
	}
	// Should be in chronological order
	if messages[0].Content != "message A" {
		t.Errorf("expected first message 'message A', got '%s'", messages[0].Content)
	}

	// Limit
	messages, err = s.GetMessages("a1", 2)
	if err != nil {
		t.Fatalf("get messages limited: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC3339 UTC", "2026-02-17T04:35:00Z", false},
		{"RFC3339 offset", "2026-02-17T10:20:00+05:45", false},
		{"RFC3339Nano", "2026-02-17T04:35:00.123456789Z", false},
		{"SQLite CURRENT_TIMESTAMP", "2026-02-17 04:35:00", false},
		{"space with offset colon", "2026-02-17 10:20:00+05:45", false},
		{"Go time.String with tz name", "2026-02-17 10:20:00 +0545 NPT", false},
		{"Go time.String UTC", "2026-02-17 04:35:00 +0000 UTC", false},
		{"Go time.String with fractional", "2026-02-17 10:20:00.123456 +0545 NPT", false},
		{"numeric offset no tz name", "2026-02-17 10:20:00 +0545", false},
		{"numeric offset with fractional", "2026-02-17 10:20:00.123 +0545", false},
		{"empty string", "", true},
		{"garbage", "not-a-time", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTimeString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			// All valid inputs represent the same instant (2026-02-17 ~04:35 UTC)
			if got.Year() != 2026 || got.Month() != 2 || got.Day() != 17 {
				t.Errorf("unexpected date: %v", got)
			}
		})
	}
}

func TestScheduledTaskCRUD(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	now := time.Now()
	nextRun := now.Add(-1 * time.Minute) // Due now
	task := &ScheduledTask{
		ID:          "task-1",
		AgentID:     "a1",
		Name:        "Test Task",
		Schedule:    `{"kind":"interval","interval_ms":60000}`,
		Prompt:      "do something",
		ContextMode: "isolated",
		Status:      "active",
		NextRunAt:   &nextRun,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	got, err := s.GetTask("task-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if got.Name != "Test Task" {
		t.Errorf("expected 'Test Task', got '%s'", got.Name)
	}

	// Verify NextRunAt round-trips correctly (within 1 second tolerance)
	if got.NextRunAt == nil {
		t.Fatal("expected NextRunAt to be set")
	}
	diff := got.NextRunAt.Sub(nextRun.UTC())
	if diff < -time.Second || diff > time.Second {
		t.Errorf("NextRunAt drift: saved %v, got %v", nextRun.UTC(), got.NextRunAt)
	}

	// Due tasks
	due, err := s.GetDueTasks(time.Now())
	if err != nil {
		t.Fatalf("get due tasks: %v", err)
	}
	if len(due) != 1 {
		t.Errorf("expected 1 due task, got %d", len(due))
	}

	// UpdateTaskRun round-trips NextRunAt
	futureRun := now.Add(1 * time.Hour)
	if err := s.UpdateTaskRun("task-1", "ok", "", &futureRun); err != nil {
		t.Fatalf("update task run: %v", err)
	}
	got, _ = s.GetTask("task-1")
	if got.NextRunAt == nil {
		t.Fatal("expected NextRunAt after update")
	}
	diff = got.NextRunAt.Sub(futureRun.UTC())
	if diff < -time.Second || diff > time.Second {
		t.Errorf("NextRunAt drift after update: saved %v, got %v", futureRun.UTC(), got.NextRunAt)
	}
	if got.LastRunAt == nil {
		t.Error("expected LastRunAt to be set after update")
	}

	// Pause
	_ = s.UpdateTaskStatus("task-1", "paused")
	due, _ = s.GetDueTasks(time.Now())
	if len(due) != 0 {
		t.Errorf("expected 0 due tasks after pause, got %d", len(due))
	}
}

func TestScheduledTaskNonStandardTimezone(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Simulate what happens when a task is created in a non-standard timezone:
	// manually insert a row with Go's time.String() format containing "+0545"
	npt := time.FixedZone("NPT", 5*3600+45*60)
	ts := time.Date(2026, 2, 17, 10, 20, 0, 0, npt)
	rawTimeStr := ts.String() // "2026-02-17 10:20:00 +0545 NPT"

	_, err := s.db.Exec(`
		INSERT INTO scheduled_tasks (id, agent_id, name, schedule, prompt, context_mode, status, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"task-npt", "a1", "Nepal Task", `{"kind":"cron","cron_expr":"0 9 * * *"}`,
		"test", "isolated", "active", rawTimeStr)
	if err != nil {
		t.Fatalf("insert raw: %v", err)
	}

	// This is the exact scenario from the bug report — ListTasks must not error
	tasks, err := s.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks failed (this is the reported bug): %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].NextRunAt == nil {
		t.Fatal("expected NextRunAt to be parsed")
	}

	// Verify the parsed time matches the original instant
	diff := tasks[0].NextRunAt.Sub(ts)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("time mismatch: original %v, parsed %v", ts, tasks[0].NextRunAt)
	}
}

func TestGetDueTasksMixedFormats(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	// Insert a task with old format (space-separated) that is in the future
	futureOld := "2099-01-01 00:00:00"
	_, err := s.db.Exec(`
		INSERT INTO scheduled_tasks (id, agent_id, name, schedule, prompt, context_mode, status, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"task-future-old", "a1", "Future Old", `{"kind":"cron","cron_expr":"0 0 1 1 *"}`,
		"test", "isolated", "active", futureOld)
	if err != nil {
		t.Fatalf("insert future old: %v", err)
	}

	// Insert a task with new RFC3339 format that is in the future
	futureNew := "2099-06-01T12:00:00Z"
	_, err = s.db.Exec(`
		INSERT INTO scheduled_tasks (id, agent_id, name, schedule, prompt, context_mode, status, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"task-future-new", "a1", "Future New", `{"kind":"cron","cron_expr":"0 12 1 6 *"}`,
		"test", "isolated", "active", futureNew)
	if err != nil {
		t.Fatalf("insert future new: %v", err)
	}

	// Insert a task with old format that IS due (past)
	pastOld := "2020-01-01 00:00:00"
	_, err = s.db.Exec(`
		INSERT INTO scheduled_tasks (id, agent_id, name, schedule, prompt, context_mode, status, next_run_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"task-past-old", "a1", "Past Old", `{"kind":"cron","cron_expr":"0 0 1 1 *"}`,
		"test", "isolated", "active", pastOld)
	if err != nil {
		t.Fatalf("insert past old: %v", err)
	}

	// Only the past task should be due — future tasks must NOT trigger
	due, err := s.GetDueTasks(time.Now())
	if err != nil {
		t.Fatalf("GetDueTasks: %v", err)
	}
	if len(due) != 1 {
		t.Errorf("expected 1 due task, got %d", len(due))
		for _, d := range due {
			t.Logf("  due: %s (next_run_at=%v)", d.ID, d.NextRunAt)
		}
	}
	if len(due) > 0 && due[0].ID != "task-past-old" {
		t.Errorf("expected task-past-old to be due, got %s", due[0].ID)
	}
}

func TestSwarmRunCRUD(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveAgent(&Agent{ID: "a1", Name: "Agent 1", Workspace: "a1"})

	agents, _ := json.Marshal([]map[string]string{{"role": "researcher"}})
	run := &SwarmRun{
		ID:      "swarm-1",
		AgentID: "a1",
		Task:    "research topic",
		Status:  "running",
		Agents:  agents,
	}

	if err := s.SaveSwarmRun(run); err != nil {
		t.Fatalf("save swarm run: %v", err)
	}

	got, err := s.GetSwarmRun("swarm-1")
	if err != nil {
		t.Fatalf("get swarm run: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", got.Status)
	}

	// Update
	results, _ := json.Marshal([]map[string]string{{"output": "done"}})
	_ = s.UpdateSwarmRun("swarm-1", "completed", results)

	got, _ = s.GetSwarmRun("swarm-1")
	if got.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", got.Status)
	}
}
