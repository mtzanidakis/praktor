package natsbus

import (
	"testing"
	"time"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/nats-io/nats.go"
)

func TestBusStartStop(t *testing.T) {
	dir := t.TempDir()
	bus, err := New(config.NATSConfig{
		Port:    0, // Random port
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}
	defer bus.Close()

	url := bus.ClientURL()
	if url == "" {
		t.Fatal("expected non-empty client URL")
	}
}

func TestPubSub(t *testing.T) {
	dir := t.TempDir()
	bus, err := New(config.NATSConfig{
		Port:    0,
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}
	defer bus.Close()

	client, err := NewClient(bus)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	received := make(chan string, 1)
	_, err = client.Subscribe("test.topic", func(msg *nats.Msg) {
		received <- string(msg.Data)
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	if err := client.Publish("test.topic", []byte("hello")); err != nil {
		t.Fatalf("publish error: %v", err)
	}
	client.Flush()

	select {
	case data := <-received:
		if data != "hello" {
			t.Errorf("expected 'hello', got '%s'", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestPublishJSON(t *testing.T) {
	dir := t.TempDir()
	bus, err := New(config.NATSConfig{
		Port:    0,
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("failed to create bus: %v", err)
	}
	defer bus.Close()

	client, err := NewClient(bus)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	received := make(chan string, 1)
	_, err = client.Subscribe("test.json", func(msg *nats.Msg) {
		received <- string(msg.Data)
	})
	if err != nil {
		t.Fatalf("subscribe error: %v", err)
	}

	payload := map[string]string{"key": "value"}
	if err := client.PublishJSON("test.json", payload); err != nil {
		t.Fatalf("publish json error: %v", err)
	}
	client.Flush()

	select {
	case data := <-received:
		if data != `{"key":"value"}` {
			t.Errorf("expected json, got '%s'", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestTopicNames(t *testing.T) {
	if got := TopicAgentInput("g1"); got != "agent.g1.input" {
		t.Errorf("expected agent.g1.input, got %s", got)
	}
	if got := TopicAgentOutput("g1"); got != "agent.g1.output" {
		t.Errorf("expected agent.g1.output, got %s", got)
	}
	if got := TopicIPC("g1"); got != "host.ipc.g1" {
		t.Errorf("expected host.ipc.g1, got %s", got)
	}
}
