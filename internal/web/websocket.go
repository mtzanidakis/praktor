package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Event struct {
	Type    string `json:"type"`
	Payload any    `json:"payload"`
}

type Hub struct {
	clients   map[*websocket.Conn]bool
	broadcast chan Event
	mu        sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*websocket.Conn]bool),
		broadcast: make(chan Event, 256),
	}
}

func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-h.broadcast:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}

			h.mu.RLock()
			for client := range h.clients {
				if err := client.WriteMessage(websocket.TextMessage, data); err != nil {
					client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(event Event) {
	select {
	case h.broadcast <- event:
	default:
		slog.Warn("websocket broadcast channel full, dropping event")
	}
}

func (h *Hub) Register(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[conn] = true
}

func (h *Hub) Unregister(conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, conn)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	s.hub.Register(conn)
	defer func() {
		s.hub.Unregister(conn)
		conn.Close()
	}()

	// Keep connection alive, read messages (for future client → server)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}
