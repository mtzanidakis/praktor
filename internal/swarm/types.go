package swarm

type SwarmRequest struct {
	ID        string       `json:"id"`
	LeadGroup string       `json:"lead_group"`
	Agents    []SwarmAgent `json:"agents"`
	Task      string       `json:"task"`
}

type SwarmAgent struct {
	Role        string `json:"role"`
	Prompt      string `json:"prompt"`
	GroupFolder string `json:"group_folder"`
}

type AgentResult struct {
	Role    string `json:"role"`
	Status  string `json:"status"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}
