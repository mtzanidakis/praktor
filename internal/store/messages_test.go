package store

import (
	"testing"
)

func TestSearchMessages(t *testing.T) {
	s := newTestStore(t)

	// Create agents
	if err := s.SaveAgent(&Agent{ID: "alice", Name: "Alice", Workspace: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAgent(&Agent{ID: "bob", Name: "Bob", Workspace: "bob"}); err != nil {
		t.Fatal(err)
	}

	// Insert messages for alice
	for _, msg := range []Message{
		{AgentID: "alice", Sender: "user", Content: "Tell me about kubernetes deployments"},
		{AgentID: "alice", Sender: "agent", Content: "Kubernetes deployments manage replica sets and pods"},
		{AgentID: "alice", Sender: "user", Content: "How do I configure nginx"},
		{AgentID: "alice", Sender: "agent", Content: "You can configure nginx using the config file"},
	} {
		m := msg
		if err := s.SaveMessage(&m); err != nil {
			t.Fatal(err)
		}
	}

	// Insert messages for bob
	for _, msg := range []Message{
		{AgentID: "bob", Sender: "user", Content: "What is kubernetes"},
		{AgentID: "bob", Sender: "agent", Content: "Kubernetes is a container orchestration platform"},
	} {
		m := msg
		if err := s.SaveMessage(&m); err != nil {
			t.Fatal(err)
		}
	}

	// Test: search for "kubernetes" in alice's messages
	results, err := s.SearchMessages("alice", "kubernetes", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for alice/kubernetes, got %d", len(results))
	}

	// Test: search for "nginx" in alice's messages
	results, err = s.SearchMessages("alice", "nginx", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for alice/nginx, got %d", len(results))
	}

	// Test: agent isolation — bob's kubernetes results don't appear in alice's search
	results, err = s.SearchMessages("alice", "orchestration", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for alice/orchestration, got %d", len(results))
	}

	// Test: bob can find his own kubernetes messages
	results, err = s.SearchMessages("bob", "kubernetes", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for bob/kubernetes, got %d", len(results))
	}

	// Test: no results
	results, err = s.SearchMessages("alice", "nonexistent", 10)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}

	// Test: default limit
	results, err = s.SearchMessages("alice", "kubernetes", 0)
	if err != nil {
		t.Fatalf("search with default limit failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results with default limit, got %d", len(results))
	}
}
