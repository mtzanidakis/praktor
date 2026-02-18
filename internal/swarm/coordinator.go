package swarm

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
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/vault"
	"github.com/nats-io/nats.go"
)

// SwarmMembership tracks a container's participation in a swarm.
type SwarmMembership struct {
	SwarmID   string
	GroupID   string
	ChatTopic string
}

type Coordinator struct {
	bus        *natsbus.Bus
	client     *natsbus.Client
	containers *container.Manager
	store      *store.Store
	registry   *registry.Registry
	vault      *vault.Vault

	swarmMembers map[string]SwarmMembership // containerAgentID -> membership
	membersMu    sync.RWMutex
}

func NewCoordinator(bus *natsbus.Bus, ctr *container.Manager, s *store.Store, reg *registry.Registry, v *vault.Vault) *Coordinator {
	c := &Coordinator{
		bus:          bus,
		containers:   ctr,
		store:        s,
		registry:     reg,
		vault:        v,
		swarmMembers: make(map[string]SwarmMembership),
	}

	client, err := natsbus.NewClient(bus)
	if err != nil {
		slog.Error("swarm coordinator nats client failed", "error", err)
		return c
	}
	c.client = client

	return c
}

func (c *Coordinator) RunSwarm(ctx context.Context, req SwarmRequest) (*store.SwarmRun, error) {
	if req.ID == "" {
		req.ID = uuid.New().String()
	}

	// Validate graph
	_, err := BuildPlan(req.Agents, req.Synapses, req.LeadAgent)
	if err != nil {
		return nil, fmt.Errorf("invalid swarm graph: %w", err)
	}

	agentsJSON, _ := json.Marshal(req.Agents)
	synapsesJSON, _ := json.Marshal(req.Synapses)

	run := &store.SwarmRun{
		ID:        req.ID,
		Name:      req.Name,
		AgentID:   req.LeadAgent,
		LeadAgent: req.LeadAgent,
		Task:      req.Task,
		Status:    "running",
		Agents:    agentsJSON,
		Synapses:  synapsesJSON,
	}

	if err := c.store.SaveSwarmRun(run); err != nil {
		return nil, fmt.Errorf("save swarm run: %w", err)
	}

	c.publishEvent(req.ID, "swarm_started", map[string]any{
		"name":   req.Name,
		"agents": len(req.Agents),
	})

	// Use a background context so the swarm outlives the HTTP request.
	go c.executeSwarm(context.Background(), req, run)

	return run, nil
}

func (c *Coordinator) executeSwarm(ctx context.Context, req SwarmRequest, run *store.SwarmRun) {
	slog.Info("starting swarm", "id", req.ID, "agents", len(req.Agents), "synapses", len(req.Synapses))

	plan, err := BuildPlan(req.Agents, req.Synapses, req.LeadAgent)
	if err != nil {
		slog.Error("swarm plan failed", "id", req.ID, "error", err)
		_ = c.store.UpdateSwarmRun(req.ID, "failed", nil)
		c.publishEvent(req.ID, "swarm_failed", map[string]any{"error": err.Error()})
		return
	}

	// Build role -> agent lookup
	roleAgent := make(map[string]SwarmAgent, len(req.Agents))
	for _, a := range req.Agents {
		roleAgent[a.Role] = a
	}

	// Build collab group lookup: role -> groupID
	collabGroupID := make(map[string]string)
	for i, group := range plan.CollabGroups {
		groupID := fmt.Sprintf("group-%d", i)
		for _, role := range group {
			collabGroupID[role] = groupID
		}
	}

	// Collect results per role
	results := make(map[string]AgentResult)
	var resultsMu sync.Mutex

	allOK := true
	for tierIdx, tier := range plan.Tiers {
		slog.Info("executing tier", "swarm", req.ID, "tier", tierIdx, "agents", tier.Agents)

		var wg sync.WaitGroup
		for _, role := range tier.Agents {
			wg.Add(1)
			go func(role string) {
				defer wg.Done()
				agent := roleAgent[role]

				// Build prompt with pipeline context
				prompt := buildAgentPrompt(agent, req.Task, role, plan, results, &resultsMu, req.LeadAgent)

				// Determine collab chat topic
				var chatTopic string
				if gid, ok := collabGroupID[role]; ok {
					chatTopic = natsbus.TopicSwarmChat(req.ID, gid)
				}

				result := c.runSwarmAgent(ctx, req.ID, agent, prompt, chatTopic)

				resultsMu.Lock()
				results[role] = result
				resultsMu.Unlock()

				c.publishEvent(req.ID, "swarm_agent_completed", map[string]any{
					"role":   role,
					"status": result.Status,
					"output": truncate(result.Output, 200),
				})

				if result.Status == "error" {
					allOK = false
				}
			}(role)
		}

		// Wait with timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			slog.Info("tier completed", "swarm", req.ID, "tier", tierIdx)
			c.publishEvent(req.ID, "swarm_tier_completed", map[string]any{
				"tier":  tierIdx,
				"total": len(plan.Tiers),
			})
		case <-time.After(30 * time.Minute):
			slog.Warn("swarm tier timed out", "swarm", req.ID, "tier", tierIdx)
			allOK = false
		case <-ctx.Done():
			slog.Info("swarm cancelled", "swarm", req.ID)
			allOK = false
		}

		if !allOK {
			break
		}
	}

	// Collect all results in order
	var allResults []AgentResult
	for _, a := range req.Agents {
		if r, ok := results[a.Role]; ok {
			allResults = append(allResults, r)
		}
	}

	resultsJSON, _ := json.Marshal(allResults)
	status := "completed"
	if !allOK {
		status = "failed"
	}
	_ = c.store.UpdateSwarmRun(req.ID, status, resultsJSON)

	c.publishEvent(req.ID, "swarm_"+status, map[string]any{
		"results_count": len(allResults),
	})

	slog.Info("swarm finished", "id", req.ID, "status", status)
}

func buildAgentPrompt(agent SwarmAgent, task, role string, plan *ExecutionPlan, results map[string]AgentResult, mu *sync.Mutex, leadAgent string) string {
	var sb strings.Builder

	// Base: swarm task
	sb.WriteString("## Swarm Task\n\n")
	sb.WriteString(task)
	sb.WriteString("\n\n")

	// Agent-specific prompt
	if agent.Prompt != "" {
		sb.WriteString("## Your Role Instructions\n\n")
		sb.WriteString(agent.Prompt)
		sb.WriteString("\n\n")
	}

	// Pipeline context: include outputs from predecessors
	if preds, ok := plan.PipelineInputs[role]; ok && len(preds) > 0 {
		mu.Lock()
		sb.WriteString("## Context from Previous Agents\n\n")
		for _, pred := range preds {
			if r, ok := results[pred]; ok && r.Output != "" {
				fmt.Fprintf(&sb, "### Output from %s\n\n%s\n\n", pred, r.Output)
			}
		}
		mu.Unlock()
	}

	// Lead agent synthesis prompt
	if role == leadAgent {
		mu.Lock()
		hasResults := false
		for _, r := range results {
			if r.Output != "" {
				hasResults = true
				break
			}
		}
		if hasResults {
			sb.WriteString("## Results from All Agents\n\nSynthesize the following results into a cohesive response:\n\n")
			for r, res := range results {
				if r != role && res.Output != "" {
					fmt.Fprintf(&sb, "### %s\n\n%s\n\n", r, res.Output)
				}
			}
		}
		mu.Unlock()
	}

	return sb.String()
}

func (c *Coordinator) runSwarmAgent(ctx context.Context, swarmID string, agent SwarmAgent, prompt, chatTopic string) AgentResult {
	agentID := fmt.Sprintf("swarm-%s-%s", swarmID[:8], agent.Role)

	result := AgentResult{
		Role:   agent.Role,
		Status: "running",
	}

	c.publishEvent(swarmID, "swarm_agent_started", map[string]any{
		"role":     agent.Role,
		"agent_id": agentID,
	})

	// Resolve agent config from registry
	opts := container.AgentOpts{
		AgentID:   agentID,
		Workspace: agent.Workspace,
		NATSUrl:   c.bus.AgentNATSURL(),
		Env:       make(map[string]string),
	}

	if agent.AgentID != "" {
		opts.Model = c.registry.ResolveModel(agent.AgentID)
		opts.Image = c.registry.ResolveImage(agent.AgentID)
		if def, hasDef := c.registry.GetDefinition(agent.AgentID); hasDef {
			for k, v := range def.Env {
				opts.Env[k] = v
			}
			opts.AllowedTools = def.AllowedTools
			c.resolveSecrets(&opts, agent.AgentID, def)
		}
	}

	// Swarm-specific env vars
	opts.Env["SWARM_ID"] = swarmID
	opts.Env["SWARM_ROLE"] = agent.Role
	if chatTopic != "" {
		opts.Env["SWARM_CHAT_TOPIC"] = chatTopic
	}

	// Wait for NATS connect (mirror orchestrator pattern)
	clientsBefore := c.bus.NumClients()

	_, err := c.containers.StartAgent(ctx, opts)
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}
	defer c.containers.StopAgent(ctx, agentID)

	// Register swarm membership
	if chatTopic != "" {
		c.membersMu.Lock()
		c.swarmMembers[agentID] = SwarmMembership{
			SwarmID:   swarmID,
			GroupID:   chatTopic,
			ChatTopic: chatTopic,
		}
		c.membersMu.Unlock()
		defer func() {
			c.membersMu.Lock()
			delete(c.swarmMembers, agentID)
			c.membersMu.Unlock()
		}()
	}

	// Wait for NATS connection
	deadline := time.After(30 * time.Second)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

waitLoop:
	for {
		select {
		case <-deadline:
			slog.Warn("swarm agent ready timeout", "agent", agentID)
			break waitLoop
		case <-ctx.Done():
			result.Status = "error"
			result.Error = "cancelled"
			return result
		case <-ticker.C:
			if c.bus.NumClients() > clientsBefore {
				time.Sleep(500 * time.Millisecond)
				break waitLoop
			}
		}
	}

	// Subscribe for result
	resultCh := make(chan string, 1)
	sub, err := c.client.Subscribe(natsbus.TopicAgentOutput(agentID), func(msg *nats.Msg) {
		var output struct {
			Type    string `json:"type"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(msg.Data, &output); err == nil && output.Type == "result" {
			resultCh <- output.Content
		}
	})
	if err != nil {
		result.Status = "error"
		result.Error = "failed to subscribe for results"
		return result
	}
	defer sub.Unsubscribe()

	// Send prompt
	payload := map[string]string{
		"text":     prompt,
		"agentID":  agentID,
		"swarm_id": swarmID,
	}
	data, _ := json.Marshal(payload)
	if err := c.client.Publish(natsbus.TopicAgentInput(agentID), data); err != nil {
		result.Status = "error"
		result.Error = "failed to send prompt"
		return result
	}

	// Wait for result
	select {
	case output := <-resultCh:
		result.Status = "completed"
		result.Output = output
	case <-time.After(15 * time.Minute):
		result.Status = "error"
		result.Error = "agent timed out"
	case <-ctx.Done():
		result.Status = "error"
		result.Error = "cancelled"
	}

	return result
}

// resolveSecrets resolves secret:name references in env vars and prepares
// file secrets for the container. Mirrors orchestrator's resolveSecrets pattern.
func (c *Coordinator) resolveSecrets(opts *container.AgentOpts, agentID string, def config.AgentDefinition) {
	if c.vault == nil {
		return
	}

	for k, v := range opts.Env {
		if !strings.HasPrefix(v, "secret:") {
			continue
		}
		secretName := strings.TrimPrefix(v, "secret:")
		sec, err := c.store.GetSecret(secretName)
		if err != nil || sec == nil {
			slog.Warn("swarm: failed to resolve env secret", "agent", agentID, "env", k, "secret", secretName)
			delete(opts.Env, k)
			continue
		}
		plaintext, err := c.vault.Decrypt(sec.Value, sec.Nonce)
		if err != nil {
			slog.Warn("swarm: failed to decrypt secret", "agent", agentID, "secret", secretName, "error", err)
			delete(opts.Env, k)
			continue
		}
		opts.Env[k] = string(plaintext)
	}

	for _, fm := range def.Files {
		sec, err := c.store.GetSecret(fm.Secret)
		if err != nil || sec == nil {
			slog.Warn("swarm: failed to resolve file secret", "agent", agentID, "secret", fm.Secret)
			continue
		}
		plaintext, err := c.vault.Decrypt(sec.Value, sec.Nonce)
		if err != nil {
			slog.Warn("swarm: failed to decrypt file secret", "agent", agentID, "secret", fm.Secret, "error", err)
			continue
		}
		mode := int64(0o600)
		opts.SecretFiles = append(opts.SecretFiles, container.SecretFile{
			Content: plaintext,
			Target:  fm.Target,
			Mode:    mode,
		})
	}
}

// GetSwarmChatTopic returns the swarm ID and chat topic for a container agent ID.
func (c *Coordinator) GetSwarmChatTopic(containerAgentID string) (swarmID, chatTopic string, ok bool) {
	c.membersMu.RLock()
	defer c.membersMu.RUnlock()
	m, exists := c.swarmMembers[containerAgentID]
	if !exists {
		return "", "", false
	}
	return m.SwarmID, m.ChatTopic, true
}

func (c *Coordinator) GetStatus(swarmID string) (*store.SwarmRun, error) {
	return c.store.GetSwarmRun(swarmID)
}

func (c *Coordinator) publishEvent(swarmID, eventType string, data map[string]any) {
	if c.client == nil {
		return
	}

	event := map[string]any{
		"type":      eventType,
		"swarm_id":  swarmID,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data":      data,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = c.client.Publish(natsbus.TopicEventsSwarmID(swarmID), payload)
}

// PublishSwarmChat publishes a message to a swarm collaborative chat topic.
func (c *Coordinator) PublishSwarmChat(topic, from, content string) error {
	if c.client == nil {
		return fmt.Errorf("no NATS client")
	}
	msg := map[string]string{
		"from":    from,
		"content": content,
	}
	data, _ := json.Marshal(msg)
	return c.client.Publish(topic, data)
}


func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
