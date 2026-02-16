package agent

import (
	"sync"
	"time"
)

type Session struct {
	ID          string    `json:"id"`
	GroupID     string    `json:"group_id"`
	ContainerID string    `json:"container_id"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	LastActive  time.Time `json:"last_active"`
}

type SessionTracker struct {
	sessions map[string]*Session // groupID → session
	mu       sync.RWMutex
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		sessions: make(map[string]*Session),
	}
}

func (t *SessionTracker) Set(groupID string, session *Session) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessions[groupID] = session
}

func (t *SessionTracker) Get(groupID string) *Session {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessions[groupID]
}

func (t *SessionTracker) Remove(groupID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.sessions, groupID)
}

func (t *SessionTracker) Touch(groupID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.sessions[groupID]; ok {
		s.LastActive = time.Now()
	}
}

func (t *SessionTracker) ListIdle(timeout time.Duration) []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var idle []string
	now := time.Now()
	for groupID, s := range t.sessions {
		if now.Sub(s.LastActive) > timeout {
			idle = append(idle, groupID)
		}
	}
	return idle
}
