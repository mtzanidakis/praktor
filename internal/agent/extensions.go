package agent

import (
	"encoding/json"
	"log/slog"

	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/extensions"
)

// resolveExtensions loads extensions from DB for the given agent, resolves
// secret references, and sets the AGENT_EXTENSIONS env var on the container opts.
func (o *Orchestrator) resolveExtensions(opts *container.AgentOpts, agentID string) {
	data, err := o.store.GetAgentExtensions(agentID)
	if err != nil {
		slog.Warn("failed to load agent extensions", "agent", agentID, "error", err)
		return
	}

	ext, err := extensions.Parse(data)
	if err != nil {
		slog.Warn("failed to parse agent extensions", "agent", agentID, "error", err)
		return
	}

	// Resolve secret:name references in MCP server env/headers
	if !ext.IsEmpty() && o.vault != nil {
		if err := ext.ResolveSecretRefs(func(name string) (string, error) {
			plaintext, err := o.decryptSecret(name)
			if err != nil {
				return "", err
			}
			return string(plaintext), nil
		}); err != nil {
			slog.Warn("failed to resolve extension secrets", "agent", agentID, "error", err)
			return
		}
	}

	resolved, err := json.Marshal(ext)
	if err != nil {
		slog.Warn("failed to marshal resolved extensions", "agent", agentID, "error", err)
		return
	}

	if opts.Env == nil {
		opts.Env = make(map[string]string)
	}
	// Always set AGENT_EXTENSIONS so the agent-runner can clean up
	// previously installed plugins/marketplaces even when config is empty.
	opts.Env["AGENT_EXTENSIONS"] = string(resolved)
}
