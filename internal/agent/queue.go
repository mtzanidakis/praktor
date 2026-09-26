package agent

import (
	"sync"

	"github.com/mtzanidakis/praktor/internal/store"
)

type QueuedMessage struct {
	AgentID string
	Text    string
	Meta    map[string]string
	// DeferredLog is the history entry for a gated scheduled task. It is
	// saved only when the run produces a reply.
	DeferredLog *store.Message
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

func (q *AgentQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = nil
}

func (q *AgentQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Busy reports whether the queue is actively processing or has pending messages.
func (q *AgentQueue) Busy() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.locked || len(q.pending) > 0
}
