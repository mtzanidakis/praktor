package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/store"
)

// newIPCHarness wires an orchestrator to a test NATS bus and returns a
// function that sends one IPC command as the given agent.
func newIPCHarness(t *testing.T) (*store.Store, func(agentID, typ string, payload any) map[string]any) {
	t.Helper()
	s, err := store.New(filepath.Join(t.TempDir(), "praktor.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	agents := map[string]config.AgentDefinition{
		"iris":   {Description: "unrestricted"},
		"reader": {Description: "no shell", AllowedTools: []string{"Read", "WebSearch"}},
	}
	for id := range agents {
		if err := s.SaveAgent(&store.Agent{ID: id, Name: id, Workspace: id}); err != nil {
			t.Fatalf("save agent: %v", err)
		}
	}

	bus, err := natsbus.NewForTest(config.NATSConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new bus: %v", err)
	}
	t.Cleanup(bus.Close)

	reg := registry.New(s, agents, config.DefaultsConfig{}, t.TempDir())
	o := NewOrchestrator(bus, nil, s, reg, config.DefaultsConfig{}, nil)
	if o.client == nil {
		t.Fatal("orchestrator has no NATS client")
	}
	if err := o.client.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	client, err := natsbus.NewClient(bus)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	t.Cleanup(client.Close)

	send := func(agentID, typ string, payload any) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(payload)
		data, _ := json.Marshal(IPCCommand{Type: typ, Payload: raw})
		msg, err := client.Request("host.ipc."+agentID, data, 5*time.Second)
		if err != nil {
			t.Fatalf("ipc %s: %v", typ, err)
		}
		var resp map[string]any
		if err := json.Unmarshal(msg.Data, &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}
	return s, send
}

func TestIPCCreateTaskValidation(t *testing.T) {
	_, send := newIPCHarness(t)

	cases := []struct {
		name    string
		agent   string
		payload map[string]any
		wantErr string
	}{
		{"prompt only", "iris", map[string]any{"name": "a", "schedule": "0 9 * * *", "prompt": "Reply with: hi"}, ""},
		{"check only", "iris", map[string]any{"name": "b", "schedule": "*/15 * * * *", "check_command": "echo due"}, ""},
		{"prompt and check", "iris", map[string]any{"name": "c", "schedule": "@hourly", "prompt": "Summarise", "check_command": "echo x"}, ""},
		{"neither", "iris", map[string]any{"name": "d", "schedule": "@hourly", "prompt": "  "}, "prompt or check_command is required"},
		{"no name", "iris", map[string]any{"schedule": "@hourly", "prompt": "hi"}, "name and schedule are required"},
		{"check without Bash", "reader", map[string]any{"name": "e", "schedule": "@hourly", "check_command": "id"}, "check_command requires Bash in this agent's allowed_tools"},
		{"unknown agent fails closed", "ghost", map[string]any{"name": "f", "schedule": "@hourly", "check_command": "id"}, "check_command requires Bash in this agent's allowed_tools"},
		{"prompt without Bash", "reader", map[string]any{"name": "g", "schedule": "@hourly", "prompt": "hi"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := send(c.agent, "create_task", c.payload)
			if c.wantErr == "" {
				if resp["ok"] != true {
					t.Fatalf("expected success, got %v", resp)
				}
				return
			}
			if resp["error"] != c.wantErr {
				t.Fatalf("error = %v, want %q", resp["error"], c.wantErr)
			}
		})
	}
}

func TestIPCUpdateAndListTaskCheckCommand(t *testing.T) {
	s, send := newIPCHarness(t)

	created := send("iris", "create_task", map[string]any{
		"name": "watch", "schedule": "*/15 * * * *", "check_command": "echo due",
	})
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("create failed: %v", created)
	}

	// Clearing the check of a task with no prompt would leave nothing to run.
	resp := send("iris", "update_task", map[string]any{"id": id, "check_command": ""})
	if resp["error"] != "prompt or check_command is required" {
		t.Fatalf("clearing the only check: got %v", resp)
	}

	list := send("iris", "list_tasks", map[string]any{})
	tasks, _ := list["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("list = %v", list)
	}
	if got := tasks[0].(map[string]any)["check_command"]; got != "echo due" {
		t.Fatalf("listed check_command = %v, want %q", got, "echo due")
	}

	stored, err := s.GetTask(id)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if stored.Prompt != "" || stored.CheckCommand != "echo due" {
		t.Fatalf("stored task = %+v", stored)
	}
}
