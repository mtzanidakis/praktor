package agent

import "sync"

type QueuedMessage struct {
	AgentID string
	Text    string
	Meta    map[string]string
}

type AgentQueue struct {
	agentID string
	pending []QueuedMessage
	mu      sync.Mutex
	locked  bool
}

func NewAgentQueue(agentID string) *AgentQueue {
	return &AgentQueue{agentID: agentID}
}

func (q *AgentQueue) Enqueue(msg QueuedMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, msg)
}

func (q *AgentQueue) Dequeue() (QueuedMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		return QueuedMessage{}, false
	}

	msg := q.pending[0]
	q.pending = q.pending[1:]
	return msg, true
}

func (q *AgentQueue) TryLock() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.locked {
		return false
	}
	q.locked = true
	return true
}

func (q *AgentQueue) Unlock() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.locked = false
}

func (q *AgentQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}
