package swarm

import (
	"errors"
	"fmt"
)

// ExecutionPlan describes the order and grouping of agents for a swarm run.
type ExecutionPlan struct {
	Tiers          []ExecutionTier   // ordered groups; within a tier, agents run in parallel
	CollabGroups   [][]string        // sets of roles connected by bidirectional synapses
	PipelineInputs map[string][]string // role -> predecessor roles whose output feeds as context
}

// ExecutionTier is a group of agent roles that execute in parallel.
type ExecutionTier struct {
	Agents []string
}

// BuildPlan analyzes the swarm graph and produces an execution plan.
// It returns an error if the directed graph contains cycles or references unknown roles.
func BuildPlan(agents []SwarmAgent, synapses []Synapse, leadAgent string) (*ExecutionPlan, error) {
	// Build role set
	roleSet := make(map[string]bool, len(agents))
	for _, a := range agents {
		roleSet[a.Role] = true
	}

	if leadAgent != "" && !roleSet[leadAgent] {
		return nil, fmt.Errorf("lead agent %q is not a member of the swarm", leadAgent)
	}

	// Validate synapse references
	for _, s := range synapses {
		if !roleSet[s.From] {
			return nil, fmt.Errorf("synapse references unknown role %q", s.From)
		}
		if !roleSet[s.To] {
			return nil, fmt.Errorf("synapse references unknown role %q", s.To)
		}
	}

	// Separate directed and bidirectional synapses
	directed := make(map[string][]string)   // from -> []to
	inDegree := make(map[string]int)
	for _, role := range agents {
		inDegree[role.Role] = 0
	}

	// Build collab groups via union-find
	parent := make(map[string]string)
	for _, a := range agents {
		parent[a.Role] = a.Role
	}
	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	for _, s := range synapses {
		if s.Bidirectional {
			union(s.From, s.To)
		} else {
			directed[s.From] = append(directed[s.From], s.To)
			inDegree[s.To]++
		}
	}

	// Collect collab groups
	groups := make(map[string][]string)
	for _, a := range agents {
		root := find(a.Role)
		groups[root] = append(groups[root], a.Role)
	}
	var collabGroups [][]string
	// Map each role to its collab group representative (for topo sort node identity)
	roleToNode := make(map[string]string)
	for root, members := range groups {
		if len(members) > 1 {
			collabGroups = append(collabGroups, members)
		}
		for _, m := range members {
			roleToNode[m] = root
		}
	}

	// Build a collapsed DAG where each collab group is a single node
	nodeSet := make(map[string]bool)
	for _, a := range agents {
		nodeSet[roleToNode[a.Role]] = true
	}

	collapsedEdges := make(map[string]map[string]bool)
	collapsedInDegree := make(map[string]int)
	for node := range nodeSet {
		collapsedInDegree[node] = 0
	}

	for from, tos := range directed {
		fromNode := roleToNode[from]
		for _, to := range tos {
			toNode := roleToNode[to]
			if fromNode == toNode {
				continue // within same collab group
			}
			if collapsedEdges[fromNode] == nil {
				collapsedEdges[fromNode] = make(map[string]bool)
			}
			if !collapsedEdges[fromNode][toNode] {
				collapsedEdges[fromNode][toNode] = true
				collapsedInDegree[toNode]++
			}
		}
	}

	// Topological sort using Kahn's algorithm, grouping by depth
	depthMap := make(map[string]int)
	queue := make([]string, 0)
	for node, deg := range collapsedInDegree {
		if deg == 0 {
			queue = append(queue, node)
			depthMap[node] = 0
		}
	}

	processed := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		processed++

		for neighbor := range collapsedEdges[node] {
			collapsedInDegree[neighbor]--
			newDepth := depthMap[node] + 1
			if newDepth > depthMap[neighbor] {
				depthMap[neighbor] = newDepth
			}
			if collapsedInDegree[neighbor] == 0 {
				queue = append(queue, neighbor)
			}
		}
	}

	if processed != len(nodeSet) {
		return nil, errors.New("directed graph contains a cycle")
	}

	// Build pipeline inputs (per-role, which predecessor roles feed into it)
	pipelineInputs := make(map[string][]string)
	for from, tos := range directed {
		for _, to := range tos {
			pipelineInputs[to] = append(pipelineInputs[to], from)
		}
	}

	// Assign tiers by depth
	maxDepth := 0
	for _, d := range depthMap {
		if d > maxDepth {
			maxDepth = d
		}
	}

	// Build role-level depth from collapsed node depth
	roleDepth := make(map[string]int)
	for _, a := range agents {
		roleDepth[a.Role] = depthMap[roleToNode[a.Role]]
	}

	// Force lead agent into final tier
	leadDepth := maxDepth
	if leadAgent != "" {
		leadDepth = maxDepth + 1
		roleDepth[leadAgent] = leadDepth
	}

	// Build tiers
	tierMap := make(map[int][]string)
	for _, a := range agents {
		tierMap[roleDepth[a.Role]] = append(tierMap[roleDepth[a.Role]], a.Role)
	}

	finalMax := leadDepth
	tiers := make([]ExecutionTier, 0)
	for d := 0; d <= finalMax; d++ {
		if roles, ok := tierMap[d]; ok && len(roles) > 0 {
			tiers = append(tiers, ExecutionTier{Agents: roles})
		}
	}

	return &ExecutionPlan{
		Tiers:          tiers,
		CollabGroups:   collabGroups,
		PipelineInputs: pipelineInputs,
	}, nil
}
