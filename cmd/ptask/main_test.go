package main

import (
	"encoding/json"
	"testing"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/nats-io/nats.go"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{
			name: "empty",
			args: []string{},
			want: map[string]string{},
		},
		{
			name: "single flag",
			args: []string{"--name", "test"},
			want: map[string]string{"name": "test"},
		},
		{
			name: "multiple flags",
			args: []string{"--name", "test", "--schedule", "* * * * *", "--prompt", "hello"},
			want: map[string]string{"name": "test", "schedule": "* * * * *", "prompt": "hello"},
		},
		{
			name: "flag without value is ignored",
			args: []string{"--name"},
			want: map[string]string{},
		},
		{
			name: "non-flag args ignored",
			args: []string{"positional", "--name", "test"},
			want: map[string]string{"name": "test"},
		},
		{
			name: "short prefix not treated as flag",
			args: []string{"-n", "test"},
			want: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Errorf("parseArgs(%v) returned %d entries, want %d", tt.args, len(got), len(tt.want))
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Errorf("parseArgs(%v)[%q] = %q, want %q", tt.args, k, got[k], v)
				}
			}
		})
	}
}

func startTestNATS(t *testing.T) *natsbus.Bus {
	t.Helper()
	bus, err := natsbus.New(config.NATSConfig{
		Port:    0,
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("start nats: %v", err)
	}
	t.Cleanup(func() { bus.Close() })
	return bus
}

func TestSendIPCCreateTask(t *testing.T) {
	bus := startTestNATS(t)
	url := bus.ClientURL()

	// Mock IPC responder
	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Subscribe("host.ipc.test-group", func(msg *nats.Msg) {
		var req ipcRequest
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			t.Errorf("unmarshal request: %v", err)
			return
		}
		if req.Type != "create_task" {
			t.Errorf("expected type create_task, got %s", req.Type)
		}
		if req.Payload["name"] != "my task" {
			t.Errorf("expected name 'my task', got %v", req.Payload["name"])
		}
		resp, _ := json.Marshal(ipcResponse{OK: true, ID: "task-123"})
		msg.Respond(resp)
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn.Flush()

	resp, err := sendIPC(url, "test-group", "create_task", map[string]any{
		"name":     "my task",
		"schedule": "* * * * *",
		"prompt":   "hello",
	})
	if err != nil {
		t.Fatalf("sendIPC: %v", err)
	}
	if resp.ID != "task-123" {
		t.Errorf("expected id task-123, got %s", resp.ID)
	}
}

func TestSendIPCListTasks(t *testing.T) {
	bus := startTestNATS(t)
	url := bus.ClientURL()

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Subscribe("host.ipc.test-group", func(msg *nats.Msg) {
		var req ipcRequest
		json.Unmarshal(msg.Data, &req)
		if req.Type != "list_tasks" {
			t.Errorf("expected type list_tasks, got %s", req.Type)
		}
		resp, _ := json.Marshal(ipcResponse{
			OK: true,
			Tasks: []task{
				{ID: "t1", Name: "task one", Schedule: "* * * * *", Status: "active"},
				{ID: "t2", Name: "task two", Schedule: "0 9 * * *", Status: "active"},
			},
		})
		msg.Respond(resp)
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn.Flush()

	resp, err := sendIPC(url, "test-group", "list_tasks", map[string]any{})
	if err != nil {
		t.Fatalf("sendIPC: %v", err)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(resp.Tasks))
	}
	if resp.Tasks[0].ID != "t1" || resp.Tasks[1].ID != "t2" {
		t.Errorf("unexpected task IDs: %v", resp.Tasks)
	}
}

func TestSendIPCDeleteTask(t *testing.T) {
	bus := startTestNATS(t)
	url := bus.ClientURL()

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Subscribe("host.ipc.test-group", func(msg *nats.Msg) {
		var req ipcRequest
		json.Unmarshal(msg.Data, &req)
		if req.Type != "delete_task" {
			t.Errorf("expected type delete_task, got %s", req.Type)
		}
		if req.Payload["id"] != "task-123" {
			t.Errorf("expected id task-123, got %v", req.Payload["id"])
		}
		resp, _ := json.Marshal(ipcResponse{OK: true})
		msg.Respond(resp)
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn.Flush()

	resp, err := sendIPC(url, "test-group", "delete_task", map[string]any{"id": "task-123"})
	if err != nil {
		t.Fatalf("sendIPC: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestSendIPCErrorResponse(t *testing.T) {
	bus := startTestNATS(t)
	url := bus.ClientURL()

	conn, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	_, err = conn.Subscribe("host.ipc.test-group", func(msg *nats.Msg) {
		resp, _ := json.Marshal(ipcResponse{Error: "task not found"})
		msg.Respond(resp)
	})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	conn.Flush()

	resp, err := sendIPC(url, "test-group", "delete_task", map[string]any{"id": "nonexistent"})
	if err != nil {
		t.Fatalf("sendIPC: %v", err)
	}
	if resp.Error != "task not found" {
		t.Errorf("expected error 'task not found', got %q", resp.Error)
	}
}
