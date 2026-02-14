package store

import (
	"encoding/json"
	"fmt"
	"time"
)

type Message struct {
	ID        int64           `json:"id"`
	GroupID   string          `json:"group_id"`
	Sender    string          `json:"sender"`
	Content   string          `json:"content"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) SaveMessage(msg *Message) error {
	result, err := s.db.Exec(`
		INSERT INTO messages (group_id, sender, content, metadata)
		VALUES (?, ?, ?, ?)`,
		msg.GroupID, msg.Sender, msg.Content, msg.Metadata)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}
	msg.ID, _ = result.LastInsertId()
	return nil
}

func (s *Store) GetMessages(groupID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, group_id, sender, content, metadata, created_at
		FROM messages
		WHERE group_id = ?
		ORDER BY created_at DESC
		LIMIT ?`, groupID, limit)
	if err != nil {
		return nil, fmt.Errorf("get messages: %w", err)
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var metadata *string
		if err := rows.Scan(&m.ID, &m.GroupID, &m.Sender, &m.Content, &metadata, &m.CreatedAt); err != nil {
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
		SELECT id, group_id, sender, content, metadata, created_at
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
		if err := rows.Scan(&m.ID, &m.GroupID, &m.Sender, &m.Content, &metadata, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		if metadata != nil {
			m.Metadata = json.RawMessage(*metadata)
		}
		messages = append(messages, m)
	}
	return messages, rows.Err()
}

type GroupMessageStats struct {
	GroupID      string
	MessageCount int
	LastActive   time.Time
}

func (s *Store) GetGroupMessageStats() (map[string]GroupMessageStats, error) {
	rows, err := s.db.Query(`
		SELECT group_id, COUNT(*) as cnt, COALESCE(MAX(created_at), '') as last_active
		FROM messages
		GROUP BY group_id`)
	if err != nil {
		return nil, fmt.Errorf("get group message stats: %w", err)
	}
	defer rows.Close()

	stats := make(map[string]GroupMessageStats)
	for rows.Next() {
		var gs GroupMessageStats
		var lastActive string
		if err := rows.Scan(&gs.GroupID, &gs.MessageCount, &lastActive); err != nil {
			return nil, fmt.Errorf("scan group stats: %w", err)
		}
		if lastActive != "" {
			gs.LastActive, _ = time.Parse("2006-01-02 15:04:05", lastActive)
		}
		stats[gs.GroupID] = gs
	}
	return stats, rows.Err()
}
