package swarm

import (
	"testing"
)

func agents(roles ...string) []SwarmAgent {
	out := make([]SwarmAgent, len(roles))
	for i, r := range roles {
		out[i] = SwarmAgent{AgentID: r, Role: r}
	}
	return out
}

func TestBuildPlan_FanOut(t *testing.T) {
	plan, err := BuildPlan(agents("a", "b", "c", "lead"), nil, "lead")
	if err != nil {
		t.Fatal(err)
	}
	// All non-lead agents in tier 0, lead alone in tier 1
	if len(plan.Tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(plan.Tiers))
	}
	if len(plan.Tiers[0].Agents) != 3 {
		t.Fatalf("expected 3 agents in tier 0, got %d", len(plan.Tiers[0].Agents))
	}
	if len(plan.Tiers[1].Agents) != 1 || plan.Tiers[1].Agents[0] != "lead" {
		t.Fatal("expected lead agent alone in last tier")
	}
	if len(plan.CollabGroups) != 0 {
		t.Fatal("expected no collab groups")
	}
}

func TestBuildPlan_LinearPipeline(t *testing.T) {
	synapses := []Synapse{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
	}
	plan, err := BuildPlan(agents("a", "b", "c"), synapses, "c")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(plan.Tiers))
	}
	if plan.Tiers[0].Agents[0] != "a" {
		t.Fatal("expected a in tier 0")
	}
	if plan.Tiers[1].Agents[0] != "b" {
		t.Fatal("expected b in tier 1")
	}
	if plan.Tiers[2].Agents[0] != "c" {
		t.Fatal("expected c in tier 2")
	}
	// Pipeline inputs: b <- a, c <- b
	if len(plan.PipelineInputs["b"]) != 1 || plan.PipelineInputs["b"][0] != "a" {
		t.Fatal("expected b's pipeline input to be [a]")
	}
	if len(plan.PipelineInputs["c"]) != 1 || plan.PipelineInputs["c"][0] != "b" {
		t.Fatal("expected c's pipeline input to be [b]")
	}
}

func TestBuildPlan_CollaborativePair(t *testing.T) {
	synapses := []Synapse{
		{From: "a", To: "b", Bidirectional: true},
	}
	plan, err := BuildPlan(agents("a", "b", "lead"), synapses, "lead")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.CollabGroups) != 1 {
		t.Fatalf("expected 1 collab group, got %d", len(plan.CollabGroups))
	}
	if len(plan.CollabGroups[0]) != 2 {
		t.Fatal("expected collab group of size 2")
	}
}

func TestBuildPlan_MixedTopology(t *testing.T) {
	// a -> b, b <-> c, c -> lead
	synapses := []Synapse{
		{From: "a", To: "b"},
		{From: "b", To: "c", Bidirectional: true},
		{From: "c", To: "lead"},
	}
	plan, err := BuildPlan(agents("a", "b", "c", "lead"), synapses, "lead")
	if err != nil {
		t.Fatal(err)
	}
	// a in tier 0, b+c (collab) in tier 1, lead in tier 2
	if len(plan.Tiers) < 3 {
		t.Fatalf("expected at least 3 tiers, got %d", len(plan.Tiers))
	}
	if len(plan.CollabGroups) != 1 {
		t.Fatalf("expected 1 collab group, got %d", len(plan.CollabGroups))
	}
}

func TestBuildPlan_CycleDetection(t *testing.T) {
	synapses := []Synapse{
		{From: "a", To: "b"},
		{From: "b", To: "c"},
		{From: "c", To: "a"},
	}
	_, err := BuildPlan(agents("a", "b", "c"), synapses, "c")
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestBuildPlan_LeadAgentPlacement(t *testing.T) {
	// Even if lead has no edges, it's placed last
	plan, err := BuildPlan(agents("a", "b", "lead"), nil, "lead")
	if err != nil {
		t.Fatal(err)
	}
	lastTier := plan.Tiers[len(plan.Tiers)-1]
	if len(lastTier.Agents) != 1 || lastTier.Agents[0] != "lead" {
		t.Fatal("expected lead alone in last tier")
	}
}

func TestBuildPlan_UnknownRole(t *testing.T) {
	synapses := []Synapse{
		{From: "a", To: "unknown"},
	}
	_, err := BuildPlan(agents("a", "b"), synapses, "a")
	if err == nil {
		t.Fatal("expected error for unknown role")
	}
}

func TestBuildPlan_UnknownLeadAgent(t *testing.T) {
	_, err := BuildPlan(agents("a", "b"), nil, "unknown")
	if err == nil {
		t.Fatal("expected error for unknown lead agent")
	}
}

func TestBuildPlan_NoLeadAgent(t *testing.T) {
	// When no lead agent specified, all agents are in the same tier
	plan, err := BuildPlan(agents("a", "b", "c"), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Tiers) != 1 {
		t.Fatalf("expected 1 tier, got %d", len(plan.Tiers))
	}
	if len(plan.Tiers[0].Agents) != 3 {
		t.Fatal("expected all 3 agents in tier 0")
	}
}
