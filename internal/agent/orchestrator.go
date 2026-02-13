package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/groups"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/nats-io/nats.go"
)

type Orchestrator struct {
	bus        *natsbus.Bus
	client     *natsbus.Client
	containers *container.Manager
	store      *store.Store
	groups     *groups.Manager
	cfg        config.AgentConfig
	queues     map[string]*GroupQueue
	mu         sync.RWMutex
	listeners  []OutputListener
	listenerMu sync.RWMutex
}

type OutputListener func(groupID, content string)

type IPCCommand struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func NewOrchestrator(bus *natsbus.Bus, ctr *container.Manager, s *store.Store, grp *groups.Manager, cfg config.AgentConfig) *Orchestrator {
	o := &Orchestrator{
		bus:        bus,
		containers: ctr,
		store:      s,
		groups:     grp,
		cfg:        cfg,
		queues:     make(map[string]*GroupQueue),
	}

	client, err := natsbus.NewClient(bus)
	if err != nil {
		slog.Error("orchestrator nats client failed", "error", err)
		return o
	}
	o.client = client

	// Subscribe to all agent output
	_, _ = client.Subscribe("agent.*.output", func(msg *nats.Msg) {
		o.handleAgentOutput(msg)
	})

	// Subscribe to all IPC commands
	_, _ = client.Subscribe("host.ipc.*", func(msg *nats.Msg) {
		o.handleIPC(msg)
	})

	return o
}

func (o *Orchestrator) OnOutput(listener OutputListener) {
	o.listenerMu.Lock()
	defer o.listenerMu.Unlock()
	o.listeners = append(o.listeners, listener)
}

func (o *Orchestrator) HandleMessage(ctx context.Context, groupID, text string, meta map[string]string) error {
	// Ensure group exists
	grp, err := o.groups.Get(groupID)
	if err != nil {
		return fmt.Errorf("get group: %w", err)
	}
	if grp == nil {
		return fmt.Errorf("group not registered: %s", groupID)
	}

	// Save incoming message
	sender := "user"
	if s, ok := meta["sender"]; ok {
		sender = s
	}
	_ = o.store.SaveMessage(&store.Message{
		GroupID: groupID,
		Sender:  sender,
		Content: text,
	})

	// Enqueue message
	q := o.getQueue(groupID)
	q.Enqueue(QueuedMessage{
		GroupID: groupID,
		Text:    text,
		Meta:    meta,
	})

	// Process queue
	go o.processQueue(ctx, groupID)

	return nil
}

func (o *Orchestrator) getQueue(groupID string) *GroupQueue {
	o.mu.Lock()
	defer o.mu.Unlock()

	q, ok := o.queues[groupID]
	if !ok {
		q = NewGroupQueue(groupID)
		o.queues[groupID] = q
	}
	return q
}

func (o *Orchestrator) processQueue(ctx context.Context, groupID string) {
	q := o.getQueue(groupID)

	if !q.TryLock() {
		return // Already processing
	}
	defer q.Unlock()

	for {
		msg, ok := q.Dequeue()
		if !ok {
			return
		}

		if err := o.executeMessage(ctx, groupID, msg); err != nil {
			slog.Error("execute message failed", "group", groupID, "error", err)
		}
	}
}

func (o *Orchestrator) executeMessage(ctx context.Context, groupID string, msg QueuedMessage) error {
	grp, err := o.groups.Get(groupID)
	if err != nil || grp == nil {
		return fmt.Errorf("get group: %w", err)
	}

	// Ensure container is running
	info := o.containers.GetRunning(groupID)
	if info == nil {
		natsURL := fmt.Sprintf("nats://host.docker.internal:%d", o.bus.Port())

		info, err = o.containers.StartAgent(ctx, container.AgentOpts{
			GroupID:     groupID,
			GroupFolder: grp.Folder,
			IsMain:      grp.IsMain,
			NATSUrl:     natsURL,
		})
		if err != nil {
			return fmt.Errorf("start agent: %w", err)
		}
	}

	// Send message to container via NATS
	payload := map[string]string{
		"text":    msg.Text,
		"groupID": groupID,
	}
	for k, v := range msg.Meta {
		payload[k] = v
	}

	data, _ := json.Marshal(payload)
	return o.client.Publish(natsbus.TopicAgentInput(groupID), data)
}

func (o *Orchestrator) handleAgentOutput(msg *nats.Msg) {
	// Extract groupID from topic: agent.{groupID}.output
	topic := msg.Subject
	var groupID string
	if _, err := fmt.Sscanf(topic, "agent.%s", &groupID); err != nil {
		return
	}
	// Remove trailing ".output"
	if len(groupID) > 7 && groupID[len(groupID)-7:] == ".output" {
		groupID = groupID[:len(groupID)-7]
	}

	var output struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		return
	}

	if output.Type == "result" || output.Type == "text" {
		_ = o.store.SaveMessage(&store.Message{
			GroupID: groupID,
			Sender:  "agent",
			Content: output.Content,
		})

		o.listenerMu.RLock()
		for _, l := range o.listeners {
			l(groupID, output.Content)
		}
		o.listenerMu.RUnlock()
	}
}

func (o *Orchestrator) handleIPC(msg *nats.Msg) {
	var cmd IPCCommand
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		slog.Warn("invalid IPC command", "error", err)
		return
	}

	slog.Info("IPC command received", "type", cmd.Type)
	// IPC commands handled in later phases
}

func (o *Orchestrator) StopAgent(ctx context.Context, groupID string) error {
	return o.containers.StopAgent(ctx, groupID)
}

func (o *Orchestrator) ListRunning(ctx context.Context) ([]container.ContainerInfo, error) {
	return o.containers.ListRunning(ctx)
}
