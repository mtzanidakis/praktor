package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mtzanidakis/praktor/internal/schedule"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/swarm"
)

const agentMDTemplate = `# Agent Identity

## Name
(Agent display name)

## Vibe
(Personality, communication style)

## Expertise
(Areas of specialization)
`

func (s *Server) registerAPI(mux *http.ServeMux) {
	// Agents (definitions from config, persisted in DB)
	mux.HandleFunc("GET /api/agents/definitions", s.listAgentDefinitions)
	mux.HandleFunc("GET /api/agents/definitions/{id}", s.getAgentDefinition)
	mux.HandleFunc("GET /api/agents/definitions/{id}/messages", s.getAgentMessages)
	mux.HandleFunc("GET /api/agents/definitions/{id}/agent-md", s.getAgentMD)
	mux.HandleFunc("PUT /api/agents/definitions/{id}/agent-md", s.updateAgentMD)
	mux.HandleFunc("GET /api/agents/definitions/{id}/extensions", s.getAgentExtensions)
	mux.HandleFunc("PUT /api/agents/definitions/{id}/extensions", s.updateAgentExtensions)

	// Agent lifecycle
	mux.HandleFunc("POST /api/agents/definitions/{id}/start", s.startAgent)
	mux.HandleFunc("POST /api/agents/definitions/{id}/stop", s.stopAgent)

	// Running agent containers
	mux.HandleFunc("GET /api/agents", s.listRunningAgents)

	// Tasks
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("PUT /api/tasks/{id}", s.updateTask)
	mux.HandleFunc("DELETE /api/tasks/completed", s.deleteCompletedTasks)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)

	// Secrets
	mux.HandleFunc("GET /api/secrets", s.listSecrets)
	mux.HandleFunc("POST /api/secrets", s.createSecret)
	mux.HandleFunc("GET /api/secrets/{id}", s.getSecret)
	mux.HandleFunc("PUT /api/secrets/{id}", s.updateSecret)
	mux.HandleFunc("DELETE /api/secrets/{id}", s.deleteSecret)
	mux.HandleFunc("GET /api/agents/definitions/{id}/secrets", s.getAgentSecrets)
	mux.HandleFunc("PUT /api/agents/definitions/{id}/secrets", s.setAgentSecrets)
	mux.HandleFunc("POST /api/agents/definitions/{id}/secrets/{secretId}", s.addAgentSecret)
	mux.HandleFunc("DELETE /api/agents/definitions/{id}/secrets/{secretId}", s.removeAgentSecret)

	// Swarms
	mux.HandleFunc("GET /api/swarms", s.listSwarms)
	mux.HandleFunc("POST /api/swarms", s.createSwarm)
	mux.HandleFunc("GET /api/swarms/{id}", s.getSwarm)
	mux.HandleFunc("DELETE /api/swarms/{id}", s.deleteSwarm)

	// User profile
	mux.HandleFunc("GET /api/user-profile", s.getUserProfile)
	mux.HandleFunc("PUT /api/user-profile", s.updateUserProfile)

	// System
	mux.HandleFunc("GET /api/status", s.getStatus)
}

func (s *Server) listAgentDefinitions(w http.ResponseWriter, r *http.Request) {
	agents, err := s.store.ListAgents()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Enrich with container status, message count, and last active
	running, _ := s.orch.ListRunning(r.Context())
	runningSet := make(map[string]bool, len(running))
	for _, c := range running {
		runningSet[c.AgentID] = true
	}

	msgStats, _ := s.store.GetAgentMessageStats()

	out := make([]map[string]any, 0, len(agents))
	for _, a := range agents {
		agentStatus := "stopped"
		if runningSet[a.ID] {
			agentStatus = "running"
		}

		entry := map[string]any{
			"id":           a.ID,
			"name":         a.Name,
			"description":  a.Description,
			"model":        s.registry.ResolveModel(a.ID),
			"image":        s.registry.ResolveImage(a.ID),
			"workspace":    a.Workspace,
			"agent_status": agentStatus,
		}

		if stats, ok := msgStats[a.ID]; ok {
			entry["message_count"] = stats.MessageCount
			entry["last_active"] = formatMessageTime(stats.LastActive)
		} else {
			entry["message_count"] = 0
		}

		out = append(out, entry)
	}
	jsonResponse(w, out)
}

func (s *Server) getAgentDefinition(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if a == nil {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, a)
}

func (s *Server) getAgentMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	messages, err := s.store.GetMessages(id, 100)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Transform to frontend Message interface: {id, role, text, time}
	out := make([]map[string]string, 0, len(messages))
	for _, m := range messages {
		out = append(out, map[string]string{
			"id":   fmt.Sprintf("%d", m.ID),
			"role": mapSenderToRole(m.Sender),
			"text": m.Content,
			"time": formatMessageTime(m.CreatedAt),
		})
	}
	jsonResponse(w, out)
}

func (s *Server) listRunningAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.orch.ListRunning(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, agents)
}

func (s *Server) startAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if a == nil {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	if err := s.orch.EnsureAgent(r.Context(), id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "started"})
}

func (s *Server) stopAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if a == nil {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	if err := s.orch.StopAgent(r.Context(), id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "stopped"})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.store.ListTasks()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	agentNames := s.agentNameMap()
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskToAPI(t, agentNames))
	}
	jsonResponse(w, out)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AgentID     string `json:"agent_id"`
		Name        string `json:"name"`
		Schedule    string `json:"schedule"`
		Prompt      string `json:"prompt"`
		ContextMode string `json:"context_mode"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.AgentID == "" || body.Name == "" || body.Schedule == "" || body.Prompt == "" {
		jsonError(w, "agent_id, name, schedule, and prompt are required", http.StatusBadRequest)
		return
	}

	// Normalize schedule (handles plain cron strings)
	normalized, err := schedule.NormalizeSchedule(body.Schedule)
	if err != nil {
		jsonError(w, fmt.Sprintf("invalid schedule: %v", err), http.StatusBadRequest)
		return
	}

	status := "active"
	if body.Enabled != nil && !*body.Enabled {
		status = "paused"
	}

	t := store.ScheduledTask{
		ID:          uuid.New().String(),
		AgentID:     body.AgentID,
		Name:        body.Name,
		Schedule:    normalized,
		Prompt:      body.Prompt,
		ContextMode: body.ContextMode,
		Status:      status,
	}
	if t.ContextMode == "" {
		t.ContextMode = "isolated"
	}

	// Calculate initial next_run_at
	if status == "active" {
		t.NextRunAt = schedule.CalculateNextRun(normalized)
	}

	if err := s.store.SaveTask(&t); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, taskToAPI(t, s.agentNameMap()))
}

func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	existing, err := s.store.GetTask(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		jsonError(w, "task not found", http.StatusNotFound)
		return
	}

	var body struct {
		Name        *string `json:"name"`
		Schedule    *string `json:"schedule"`
		Prompt      *string `json:"prompt"`
		AgentID     *string `json:"agent_id"`
		ContextMode *string `json:"context_mode"`
		Enabled     *bool   `json:"enabled"`
		Status      *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Apply updates
	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.Prompt != nil {
		existing.Prompt = *body.Prompt
	}
	if body.AgentID != nil {
		existing.AgentID = *body.AgentID
	}
	if body.ContextMode != nil {
		existing.ContextMode = *body.ContextMode
	}

	// Handle enabled bool → status mapping
	if body.Enabled != nil {
		if *body.Enabled {
			existing.Status = "active"
		} else if existing.Status != "completed" {
			existing.Status = "paused"
		}
	} else if body.Status != nil {
		existing.Status = *body.Status
	}

	// Handle schedule change
	if body.Schedule != nil {
		normalized, err := schedule.NormalizeSchedule(*body.Schedule)
		if err != nil {
			jsonError(w, fmt.Sprintf("invalid schedule: %v", err), http.StatusBadRequest)
			return
		}
		existing.Schedule = normalized
	}

	// Recalculate next_run_at
	if existing.Status == "active" {
		existing.NextRunAt = schedule.CalculateNextRun(existing.Schedule)
	} else {
		existing.NextRunAt = nil
	}

	if err := s.store.SaveTask(existing); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, taskToAPI(*existing, s.agentNameMap()))
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteTask(id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "deleted"})
}

func (s *Server) deleteCompletedTasks(w http.ResponseWriter, r *http.Request) {
	count, err := s.store.DeleteCompletedTasks()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]any{"status": "deleted", "count": count})
}

func (s *Server) listSwarms(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListSwarmRuns()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, runs)
}

func (s *Server) createSwarm(w http.ResponseWriter, r *http.Request) {
	var req swarm.SwarmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Task == "" || len(req.Agents) == 0 {
		jsonError(w, "task and agents are required", http.StatusBadRequest)
		return
	}

	// Validate graph before launching
	if _, err := swarm.BuildPlan(req.Agents, req.Synapses, req.LeadAgent); err != nil {
		jsonError(w, fmt.Sprintf("invalid swarm graph: %v", err), http.StatusBadRequest)
		return
	}

	run, err := s.swarmCoord.RunSwarm(r.Context(), req)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, run)
}

func (s *Server) deleteSwarm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteSwarmRun(id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "deleted"})
}

func (s *Server) getSwarm(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	run, err := s.swarmCoord.GetStatus(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if run == nil {
		jsonError(w, "swarm not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, run)
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	agents, _ := s.orch.ListRunning(r.Context())
	agentDefs, _ := s.store.ListAgents()
	tasks, _ := s.store.ListTasks()

	pendingTasks := 0
	for _, t := range tasks {
		if t.Status == "active" {
			pendingTasks++
		}
	}

	// Build agent ID → name lookup
	agentNames := make(map[string]string, len(agentDefs))
	for _, a := range agentDefs {
		agentNames[a.ID] = a.Name
	}

	// Format uptime
	uptime := formatUptime(time.Since(s.startedAt))

	// Recent messages
	recentMsgs, _ := s.store.GetRecentMessages(10)
	recentOut := make([]map[string]string, 0, len(recentMsgs))
	for _, m := range recentMsgs {
		agentName := agentNames[m.AgentID]
		if agentName == "" {
			agentName = m.AgentID
		}
		recentOut = append(recentOut, map[string]string{
			"id":    fmt.Sprintf("%d", m.ID),
			"agent": agentName,
			"role":  mapSenderToRole(m.Sender),
			"text":  m.Content,
			"time":  formatMessageTime(m.CreatedAt),
		})
	}

	status := map[string]any{
		"status":          "ok",
		"active_agents":   len(agents),
		"agents_count":    len(agentDefs),
		"pending_tasks":   pendingTasks,
		"uptime":          uptime,
		"recent_messages": recentOut,
		"nats":            "ok",
		"timestamp":       time.Now().UTC(),
		"version":         strings.TrimSpace("dev"),
	}

	jsonResponse(w, status)
}

func (s *Server) getAgentMD(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if a == nil {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	workspace := a.Workspace
	if workspace == "" {
		workspace = id
	}
	image := s.registry.ResolveImage(id)
	content, err := s.orch.ReadVolumeFile(r.Context(), workspace, "AGENT.md", image)
	if err != nil || content == "" {
		// Volume or file doesn't exist yet — return template
		content = agentMDTemplate
	}
	jsonResponse(w, map[string]string{"content": content})
}

func (s *Server) updateAgentMD(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a, err := s.store.GetAgent(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if a == nil {
		jsonError(w, "agent not found", http.StatusNotFound)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	workspace := a.Workspace
	if workspace == "" {
		workspace = id
	}
	image := s.registry.ResolveImage(id)
	if err := s.orch.WriteVolumeFile(r.Context(), workspace, "AGENT.md", body.Content, image); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "saved"})
}

func (s *Server) getUserProfile(w http.ResponseWriter, r *http.Request) {
	content, err := s.registry.GetUserMD()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"content": content})
}

func (s *Server) updateUserProfile(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.registry.SaveUserMD(body.Content); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "saved"})
}

func (s *Server) agentNameMap() map[string]string {
	agents, _ := s.store.ListAgents()
	m := make(map[string]string, len(agents))
	for _, a := range agents {
		m[a.ID] = a.Name
	}
	return m
}

func taskToAPI(t store.ScheduledTask, agentNames map[string]string) map[string]any {
	m := map[string]any{
		"id":               t.ID,
		"name":             t.Name,
		"schedule":         t.Schedule,
		"schedule_display": schedule.FormatSchedule(t.Schedule),
		"agent_id":         t.AgentID,
		"prompt":           t.Prompt,
		"enabled":          t.Status == "active",
		"status":           t.Status,
	}
	if name, ok := agentNames[t.AgentID]; ok {
		m["agent_name"] = name
	}
	if t.LastRunAt != nil {
		m["last_run"] = formatMessageTime(*t.LastRunAt)
	}
	if t.NextRunAt != nil {
		m["next_run"] = formatMessageTime(*t.NextRunAt)
	}
	return m
}

func mapSenderToRole(sender string) string {
	if sender == "agent" {
		return "assistant"
	}
	return "user"
}

func formatMessageTime(t time.Time) string {
	local := t.Local()
	now := time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return local.Format("15:04")
	}
	return local.Format("Jan 2 15:04")
}

func formatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
