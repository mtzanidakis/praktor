package registry

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/embeddings"
	"github.com/mtzanidakis/praktor/internal/store"
)

type Registry struct {
	mu       sync.RWMutex
	store    *store.Store
	agents   map[string]config.AgentDefinition
	cfg      config.DefaultsConfig
	basePath string
	embedder embeddings.Embedder
}

func New(s *store.Store, agents map[string]config.AgentDefinition, cfg config.DefaultsConfig, basePath string) *Registry {
	return &Registry{
		store:    s,
		agents:   agents,
		cfg:      cfg,
		basePath: basePath,
	}
}

// Update replaces the agent definitions and defaults, then syncs to the store.
func (r *Registry) Update(agents map[string]config.AgentDefinition, defaults config.DefaultsConfig) error {
	r.mu.Lock()
	r.agents = agents
	r.cfg = defaults
	r.mu.Unlock()

	return r.Sync()
}

func (r *Registry) Sync() error {
	ids := make([]string, 0, len(r.agents))
	for name, def := range r.agents {
		ids = append(ids, name)

		a := &store.Agent{
			ID:          name,
			Name:        name,
			Description: def.Description,
			Model:       def.Model,
			Image:       def.Image,
			Workspace:   def.Workspace,
			ClaudeMD:    def.ClaudeMD,
		}
		if a.Workspace == "" {
			a.Workspace = name
		}

		if err := r.store.SaveAgent(a); err != nil {
			return fmt.Errorf("save agent %s: %w", name, err)
		}

		if err := r.ensureDirectories(a.Workspace); err != nil {
			return fmt.Errorf("ensure directories for %s: %w", name, err)
		}
	}

	if err := r.store.DeleteAgentsNotIn(ids); err != nil {
		return fmt.Errorf("delete stale agents: %w", err)
	}

	if err := r.ensureGlobalDirectory(); err != nil {
		return err
	}

	r.syncEmbeddings()
	return nil
}

func (r *Registry) Get(agentID string) (*store.Agent, error) {
	return r.store.GetAgent(agentID)
}

func (r *Registry) List() ([]store.Agent, error) {
	return r.store.ListAgents()
}

func (r *Registry) GetDefinition(agentID string) (config.AgentDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.agents[agentID]
	return def, ok
}

func (r *Registry) ResolveModel(agentID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if def, ok := r.agents[agentID]; ok && def.Model != "" {
		return def.Model
	}
	return r.cfg.Model
}

func (r *Registry) ResolveImage(agentID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if def, ok := r.agents[agentID]; ok && def.Image != "" {
		return def.Image
	}
	return r.cfg.Image
}

func (r *Registry) GetClaudeMD(agentID string) (string, error) {
	r.mu.RLock()
	def, hasDef := r.agents[agentID]
	r.mu.RUnlock()

	// Check config-specified path first
	if hasDef && def.ClaudeMD != "" {
		path := filepath.Join(r.basePath, def.ClaudeMD)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "", nil
			}
			return "", err
		}
		return string(data), nil
	}

	// Default: look in agent workspace dir
	workspace := agentID
	if hasDef && def.Workspace != "" {
		workspace = def.Workspace
	}
	path := filepath.Join(r.basePath, workspace, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (r *Registry) GetGlobalClaudeMD() (string, error) {
	path := filepath.Join(r.basePath, "global", "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (r *Registry) FindByAgentMailInbox(inboxID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, def := range r.agents {
		if def.AgentMailInboxID == inboxID {
			return name, true
		}
	}
	return "", false
}

// AgentMailInboxes returns a map of inbox_id → agent_id for all agents
// that have an AgentMail inbox configured.
func (r *Registry) AgentMailInboxes() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inboxes := make(map[string]string)
	for name, def := range r.agents {
		if def.AgentMailInboxID != "" {
			inboxes[def.AgentMailInboxID] = name
		}
	}
	return inboxes
}

func (r *Registry) AgentDescriptions() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descs := make(map[string]string, len(r.agents))
	for name, def := range r.agents {
		descs[name] = def.Description
	}
	return descs
}

func (r *Registry) AgentPath(workspace string) string {
	return filepath.Join(r.basePath, workspace)
}

func (r *Registry) GlobalPath() string {
	return filepath.Join(r.basePath, "global")
}

func (r *Registry) ensureDirectories(workspace string) error {
	dir := filepath.Join(r.basePath, workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		if err := os.WriteFile(claudeMD, []byte("# Agent Memory\n\nThis file stores context for this agent.\n"), 0o644); err != nil {
			return fmt.Errorf("create CLAUDE.md: %w", err)
		}
	}

	agentMD := filepath.Join(dir, "AGENT.md")
	if _, err := os.Stat(agentMD); os.IsNotExist(err) {
		if err := os.WriteFile(agentMD, []byte(agentMDTemplate), 0o644); err != nil {
			return fmt.Errorf("create AGENT.md: %w", err)
		}
	}
	return nil
}

func (r *Registry) GetAgentMD(agentID string) (string, error) {
	r.mu.RLock()
	def, hasDef := r.agents[agentID]
	r.mu.RUnlock()

	workspace := agentID
	if hasDef && def.Workspace != "" {
		workspace = def.Workspace
	}
	path := filepath.Join(r.basePath, workspace, "AGENT.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (r *Registry) SaveAgentMD(agentID, content string) error {
	r.mu.RLock()
	def, hasDef := r.agents[agentID]
	r.mu.RUnlock()

	workspace := agentID
	if hasDef && def.Workspace != "" {
		workspace = def.Workspace
	}
	path := filepath.Join(r.basePath, workspace, "AGENT.md")
	return os.WriteFile(path, []byte(content), 0o644)
}

func (r *Registry) GetUserMD() (string, error) {
	path := filepath.Join(r.basePath, "global", "USER.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (r *Registry) SaveUserMD(content string) error {
	path := filepath.Join(r.basePath, "global", "USER.md")
	return os.WriteFile(path, []byte(content), 0o644)
}

// SetEmbedder sets the embedder used for vector routing.
func (r *Registry) SetEmbedder(e embeddings.Embedder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.embedder = e
}

// SyncEmbeddings triggers a re-sync of agent embeddings. Call this after
// updating agent profile data (e.g. AGENT.md) so vector routing stays current.
func (r *Registry) SyncEmbeddings() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	r.syncEmbeddings()
}

// syncEmbeddings computes and stores embeddings for agent descriptions
// that have changed since last sync. Must be called with agents already set.
func (r *Registry) syncEmbeddings() {
	if r.embedder == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Collect agents needing (re-)embedding
	type needsEmbed struct {
		name string
		desc string
		hash string
	}
	var pending []needsEmbed

	for name, def := range r.agents {
		if def.Description == "" {
			continue
		}
		// Build embedding text from description + AGENT.md profile
		embText := name + ": " + def.Description
		if profile, err := r.GetAgentMD(name); err == nil && profile != "" {
			embText += "\n" + profile
		}
		embHash := fmt.Sprintf("%x", sha256.Sum256([]byte(embText)))
		existing, _ := r.store.GetAgentEmbeddingHash(name)
		if existing == embHash {
			continue
		}
		pending = append(pending, needsEmbed{name: name, desc: embText, hash: embHash})
	}

	if len(pending) == 0 {
		return
	}

	// Batch embed all descriptions at once
	texts := make([]string, len(pending))
	for i, p := range pending {
		texts[i] = p.desc
	}

	vecs, err := r.embedder.Embed(ctx, texts)
	if err != nil {
		slog.Error("failed to embed agent descriptions", "error", err)
		return
	}

	for i, p := range pending {
		if i >= len(vecs) {
			break
		}
		if err := r.store.SaveAgentEmbedding(p.name, p.hash, vecs[i]); err != nil {
			slog.Error("failed to save agent embedding", "agent", p.name, "error", err)
		} else {
			slog.Info("agent embedding updated", "agent", p.name)
		}
	}

	// Clean up embeddings for removed agents
	agents, _ := r.store.ListAgents()
	activeIDs := make(map[string]bool, len(r.agents))
	for name := range r.agents {
		activeIDs[name] = true
	}
	for _, a := range agents {
		if !activeIDs[a.ID] {
			r.store.DeleteAgentEmbedding(a.ID)
			r.store.DeleteLearnedEmbeddings(a.ID)
		}
	}
}

const agentMDTemplate = `# Agent Identity

## Name
(Agent display name)

## Vibe
(Personality, communication style)

## Expertise
(Areas of specialization)
`

const userMDTemplate = `# User Profile

## Name
(Your full name)

## Preferred Name
(What you'd like to be called)

## Pronouns
(e.g. he/him, she/her, they/them)

## Timezone
(e.g. Europe/Athens)

## Notes
(Anything else you'd like agents to know about you)

## Interests
(Topics, hobbies, areas of expertise)
`

func (r *Registry) ensureGlobalDirectory() error {
	dir := filepath.Join(r.basePath, "global")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create global dir: %w", err)
	}

	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		defaultContent := "# Global Instructions\n\nThis file is loaded by all agents.\n"
		if err := os.WriteFile(claudeMD, []byte(defaultContent), 0o644); err != nil {
			return fmt.Errorf("create global CLAUDE.md: %w", err)
		}
	}

	userMD := filepath.Join(dir, "USER.md")
	if _, err := os.Stat(userMD); os.IsNotExist(err) {
		if err := os.WriteFile(userMD, []byte(userMDTemplate), 0o644); err != nil {
			return fmt.Errorf("create USER.md: %w", err)
		}
	}

	return nil
}
