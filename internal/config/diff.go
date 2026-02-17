package config

import "reflect"

// ConfigDiff describes what changed between two configs.
type ConfigDiff struct {
	AgentsAdded   []string
	AgentsRemoved []string
	AgentsChanged []string

	DefaultsChanged bool
	NewDefaults     DefaultsConfig

	RouterChanged  bool
	NewDefaultAgent string

	SchedulerChanged bool
	NewPollInterval  SchedulerConfig

	MainChatIDChanged bool
	NewMainChatID     int64

	// Non-reloadable fields that changed (log warnings only)
	NonReloadable []string
}

// HasChanges reports whether any reloadable field changed.
func (d *ConfigDiff) HasChanges() bool {
	return len(d.AgentsAdded) > 0 ||
		len(d.AgentsRemoved) > 0 ||
		len(d.AgentsChanged) > 0 ||
		d.DefaultsChanged ||
		d.RouterChanged ||
		d.SchedulerChanged ||
		d.MainChatIDChanged
}

// Diff compares two configs and returns what changed.
func Diff(old, new *Config) ConfigDiff {
	var d ConfigDiff

	// Agent diffs
	for name := range new.Agents {
		if _, ok := old.Agents[name]; !ok {
			d.AgentsAdded = append(d.AgentsAdded, name)
		}
	}
	for name := range old.Agents {
		if _, ok := new.Agents[name]; !ok {
			d.AgentsRemoved = append(d.AgentsRemoved, name)
		}
	}
	for name, newDef := range new.Agents {
		if oldDef, ok := old.Agents[name]; ok {
			if !reflect.DeepEqual(oldDef, newDef) {
				d.AgentsChanged = append(d.AgentsChanged, name)
			}
		}
	}

	// Defaults
	if !reflect.DeepEqual(old.Defaults, new.Defaults) {
		d.DefaultsChanged = true
		d.NewDefaults = new.Defaults
	}

	// Router
	if old.Router.DefaultAgent != new.Router.DefaultAgent {
		d.RouterChanged = true
		d.NewDefaultAgent = new.Router.DefaultAgent
	}

	// Scheduler
	if old.Scheduler.PollInterval != new.Scheduler.PollInterval {
		d.SchedulerChanged = true
		d.NewPollInterval = new.Scheduler
	}

	// Telegram main_chat_id
	if old.Telegram.MainChatID != new.Telegram.MainChatID {
		d.MainChatIDChanged = true
		d.NewMainChatID = new.Telegram.MainChatID
	}

	// Non-reloadable warnings
	if old.Telegram.Token != new.Telegram.Token {
		d.NonReloadable = append(d.NonReloadable, "telegram.token")
	}
	if old.Web.Port != new.Web.Port {
		d.NonReloadable = append(d.NonReloadable, "web.port")
	}
	if old.NATS.DataDir != new.NATS.DataDir {
		d.NonReloadable = append(d.NonReloadable, "nats.data_dir")
	}
	if old.Vault.Passphrase != new.Vault.Passphrase {
		d.NonReloadable = append(d.NonReloadable, "vault.passphrase")
	}

	return d
}
