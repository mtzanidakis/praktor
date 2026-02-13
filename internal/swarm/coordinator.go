package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/nats-io/nats.go"
)

type Coordinator struct {
	bus        *natsbus.Bus
	client     *natsbus.Client
	containers *container.Manager
	store      *store.Store
}

func NewCoordinator(bus *natsbus.Bus, ctr *container.Manager, s *store.Store) *Coordinator {
	c := &Coordinator{
		bus:        bus,
		containers: ctr,
		store:      s,
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

	agentsJSON, _ := json.Marshal(req.Agents)

	run := &store.SwarmRun{
		ID:      req.ID,
		GroupID: req.LeadGroup,
		Task:    req.Task,
		Status:  "running",
		Agents:  agentsJSON,
	}

	if err := c.store.SaveSwarmRun(run); err != nil {
		return nil, fmt.Errorf("save swarm run: %w", err)
	}

	go c.executeSwarm(ctx, req, run)

	return run, nil
}

func (c *Coordinator) executeSwarm(ctx context.Context, req SwarmRequest, run *store.SwarmRun) {
	slog.Info("starting swarm", "id", req.ID, "agents", len(req.Agents))

	var (
		results []AgentResult
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	for _, agent := range req.Agents {
		wg.Add(1)
		go func(a SwarmAgent) {
			defer wg.Done()
			result := c.runSwarmAgent(ctx, req.ID, a)
			mu.Lock()
			results = append(results, result)
			mu.Unlock()
		}(agent)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		slog.Info("swarm completed", "id", req.ID)
	case <-time.After(30 * time.Minute):
		slog.Warn("swarm timed out", "id", req.ID)
	case <-ctx.Done():
		slog.Info("swarm cancelled", "id", req.ID)
	}

	// Save results
	resultsJSON, _ := json.Marshal(results)
	status := "completed"
	for _, r := range results {
		if r.Status == "error" {
			status = "failed"
			break
		}
	}
	_ = c.store.UpdateSwarmRun(req.ID, status, resultsJSON)
}

func (c *Coordinator) runSwarmAgent(ctx context.Context, swarmID string, agent SwarmAgent) AgentResult {
	groupID := fmt.Sprintf("swarm-%s-%s", swarmID[:8], agent.Role)

	result := AgentResult{
		Role:   agent.Role,
		Status: "running",
	}

	natsURL := fmt.Sprintf("nats://host.docker.internal:%d", c.bus.Port())

	_, err := c.containers.StartAgent(ctx, container.AgentOpts{
		GroupID:     groupID,
		GroupFolder: agent.GroupFolder,
		IsMain:      false,
		NATSUrl:     natsURL,
	})
	if err != nil {
		result.Status = "error"
		result.Error = err.Error()
		return result
	}

	defer c.containers.StopAgent(ctx, groupID)

	// Send task to agent
	payload := map[string]string{
		"text":     agent.Prompt,
		"groupID":  groupID,
		"swarm_id": swarmID,
	}
	data, _ := json.Marshal(payload)

	// Subscribe for result
	resultCh := make(chan string, 1)
	sub, err := c.client.Subscribe(natsbus.TopicAgentOutput(groupID), func(msg *nats.Msg) {
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
	if err := c.client.Publish(natsbus.TopicAgentInput(groupID), data); err != nil {
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

func (c *Coordinator) GetStatus(swarmID string) (*store.SwarmRun, error) {
	return c.store.GetSwarmRun(swarmID)
}
