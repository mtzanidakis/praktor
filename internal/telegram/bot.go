package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/router"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/swarm"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"github.com/nats-io/nats.go"
)

type Bot struct {
	bot        *telego.Bot
	handler    *th.BotHandler
	orch       *agent.Orchestrator
	router     *router.Router
	store      *store.Store
	cfg        config.TelegramConfig
	cancel     context.CancelFunc
	swarmCoord *swarm.Coordinator
	registry   *registry.Registry
	bus        *natsbus.Bus

	// Track chat_id → agentID mapping for responses
	chatAgentMu sync.RWMutex
	chatAgent   map[int64]string // chatID → agentID that last handled a message

	// Track swarm → chat_id for result delivery
	swarmChatMu sync.RWMutex
	swarmChat   map[string]int64 // swarmID → chatID
}

func NewBot(cfg config.TelegramConfig, orch *agent.Orchestrator, rtr *router.Router, sc *swarm.Coordinator, reg *registry.Registry, bus *natsbus.Bus, s *store.Store) (*Bot, error) {
	bot, err := telego.NewBot(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}

	b := &Bot{
		bot:        bot,
		orch:       orch,
		router:     rtr,
		store:      s,
		cfg:        cfg,
		swarmCoord: sc,
		registry:   reg,
		bus:        bus,
		chatAgent:  make(map[int64]string),
		swarmChat:  make(map[string]int64),
	}

	// Register bot commands with Telegram so they appear in the menu
	_ = bot.SetMyCommands(context.Background(), &telego.SetMyCommandsParams{
		Commands: []telego.BotCommand{
			{Command: "start", Description: "Say hello to an agent"},
			{Command: "stop", Description: "Abort the active agent run"},
			{Command: "reset", Description: "Reset conversation session"},
			{Command: "agents", Description: "List available agents"},
		},
	})

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
			attributed = fmt.Sprintf("_%s:_ %s", agentID, content)
		}
		if err := b.SendMessage(context.Background(), chatID, attributed); err != nil {
			slog.Error("failed to send telegram message", "chat", chatID, "error", err)
		}
	})

	// Subscribe to swarm events for result delivery
	if bus != nil && sc != nil {
		client, cerr := natsbus.NewClient(bus)
		if cerr == nil {
			_, _ = client.Subscribe(natsbus.TopicEventsSwarm, func(msg *nats.Msg) {
				b.handleSwarmEvent(msg)
			})
		}
	}

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

	// Command handlers — registered before the catch-all so they match first
	handler.HandleMessage(func(hctx *th.Context, message telego.Message) error {
		if !b.allowedUser(message) {
			return nil
		}
		_, _, payload := tu.ParseCommandPayload(message.Text)
		b.cmdStart(ctx, message, payload)
		return nil
	}, th.CommandEqual("start"))

	handler.HandleMessage(func(hctx *th.Context, message telego.Message) error {
		if !b.allowedUser(message) {
			return nil
		}
		_, _, payload := tu.ParseCommandPayload(message.Text)
		b.cmdStop(ctx, message.Chat.ID, payload)
		return nil
	}, th.CommandEqual("stop"))

	handler.HandleMessage(func(hctx *th.Context, message telego.Message) error {
		if !b.allowedUser(message) {
			return nil
		}
		_, _, payload := tu.ParseCommandPayload(message.Text)
		b.cmdReset(ctx, message.Chat.ID, payload)
		return nil
	}, th.CommandEqual("reset"))

	handler.HandleMessage(func(hctx *th.Context, message telego.Message) error {
		if !b.allowedUser(message) {
			return nil
		}
		b.cmdAgents(ctx, message.Chat.ID)
		return nil
	}, th.CommandEqual("agents"))

	// Catch-all for regular messages
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
	if !b.allowedUser(msg) {
		return
	}

	chatID := msg.Chat.ID
	userID := msg.From.ID

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

	// Handle @swarm command
	if agentID == "swarm" {
		b.handleSwarmCommand(ctx, chatID, cleanedMessage)
		return
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
	text = toTelegramMarkdown(text)
	chunks := chunkMessage(text, 4096)
	for _, chunk := range chunks {
		msg := tu.Message(tu.ID(chatID), chunk)
		msg.ParseMode = telego.ModeMarkdown
		_, err := b.bot.SendMessage(ctx, msg)
		if err != nil {
			// Markdown parsing can fail on unescaped characters;
			// retry as plain text so the message still gets delivered.
			msg.ParseMode = ""
			_, err = b.bot.SendMessage(ctx, msg)
		}
		if err != nil {
			return fmt.Errorf("send message: %w", err)
		}
	}
	return nil
}

func (b *Bot) sendChatAction(ctx context.Context, chatID int64, action string) error {
	return b.bot.SendChatAction(ctx, tu.ChatAction(tu.ID(chatID), action))
}

// handleSwarmCommand parses the swarm syntax and launches a swarm.
//
// Syntax:
//   - agent1,agent2,agent3: task    -> fan-out, first agent = lead
//   - agent1>agent2>agent3: task    -> pipeline, last agent = lead
//   - agent1<>agent2,agent3: task   -> collaborative + independent
func (b *Bot) handleSwarmCommand(ctx context.Context, chatID int64, message string) {
	if b.swarmCoord == nil || b.registry == nil {
		_ = b.SendMessage(ctx, chatID, "Swarm support is not configured.")
		return
	}

	// Split at first ": " to get agents spec and task
	colonIdx := strings.Index(message, ": ")
	if colonIdx < 0 {
		_ = b.SendMessage(ctx, chatID, "Invalid swarm syntax. Use: `agent1,agent2: task` or `agent1>agent2: task` or `agent1<>agent2: task`")
		return
	}
	agentSpec := strings.TrimSpace(message[:colonIdx])
	task := strings.TrimSpace(message[colonIdx+2:])
	if task == "" {
		_ = b.SendMessage(ctx, chatID, "Task is required after the colon.")
		return
	}

	agents, synapses, leadAgent, err := b.parseSwarmSpec(agentSpec)
	if err != nil {
		_ = b.SendMessage(ctx, chatID, fmt.Sprintf("Invalid swarm spec: %s", err))
		return
	}

	req := swarm.SwarmRequest{
		Name:      fmt.Sprintf("Telegram Swarm"),
		LeadAgent: leadAgent,
		Agents:    agents,
		Synapses:  synapses,
		Task:      task,
	}

	_ = b.SendMessage(ctx, chatID, fmt.Sprintf("Launching swarm with %d agents...", len(agents)))

	run, err := b.swarmCoord.RunSwarm(ctx, req)
	if err != nil {
		_ = b.SendMessage(ctx, chatID, fmt.Sprintf("Failed to launch swarm: %s", err))
		return
	}

	// Track which chat started this swarm
	b.swarmChatMu.Lock()
	b.swarmChat[run.ID] = chatID
	b.swarmChatMu.Unlock()
}

func (b *Bot) parseSwarmSpec(spec string) ([]swarm.SwarmAgent, []swarm.Synapse, string, error) {
	var agents []swarm.SwarmAgent
	var synapses []swarm.Synapse
	var leadAgent string
	seen := make(map[string]bool)

	addAgent := func(name string) error {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("empty agent name")
		}
		if seen[name] {
			return nil
		}
		if _, ok := b.registry.GetDefinition(name); !ok {
			return fmt.Errorf("unknown agent: %s", name)
		}
		seen[name] = true
		agents = append(agents, swarm.SwarmAgent{
			AgentID:   name,
			Role:      name,
			Workspace: name,
		})
		return nil
	}

	// Check for pipeline syntax (>)
	if strings.Contains(spec, ">") && !strings.Contains(spec, "<>") {
		parts := strings.Split(spec, ">")
		for _, p := range parts {
			if err := addAgent(p); err != nil {
				return nil, nil, "", err
			}
		}
		// Create pipeline synapses
		for i := 0; i < len(parts)-1; i++ {
			synapses = append(synapses, swarm.Synapse{
				From: strings.TrimSpace(parts[i]),
				To:   strings.TrimSpace(parts[i+1]),
			})
		}
		leadAgent = strings.TrimSpace(parts[len(parts)-1])
		return agents, synapses, leadAgent, nil
	}

	// Check for collaborative syntax (<>)
	// Split by comma first, then check each segment for <>
	segments := strings.Split(spec, ",")
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if strings.Contains(seg, "<>") {
			pair := strings.SplitN(seg, "<>", 2)
			a := strings.TrimSpace(pair[0])
			bName := strings.TrimSpace(pair[1])
			if err := addAgent(a); err != nil {
				return nil, nil, "", err
			}
			if err := addAgent(bName); err != nil {
				return nil, nil, "", err
			}
			synapses = append(synapses, swarm.Synapse{
				From:          a,
				To:            bName,
				Bidirectional: true,
			})
		} else {
			if err := addAgent(seg); err != nil {
				return nil, nil, "", err
			}
		}
	}

	// Default lead: first agent
	if len(agents) > 0 {
		leadAgent = agents[0].Role
	}

	return agents, synapses, leadAgent, nil
}

// allowedUser checks whether the message sender is in the allow list.
func (b *Bot) allowedUser(msg telego.Message) bool {
	if len(b.cfg.AllowFrom) == 0 {
		return true
	}
	for _, id := range b.cfg.AllowFrom {
		if id == msg.From.ID {
			return true
		}
	}
	slog.Warn("unauthorized telegram user", "user_id", msg.From.ID, "chat_id", msg.Chat.ID)
	return false
}

// resolveAgent returns the agent ID from payload or falls back to the last agent for the chat.
func (b *Bot) resolveAgent(chatID int64, payload string) string {
	if payload != "" {
		name := strings.Fields(payload)[0]
		return strings.TrimPrefix(name, "@")
	}
	b.chatAgentMu.RLock()
	defer b.chatAgentMu.RUnlock()
	return b.chatAgent[chatID]
}

func (b *Bot) cmdStart(ctx context.Context, msg telego.Message, payload string) {
	chatID := msg.Chat.ID
	agentID := b.resolveAgent(chatID, payload)
	if agentID == "" {
		agentID = b.router.DefaultAgent()
	}

	b.chatAgentMu.Lock()
	b.chatAgent[chatID] = agentID
	b.chatAgentMu.Unlock()

	_ = b.sendChatAction(ctx, chatID, "typing")

	meta := map[string]string{
		"sender":  fmt.Sprintf("user:%d", msg.From.ID),
		"chat_id": strconv.FormatInt(chatID, 10),
	}
	if err := b.orch.HandleMessage(ctx, agentID, "Hello!", meta); err != nil {
		slog.Error("handle start failed", "agent", agentID, "error", err)
		_ = b.SendMessage(ctx, chatID, "Sorry, I encountered an error starting the conversation.")
	}
}

func (b *Bot) cmdStop(ctx context.Context, chatID int64, payload string) {
	agentID := b.resolveAgent(chatID, payload)
	if agentID == "" {
		_ = b.SendMessage(ctx, chatID, "Usage: /stop [agent]")
		return
	}
	if err := b.orch.AbortSession(ctx, agentID); err != nil {
		_ = b.SendMessage(ctx, chatID, fmt.Sprintf("Failed to stop *%s*: %s", agentID, err))
		return
	}
	_ = b.SendMessage(ctx, chatID, fmt.Sprintf("Stopped *%s*.", agentID))
}

func (b *Bot) cmdReset(ctx context.Context, chatID int64, payload string) {
	agentID := b.resolveAgent(chatID, payload)
	if agentID == "" {
		_ = b.SendMessage(ctx, chatID, "Usage: /reset [agent]")
		return
	}
	if err := b.orch.ClearSession(ctx, agentID); err != nil {
		_ = b.SendMessage(ctx, chatID, fmt.Sprintf("Failed to clear session for *%s*: %s", agentID, err))
		return
	}
	_ = b.SendMessage(ctx, chatID, fmt.Sprintf("New session started for *%s*.", agentID))
}

func (b *Bot) cmdAgents(ctx context.Context, chatID int64) {
	agents, err := b.store.ListAgents()
	if err != nil {
		_ = b.SendMessage(ctx, chatID, "Failed to list agents.")
		return
	}

	running, _ := b.orch.ListRunning(ctx)
	runningSet := make(map[string]bool, len(running))
	for _, c := range running {
		runningSet[c.AgentID] = true
	}

	msgStats, _ := b.store.GetAgentMessageStats()

	var sb strings.Builder
	sb.WriteString("*Agents*\n\n")
	for _, a := range agents {
		status := "stopped"
		if runningSet[a.ID] {
			status = "running"
		}

		model := b.registry.ResolveModel(a.ID)

		sb.WriteString(fmt.Sprintf("*%s*", a.ID))
		if a.Description != "" {
			sb.WriteString(fmt.Sprintf(" — %s", a.Description))
		}
		sb.WriteString(fmt.Sprintf("\n  Status: `%s` | Model: `%s`", status, model))

		if stats, ok := msgStats[a.ID]; ok {
			sb.WriteString(fmt.Sprintf(" | Messages: %d", stats.MessageCount))
		}
		sb.WriteString("\n\n")
	}

	if len(agents) == 0 {
		sb.WriteString("No agents configured.")
	}

	_ = b.SendMessage(ctx, chatID, sb.String())
}

// handleSwarmEvent handles swarm completion events and delivers results to Telegram.
func (b *Bot) handleSwarmEvent(msg *nats.Msg) {
	var event struct {
		Type    string          `json:"type"`
		SwarmID string          `json:"swarm_id"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		return
	}

	if event.Type != "swarm_completed" && event.Type != "swarm_failed" {
		return
	}

	b.swarmChatMu.RLock()
	chatID, ok := b.swarmChat[event.SwarmID]
	b.swarmChatMu.RUnlock()

	if ok {
		// Clean up tracking
		b.swarmChatMu.Lock()
		delete(b.swarmChat, event.SwarmID)
		b.swarmChatMu.Unlock()
	} else if b.cfg.MainChatID != 0 {
		// Swarm launched from Mission Control — deliver to main chat
		chatID = b.cfg.MainChatID
	} else {
		return
	}

	ctx := context.Background()

	if event.Type == "swarm_failed" {
		_ = b.SendMessage(ctx, chatID, "Swarm failed.")
		return
	}

	// Get the swarm run to extract results
	run, err := b.swarmCoord.GetStatus(event.SwarmID)
	if err != nil || run == nil {
		_ = b.SendMessage(ctx, chatID, "Swarm completed but could not retrieve results.")
		return
	}

	var results []swarm.AgentResult
	if run.Results != nil {
		_ = json.Unmarshal(run.Results, &results)
	}

	// Find lead agent's result
	var leadResult string
	for _, r := range results {
		if r.Role == run.LeadAgent && r.Output != "" {
			leadResult = r.Output
			break
		}
	}

	if leadResult != "" {
		_ = b.SendMessage(ctx, chatID, fmt.Sprintf("*Swarm Result* (%s):\n\n%s", run.Name, leadResult))
	} else {
		// Send all results if no lead result
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("*Swarm Complete* (%s):\n\n", run.Name))
		for _, r := range results {
			sb.WriteString(fmt.Sprintf("*%s* [%s]", r.Role, r.Status))
			if r.Output != "" {
				output := r.Output
				if len(output) > 500 {
					output = output[:500] + "..."
				}
				sb.WriteString(fmt.Sprintf(":\n%s", output))
			}
			sb.WriteString("\n\n")
		}
		_ = b.SendMessage(ctx, chatID, sb.String())
	}
}
