package natsbus

import "fmt"

// Topic patterns for NATS pub/sub communication.

func TopicAgentInput(agentID string) string {
	return fmt.Sprintf("agent.%s.input", agentID)
}

func TopicAgentOutput(agentID string) string {
	return fmt.Sprintf("agent.%s.output", agentID)
}

func TopicAgentControl(agentID string) string {
	return fmt.Sprintf("agent.%s.control", agentID)
}

func TopicAgentRoute(agentID string) string {
	return fmt.Sprintf("agent.%s.route", agentID)
}

func TopicIPC(agentID string) string {
	return fmt.Sprintf("host.ipc.%s", agentID)
}

func TopicSwarmOrchestrate(swarmID string) string {
	return fmt.Sprintf("swarm.%s.orchestrate", swarmID)
}

func TopicSwarmAgent(swarmID, role string) string {
	return fmt.Sprintf("swarm.%s.%s", swarmID, role)
}

func TopicSwarmResults(swarmID string) string {
	return fmt.Sprintf("swarm.%s.results", swarmID)
}

func TopicEventsAgent(agentID string) string {
	return fmt.Sprintf("events.agent.%s", agentID)
}

const (
	TopicEventsAll   = "events.>"
	TopicEventsTask  = "events.task.*"
	TopicEventsSwarm = "events.swarm.*"
)
