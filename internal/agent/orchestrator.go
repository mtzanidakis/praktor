package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/schedule"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/vault"
	"github.com/nats-io/nats.go"
)

type Orchestrator struct {
	bus        *natsbus.Bus
	client     *natsbus.Client
	containers *container.Manager
	store      *store.Store
	registry   *registry.Registry
	vault      *vault.Vault
	cfg        config.DefaultsConfig
	sessions   *SessionTracker
	queues     map[string]*AgentQueue
	lastMeta   map[string]map[string]string // agentID → last message meta
	mu         sync.RWMutex
	listeners  []OutputListener
	listenerMu sync.RWMutex
}

type OutputListener func(agentID, content string, meta map[string]string)

type IPCCommand struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func NewOrchestrator(bus *natsbus.Bus, ctr *container.Manager, s *store.Store, reg *registry.Registry, cfg config.DefaultsConfig, v *vault.Vault) *Orchestrator {
	o := &Orchestrator{
		bus:        bus,
		containers: ctr,
		store:      s,
		registry:   reg,
		vault:      v,
		cfg:        cfg,
		sessions:   NewSessionTracker(),
		queues:     make(map[string]*AgentQueue),
		lastMeta:   make(map[string]map[string]string),
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

// UpdateDefaults replaces the defaults config used for new containers.
func (o *Orchestrator) UpdateDefaults(cfg config.DefaultsConfig) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.cfg = cfg
}

func (o *Orchestrator) OnOutput(listener OutputListener) {
	o.listenerMu.Lock()
	defer o.listenerMu.Unlock()
	o.listeners = append(o.listeners, listener)
}

func (o *Orchestrator) HandleMessage(ctx context.Context, agentID, text string, meta map[string]string) error {
	// Ensure agent exists
	ag, err := o.registry.Get(agentID)
	if err != nil {
		return fmt.Errorf("get agent: %w", err)
	}
	if ag == nil {
		return fmt.Errorf("agent not registered: %s", agentID)
	}

	// Save incoming message
	sender := "user"
	if s, ok := meta["sender"]; ok {
		sender = s
	}
	msg := &store.Message{
		AgentID: agentID,
		Sender:  sender,
		Content: text,
	}
	_ = o.store.SaveMessage(msg)
	o.publishMessageEvent(msg)

	// Enqueue message
	q := o.getQueue(agentID)
	q.Enqueue(QueuedMessage{
		AgentID: agentID,
		Text:    text,
		Meta:    meta,
	})

	// Process queue
	go o.processQueue(ctx, agentID)

	return nil
}

func (o *Orchestrator) getQueue(agentID string) *AgentQueue {
	o.mu.Lock()
	defer o.mu.Unlock()

	q, ok := o.queues[agentID]
	if !ok {
		q = NewAgentQueue(agentID)
		o.queues[agentID] = q
	}
	return q
}

func (o *Orchestrator) processQueue(ctx context.Context, agentID string) {
	q := o.getQueue(agentID)

	if !q.TryLock() {
		return // Already processing
	}
	defer q.Unlock()

	for {
		msg, ok := q.Dequeue()
		if !ok {
			return
		}

		if err := o.executeMessage(ctx, agentID, msg); err != nil {
			slog.Error("execute message failed", "agent", agentID, "error", err)
		}
	}
}

func (o *Orchestrator) executeMessage(ctx context.Context, agentID string, msg QueuedMessage) error {
	// Resolve agent config from registry
	def, hasDef := o.registry.GetDefinition(agentID)

	ag, err := o.registry.Get(agentID)
	if err != nil || ag == nil {
		return fmt.Errorf("get agent: %w", err)
	}

	// Ensure container is running
	info := o.containers.GetRunning(agentID)
	if info == nil {
		// Capture NATS client count before starting so we can detect when agent connects
		clientsBefore := o.bus.NumClients()
		slog.Info("starting agent", "agent", agentID, "nats_clients_before", clientsBefore)

		opts := container.AgentOpts{
			AgentID:   agentID,
			Workspace: ag.Workspace,
			Model:     o.registry.ResolveModel(agentID),
			Image:     o.registry.ResolveImage(agentID),
			NATSUrl:   o.bus.AgentNATSURL(),
		}
		if hasDef {
			opts.Env = cloneMap(def.Env)
			opts.AllowedTools = def.AllowedTools
		}

		o.resolveSecrets(&opts, agentID, def, hasDef)

		info, err = o.containers.StartAgent(ctx, opts)
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
				slog.Warn("agent ready timeout, sending anyway", "agent", agentID, "nats_clients", o.bus.NumClients())
				break waitLoop
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				current := o.bus.NumClients()
				if current > clientsBefore {
					// Give the agent a moment to set up subscriptions after connecting
					time.Sleep(500 * time.Millisecond)
					slog.Info("agent container ready", "agent", agentID, "nats_clients", current)
					break waitLoop
				}
			}
		}

		now := time.Now()
		o.sessions.Set(agentID, &Session{
			ID:          info.ID,
			AgentID:     agentID,
			ContainerID: info.ID,
			Status:      "running",
			StartedAt:   now,
			LastActive:  now,
		})
	}

	// Send message to container via NATS
	payload := map[string]string{
		"text":    msg.Text,
		"agentID": agentID,
	}
	for k, v := range msg.Meta {
		payload[k] = v
	}

	// Store meta so output handler can route responses back
	o.mu.Lock()
	o.lastMeta[agentID] = msg.Meta
	o.mu.Unlock()

	data, _ := json.Marshal(payload)
	topic := natsbus.TopicAgentInput(agentID)
	slog.Info("publishing message to agent", "agent", agentID, "topic", topic)
	if err := o.client.Publish(topic, data); err != nil {
		return fmt.Errorf("publish message: %w", err)
	}
	o.sessions.Touch(agentID)
	return o.client.Flush()
}

func (o *Orchestrator) RouteQuery(ctx context.Context, agentID string, message string) (string, error) {
	// Ensure the agent container is running
	info := o.containers.GetRunning(agentID)
	if info == nil {
		ag, err := o.registry.Get(agentID)
		if err != nil || ag == nil {
			return "", fmt.Errorf("agent not found: %s", agentID)
		}

		clientsBefore := o.bus.NumClients()
		opts := container.AgentOpts{
			AgentID:   agentID,
			Workspace: ag.Workspace,
			Model:     o.registry.ResolveModel(agentID),
			Image:     o.registry.ResolveImage(agentID),
			NATSUrl:   o.bus.AgentNATSURL(),
		}
		def, hasDef := o.registry.GetDefinition(agentID)
		if hasDef {
			opts.Env = cloneMap(def.Env)
		}

		o.resolveSecrets(&opts, agentID, def, hasDef)

		info, err = o.containers.StartAgent(ctx, opts)
		if err != nil {
			return "", fmt.Errorf("start agent for routing: %w", err)
		}

		// Wait for NATS connection
		deadline := time.After(30 * time.Second)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
	waitLoop:
		for {
			select {
			case <-deadline:
				break waitLoop
			case <-ctx.Done():
				return "", ctx.Err()
			case <-ticker.C:
				if o.bus.NumClients() > clientsBefore {
					time.Sleep(500 * time.Millisecond)
					break waitLoop
				}
			}
		}

		now := time.Now()
		o.sessions.Set(agentID, &Session{
			ID:          info.ID,
			AgentID:     agentID,
			ContainerID: info.ID,
			Status:      "running",
			StartedAt:   now,
			LastActive:  now,
		})
	}

	o.sessions.Touch(agentID)

	// Send routing query via NATS request-reply
	topic := natsbus.TopicAgentRoute(agentID)
	data, _ := json.Marshal(map[string]string{"text": message})
	resp, err := o.client.Request(topic, data, 30*time.Second)
	if err != nil {
		return "", fmt.Errorf("route query: %w", err)
	}

	var result struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		// Try plain text response
		return strings.TrimSpace(string(resp.Data)), nil
	}
	return strings.TrimSpace(result.Agent), nil
}

func (o *Orchestrator) handleAgentOutput(msg *nats.Msg) {
	// Extract agentID from topic: agent.{agentID}.output
	topic := msg.Subject
	var agentID string
	if _, err := fmt.Sscanf(topic, "agent.%s", &agentID); err != nil {
		return
	}
	// Remove trailing ".output"
	if len(agentID) > 7 && agentID[len(agentID)-7:] == ".output" {
		agentID = agentID[:len(agentID)-7]
	}

	var output struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(msg.Data, &output); err != nil {
		return
	}

	o.sessions.Touch(agentID)

	if output.Type == "result" {
		content := o.redactSecrets(agentID, output.Content)

		agentMsg := &store.Message{
			AgentID: agentID,
			Sender:  "agent",
			Content: content,
		}
		_ = o.store.SaveMessage(agentMsg)
		o.publishMessageEvent(agentMsg)

		// Get metadata from the latest queued message for this agent
		meta := o.getLastMeta(agentID)

		o.listenerMu.RLock()
		for _, l := range o.listeners {
			l(agentID, content, meta)
		}
		o.listenerMu.RUnlock()
	}
}

func (o *Orchestrator) getLastMeta(agentID string) map[string]string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.lastMeta[agentID]
}

func (o *Orchestrator) handleIPC(msg *nats.Msg) {
	var cmd IPCCommand
	if err := json.Unmarshal(msg.Data, &cmd); err != nil {
		slog.Warn("invalid IPC command", "error", err)
		o.respondIPC(msg, map[string]any{"error": "invalid command"})
		return
	}

	// Extract agentID from subject: host.ipc.{agentID}
	agentID := msg.Subject
	if idx := len("host.ipc."); idx < len(agentID) {
		agentID = agentID[idx:]
	}

	slog.Info("IPC command received", "type", cmd.Type, "agent", agentID)

	switch cmd.Type {
	case "create_task":
		o.ipcCreateTask(msg, agentID, cmd.Payload)
	case "list_tasks":
		o.ipcListTasks(msg, agentID)
	case "delete_task":
		o.ipcDeleteTask(msg, cmd.Payload)
	case "read_user_md":
		o.ipcReadUserMD(msg)
	case "update_user_md":
		o.ipcUpdateUserMD(msg, cmd.Payload)
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

func (o *Orchestrator) ipcCreateTask(msg *nats.Msg, agentID string, payload json.RawMessage) {
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
		AgentID:     agentID,
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

	slog.Info("task created via IPC", "id", t.ID, "name", t.Name, "agent", agentID)
	o.respondIPC(msg, map[string]any{"ok": true, "id": t.ID})
}

func (o *Orchestrator) ipcListTasks(msg *nats.Msg, agentID string) {
	tasks, err := o.store.ListTasksForAgent(agentID)
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

func (o *Orchestrator) ipcReadUserMD(msg *nats.Msg) {
	content, err := o.registry.GetUserMD()
	if err != nil {
		o.respondIPC(msg, map[string]any{"error": fmt.Sprintf("read failed: %v", err)})
		return
	}
	o.respondIPC(msg, map[string]any{"ok": true, "content": content})
}

func (o *Orchestrator) ipcUpdateUserMD(msg *nats.Msg, payload json.RawMessage) {
	var req struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		o.respondIPC(msg, map[string]any{"error": "invalid payload"})
		return
	}
	if err := o.registry.SaveUserMD(req.Content); err != nil {
		o.respondIPC(msg, map[string]any{"error": fmt.Sprintf("save failed: %v", err)})
		return
	}
	slog.Info("user profile updated via IPC")
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
		"agent_id":  msg.AgentID,
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

	topic := natsbus.TopicEventsAgent(msg.AgentID)
	_ = o.client.Publish(topic, data)
}

func (o *Orchestrator) StopAgent(ctx context.Context, agentID string) error {
	o.sessions.Remove(agentID)
	return o.containers.StopAgent(ctx, agentID)
}

func (o *Orchestrator) StartIdleReaper(ctx context.Context) {
	if o.cfg.IdleTimeout == 0 {
		return
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			idle := o.sessions.ListIdle(o.cfg.IdleTimeout)
			for _, agentID := range idle {
				slog.Info("stopping idle agent", "agent", agentID, "timeout", o.cfg.IdleTimeout)
				if err := o.StopAgent(ctx, agentID); err != nil {
					slog.Error("failed to stop idle agent", "agent", agentID, "error", err)
					continue
				}
				o.publishIdleStopEvent(agentID)
			}
		}
	}
}

func (o *Orchestrator) publishIdleStopEvent(agentID string) {
	if o.client == nil {
		return
	}

	event := map[string]any{
		"type":      "agent_stopped",
		"agent_id":  agentID,
		"reason":    "idle_timeout",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	_ = o.client.Publish(natsbus.TopicEventsAgent(agentID), data)
}

func (o *Orchestrator) ListRunning(ctx context.Context) ([]container.ContainerInfo, error) {
	return o.containers.ListRunning(ctx)
}

func (o *Orchestrator) ReadVolumeFile(ctx context.Context, workspace, filePath, image string) (string, error) {
	return o.containers.ReadVolumeFile(ctx, workspace, filePath, image)
}

func (o *Orchestrator) WriteVolumeFile(ctx context.Context, workspace, filePath, content, image string) error {
	return o.containers.WriteVolumeFile(ctx, workspace, filePath, content, image)
}
