package web

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mtzanidakis/praktor/internal/extensions"
)

func (s *Server) getAgentExtensions(w http.ResponseWriter, r *http.Request) {
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

	data, err := s.store.GetAgentExtensions(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	statusData, err := s.store.GetExtensionStatus(id)
	if err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Merge _status into extensions JSON
	var combined map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &combined); err != nil {
		combined = make(map[string]json.RawMessage)
	}
	combined["_status"] = json.RawMessage(statusData)

	w.Header().Set("Content-Type", "application/json")
	resp, _ := json.Marshal(combined)
	w.Write(resp)
}

func (s *Server) updateAgentExtensions(w http.ResponseWriter, r *http.Request) {
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

	// Parse and validate
	var ext extensions.AgentExtensions
	if err := json.NewDecoder(r.Body).Decode(&ext); err != nil {
		jsonError(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := ext.Validate(); err != nil {
		jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}

	// If extensions are non-empty, require nix_enabled
	if !ext.IsEmpty() {
		def, ok := s.registry.GetDefinition(id)
		if !ok || !def.NixEnabled {
			jsonError(w, "extensions require nix to be enabled for this agent", http.StatusBadRequest)
			return
		}
	}

	data, err := json.Marshal(ext)
	if err != nil {
		jsonError(w, "marshal failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.store.SetAgentExtensions(id, string(data)); err != nil {
		jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Stop running agent so it picks up new extensions on next message
	_ = s.orch.StopAgent(context.Background(), id)

	jsonResponse(w, map[string]string{"status": "saved"})
}
