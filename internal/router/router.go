package router

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/embeddings"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/store"
)

// minConfidenceGap is the minimum L2 distance gap between the best and
// runner-up vector matches. Below this, routing is considered ambiguous and
// falls through to smart routing.
const minConfidenceGap float32 = 0.05

type Orchestrator interface {
	RouteQuery(ctx context.Context, agentID string, message string) (string, error)
}

type Router struct {
	registry     *registry.Registry
	defaultAgent string
	orch         Orchestrator
	embedder     embeddings.Embedder
	store        *store.Store
	threshold    float32
}

func New(reg *registry.Registry, cfg config.RouterConfig) *Router {
	threshold := float32(1.5)
	if cfg.VectorThreshold > 0 {
		threshold = float32(cfg.VectorThreshold)
	}
	return &Router{
		registry:     reg,
		defaultAgent: cfg.DefaultAgent,
		threshold:    threshold,
	}
}

func (r *Router) SetOrchestrator(orch Orchestrator) {
	r.orch = orch
}

// SetEmbedder enables vector-based routing using the given embedder and store.
func (r *Router) SetEmbedder(e embeddings.Embedder, s *store.Store) {
	r.embedder = e
	r.store = s
}

// SetVectorThreshold updates the distance threshold for vector routing.
func (r *Router) SetVectorThreshold(t float32) {
	r.threshold = t
}

func (r *Router) Route(ctx context.Context, message string) (agentID string, cleanedMessage string, err error) {
	// 0. Check for @swarm prefix
	if strings.HasPrefix(message, "@swarm ") {
		return "swarm", strings.TrimPrefix(message, "@swarm "), nil
	}

	// 1. Check for @agent_name prefix
	if strings.HasPrefix(message, "@") {
		parts := strings.SplitN(message, " ", 2)
		name := strings.TrimPrefix(parts[0], "@")
		if _, ok := r.registry.GetDefinition(name); ok {
			cleaned := ""
			if len(parts) > 1 {
				cleaned = parts[1]
			}
			return name, cleaned, nil
		}
		// Unknown agent name in prefix — fall through to smart routing
	}

	// 2. Try vector similarity routing (if configured)
	if r.embedder != nil && r.store != nil {
		vecs, err := r.embedder.Embed(ctx, []string{message})
		if err != nil {
			slog.Debug("vector embed failed, falling through", "error", err)
		} else if len(vecs) > 0 {
			results, err := r.store.FindNearestAgent(vecs[0], 2)
			if err != nil {
				slog.Debug("vector search failed, falling through", "error", err)
			} else if len(results) > 0 {
				best := results[0]
				if best.Distance < r.threshold {
					// If top-2 distances are too close, the match is ambiguous.
					if len(results) > 1 {
						gap := results[1].Distance - best.Distance
						if gap < minConfidenceGap {
							slog.Info("vector routing ambiguous, falling through",
								"best", best.AgentID,
								"distance", fmt.Sprintf("%.3f", best.Distance),
								"runner_up", results[1].AgentID,
								"runner_up_distance", fmt.Sprintf("%.3f", results[1].Distance),
								"gap", fmt.Sprintf("%.3f", gap))
							goto smartRoute
						}
					}
					if _, ok := r.registry.GetDefinition(best.AgentID); ok {
						slog.Info("vector routing matched",
							"agent", best.AgentID,
							"distance", fmt.Sprintf("%.3f", best.Distance),
							"threshold", fmt.Sprintf("%.3f", r.threshold))
						return best.AgentID, message, nil
					}
				} else {
					slog.Info("vector routing no confident match",
						"best", best.AgentID,
						"distance", fmt.Sprintf("%.3f", best.Distance),
						"threshold", fmt.Sprintf("%.3f", r.threshold))
				}
			}
		}
	}

smartRoute:
	// 3. Try smart routing via default agent
	if r.orch != nil && r.defaultAgent != "" {
		descs := r.registry.AgentDescriptions()
		if len(descs) > 1 {
			routedAgent, routeErr := r.orch.RouteQuery(ctx, r.defaultAgent, buildRoutingPrompt(descs, message))
			if routeErr != nil {
				slog.Debug("route query failed, using default agent", "error", routeErr)
			} else {
				// Validate the routed agent exists
				routedAgent = strings.TrimSpace(routedAgent)
				if _, ok := r.registry.GetDefinition(routedAgent); ok {
					r.learnRouting(ctx, routedAgent, message)
					return routedAgent, message, nil
				}
				slog.Debug("route query returned unknown agent, using default", "agent", routedAgent)
			}
		}
	}

	// 4. Fall back to default agent
	if r.defaultAgent == "" {
		return "", message, fmt.Errorf("no default agent configured")
	}
	return r.defaultAgent, message, nil
}

func (r *Router) DefaultAgent() string {
	return r.defaultAgent
}

// SetDefaultAgent updates the default agent used for routing.
func (r *Router) SetDefaultAgent(agent string) {
	r.defaultAgent = agent
}

// learnRouting saves the message embedding as a routing example for the agent.
func (r *Router) learnRouting(ctx context.Context, agentID, message string) {
	if r.embedder == nil || r.store == nil {
		return
	}
	vecs, err := r.embedder.Embed(ctx, []string{message})
	if err != nil || len(vecs) == 0 {
		return
	}
	if err := r.store.SaveLearnedEmbedding(agentID, vecs[0]); err != nil {
		slog.Error("failed to save learned routing embedding", "agent", agentID, "error", err)
	} else {
		slog.Info("learned routing example saved", "agent", agentID)
	}
}

func buildRoutingPrompt(descs map[string]string, message string) string {
	var sb strings.Builder
	sb.WriteString("You are a message router. Given the user's message, determine which agent should handle it.\n\n")
	sb.WriteString("Available agents:\n")
	for name, desc := range descs {
		fmt.Fprintf(&sb, "- %s: %s\n", name, desc)
	}
	sb.WriteString("\nUser message: ")
	sb.WriteString(message)
	sb.WriteString("\n\nRespond with ONLY the agent name, nothing else.")
	return sb.String()
}
