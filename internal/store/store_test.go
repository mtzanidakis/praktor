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
