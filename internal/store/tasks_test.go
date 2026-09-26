package store

import (
	"path/filepath"
	"testing"
)

func newTaskStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "praktor.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.SaveAgent(&Agent{ID: "iris", Name: "iris", Workspace: "general"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return s
}

func TestSaveTaskRoundTripsCheckCommand(t *testing.T) {
	s := newTaskStore(t)
	task := &ScheduledTask{
		ID:           "task-1",
		AgentID:      "iris",
		Name:         "reminders",
		Schedule:     `{"kind":"cron","cron_expr":"*/15 * * * *"}`,
		Prompt:       "",
		CheckCommand: "caldav-cli reminders -state /workspace/agent/.state/watermark",
		ContextMode:  "isolated",
		Status:       "active",
	}
	if err := s.SaveTask(task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	got, err := s.GetTask("task-1")
	if err != nil || got == nil {
		t.Fatalf("get task: %v (task %v)", err, got)
	}
	if got.CheckCommand != task.CheckCommand {
		t.Errorf("check_command = %q, want %q", got.CheckCommand, task.CheckCommand)
	}

	// Clearing it must persist too.
	task.CheckCommand = ""
	if err := s.SaveTask(task); err != nil {
		t.Fatalf("resave task: %v", err)
	}
	got, err = s.GetTask("task-1")
	if err != nil || got == nil {
		t.Fatalf("get task after clear: %v", err)
	}
	if got.CheckCommand != "" {
		t.Errorf("check_command = %q, want empty", got.CheckCommand)
	}

	listed, err := s.ListTasksForAgent("iris")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list tasks: %v (%d rows)", err, len(listed))
	}
}
