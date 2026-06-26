package container

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
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
	cfg         config.DefaultsConfig
	mu          sync.RWMutex
	active      map[string]*ContainerInfo // agentID → container
	networkName string                    // resolved network name
}

type ContainerInfo struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	SessionID string    `json:"session_id"`
}

type AgentOpts struct {
	AgentID      string
	Workspace    string
	Model        string
	Image        string
	SessionID    string
	Mounts       []Mount
	NATSUrl      string
	Env          map[string]string
	SecretFiles  []SecretFile
	AllowedTools []string
	NixEnabled   bool
	Security     *config.SecurityConfig // nil = use manager defaults
}

type SecretFile struct {
	Content []byte
	Target  string
	Mode    int64
}

func NewManager(bus *natsbus.Bus, cfg config.DefaultsConfig) (*Manager, error) {
	docker, err := client.New(client.FromEnv)
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

// UpdateDefaults replaces the defaults config used for new containers.
func (m *Manager) UpdateDefaults(cfg config.DefaultsConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
}

func (m *Manager) ensureNetwork(ctx context.Context) error {
	if m.networkName != "" {
		return nil
	}

	_, err := m.docker.NetworkInspect(ctx, networkName, client.NetworkInspectOptions{})
	if err == nil {
		m.networkName = networkName
		return nil
	}

	// Create it (for non-Compose runs like make dev)
	_, err = m.docker.NetworkCreate(ctx, networkName, client.NetworkCreateOptions{
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

	if existing, ok := m.active[opts.AgentID]; ok {
		return existing, nil
	}

	if len(m.active) >= m.cfg.MaxRunning {
		return nil, fmt.Errorf("max containers (%d) reached", m.cfg.MaxRunning)
	}

	if err := m.ensureNetwork(ctx); err != nil {
		return nil, err
	}

	containerName := fmt.Sprintf("praktor-agent-%s", opts.AgentID)

	// Remove any stale container with the same name
	timeout := 5
	_, _ = m.docker.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout})
	_, _ = m.docker.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{Force: true})

	env := []string{
		fmt.Sprintf("NATS_URL=%s", opts.NATSUrl),
		fmt.Sprintf("AGENT_ID=%s", opts.AgentID),
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

	// Per-agent env vars (secret:name values already resolved by orchestrator)
	for k, v := range opts.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Allowed tools
	if len(opts.AllowedTools) > 0 {
		env = append(env, fmt.Sprintf("ALLOWED_TOOLS=%s", strings.Join(opts.AllowedTools, ",")))
	}
	mounts := buildMounts(opts)

	image := opts.Image
	if image == "" {
		image = m.cfg.Image
	}

	containerCfg := &dockercontainer.Config{
		Image:  image,
		Env:    env,
		Labels: map[string]string{labelPrefix + ".managed": "true", labelPrefix + ".agent": opts.AgentID},
	}

	hostCfg := &dockercontainer.HostConfig{
		Binds:       mounts,
		NetworkMode: dockercontainer.NetworkMode(m.networkName),
	}
	m.applySecurity(hostCfg, opts.Security)

	networkCfg := &network.NetworkingConfig{}

	resp, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           containerCfg,
		HostConfig:       hostCfg,
		NetworkingConfig: networkCfg,
		Name:             containerName,
	})
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	// Copy secret files into container before starting
	for _, sf := range opts.SecretFiles {
		if err := m.copyFileToContainer(ctx, resp.ID, sf); err != nil {
			_, _ = m.docker.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
			return nil, fmt.Errorf("copy secret file %s: %w", sf.Target, err)
		}
	}

	if _, err := m.docker.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	// Ensure volume mount points are owned by praktor (uid 10321).
	// Docker named volumes may be created with root ownership.
	chownResp, err := m.docker.ExecCreate(ctx, resp.ID, client.ExecCreateOptions{
		User: "root",
		Cmd:  []string{"chown", "-R", "10321:10321", "/workspace/agent", "/home/praktor"},
	})
	if err != nil {
		slog.Warn("failed to create chown exec", "agent", opts.AgentID, "error", err)
	} else if _, err := m.docker.ExecStart(ctx, chownResp.ID, client.ExecStartOptions{}); err != nil {
		slog.Warn("failed to chown volumes", "agent", opts.AgentID, "error", err)
	}

	// Start nix-daemon as root via Docker exec (container runs as praktor)
	if opts.NixEnabled {
		execResp, err := m.docker.ExecCreate(ctx, resp.ID, client.ExecCreateOptions{
			User: "root",
			Cmd:  []string{"nix-daemon"},
		})
		if err != nil {
			slog.Warn("failed to create nix-daemon exec", "agent", opts.AgentID, "error", err)
		} else if _, err := m.docker.ExecStart(ctx, execResp.ID, client.ExecStartOptions{Detach: true}); err != nil {
			slog.Warn("failed to start nix-daemon", "agent", opts.AgentID, "error", err)
		} else {
			slog.Info("nix-daemon started", "agent", opts.AgentID)
		}
	}

	info := &ContainerInfo{
		ID:        resp.ID,
		AgentID:   opts.AgentID,
		Name:      containerName,
		Status:    "running",
		StartedAt: time.Now(),
		SessionID: opts.SessionID,
	}
	m.active[opts.AgentID] = info

	slog.Info("agent container started", "agent", opts.AgentID, "container", resp.ID[:12])
	return info, nil
}

// applySecurity applies the resolved Docker hardening profile to the
// container's HostConfig. A per-agent override takes precedence over the
// manager's deployment-wide defaults; both are reloadable via hot config
// reload. Zero-valued limits (PidsLimit/MemoryMB/CPUs) mean "unlimited".
func (m *Manager) applySecurity(hostCfg *dockercontainer.HostConfig, override *config.SecurityConfig) {
	sec := m.cfg.Security
	if override != nil {
		sec = *override
	}

	if sec.NoNewPrivileges {
		hostCfg.SecurityOpt = append(hostCfg.SecurityOpt, "no-new-privileges=true")
	}
	if sec.DropCapabilities {
		hostCfg.CapDrop = []string{"ALL"}
		hostCfg.CapAdd = sec.AddCapabilities
	}
	if sec.PidsLimit > 0 {
		limit := sec.PidsLimit
		hostCfg.PidsLimit = &limit
	}
	if sec.MemoryMB > 0 {
		hostCfg.Memory = sec.MemoryMB * 1024 * 1024
	}
	if sec.CPUs > 0 {
		hostCfg.NanoCPUs = int64(sec.CPUs * 1e9)
	}
	if sec.ReadonlyRootfs {
		hostCfg.ReadonlyRootfs = true
	}
	if sec.Tmpfs {
		if hostCfg.Tmpfs == nil {
			hostCfg.Tmpfs = make(map[string]string)
		}
		// /tmp keeps exec (nix and build tools exec from it); /var/tmp does not.
		hostCfg.Tmpfs["/tmp"] = "rw,nosuid,size=512m"
		hostCfg.Tmpfs["/var/tmp"] = "rw,noexec,nosuid,size=256m"
	}
}

func (m *Manager) copyFileToContainer(ctx context.Context, containerID string, sf SecretFile) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	fileMode := sf.Mode
	if fileMode == 0 {
		fileMode = 0o600
	}

	// Derive directory mode: add execute bit for each read bit
	// e.g. 0600 → 0700, 0644 → 0755
	dirMode := fileMode | ((fileMode & 0o444) >> 2)

	// Create directory entries for each path component so that parent
	// directories are created with proper permissions and ownership.
	// Docker's tar extraction preserves existing directory permissions.
	targetPath := strings.TrimPrefix(sf.Target, "/")
	parts := strings.Split(path.Dir(targetPath), "/")
	for i := range parts {
		dir := strings.Join(parts[:i+1], "/") + "/"
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     dir,
			Mode:     dirMode,
			Uid:      10321,
			Gid:      10321,
		}); err != nil {
			return fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	if err := tw.WriteHeader(&tar.Header{
		Name: targetPath,
		Mode: fileMode,
		Size: int64(len(sf.Content)),
		Uid:  10321,
		Gid:  10321,
	}); err != nil {
		return fmt.Errorf("write tar header: %w", err)
	}
	if _, err := tw.Write(sf.Content); err != nil {
		return fmt.Errorf("write tar body: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}

	_, err := m.docker.CopyToContainer(ctx, containerID, client.CopyToContainerOptions{
		DestinationPath: "/",
		Content:         &buf,
	})
	return err
}

func (m *Manager) StopAgent(ctx context.Context, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	info, ok := m.active[agentID]
	if !ok {
		return nil
	}

	timeout := 10
	if _, err := m.docker.ContainerStop(ctx, info.ID, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
		slog.Warn("failed to stop container gracefully", "container", info.ID[:12], "error", err)
	}

	if _, err := m.docker.ContainerRemove(ctx, info.ID, client.ContainerRemoveOptions{Force: true}); err != nil {
		slog.Warn("failed to remove container", "container", info.ID[:12], "error", err)
	}

	delete(m.active, agentID)
	slog.Info("agent container stopped", "agent", agentID)
	return nil
}

func (m *Manager) StopAll(ctx context.Context) {
	m.mu.RLock()
	agentIDs := make([]string, 0, len(m.active))
	for id := range m.active {
		agentIDs = append(agentIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range agentIDs {
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

func (m *Manager) GetRunning(agentID string) *ContainerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if info, ok := m.active[agentID]; ok {
		return info
	}
	return nil
}

// Exec runs a command inside a running agent container and returns the combined output.
func (m *Manager) Exec(ctx context.Context, agentID string, cmd []string) (string, error) {
	m.mu.RLock()
	info, ok := m.active[agentID]
	m.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("agent %s is not running", agentID)
	}

	execResp, err := m.docker.ExecCreate(ctx, info.ID, client.ExecCreateOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", fmt.Errorf("exec create: %w", err)
	}

	attach, err := m.docker.ExecAttach(ctx, execResp.ID, client.ExecAttachOptions{})
	if err != nil {
		return "", fmt.Errorf("exec attach: %w", err)
	}
	defer attach.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attach.Reader); err != nil {
		return "", fmt.Errorf("exec read: %w", err)
	}

	inspect, err := m.docker.ExecInspect(ctx, execResp.ID, client.ExecInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("exec inspect: %w", err)
	}

	output := stdout.String() + stderr.String()
	if inspect.ExitCode != 0 {
		return output, fmt.Errorf("exit code %d: %s", inspect.ExitCode, output)
	}
	return output, nil
}

func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

func (m *Manager) CleanupStale(ctx context.Context) error {
	resp, err := m.docker.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("label", labelPrefix+".managed=true"),
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

	for _, c := range resp.Items {
		if !activeIDs[c.ID] {
			slog.Info("cleaning up stale container", "container", c.ID[:12])
			_, _ = m.docker.ContainerRemove(ctx, c.ID, client.ContainerRemoveOptions{Force: true})
		}
	}
	return nil
}

func (m *Manager) BuildImage(ctx context.Context) error {
	return BuildAgentImage(ctx, m.docker, m.cfg.Image)
}

// ReadVolumeFile reads a file from a Docker named volume by creating a
// temporary container, copying the file out, and removing the container.
func (m *Manager) ReadVolumeFile(ctx context.Context, workspace, filePath, image string) (string, error) {
	volName := fmt.Sprintf("praktor-wk-%s", sanitizeVolumeName(workspace))
	containerName := fmt.Sprintf("praktor-vol-tmp-%s-%d", sanitizeVolumeName(workspace), time.Now().UnixNano())

	resp, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &dockercontainer.Config{Image: image, Entrypoint: []string{"true"}},
		HostConfig: &dockercontainer.HostConfig{Binds: []string{volName + ":/vol"}},
		Name:       containerName,
	})
	if err != nil {
		return "", fmt.Errorf("create temp container: %w", err)
	}
	defer func() {
		_, _ = m.docker.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
	}()

	srcPath := path.Join("/vol", filePath)
	copyResp, err := m.docker.CopyFromContainer(ctx, resp.ID, client.CopyFromContainerOptions{SourcePath: srcPath})
	if err != nil {
		return "", fmt.Errorf("copy from volume: %w", err)
	}
	defer func() { _ = copyResp.Content.Close() }()

	tr := tar.NewReader(copyResp.Content)
	if _, err := tr.Next(); err != nil {
		return "", fmt.Errorf("read tar: %w", err)
	}
	data, err := io.ReadAll(tr)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// WriteVolumeFile writes a file into a Docker named volume by creating a
// temporary container, copying the file in, and removing the container.
func (m *Manager) WriteVolumeFile(ctx context.Context, workspace, filePath, content, image string) error {
	volName := fmt.Sprintf("praktor-wk-%s", sanitizeVolumeName(workspace))
	containerName := fmt.Sprintf("praktor-vol-tmp-%s-%d", sanitizeVolumeName(workspace), time.Now().UnixNano())

	resp, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &dockercontainer.Config{Image: image, Entrypoint: []string{"true"}},
		HostConfig: &dockercontainer.HostConfig{Binds: []string{volName + ":/vol"}},
		Name:       containerName,
	})
	if err != nil {
		return fmt.Errorf("create temp container: %w", err)
	}
	defer func() {
		_, _ = m.docker.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
	}()

	// Build tar archive with the file
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name: path.Base(filePath),
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		return fmt.Errorf("write tar header: %w", err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		return fmt.Errorf("write tar body: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}

	dstDir := path.Join("/vol", path.Dir(filePath))
	if _, err := m.docker.CopyToContainer(ctx, resp.ID, client.CopyToContainerOptions{
		DestinationPath: dstDir,
		Content:         &buf,
	}); err != nil {
		return fmt.Errorf("copy to volume: %w", err)
	}
	return nil
}

// WriteVolumeBytes writes binary data into a Docker named volume. Same
// temp-container pattern as WriteVolumeFile but accepts []byte and creates
// parent directories with correct ownership (uid/gid 10321).
func (m *Manager) WriteVolumeBytes(ctx context.Context, workspace, filePath string, data []byte, image string) error {
	volName := fmt.Sprintf("praktor-wk-%s", sanitizeVolumeName(workspace))
	containerName := fmt.Sprintf("praktor-vol-tmp-%s-%d", sanitizeVolumeName(workspace), time.Now().UnixNano())

	resp, err := m.docker.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:     &dockercontainer.Config{Image: image, Entrypoint: []string{"true"}},
		HostConfig: &dockercontainer.HostConfig{Binds: []string{volName + ":/vol"}},
		Name:       containerName,
	})
	if err != nil {
		return fmt.Errorf("create temp container: %w", err)
	}
	defer func() {
		_, _ = m.docker.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
	}()

	// Build tar archive with directory entries and the file
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Defense-in-depth: reject any filePath that escapes /vol after path.Join
	// collapses "../" sequences (e.g. a traversal-laden upload filename).
	cleaned := path.Join("/vol", filePath)
	if cleaned != "/vol" && !strings.HasPrefix(cleaned, "/vol/") {
		return fmt.Errorf("invalid file path %q: escapes volume root", filePath)
	}
	targetPath := strings.TrimPrefix(cleaned, "/")

	// Create parent directory entries with correct ownership
	parts := strings.Split(path.Dir(targetPath), "/")
	for i := range parts {
		dir := strings.Join(parts[:i+1], "/") + "/"
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     dir,
			Mode:     0o755,
			Uid:      10321,
			Gid:      10321,
		}); err != nil {
			return fmt.Errorf("write dir header %s: %w", dir, err)
		}
	}

	if err := tw.WriteHeader(&tar.Header{
		Name: targetPath,
		Mode: 0o644,
		Size: int64(len(data)),
		Uid:  10321,
		Gid:  10321,
	}); err != nil {
		return fmt.Errorf("write tar header: %w", err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar body: %w", err)
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}

	if _, err := m.docker.CopyToContainer(ctx, resp.ID, client.CopyToContainerOptions{
		DestinationPath: "/",
		Content:         &buf,
	}); err != nil {
		return fmt.Errorf("copy to volume: %w", err)
	}
	return nil
}
