package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtzanidakis/praktor/internal/config"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(config.StoreConfig{Path: filepath.Join(dir, "test.db")})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestGroupCRUD(t *testing.T) {
	s := newTestStore(t)

	g := &Group{ID: "123", Name: "Test Group", Folder: "test-group"}
	if err := s.SaveGroup(g); err != nil {
		t.Fatalf("save group: %v", err)
	}

	got, err := s.GetGroup("123")
	if err != nil {
		t.Fatalf("get group: %v", err)
	}
	if got == nil {
		t.Fatal("expected group, got nil")
	}
	if got.Name != "Test Group" {
		t.Errorf("expected name 'Test Group', got '%s'", got.Name)
	}

	// List
	groups, err := s.ListGroups()
	if err != nil {
		t.Fatalf("list groups: %v", err)
	}
	if len(groups) != 1 {
		t.Errorf("expected 1 group, got %d", len(groups))
	}

	// Update
	g.Name = "Updated Group"
	if err := s.SaveGroup(g); err != nil {
		t.Fatalf("update group: %v", err)
	}
	got, _ = s.GetGroup("123")
	if got.Name != "Updated Group" {
		t.Errorf("expected 'Updated Group', got '%s'", got.Name)
	}

	// Not found
	got, err = s.GetGroup("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Error("expected nil for nonexistent group")
	}
}

func TestMessageCRUD(t *testing.T) {
	s := newTestStore(t)

	// Create group first
	_ = s.SaveGroup(&Group{ID: "g1", Name: "Group 1", Folder: "g1"})

	// Save messages
	for i := 0; i < 5; i++ {
		_ = s.SaveMessage(&Message{
			GroupID: "g1",
			Sender:  "user:1",
			Content: "message " + string(rune('A'+i)),
		})
	}

	messages, err := s.GetMessages("g1", 10)
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
	messages, err = s.GetMessages("g1", 2)
	if err != nil {
		t.Fatalf("get messages limited: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}

func TestScheduledTaskCRUD(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveGroup(&Group{ID: "g1", Name: "Group 1", Folder: "g1"})

	now := time.Now()
	nextRun := now.Add(-1 * time.Minute) // Due now
	task := &ScheduledTask{
		ID:          "task-1",
		GroupID:     "g1",
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

	// Due tasks
	due, err := s.GetDueTasks(time.Now())
	if err != nil {
		t.Fatalf("get due tasks: %v", err)
	}
	if len(due) != 1 {
		t.Errorf("expected 1 due task, got %d", len(due))
	}

	// Pause
	_ = s.UpdateTaskStatus("task-1", "paused")
	due, _ = s.GetDueTasks(time.Now())
	if len(due) != 0 {
		t.Errorf("expected 0 due tasks after pause, got %d", len(due))
	}
}

func TestSwarmRunCRUD(t *testing.T) {
	s := newTestStore(t)
	_ = s.SaveGroup(&Group{ID: "g1", Name: "Group 1", Folder: "g1"})

	agents, _ := json.Marshal([]map[string]string{{"role": "researcher"}})
	run := &SwarmRun{
		ID:      "swarm-1",
		GroupID: "g1",
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
