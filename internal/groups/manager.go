package groups

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/store"
)

type Manager struct {
	store    *store.Store
	basePath string
	cfg      config.GroupsConfig
}

func NewManager(s *store.Store, cfg config.GroupsConfig) *Manager {
	return &Manager{
		store:    s,
		basePath: cfg.BasePath,
		cfg:      cfg,
	}
}

func (m *Manager) Register(g store.Group) error {
	if err := m.EnsureDirectories(g.Folder); err != nil {
		return fmt.Errorf("ensure directories: %w", err)
	}
	return m.store.SaveGroup(&g)
}

func (m *Manager) Get(id string) (*store.Group, error) {
	return m.store.GetGroup(id)
}

func (m *Manager) List() ([]store.Group, error) {
	return m.store.ListGroups()
}

func (m *Manager) GetClaudeMD(groupFolder string) (string, error) {
	path := filepath.Join(m.basePath, groupFolder, "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (m *Manager) GetGlobalClaudeMD() (string, error) {
	path := filepath.Join(m.basePath, "global", "CLAUDE.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (m *Manager) EnsureDirectories(groupFolder string) error {
	dir := filepath.Join(m.basePath, groupFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create group dir: %w", err)
	}

	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		if err := os.WriteFile(claudeMD, []byte("# Group Memory\n\nThis file stores context for this group.\n"), 0o644); err != nil {
			return fmt.Errorf("create CLAUDE.md: %w", err)
		}
	}
	return nil
}

func (m *Manager) EnsureGlobalDirectory() error {
	dir := filepath.Join(m.basePath, "global")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create global dir: %w", err)
	}

	claudeMD := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(claudeMD); os.IsNotExist(err) {
		defaultContent := "# Global Instructions\n\nThis file is loaded by all agent groups.\n"
		if err := os.WriteFile(claudeMD, []byte(defaultContent), 0o644); err != nil {
			return fmt.Errorf("create global CLAUDE.md: %w", err)
		}
	}
	return nil
}

// EnsureMainGroup registers the main group in the database if main_chat_id
// is configured and the group doesn't already exist.
func (m *Manager) EnsureMainGroup() error {
	if m.cfg.MainChatID == "" {
		return nil
	}

	existing, err := m.store.GetGroup(m.cfg.MainChatID)
	if err != nil {
		return fmt.Errorf("check main group: %w", err)
	}
	if existing != nil {
		if existing.IsMain {
			return nil
		}
		existing.IsMain = true
		existing.Folder = "main"
		return m.store.SaveGroup(existing)
	}

	return m.Register(store.Group{
		ID:     m.cfg.MainChatID,
		Name:   "Main",
		Folder: "main",
		IsMain: true,
	})
}

func (m *Manager) GroupPath(groupFolder string) string {
	return filepath.Join(m.basePath, groupFolder)
}

func (m *Manager) GlobalPath() string {
	return filepath.Join(m.basePath, "global")
}

func (m *Manager) IsMainChat(chatID string) bool {
	return m.cfg.MainChatID != "" && chatID == m.cfg.MainChatID
}
