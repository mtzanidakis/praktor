package config

import (
	"testing"
	"time"
)

func TestDiff_NoChanges(t *testing.T) {
	cfg := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Description: "test bot", Model: "claude-opus-4-6"},
		},
		Defaults: DefaultsConfig{Model: "claude-opus-4-6", Image: "img:latest"},
		Router:   RouterConfig{DefaultAgent: "bot"},
	}
	d := Diff(cfg, cfg)
	if d.HasChanges() {
		t.Error("expected no changes")
	}
}

func TestDiff_AgentAdded(t *testing.T) {
	old := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Description: "test"},
		},
	}
	new := &Config{
		Agents: map[string]AgentDefinition{
			"bot":  {Description: "test"},
			"bot2": {Description: "new bot"},
		},
	}
	d := Diff(old, new)
	if len(d.AgentsAdded) != 1 || d.AgentsAdded[0] != "bot2" {
		t.Errorf("expected bot2 added, got %v", d.AgentsAdded)
	}
	if len(d.AgentsRemoved) != 0 {
		t.Errorf("expected no removals, got %v", d.AgentsRemoved)
	}
	if len(d.AgentsChanged) != 0 {
		t.Errorf("expected no changes, got %v", d.AgentsChanged)
	}
}

func TestDiff_AgentRemoved(t *testing.T) {
	old := &Config{
		Agents: map[string]AgentDefinition{
			"bot":  {Description: "test"},
			"bot2": {Description: "old bot"},
		},
	}
	new := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Description: "test"},
		},
	}
	d := Diff(old, new)
	if len(d.AgentsRemoved) != 1 || d.AgentsRemoved[0] != "bot2" {
		t.Errorf("expected bot2 removed, got %v", d.AgentsRemoved)
	}
}

func TestDiff_AgentModelChanged(t *testing.T) {
	old := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Description: "test", Model: "claude-opus-4-6"},
		},
	}
	new := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Description: "test", Model: "claude-sonnet-4-5-20250929"},
		},
	}
	d := Diff(old, new)
	if len(d.AgentsChanged) != 1 || d.AgentsChanged[0] != "bot" {
		t.Errorf("expected bot changed, got %v", d.AgentsChanged)
	}
}

func TestDiff_AgentEnvChanged(t *testing.T) {
	old := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Env: map[string]string{"FOO": "bar"}},
		},
	}
	new := &Config{
		Agents: map[string]AgentDefinition{
			"bot": {Env: map[string]string{"FOO": "baz"}},
		},
	}
	d := Diff(old, new)
	if len(d.AgentsChanged) != 1 {
		t.Errorf("expected bot changed, got %v", d.AgentsChanged)
	}
}

func TestDiff_DefaultsChanged(t *testing.T) {
	old := &Config{
		Defaults: DefaultsConfig{Model: "claude-opus-4-6", Image: "img:latest"},
	}
	new := &Config{
		Defaults: DefaultsConfig{Model: "claude-sonnet-4-5-20250929", Image: "img:latest"},
	}
	d := Diff(old, new)
	if !d.DefaultsChanged {
		t.Error("expected defaults changed")
	}
	if d.NewDefaults.Model != "claude-sonnet-4-5-20250929" {
		t.Errorf("expected new model, got %s", d.NewDefaults.Model)
	}
}

func TestDiff_RouterChanged(t *testing.T) {
	old := &Config{Router: RouterConfig{DefaultAgent: "bot"}}
	new := &Config{Router: RouterConfig{DefaultAgent: "bot2"}}
	d := Diff(old, new)
	if !d.RouterChanged {
		t.Error("expected router changed")
	}
	if d.NewDefaultAgent != "bot2" {
		t.Errorf("expected bot2, got %s", d.NewDefaultAgent)
	}
}

func TestDiff_SchedulerChanged(t *testing.T) {
	old := &Config{Scheduler: SchedulerConfig{PollInterval: 30 * time.Second}}
	new := &Config{Scheduler: SchedulerConfig{PollInterval: 60 * time.Second}}
	d := Diff(old, new)
	if !d.SchedulerChanged {
		t.Error("expected scheduler changed")
	}
}

func TestDiff_NonReloadable(t *testing.T) {
	old := &Config{
		Telegram: TelegramConfig{Token: "old-token"},
		Web:      WebConfig{Port: 8080},
	}
	new := &Config{
		Telegram: TelegramConfig{Token: "new-token"},
		Web:      WebConfig{Port: 9090},
	}
	d := Diff(old, new)
	if len(d.NonReloadable) != 2 {
		t.Errorf("expected 2 non-reloadable warnings, got %v", d.NonReloadable)
	}
}

func TestDiff_MainChatIDChanged(t *testing.T) {
	old := &Config{Telegram: TelegramConfig{MainChatID: 123}}
	new := &Config{Telegram: TelegramConfig{MainChatID: 456}}
	d := Diff(old, new)
	if !d.MainChatIDChanged {
		t.Error("expected main chat ID changed")
	}
	if d.NewMainChatID != 456 {
		t.Errorf("expected 456, got %d", d.NewMainChatID)
	}
}
