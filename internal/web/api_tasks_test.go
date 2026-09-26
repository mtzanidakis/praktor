package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/store"
)

func newTaskAPIServer(t *testing.T) *Server {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "praktor.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.SaveAgent(&store.Agent{ID: "iris", Name: "iris", Workspace: "general"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return NewServer(s, nil, nil, nil, nil, nil, config.WebConfig{}, nil, "test")
}

func TestCreateTaskRequiresPromptOrCheckCommand(t *testing.T) {
	srv := newTaskAPIServer(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"prompt only", `{"agent_id":"iris","name":"a","schedule":"0 9 * * *","prompt":"Reply with: hi"}`, http.StatusOK},
		{"check only", `{"agent_id":"iris","name":"b","schedule":"*/15 * * * *","check_command":"echo due"}`, http.StatusOK},
		{"neither", `{"agent_id":"iris","name":"c","schedule":"@hourly","prompt":" "}`, http.StatusBadRequest},
		{"no schedule", `{"agent_id":"iris","name":"d","prompt":"hi"}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(c.body))
			srv.createTask(rec, req)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.want, rec.Body.String())
			}
		})
	}
}

func TestUpdateTaskKeepsPromptOrCheckCommand(t *testing.T) {
	srv := newTaskAPIServer(t)
	task := &store.ScheduledTask{
		ID: "t1", AgentID: "iris", Name: "watch", Schedule: `{"kind":"cron","cron_expr":"@hourly"}`,
		CheckCommand: "echo due", ContextMode: "isolated", Status: "active",
	}
	if err := srv.store.SaveTask(task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	update := func(body string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/tasks/t1", strings.NewReader(body))
		req.SetPathValue("id", "t1")
		srv.updateTask(rec, req)
		return rec.Code
	}
	if code := update(`{"check_command":""}`); code != http.StatusBadRequest {
		t.Fatalf("clearing the only check: status %d, want 400", code)
	}
	if code := update(`{"prompt":"Summarise","check_command":""}`); code != http.StatusOK {
		t.Fatalf("swapping check for prompt: status %d, want 200", code)
	}
}
