package store

import (
	"testing"
)

func TestGetAgentSecretsIncludesGlobal(t *testing.T) {
	s := newTestStore(t)

	// Create an agent
	if err := s.SaveAgent(&Agent{ID: "general", Name: "General", Workspace: "general"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	// Create a global secret
	sec := &Secret{
		ID:     "test-token",
		Name:   "test-token",
		Kind:   "string",
		Value:  []byte("encrypted"),
		Nonce:  []byte("nonce"),
		Global: true,
	}
	if err := s.SaveSecret(sec); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	// GetAgentSecrets should return the global secret even without agent_secrets entry
	secrets, err := s.GetAgentSecrets("general")
	if err != nil {
		t.Fatalf("get agent secrets: %v", err)
	}
	if len(secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(secrets))
	}
	if secrets[0].Name != "test-token" {
		t.Errorf("expected name 'test-token', got %q", secrets[0].Name)
	}
	if !secrets[0].Global {
		t.Error("expected secret to be global")
	}
}

func TestGetAgentSecretByNameGlobal(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveAgent(&Agent{ID: "general", Name: "General", Workspace: "general"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	sec := &Secret{
		ID:     "test-token-id",
		Name:   "test-token",
		Kind:   "string",
		Value:  []byte("encrypted"),
		Nonce:  []byte("nonce"),
		Global: true,
	}
	if err := s.SaveSecret(sec); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	// Should find by name even when agent has no explicit assignment
	got, err := s.GetAgentSecretByName("general", "test-token")
	if err != nil {
		t.Fatalf("get agent secret by name: %v", err)
	}
	if got == nil {
		t.Fatal("expected secret, got nil")
	}
	if got.Name != "test-token" {
		t.Errorf("expected name 'test-token', got %q", got.Name)
	}
}

func TestGetAgentSecretByNameAssigned(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveAgent(&Agent{ID: "general", Name: "General", Workspace: "general"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	if err := s.SaveAgent(&Agent{ID: "coder", Name: "Coder", Workspace: "coder"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	// Non-global secret assigned only to "general"
	sec := &Secret{
		ID:     "assigned-token",
		Name:   "assigned-token",
		Kind:   "string",
		Value:  []byte("encrypted"),
		Nonce:  []byte("nonce"),
		Global: false,
	}
	if err := s.SaveSecret(sec); err != nil {
		t.Fatalf("save secret: %v", err)
	}
	if err := s.AddAgentSecret("general", "assigned-token"); err != nil {
		t.Fatalf("add agent secret: %v", err)
	}

	// "general" should access it
	got, err := s.GetAgentSecretByName("general", "assigned-token")
	if err != nil {
		t.Fatalf("get agent secret by name: %v", err)
	}
	if got == nil {
		t.Fatal("expected secret for assigned agent, got nil")
	}

	// "coder" should NOT access it
	got, err = s.GetAgentSecretByName("coder", "assigned-token")
	if err != nil {
		t.Fatalf("get agent secret by name: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for unassigned agent, got secret")
	}
}

func TestGetAgentSecretByNameDenied(t *testing.T) {
	s := newTestStore(t)

	if err := s.SaveAgent(&Agent{ID: "general", Name: "General", Workspace: "general"}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	// Non-global secret, not assigned to this agent
	sec := &Secret{
		ID:     "private-token-id",
		Name:   "private-token",
		Kind:   "string",
		Value:  []byte("encrypted"),
		Nonce:  []byte("nonce"),
		Global: false,
	}
	if err := s.SaveSecret(sec); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	// Should NOT find it
	got, err := s.GetAgentSecretByName("general", "private-token")
	if err != nil {
		t.Fatalf("get agent secret by name: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil (access denied), got secret")
	}
}
