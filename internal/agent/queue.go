package agent

import "sync"

type QueuedMessage struct {
	GroupID string
	Text    string
	Meta    map[string]string
}

type GroupQueue struct {
	groupID string
	pending []QueuedMessage
	mu      sync.Mutex
	locked  bool
}

func NewGroupQueue(groupID string) *GroupQueue {
	return &GroupQueue{groupID: groupID}
}

func (q *GroupQueue) Enqueue(msg QueuedMessage) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pending = append(q.pending, msg)
}

func (q *GroupQueue) Dequeue() (QueuedMessage, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.pending) == 0 {
		return QueuedMessage{}, false
	}

	msg := q.pending[0]
	q.pending = q.pending[1:]
	return msg, true
}

func (q *GroupQueue) TryLock() bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.locked {
		return false
	}
	q.locked = true
	return true
}

func (q *GroupQueue) Unlock() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.locked = false
}

func (q *GroupQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}
