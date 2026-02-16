package telegram

import "testing"

func TestChunkMessage(t *testing.T) {
	// Short message
	chunks := chunkMessage("hello", 4096)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}

	// Exact limit
	msg := make([]byte, 4096)
	for i := range msg {
		msg[i] = 'a'
	}
	chunks = chunkMessage(string(msg), 4096)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk for exact limit, got %d", len(chunks))
	}

	// Over limit
	msg = make([]byte, 8192)
	for i := range msg {
		msg[i] = 'a'
	}
	chunks = chunkMessage(string(msg), 4096)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks, got %d", len(chunks))
	}

	// Split at newline
	msg = make([]byte, 5000)
	for i := range msg {
		msg[i] = 'a'
	}
	msg[3000] = '\n'
	chunks = chunkMessage(string(msg), 4096)
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks with newline split, got %d", len(chunks))
	}
	if len(chunks[0]) != 3001 { // Up to and including the newline
		t.Errorf("expected first chunk length 3001, got %d", len(chunks[0]))
	}
}
