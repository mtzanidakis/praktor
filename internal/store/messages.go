package store

import (
	"encoding/json"
	"fmt"
	"time"
)

type Message struct {
	ID        int64           `json:"id"`
	AgentID   string          `json:"agent_id"`
	Sender    string          `json:"sender"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) SaveMessage(msg *Message) error {
	result, err := s.db.Exec(`
		INSERT INTO messages (agent_id, sender, content, metadata)
		VALUES (?, ?, ?, ?)`,
		msg.AgentID, msg.Sender, msg.Content, msg.Metadata)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	msg.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) GetMessages(agentID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, agent_id, sender, content, metadata, created_at
		FROM messages
		WHERE agent_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var metadata *string
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Sender, &m.Content, &metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if metadata != nil {
			m.Metadata = json.RawMessage(*metadata)
		}
		messages = append(messages, m)
	}

	// Reverse to get chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, rows.Err()
}

func (s *Store) GetRecentMessages(limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, agent_id, sender, content, metadata, created_at
		FROM messages
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var metadata *string
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Sender, &m.Content, &metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if metadata != nil {
			m.Metadata = json.RawMessage(*metadata)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

type AgentMessageStats struct {
	AgentID      string
	MessageCount int
	LastActive   time.Time
}

func (s *Store) GetAgentMessageStats() (map[string]AgentMessageStats, error) {
	rows, err := s.db.Query(`
		SELECT agent_id, COUNT(*) as cnt, COALESCE(MAX(created_at), '') as last_active
		FROM messages
		GROUP BY agent_id`)
	if err != nil {
		return nil, fmt.Errorf("get agent message stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]AgentMessageStats)
	for rows.Next() {
		var as AgentMessageStats
		var lastActive string
		if err := rows.Scan(&as.AgentID, &as.MessageCount, &lastActive); err != nil {
			return nil, fmt.Errorf("scan agent stats: %w", err)
		}
		if lastActive != "" {
			as.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
		}
		stats[as.AgentID] = as
	}
	return stats, rows.Err()
}
