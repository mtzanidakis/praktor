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

func (s *Server) registerAPI(mux *http.ServeMux) {
	// Groups
	mux.HandleFunc("GET /api/groups", s.listGroups)
	mux.HandleFunc("POST /api/groups", s.createGroup)
	mux.HandleFunc("GET /api/groups/{id}", s.getGroup)
	mux.HandleFunc("GET /api/groups/{id}/messages", s.getGroupMessages)

	// Agents
	mux.HandleFunc("GET /api/agents", s.listAgents)
	mux.HandleFunc("POST /api/agents/{groupID}/stop", s.stopAgent)

	// Tasks
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("PUT /api/tasks/{id}", s.updateTask)
	mux.HandleFunc("DELETE /api/tasks/{id}", s.deleteTask)

	// Swarms
	mux.HandleFunc("GET /api/swarms", s.listSwarms)
	mux.HandleFunc("POST /api/swarms", s.createSwarm)
	mux.HandleFunc("GET /api/swarms/{id}", s.getSwarm)

	// System
	mux.HandleFunc("GET /api/status", s.getStatus)
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.store.ListGroups()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Enrich with agent status, message count, and last active
	running, _ := s.orch.ListRunning(r.Context())
	runningSet := make(map[string]bool, len(running))
	for _, c := range running {
		runningSet[c.GroupID] = true
	}

	msgStats, _ := s.store.GetGroupMessageStats()

	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		groupType := "standard"
		if g.IsMain {
			groupType = "main"
		}

		agentStatus := "stopped"
		if runningSet[g.ID] {
			agentStatus = "running"
		}

		entry := map[string]any{
			"id":           g.ID,
			"name":         g.Name,
			"type":         groupType,
			"agent_status": agentStatus,
		}

		if stats, ok := msgStats[g.ID]; ok {
			entry["message_count"] = stats.MessageCount
			entry["last_active"] = formatMessageTime(stats.LastActive)
		} else {
			entry["message_count"] = 0
		}

		out = append(out, entry)
	}
	jsonResponse(w, out)
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var g store.Group
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if g.ID == "" || g.Name == "" || g.Folder == "" {
		jsonError(w, "id, name, and folder are required", http.StatusBadRequest)
		return
	}
	if err := s.store.SaveGroup(&g); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, g)
}

func (s *Server) getGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	g, err := s.store.GetGroup(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if g == nil {
		jsonError(w, "group not found", http.StatusNotFound)
		return
	}
	jsonResponse(w, g)
}

func (s *Server) getGroupMessages(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.orch.ListRunning(r.Context())
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, agents)
}

func (s *Server) stopAgent(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	if err := s.orch.StopAgent(r.Context(), groupID); err != nil {
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
	groupNames := s.groupNameMap()
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskToAPI(t, groupNames))
	}
	jsonResponse(w, out)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GroupID     string `json:"group_id"`
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
	if body.GroupID == "" || body.Name == "" || body.Schedule == "" || body.Prompt == "" {
		jsonError(w, "group_id, name, schedule, and prompt are required", http.StatusBadRequest)
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
		GroupID:     body.GroupID,
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
	jsonResponse(w, taskToAPI(t, s.groupNameMap()))
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
		GroupID     *string `json:"group_id"`
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
	if body.GroupID != nil {
		existing.GroupID = *body.GroupID
	}
	if body.ContextMode != nil {
		existing.ContextMode = *body.ContextMode
	}

	// Handle enabled bool → status mapping
	if body.Enabled != nil {
		if *body.Enabled {
			existing.Status = "active"
		} else {
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
	jsonResponse(w, taskToAPI(*existing, s.groupNameMap()))
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteTask(id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "deleted"})
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
	run, err := s.swarmCoord.RunSwarm(r.Context(), req)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, run)
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
	groups, _ := s.store.ListGroups()
	tasks, _ := s.store.ListTasks()

	pendingTasks := 0
	for _, t := range tasks {
		if t.Status == "active" {
			pendingTasks++
		}
	}

	// Build group ID → name lookup
	groupNames := make(map[string]string, len(groups))
	for _, g := range groups {
		groupNames[g.ID] = g.Name
	}

	// Format uptime
	uptime := formatUptime(time.Since(s.startedAt))

	// Recent messages
	recentMsgs, _ := s.store.GetRecentMessages(10)
	recentOut := make([]map[string]string, 0, len(recentMsgs))
	for _, m := range recentMsgs {
		groupName := groupNames[m.GroupID]
		if groupName == "" {
			groupName = m.GroupID
		}
		recentOut = append(recentOut, map[string]string{
			"id":    fmt.Sprintf("%d", m.ID),
			"group": groupName,
			"role":  mapSenderToRole(m.Sender),
			"text":  m.Content,
			"time":  formatMessageTime(m.CreatedAt),
		})
	}

	status := map[string]any{
		"status":          "ok",
		"active_agents":   len(agents),
		"groups_count":    len(groups),
		"pending_tasks":   pendingTasks,
		"uptime":          uptime,
		"recent_messages": recentOut,
		"nats":            "ok",
		"timestamp":       time.Now().UTC(),
		"version":         strings.TrimSpace("dev"),
	}

	jsonResponse(w, status)
}

func (s *Server) groupNameMap() map[string]string {
	groups, _ := s.store.ListGroups()
	m := make(map[string]string, len(groups))
	for _, g := range groups {
		m[g.ID] = g.Name
	}
	return m
}

func taskToAPI(t store.ScheduledTask, groupNames map[string]string) map[string]any {
	m := map[string]any{
		"id":               t.ID,
		"name":             t.Name,
		"schedule":         t.Schedule,
		"schedule_display": schedule.FormatSchedule(t.Schedule),
		"group_id":         t.GroupID,
		"prompt":           t.Prompt,
		"enabled":          t.Status == "active",
	}
	if name, ok := groupNames[t.GroupID]; ok {
		m["group_name"] = name
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
	now := time.Now()
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("Jan 2 15:04")
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
