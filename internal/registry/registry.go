package registry

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/store"
)

type Registry struct {
	store    *store.Store
	agents   map[string]config.AgentDefinition
	cfg      config.DefaultsConfig
	basePath string
}

func New(s *store.Store, agents map[string]config.AgentDefinition, cfg config.DefaultsConfig, basePath string) *Registry {
	return &Registry{
		store:    s,
		agents:   agents,
		cfg:      cfg,
		basePath: basePath,
	}
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

	return r.ensureGlobalDirectory()
}

func (r *Registry) Get(agentID string) (*store.Agent, error) {
	return r.store.GetAgent(agentID)
}

func (r *Registry) List() ([]store.Agent, error) {
	return r.store.ListAgents()
}

func (r *Registry) GetDefinition(agentID string) (config.AgentDefinition, bool) {
	def, ok := r.agents[agentID]
	return def, ok
}

func (r *Registry) ResolveModel(agentID string) string {
	if def, ok := r.agents[agentID]; ok && def.Model != "" {
		return def.Model
	}
	return r.cfg.Model
}

func (r *Registry) ResolveImage(agentID string) string {
	if def, ok := r.agents[agentID]; ok && def.Image != "" {
		return def.Image
	}
	return r.cfg.Image
}

func (r *Registry) GetClaudeMD(agentID string) (string, error) {
	// Check config-specified path first
	if def, ok := r.agents[agentID]; ok && def.ClaudeMD != "" {
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
	if def, ok := r.agents[agentID]; ok && def.Workspace != "" {
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

func (r *Registry) AgentDescriptions() map[string]string {
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
	return nil
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
