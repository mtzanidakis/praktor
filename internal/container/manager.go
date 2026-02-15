package container

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
)

const (
	labelPrefix = "praktor"
	networkName = "praktor-net"
)

type Manager struct {
	docker      *client.Client
	bus         *natsbus.Bus
	cfg         config.AgentConfig
	mu          sync.RWMutex
	active      map[string]*ContainerInfo // groupID → container
	networkName string                    // resolved network name
}

type ContainerInfo struct {
	ID        string    `json:"id"`
	GroupID   string    `json:"group_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	SessionID string    `json:"session_id"`
}

type AgentOpts struct {
	GroupID     string
	GroupFolder string
	IsMain      bool
	Model       string
	SessionID   string
	Mounts      []Mount
	NATSUrl     string
}

func NewManager(bus *natsbus.Bus, cfg config.AgentConfig) (*Manager, error) {
	docker, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}

	return &Manager{
		docker: docker,
		bus:    bus,
		cfg:    cfg,
		active: make(map[string]*ContainerInfo),
	}, nil
}

func (m *Manager) ensureNetwork(ctx context.Context) error {
	if m.networkName != "" {
		return nil
	}

	_, err := m.docker.NetworkInspect(ctx, networkName, network.InspectOptions{})
	if err == nil {
		m.networkName = networkName
		return nil
	}

	// Create it (for non-Compose runs like make dev)
	_, err = m.docker.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("create network %s: %w", networkName, err)
	}
	m.networkName = networkName
	slog.Info("created docker network", "network", networkName)
	return nil
}

func (m *Manager) StartAgent(ctx context.Context, opts AgentOpts) (*ContainerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.active[opts.GroupID]; ok {
		return existing, nil
	}

	if len(m.active) >= m.cfg.MaxContainers {
		return nil, fmt.Errorf("max containers (%d) reached", m.cfg.MaxContainers)
	}

	if err := m.ensureNetwork(ctx); err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("praktor-agent-%s", opts.GroupID)

	// Remove any stale container with the same name
	timeout := 5
	_ = m.docker.ContainerStop(ctx, containerName, dockercontainer.StopOptions{Timeout: &timeout})
	_ = m.docker.ContainerRemove(ctx, containerName, dockercontainer.RemoveOptions{Force: true})

	env := []string{
		fmt.Sprintf("NATS_URL=%s", opts.NATSUrl),
		fmt.Sprintf("GROUP_ID=%s", opts.GroupID),
		fmt.Sprintf("IS_MAIN=%t", opts.IsMain),
	}
	if opts.SessionID != "" {
		env = append(env, fmt.Sprintf("SESSION_ID=%s", opts.SessionID))
	}
	if m.cfg.AnthropicAPIKey != "" {
		env = append(env, fmt.Sprintf("ANTHROPIC_API_KEY=%s", m.cfg.AnthropicAPIKey))
	}
	if m.cfg.OAuthToken != "" {
		env = append(env, fmt.Sprintf("CLAUDE_CODE_OAUTH_TOKEN=%s", m.cfg.OAuthToken))
	}
	if model := opts.Model; model != "" {
		env = append(env, fmt.Sprintf("CLAUDE_MODEL=%s", model))
	} else if m.cfg.Model != "" {
		env = append(env, fmt.Sprintf("CLAUDE_MODEL=%s", m.cfg.Model))
	}
	if tz := os.Getenv("TZ"); tz != "" {
		env = append(env, fmt.Sprintf("TZ=%s", tz))
	}

	mounts := buildMounts(opts)

	containerCfg := &dockercontainer.Config{
		Image:  m.cfg.Image,
		Env:    env,
		Labels: map[string]string{labelPrefix + ".managed": "true", labelPrefix + ".group": opts.GroupID},
	}

	hostCfg := &dockercontainer.HostConfig{
		Binds:       mounts,
		NetworkMode: dockercontainer.NetworkMode(m.networkName),
	}

	networkCfg := &network.NetworkingConfig{}

	resp, err := m.docker.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, containerName)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	if err := m.docker.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	info := &ContainerInfo{
		ID:        resp.ID,
		GroupID:   opts.GroupID,
		Name:      containerName,
		Status:    "running",
		StartedAt: time.Now(),
		SessionID: opts.SessionID,
	}
	m.active[opts.GroupID] = info

	slog.Info("agent container started", "group", opts.GroupID, "container", resp.ID[:12])
	return info, nil
}

func (m *Manager) StopAgent(ctx context.Context, groupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.active[groupID]
	if !ok {
		return nil
	}

	timeout := 10
	if err := m.docker.ContainerStop(ctx, info.ID, dockercontainer.StopOptions{Timeout: &timeout}); err != nil {
		slog.Warn("failed to stop container gracefully", "container", info.ID[:12], "error", err)
	}

	if err := m.docker.ContainerRemove(ctx, info.ID, dockercontainer.RemoveOptions{Force: true}); err != nil {
		slog.Warn("failed to remove container", "container", info.ID[:12], "error", err)
	}

	delete(m.active, groupID)
	slog.Info("agent container stopped", "group", groupID)
	return nil
}

func (m *Manager) StopAll(ctx context.Context) {
	m.mu.RLock()
	groupIDs := make([]string, 0, len(m.active))
	for id := range m.active {
		groupIDs = append(groupIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range groupIDs {
		_ = m.StopAgent(ctx, id)
	}
}

func (m *Manager) ListRunning(ctx context.Context) ([]ContainerInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ContainerInfo, 0, len(m.active))
	for _, info := range m.active {
		result = append(result, *info)
	}
	return result, nil
}

func (m *Manager) GetRunning(groupID string) *ContainerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.active[groupID]; ok {
		return info
	}
	return nil
}

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

func (m *Manager) CleanupStale(ctx context.Context) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", labelPrefix+".managed=true")

	containers, err := m.docker.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	m.mu.RLock()
	activeIDs := make(map[string]bool)
	for _, info := range m.active {
		activeIDs[info.ID] = true
	}
	m.mu.RUnlock()

	for _, c := range containers {
		if !activeIDs[c.ID] {
			slog.Info("cleaning up stale container", "container", c.ID[:12])
			_ = m.docker.ContainerRemove(ctx, c.ID, dockercontainer.RemoveOptions{Force: true})
		}
	}
	return nil
}

func (m *Manager) BuildImage(ctx context.Context) error {
	return BuildAgentImage(ctx, m.docker, m.cfg.Image)
}
