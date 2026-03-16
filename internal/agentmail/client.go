package agentmail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mtzanidakis/praktor/internal/registry"
)

const (
	wsURL  = "wss://ws.agentmail.to/v0"
	apiURL = "https://api.agentmail.to/v0"
)

// MessageHandler is called when an agent should receive an email notification.
type MessageHandler func(ctx context.Context, agentID, text string, meta map[string]string) error

// Client maintains a WebSocket connection to AgentMail for real-time events.
type Client struct {
	apiKey     string
	registry   *registry.Registry
	handler    MessageHandler
	mainChatID int64
	httpClient *http.Client

	mu   sync.Mutex
	conn *websocket.Conn

	// Track last event time and seen message IDs for catch-up dedup.
	seenMu    sync.Mutex
	lastEvent time.Time
	seen      map[string]struct{}
}

// NewClient creates a new AgentMail WebSocket client.
func NewClient(apiKey string, reg *registry.Registry, handler MessageHandler, mainChatID int64) *Client {
	return &Client{
		apiKey:     apiKey,
		registry:   reg,
		handler:    handler,
		mainChatID: mainChatID,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		lastEvent:  time.Now(),
		seen:       make(map[string]struct{}),
	}
}

// Run connects to the AgentMail WebSocket and processes events.
// It reconnects automatically with exponential backoff on disconnection.
// Blocks until the context is cancelled.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 60 * time.Second

	attempt := 0
	for {
		select {
		case <-ctx.Done():
			c.close()
			return
		default:
		}

		err := c.connect(ctx)
		if err != nil {
			attempt++
			if attempt <= 3 {
				slog.Debug("agentmail websocket connect attempt failed, retrying", "attempt", attempt, "error", err)
			} else {
				slog.Error("agentmail websocket connect failed", "attempt", attempt, "error", err)
			}
		} else {
			backoff = time.Second
			attempt = 0
			c.catchUp(ctx)
			c.readLoop(ctx)
		}

		c.close()

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *Client) connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.apiKey)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Subscribe to message events
	sub := map[string]any{
		"type": "subscribe",
		"filters": map[string]any{
			"event_types": []string{"message.received", "message.sent"},
		},
	}
	if err := conn.WriteJSON(sub); err != nil {
		conn.Close()
		return fmt.Errorf("subscribe: %w", err)
	}

	slog.Info("agentmail websocket connected")
	return nil
}

func (c *Client) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) readLoop(ctx context.Context) {
	// Send pings every 5 minutes to keep the connection alive
	pingTicker := time.NewTicker(5 * time.Minute)
	defer pingTicker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-pingTicker.C:
				c.mu.Lock()
				conn := c.conn
				c.mu.Unlock()
				if conn == nil {
					return
				}
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("agentmail websocket disconnected, reconnecting", "error", err)
			} else {
				slog.Info("agentmail websocket disconnected, reconnecting", "error", err)
			}
			return
		}

		c.handleEvent(ctx, data)
	}
}

// wsEvent represents an AgentMail WebSocket event.
type wsEvent struct {
	Type      string          `json:"type"`
	EventType string          `json:"event_type"`
	Message   json.RawMessage `json:"message,omitempty"`
}

type wsMessage struct {
	InboxID   string   `json:"inbox_id"`
	MessageID string   `json:"message_id"`
	ThreadID  string   `json:"thread_id"`
	From      string   `json:"from"`
	To        []string `json:"to"`
	Cc        []string `json:"cc"`
	Bcc       []string `json:"bcc"`
	Subject   string   `json:"subject"`
}

// markSeen records a message ID and updates lastEvent. Returns false if already seen.
func (c *Client) markSeen(messageID string) bool {
	c.seenMu.Lock()
	defer c.seenMu.Unlock()
	if _, ok := c.seen[messageID]; ok {
		return false
	}
	c.seen[messageID] = struct{}{}
	c.lastEvent = time.Now()
	// Prune old entries to avoid unbounded growth.
	if len(c.seen) > 1000 {
		c.seen = make(map[string]struct{})
	}
	return true
}

// catchUp polls the AgentMail REST API for messages received since the last
// WebSocket event to recover any missed during reconnection.
func (c *Client) catchUp(ctx context.Context) {
	inboxes := c.registry.AgentMailInboxes()
	if len(inboxes) == 0 {
		return
	}

	c.seenMu.Lock()
	since := c.lastEvent
	c.seenMu.Unlock()

	// Add a small buffer to avoid edge cases.
	since = since.Add(-30 * time.Second)

	for inboxID, agentID := range inboxes {
		msgs, err := c.listMessages(ctx, inboxID, since)
		if err != nil {
			slog.Warn("agentmail: catch-up failed", "inbox", inboxID, "agent", agentID, "error", err)
			continue
		}
		for _, msg := range msgs {
			if !c.markSeen(msg.MessageID) {
				continue
			}
			slog.Info("agentmail: catch-up message",
				"agent", agentID,
				"from", msg.From,
				"subject", msg.Subject,
				"thread_id", msg.ThreadID,
			)
			c.dispatchMessage(ctx, agentID, &msg)
		}
	}
}

// apiListResponse is the response from the list messages endpoint.
type apiListResponse struct {
	Messages []wsMessage `json:"messages"`
}

func (c *Client) listMessages(ctx context.Context, inboxID string, since time.Time) ([]wsMessage, error) {
	u, err := url.Parse(apiURL + "/inboxes/" + inboxID + "/messages")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("after", since.UTC().Format(time.RFC3339))
	q.Set("limit", "50")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	var result apiListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Messages, nil
}

func (c *Client) handleEvent(ctx context.Context, data []byte) {
	var ev wsEvent
	if err := json.Unmarshal(data, &ev); err != nil {
		slog.Warn("agentmail: malformed event", "error", err)
		return
	}

	if ev.Type != "event" || len(ev.Message) == 0 {
		return
	}

	var msg wsMessage
	if err := json.Unmarshal(ev.Message, &msg); err != nil {
		// message field is not an object (e.g. subscription confirmation string)
		return
	}

	switch ev.EventType {
	case "message.received":
		c.handleMessageReceived(ctx, &msg)
	case "message.sent":
		c.handleMessageSent(&msg)
	}
}

func (c *Client) handleMessageReceived(ctx context.Context, msg *wsMessage) {
	agentID, ok := c.registry.FindByAgentMailInbox(msg.InboxID)
	if !ok {
		slog.Warn("agentmail: no agent for inbox", "inbox_id", msg.InboxID)
		return
	}

	if !c.markSeen(msg.MessageID) {
		return
	}

	slog.Info("agentmail: message received",
		"agent", agentID,
		"from", msg.From,
		"subject", msg.Subject,
		"thread_id", msg.ThreadID,
	)

	c.dispatchMessage(ctx, agentID, msg)
}

func (c *Client) dispatchMessage(ctx context.Context, agentID string, msg *wsMessage) {
	prompt := fmt.Sprintf(
		"You received a new email from %s, subject: \"%s\".\n"+
			"Read the thread: agentmail inboxes:threads retrieve --inbox-id %s --thread-id %s\n"+
			"If a reply is needed, use: agentmail inboxes:messages reply --inbox-id %s --message-id %s --text \"your reply\"\n"+
			"Reply if the email is a direct message, question, or request addressed to you. "+
			"Do NOT reply to newsletters, automated notifications, marketing emails, no-reply senders, or bulk messages.",
		msg.From, msg.Subject, msg.InboxID, msg.ThreadID, msg.InboxID, msg.MessageID,
	)

	meta := map[string]string{
		"sender":  "agentmail",
		"chat_id": fmt.Sprintf("%d", c.mainChatID),
	}

	if err := c.handler(ctx, agentID, prompt, meta); err != nil {
		slog.Error("agentmail: failed to dispatch to agent", "agent", agentID, "error", err)
	}
}

func (c *Client) handleMessageSent(msg *wsMessage) {
	agentID, ok := c.registry.FindByAgentMailInbox(msg.InboxID)
	if !ok {
		return
	}

	slog.Info("agentmail: message sent",
		"agent", agentID,
		"to", msg.To,
		"subject", msg.Subject,
	)

}
