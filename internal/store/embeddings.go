package store

import (
	"encoding/json"
	"fmt"
	"sort"
)

// AgentDistance represents a KNN search result.
type AgentDistance struct {
	AgentID  string  `json:"agent_id"`
	Distance float32 `json:"distance"`
}

// SaveAgentEmbedding upserts an agent's description embedding.
// vec0 tables don't support ON CONFLICT, so we delete then insert.
func (s *Store) SaveAgentEmbedding(agentID, descHash string, embedding []float32) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM agent_embeddings WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("delete old embedding: %w", err)
	}

	vecJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	if _, err := tx.Exec(`INSERT INTO agent_embeddings(agent_id, desc_hash, embedding) VALUES (?, ?, ?)`,
		agentID, descHash, string(vecJSON)); err != nil {
		return fmt.Errorf("insert embedding: %w", err)
	}

	return tx.Commit()
}

// DeleteAgentEmbedding removes an agent's embedding.
func (s *Store) DeleteAgentEmbedding(agentID string) error {
	_, err := s.db.Exec(`DELETE FROM agent_embeddings WHERE agent_id = ?`, agentID)
	return err
}

// FindNearestAgent performs KNN search against both profile and learned
// embeddings, returning the best match per agent ordered by distance.
func (s *Store) FindNearestAgent(embedding []float32, limit int) ([]AgentDistance, error) {
	vecJSON, err := json.Marshal(embedding)
	if err != nil {
		return nil, fmt.Errorf("marshal query embedding: %w", err)
	}
	vec := string(vecJSON)

	// Search both tables and keep the best (smallest) distance per agent.
	best := make(map[string]float32)

	for _, table := range []string{"agent_embeddings", "learned_embeddings"} {
		rows, err := s.db.Query(
			fmt.Sprintf(`SELECT agent_id, distance FROM %s WHERE embedding MATCH ? ORDER BY distance LIMIT ?`, table),
			vec, limit*2)
		if err != nil {
			return nil, fmt.Errorf("knn query %s: %w", table, err)
		}
		for rows.Next() {
			var ad AgentDistance
			if err := rows.Scan(&ad.AgentID, &ad.Distance); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan result: %w", err)
			}
			if d, ok := best[ad.AgentID]; !ok || ad.Distance < d {
				best[ad.AgentID] = ad.Distance
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Sort by distance, return up to limit.
	results := make([]AgentDistance, 0, len(best))
	for id, d := range best {
		results = append(results, AgentDistance{AgentID: id, Distance: d})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Distance < results[j].Distance
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// MaxLearnedEmbeddings is the maximum number of learned routing examples per agent.
const MaxLearnedEmbeddings = 50

// SaveLearnedEmbedding stores a routing example for an agent. Evicts the oldest
// entries when the per-agent count exceeds MaxLearnedEmbeddings.
func (s *Store) SaveLearnedEmbedding(agentID string, embedding []float32) error {
	vecJSON, err := json.Marshal(embedding)
	if err != nil {
		return fmt.Errorf("marshal embedding: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`INSERT INTO learned_embeddings(agent_id, embedding) VALUES (?, ?)`,
		agentID, string(vecJSON)); err != nil {
		return fmt.Errorf("insert learned embedding: %w", err)
	}

	// Evict oldest entries beyond the cap.
	if _, err := tx.Exec(`DELETE FROM learned_embeddings WHERE rowid IN (
		SELECT rowid FROM learned_embeddings WHERE agent_id = ?
		ORDER BY rowid DESC LIMIT -1 OFFSET ?
	)`, agentID, MaxLearnedEmbeddings); err != nil {
		return fmt.Errorf("evict old learned embeddings: %w", err)
	}

	return tx.Commit()
}

// DeleteLearnedEmbeddings removes all learned embeddings for an agent.
func (s *Store) DeleteLearnedEmbeddings(agentID string) error {
	_, err := s.db.Exec(`DELETE FROM learned_embeddings WHERE agent_id = ?`, agentID)
	return err
}

// GetAgentEmbeddingHash returns the stored description hash for an agent.
// Returns empty string if no embedding exists.
func (s *Store) GetAgentEmbeddingHash(agentID string) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT desc_hash FROM agent_embeddings WHERE agent_id = ?`, agentID).Scan(&hash)
	if err != nil {
		return "", nil // not found is not an error
	}
	return hash, nil
}
