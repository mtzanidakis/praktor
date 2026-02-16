package swarm

type SwarmRequest struct {
	ID        string       `json:"id"`
	LeadAgent string       `json:"lead_agent"`
	Agents    []SwarmAgent `json:"agents"`
	Task      string       `json:"task"`
}

type SwarmAgent struct {
	Role      string `json:"role"`
	Prompt    string `json:"prompt"`
	Workspace string `json:"workspace"`
}

type AgentResult struct {
	Role   string `json:"role"`
	Status string `json:"status"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}
