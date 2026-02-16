package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/router"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

type Bot struct {
	bot     *telego.Bot
	handler *th.BotHandler
	orch    *agent.Orchestrator
	router  *router.Router
	cfg     config.TelegramConfig
	cancel  context.CancelFunc

	// Track chat_id → agentID mapping for responses
	chatAgentMu sync.RWMutex
	chatAgent   map[int64]string // chatID → agentID that last handled a message
}

func NewBot(cfg config.TelegramConfig, orch *agent.Orchestrator, rtr *router.Router) (*Bot, error) {
	bot, err := telego.NewBot(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	b := &Bot{
		bot:       bot,
		orch:      orch,
		router:    rtr,
		cfg:       cfg,
		chatAgent: make(map[int64]string),
	}

	// Register output listener to send responses back to Telegram
	orch.OnOutput(func(agentID, content string, meta map[string]string) {
		// Try to get chat_id from meta
		chatIDStr := ""
		if meta != nil {
			chatIDStr = meta["chat_id"]
		}

		if chatIDStr == "" {
			// Fall back to looking up which chat last talked to this agent
			b.chatAgentMu.RLock()
			for cid, aid := range b.chatAgent {
				if aid == agentID {
					chatIDStr = strconv.FormatInt(cid, 10)
					break
				}
			}
			b.chatAgentMu.RUnlock()
		}

		if chatIDStr == "" {
			return
		}

		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			return
		}

		// Prefix with agent name for attribution (skip for default agent)
		attributed := content
		if agentID != rtr.DefaultAgent() {
			attributed = fmt.Sprintf("[%s] %s", agentID, content)
		}
		if err := b.SendMessage(context.Background(), chatID, attributed); err != nil {
			slog.Error("failed to send telegram message", "chat", chatID, "error", err)
		}
	})

	return b, nil
}

func (b *Bot) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel

	updates, err := b.bot.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		cancel()
		return fmt.Errorf("start long polling: %w", err)
	}

	handler, err := th.NewBotHandler(b.bot, updates)
	if err != nil {
		cancel()
		return fmt.Errorf("create handler: %w", err)
	}
	b.handler = handler

	handler.HandleMessage(func(hctx *th.Context, message telego.Message) error {
		b.handleMessage(ctx, message)
		return nil
	})

	go handler.Start()

	<-ctx.Done()
	_ = handler.Stop()
	return nil
}

func (b *Bot) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	if b.handler != nil {
		_ = b.handler.Stop()
	}
}

func (b *Bot) handleMessage(ctx context.Context, msg telego.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID

	// Check allow list
	if len(b.cfg.AllowFrom) > 0 {
		allowed := false
		for _, id := range b.cfg.AllowFrom {
			if id == userID {
				allowed = true
				break
			}
		}
		if !allowed {
			slog.Warn("unauthorized telegram user", "user_id", userID, "chat_id", chatID)
			return
		}
	}

	text := msg.Text
	if text == "" {
		if msg.Caption != "" {
			text = msg.Caption
		} else {
			return
		}
	}

	senderID := strconv.FormatInt(userID, 10)
	chatIDStr := strconv.FormatInt(chatID, 10)

	// Route message to appropriate agent
	agentID, cleanedMessage, err := b.router.Route(ctx, text)
	if err != nil {
		slog.Error("routing failed", "error", err)
		_ = b.SendMessage(ctx, chatID, "Sorry, I couldn't route your message to an agent.")
		return
	}

	if cleanedMessage == "" {
		cleanedMessage = text
	}

	// Track which chat is talking to which agent
	b.chatAgentMu.Lock()
	b.chatAgent[chatID] = agentID
	b.chatAgentMu.Unlock()

	// Send thinking indicator
	_ = b.sendChatAction(ctx, chatID, "typing")

	meta := map[string]string{
		"sender":  fmt.Sprintf("user:%s", senderID),
		"chat_id": chatIDStr,
	}

	if err := b.orch.HandleMessage(ctx, agentID, cleanedMessage, meta); err != nil {
		slog.Error("handle message failed", "agent", agentID, "error", err)
		_ = b.SendMessage(ctx, chatID, "Sorry, I encountered an error processing your message.")
	}
}

func (b *Bot) SendMessage(ctx context.Context, chatID int64, text string) error {
	chunks := chunkMessage(text, 4096)
	for _, chunk := range chunks {
		msg := tu.Message(tu.ID(chatID), chunk)
		_, err := b.bot.SendMessage(ctx, msg)
		if err != nil {
			return fmt.Errorf("send message: %w", err)
		}
	}
	return nil
}

func (b *Bot) sendChatAction(ctx context.Context, chatID int64, action string) error {
	return b.bot.SendChatAction(ctx, tu.ChatAction(tu.ID(chatID), action))
}
