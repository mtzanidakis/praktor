package web

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/store"
)

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	secrets, err := s.store.ListSecrets()
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if secrets == nil {
		secrets = []store.Secret{}
	}

	// Enrich with agent assignments
	out := make([]map[string]any, 0, len(secrets))
	for _, sec := range secrets {
		agentIDs, _ := s.store.GetSecretAgentIDs(sec.ID)
		if agentIDs == nil {
			agentIDs = []string{}
		}
		out = append(out, map[string]any{
			"id":          sec.ID,
			"name":        sec.Name,
			"description": sec.Description,
			"kind":        sec.Kind,
			"filename":    sec.Filename,
			"global":      sec.Global,
			"agent_ids":   agentIDs,
			"created_at":  sec.CreatedAt,
			"updated_at":  sec.UpdatedAt,
		})
	}
	jsonResponse(w, out)
}

func (s *Server) createSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Kind        string   `json:"kind"`
		Filename    string   `json:"filename"`
		Value       string   `json:"value"`
		Global      bool     `json:"global"`
		AgentIDs    []string `json:"agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if body.Name == "" || body.Value == "" {
		jsonError(w, "name and value are required", http.StatusBadRequest)
		return
	}
	if body.Kind == "" {
		body.Kind = "string"
	}
	if body.Kind != "string" && body.Kind != "file" {
		jsonError(w, "kind must be 'string' or 'file'", http.StatusBadRequest)
		return
	}

	ciphertext, nonce, err := s.vault.Encrypt([]byte(body.Value))
	if err != nil {
		jsonError(w, "encryption failed", http.StatusInternalServerError)
		return
	}

	sec := &store.Secret{
		ID:          body.Name,
		Name:        body.Name,
		Description: body.Description,
		Kind:        body.Kind,
		Filename:    body.Filename,
		Value:       ciphertext,
		Nonce:       nonce,
		Global:      body.Global,
	}
	if err := s.store.SaveSecret(sec); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Set agent assignments
	_ = s.store.SetSecretAgents(body.Name, body.AgentIDs)

	s.publishSecretEvent(natsbus.TopicEventsSecretCreated, sec.ID, sec.Name)

	jsonResponse(w, map[string]any{
		"id":          sec.ID,
		"name":        sec.Name,
		"description": sec.Description,
		"kind":        sec.Kind,
		"global":      sec.Global,
	})
}

func (s *Server) getSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sec, err := s.store.GetSecret(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if sec == nil {
		jsonError(w, "secret not found", http.StatusNotFound)
		return
	}
	agentIDs, _ := s.store.GetSecretAgentIDs(sec.ID)
	if agentIDs == nil {
		agentIDs = []string{}
	}
	jsonResponse(w, map[string]any{
		"id":          sec.ID,
		"name":        sec.Name,
		"description": sec.Description,
		"kind":        sec.Kind,
		"filename":    sec.Filename,
		"global":      sec.Global,
		"agent_ids":   agentIDs,
		"created_at":  sec.CreatedAt,
		"updated_at":  sec.UpdatedAt,
	})
}

func (s *Server) updateSecret(w http.ResponseWriter, r *http.Request) {
	if s.vault == nil {
		jsonError(w, "vault not configured", http.StatusServiceUnavailable)
		return
	}

	id := r.PathValue("id")
	existing, err := s.store.GetSecret(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		jsonError(w, "secret not found", http.StatusNotFound)
		return
	}

	var body struct {
		Description *string  `json:"description"`
		Kind        *string  `json:"kind"`
		Filename    *string  `json:"filename"`
		Value       *string  `json:"value"`
		Global      *bool    `json:"global"`
		AgentIDs    []string `json:"agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if body.Description != nil {
		existing.Description = *body.Description
	}
	if body.Kind != nil {
		existing.Kind = *body.Kind
	}
	if body.Filename != nil {
		existing.Filename = *body.Filename
	}
	if body.Global != nil {
		existing.Global = *body.Global
	}

	// Re-encrypt if value provided
	if body.Value != nil {
		ciphertext, nonce, err := s.vault.Encrypt([]byte(*body.Value))
		if err != nil {
			jsonError(w, "encryption failed", http.StatusInternalServerError)
			return
		}
		existing.Value = ciphertext
		existing.Nonce = nonce
	}

	if err := s.store.SaveSecret(existing); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Update agent assignments if provided
	if body.AgentIDs != nil {
		_ = s.store.SetSecretAgents(id, body.AgentIDs)
	}

	s.publishSecretEvent(natsbus.TopicEventsSecretUpdated, existing.ID, existing.Name)

	jsonResponse(w, map[string]any{
		"id":          existing.ID,
		"name":        existing.Name,
		"description": existing.Description,
		"kind":        existing.Kind,
		"global":      existing.Global,
	})
}

func (s *Server) deleteSecret(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteSecret(id); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.publishSecretEvent(natsbus.TopicEventsSecretDeleted, id, id)
	jsonResponse(w, map[string]string{"status": "deleted"})
}

func (s *Server) getAgentSecrets(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	secrets, err := s.store.GetAgentSecrets(agentID)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if secrets == nil {
		secrets = []store.Secret{}
	}
	jsonResponse(w, secrets)
}

func (s *Server) setAgentSecrets(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	var body struct {
		SecretIDs []string `json:"secret_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.store.SetAgentSecrets(agentID, body.SecretIDs); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "updated"})
}

func (s *Server) addAgentSecret(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	secretID := r.PathValue("secretId")
	if err := s.store.AddAgentSecret(agentID, secretID); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "added"})
}

func (s *Server) removeAgentSecret(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("id")
	secretID := r.PathValue("secretId")
	if err := s.store.RemoveAgentSecret(agentID, secretID); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	jsonResponse(w, map[string]string{"status": "removed"})
}

func (s *Server) publishSecretEvent(topic, secretID, name string) {
	if s.nats == nil {
		return
	}
	event := map[string]any{
		"type":      topic,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]any{
			"id":   secretID,
			"name": name,
		},
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_ = s.nats.Publish(topic, data)
}

