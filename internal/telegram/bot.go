package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
)

type Bot struct {
	bot     *telego.Bot
	handler *th.BotHandler
	orch    *agent.Orchestrator
	store   *store.Store
	cfg     config.TelegramConfig
	cancel  context.CancelFunc
}

func NewBot(cfg config.TelegramConfig, orch *agent.Orchestrator, s *store.Store) (*Bot, error) {
	bot, err := telego.NewBot(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	b := &Bot{
		bot:   bot,
		orch:  orch,
		store: s,
		cfg:   cfg,
	}

	// Register output listener to send responses back to Telegram
	orch.OnOutput(func(groupID, content string) {
		chatID, err := strconv.ParseInt(groupID, 10, 64)
		if err != nil {
			return
		}
		if err := b.SendMessage(context.Background(), chatID, content); err != nil {
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

	groupID := strconv.FormatInt(chatID, 10)
	senderID := strconv.FormatInt(userID, 10)

	// Auto-register group if not exists
	existing, _ := b.store.GetGroup(groupID)
	if existing == nil {
		chatName := msg.Chat.Title
		if chatName == "" {
			chatName = fmt.Sprintf("chat-%s", groupID)
		}
		_ = b.store.SaveGroup(&store.Group{
			ID:     groupID,
			Name:   chatName,
			Folder: sanitizeFolder(groupID),
			IsMain: false,
		})
	}

	// Send thinking indicator
	_ = b.sendChatAction(ctx, chatID, "typing")

	meta := map[string]string{
		"sender":  fmt.Sprintf("user:%s", senderID),
		"chat_id": groupID,
	}

	if err := b.orch.HandleMessage(ctx, groupID, text, meta); err != nil {
		slog.Error("handle message failed", "group", groupID, "error", err)
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

func sanitizeFolder(id string) string {
	return "group-" + id
}
