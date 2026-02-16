package agent

import (
	"sync"
	"time"
)

type Session struct {
	ID          string    `json:"id"`
	AgentID     string    `json:"agent_id"`
	ContainerID string    `json:"container_id"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	LastActive  time.Time `json:"last_active"`
}

type SessionTracker struct {
	sessions map[string]*Session // agentID → session
	mu       sync.RWMutex
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]*Session),
	}
}

func (t *SessionTracker) Set(agentID string, session *Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[agentID] = session
}

func (t *SessionTracker) Get(agentID string) *Session {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessions[agentID]
}

func (t *SessionTracker) Remove(agentID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, agentID)
}

func (t *SessionTracker) Touch(agentID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[agentID]; ok {
		s.LastActive = time.Now()
	}
}

func (t *SessionTracker) ListIdle(timeout time.Duration) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var idle []string
	now := time.Now()
	for agentID, s := range t.sessions {
		if now.Sub(s.LastActive) > timeout {
			idle = append(idle, agentID)
		}
	}
	return idle
}
