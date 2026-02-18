package swarm

type Synapse struct {
	From          string `json:"from"`          // agent role
	To            string `json:"to"`            // agent role
	Bidirectional bool   `json:"bidirectional"` // false=pipeline, true=collaborative
}

type SwarmRequest struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	LeadAgent string       `json:"lead_agent"` // role of lead agent
	Agents    []SwarmAgent `json:"agents"`
	Synapses  []Synapse    `json:"synapses"`
	Task      string       `json:"task"`
}

type SwarmAgent struct {
	AgentID   string `json:"agent_id"`   // references config agent name
	Role      string `json:"role"`       // display label in swarm
	Prompt    string `json:"prompt"`     // per-agent instructions
	Workspace string `json:"workspace"`
}

type AgentResult struct {
	Role   string `json:"role"`
	Status string `json:"status"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}
