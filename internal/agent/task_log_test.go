package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/store"
)

func TestIncomingMessage(t *testing.T) {
	cases := []struct {
		name         string
		text         string
		meta         map[string]string
		wantSender   string
		wantContent  string
		wantDeferred bool
	}{
		{"user message", "hi", nil, "user", "hi", false},
		{"plain task", "Summarise", map[string]string{"sender": "scheduler"}, "scheduler", "Summarise", false},
		{"gated task with prompt", "Summarise",
			map[string]string{"sender": "scheduler", "check_command": "echo due"}, "scheduler", "Summarise", true},
		{"check-only task", "",
			map[string]string{"sender": "scheduler", "check_command": "echo due"}, "scheduler", "[check] echo due", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg, deferred := incomingMessage("iris", c.text, c.meta)
			if deferred != c.wantDeferred {
				t.Errorf("deferred = %v, want %v", deferred, c.wantDeferred)
			}
			if msg.AgentID != "iris" || msg.Sender != c.wantSender || msg.Content != c.wantContent {
				t.Errorf("msg = %+v, want sender %q content %q", msg, c.wantSender, c.wantContent)
			}
		})
	}
}

func TestGatedTaskLoggedOnlyWhenRunReplies(t *testing.T) {
	s, err := store.New(filepath.Join(t.TempDir(), "praktor.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.SaveAgent(&store.Agent{ID: "iris", Name: "iris", Workspace: "iris"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	bus, err := natsbus.NewForTest(config.NATSConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new bus: %v", err)
	}
	t.Cleanup(bus.Close)

	agents := map[string]config.AgentDefinition{"iris": {Description: "test"}}
	reg := registry.New(s, agents, config.DefaultsConfig{}, t.TempDir())
	o := NewOrchestrator(bus, nil, s, reg, config.DefaultsConfig{}, nil)

	// run tracks a gated task the way executeMessage does, then feeds the
	// runner's result back through the output handler.
	run := func(msgID, reply string) {
		t.Helper()
		meta := map[string]string{"sender": "scheduler", "check_command": "echo due"}
		msg, deferred := incomingMessage("iris", "", meta)
		if !deferred {
			t.Fatal("gated task was not deferred")
		}
		o.trackPending("iris", msgID, QueuedMessage{AgentID: "iris", Meta: meta, DeferredLog: msg})
		data, _ := json.Marshal(map[string]string{"type": "result", "content": reply, "msg_id": msgID})
		o.handleAgentOutput(&nats.Msg{Subject: "agent.iris.output", Data: data})
	}

	run("silent", "")
	msgs, err := s.GetMessages("iris", 10)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("silent run logged %d messages: %+v", len(msgs), msgs)
	}
	if _, deferred := o.popPending("silent"); deferred != nil {
		t.Fatal("silent run left its deferred entry behind")
	}

	run("reply", "⏰ Dentist")
	msgs, err = s.GetMessages("iris", 10)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(msgs), msgs)
	}
	if msgs[0].Sender != "scheduler" || msgs[0].Content != "[check] echo due" {
		t.Errorf("first message = %+v, want the task entry", msgs[0])
	}
	if msgs[1].Sender != "agent" || msgs[1].Content != "⏰ Dentist" {
		t.Errorf("second message = %+v, want the reply", msgs[1])
	}
}
