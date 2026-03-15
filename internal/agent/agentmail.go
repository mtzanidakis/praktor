package agent

import (
	"github.com/mtzanidakis/praktor/internal/container"
)

// SetAgentMailAPIKey sets the AgentMail API key for injection into agent containers.
func (o *Orchestrator) SetAgentMailAPIKey(key string) {
	o.agentMailAPIKey = key
}

// resolveAgentMail injects AGENTMAIL_API_KEY and AGENTMAIL_INBOX_ID env vars
// into container opts for agents with an agentmail_inbox_id configured.
func (o *Orchestrator) resolveAgentMail(opts *container.AgentOpts, agentID string) {
	if o.agentMailAPIKey == "" {
		return
	}

	def, ok := o.registry.GetDefinition(agentID)
	if !ok || def.AgentMailInboxID == "" {
		return
	}

	if opts.Env == nil {
		opts.Env = make(map[string]string)
	}
	opts.Env["AGENTMAIL_API_KEY"] = o.agentMailAPIKey
	opts.Env["AGENTMAIL_INBOX_ID"] = def.AgentMailInboxID
}
