package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/groups"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/schedule"
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
	msg := &store.Message{
		GroupID: groupID,
		Sender:  sender,
		Content: text,
	}
	_ = o.store.SaveMessage(msg)
	o.publishMessageEvent(msg)

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
		// Capture NATS client count before starting so we can detect when agent connects
		clientsBefore := o.bus.NumClients()
		slog.Info("starting agent", "group", groupID, "nats_clients_before", clientsBefore)

		info, err = o.containers.StartAgent(ctx, container.AgentOpts{
			GroupID:     groupID,
			GroupFolder: grp.Folder,
			IsMain:      grp.IsMain,
			Model:       grp.Model,
			NATSUrl:     o.bus.AgentNATSURL(),
		})
		if err != nil {
			return fmt.Errorf("start agent: %w", err)
		}

		// Wait for agent to connect to NATS by watching client count
		deadline := time.After(30 * time.Second)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()

	waitLoop:
		for {
			select {
			case <-deadline:
				slog.Warn("agent ready timeout, sending anyway", "group", groupID, "nats_clients", o.bus.NumClients())
				break waitLoop
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				current := o.bus.NumClients()
				if current > clientsBefore {
					// Give the agent a moment to set up subscriptions after connecting
					time.Sleep(500 * time.Millisecond)
					slog.Info("agent container ready", "group", groupID, "nats_clients", current)
					break waitLoop
				}
			}
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
	topic := natsbus.TopicAgentInput(groupID)
	slog.Info("publishing message to agent", "group", groupID, "topic", topic)
	if err := o.client.Publish(topic, data); err != nil {
		return fmt.Errorf("publish message: %w", err)
	}
	return o.client.Flush()
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

	if output.Type == "result" {
		// Only forward the final result to Telegram; "text" events are
		// intermediate streaming chunks that are part of the same response.
		agentMsg := &store.Message{
			GroupID: groupID,
			Sender:  "agent",
			Content: output.Content,
		}
		_ = o.store.SaveMessage(agentMsg)
		o.publishMessageEvent(agentMsg)

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
		o.respondIPC(msg, map[string]any{"error": "invalid command"})
		return
	}

	// Extract groupID from subject: host.ipc.{groupID}
	groupID := msg.Subject
	if idx := len("host.ipc."); idx < len(groupID) {
		groupID = groupID[idx:]
	}

	slog.Info("IPC command received", "type", cmd.Type, "group", groupID)

	switch cmd.Type {
	case "create_task":
		o.ipcCreateTask(msg, groupID, cmd.Payload)
	case "list_tasks":
		o.ipcListTasks(msg, groupID)
	case "delete_task":
		o.ipcDeleteTask(msg, cmd.Payload)
	default:
		slog.Warn("unknown IPC command", "type", cmd.Type)
		o.respondIPC(msg, map[string]any{"error": "unknown command: " + cmd.Type})
	}
}

func (o *Orchestrator) respondIPC(msg *nats.Msg, data any) {
	resp, err := json.Marshal(data)
	if err != nil {
		slog.Error("failed to marshal IPC response", "error", err)
		return
	}
	if err := msg.Respond(resp); err != nil {
		slog.Error("failed to respond to IPC", "error", err)
	}
}

func (o *Orchestrator) ipcCreateTask(msg *nats.Msg, groupID string, payload json.RawMessage) {
	var req struct {
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		o.respondIPC(msg, map[string]any{"error": "invalid payload"})
		return
	}
	if req.Name == "" || req.Schedule == "" || req.Prompt == "" {
		o.respondIPC(msg, map[string]any{"error": "name, schedule, and prompt are required"})
		return
	}

	normalized, err := schedule.NormalizeSchedule(req.Schedule)
	if err != nil {
		o.respondIPC(msg, map[string]any{"error": fmt.Sprintf("invalid schedule: %v", err)})
		return
	}

	t := &store.ScheduledTask{
		ID:          uuid.New().String(),
		GroupID:     groupID,
		Name:        req.Name,
		Schedule:    normalized,
		Prompt:      req.Prompt,
		ContextMode: "isolated",
		Status:      "active",
		NextRunAt:   schedule.CalculateNextRun(normalized),
	}

	if err := o.store.SaveTask(t); err != nil {
		o.respondIPC(msg, map[string]any{"error": fmt.Sprintf("save failed: %v", err)})
		return
	}

	slog.Info("task created via IPC", "id", t.ID, "name", t.Name, "group", groupID)
	o.respondIPC(msg, map[string]any{"ok": true, "id": t.ID})
}

func (o *Orchestrator) ipcListTasks(msg *nats.Msg, groupID string) {
	tasks, err := o.store.ListTasksForGroup(groupID)
	if err != nil {
		o.respondIPC(msg, map[string]any{"error": fmt.Sprintf("list failed: %v", err)})
		return
	}

	type taskEntry struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
		Status   string `json:"status"`
	}
	out := make([]taskEntry, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskEntry{
			ID:       t.ID,
			Name:     t.Name,
			Schedule: t.Schedule,
			Prompt:   t.Prompt,
			Status:   t.Status,
		})
	}
	o.respondIPC(msg, map[string]any{"ok": true, "tasks": out})
}

func (o *Orchestrator) ipcDeleteTask(msg *nats.Msg, payload json.RawMessage) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil || req.ID == "" {
		o.respondIPC(msg, map[string]any{"error": "id is required"})
		return
	}
	if err := o.store.DeleteTask(req.ID); err != nil {
		o.respondIPC(msg, map[string]any{"error": fmt.Sprintf("delete failed: %v", err)})
		return
	}
	slog.Info("task deleted via IPC", "id", req.ID)
	o.respondIPC(msg, map[string]any{"ok": true})
}

func (o *Orchestrator) publishMessageEvent(msg *store.Message) {
	if o.client == nil {
		return
	}

	role := "user"
	if msg.Sender == "agent" {
		role = "assistant"
	}

	now := time.Now()
	timeStr := msg.CreatedAt.Format("15:04")
	if msg.CreatedAt.IsZero() {
		timeStr = now.Format("15:04")
	}

	event := map[string]any{
		"type":      "message",
		"group_id":  msg.GroupID,
		"timestamp": now.UTC().Format(time.RFC3339),
		"data": map[string]any{
			"id":   msg.ID,
			"role": role,
			"text": msg.Content,
			"time": timeStr,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	topic := natsbus.TopicEventsAgent(msg.GroupID)
	_ = o.client.Publish(topic, data)
}

func (o *Orchestrator) StopAgent(ctx context.Context, groupID string) error {
	return o.containers.StopAgent(ctx, groupID)
}

func (o *Orchestrator) ListRunning(ctx context.Context) ([]container.ContainerInfo, error) {
	return o.containers.ListRunning(ctx)
}
