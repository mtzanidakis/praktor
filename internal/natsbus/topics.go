package natsbus

import "fmt"

// Topic patterns for NATS pub/sub communication.
// %s placeholders are replaced with group/swarm IDs.

func TopicAgentInput(groupID string) string {
	return fmt.Sprintf("agent.%s.input", groupID)
}

func TopicAgentOutput(groupID string) string {
	return fmt.Sprintf("agent.%s.output", groupID)
}

func TopicAgentControl(groupID string) string {
	return fmt.Sprintf("agent.%s.control", groupID)
}

func TopicIPC(groupID string) string {
	return fmt.Sprintf("host.ipc.%s", groupID)
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

func TopicEventsAgent(groupID string) string {
	return fmt.Sprintf("events.agent.%s", groupID)
}

const (
	TopicEventsAll   = "events.>"
	TopicEventsTask  = "events.task.*"
	TopicEventsSwarm = "events.swarm.*"
)
