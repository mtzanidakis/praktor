package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/schedule"
	"github.com/mtzanidakis/praktor/internal/store"
)

type Scheduler struct {
	store        *store.Store
	orch         *agent.Orchestrator
	bus          *natsbus.Bus
	natsClient   *natsbus.Client
	pollInterval time.Duration
	mainChatID   int64
	reloadCh     chan struct{}
}

func New(s *store.Store, orch *agent.Orchestrator, bus *natsbus.Bus, cfg config.SchedulerConfig, mainChatID int64) *Scheduler {
	sched := &Scheduler{
		store:        s,
		orch:         orch,
		bus:          bus,
		pollInterval: cfg.PollInterval,
		mainChatID:   mainChatID,
		reloadCh:     make(chan struct{}, 1),
	}

	if bus != nil {
		client, err := natsbus.NewClient(bus)
		if err != nil {
			slog.Error("scheduler nats client failed", "error", err)
		} else {
			sched.natsClient = client
		}
	}

	return sched
}

// UpdateConfig updates the scheduler's poll interval and main chat ID,
// then signals the run loop to reset its ticker.
func (s *Scheduler) UpdateConfig(pollInterval time.Duration, mainChatID int64) {
	s.pollInterval = pollInterval
	s.mainChatID = mainChatID
	select {
	case s.reloadCh <- struct{}{}:
	default:
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	if s.pollInterval == 0 {
		s.pollInterval = 30 * time.Second
	}

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	slog.Info("scheduler started", "poll_interval", s.pollInterval)

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-s.reloadCh:
			ticker.Reset(s.pollInterval)
			slog.Info("scheduler config reloaded", "poll_interval", s.pollInterval)
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

func (s *Scheduler) poll(ctx context.Context) {
	tasks, err := s.store.GetDueTasks(time.Now())
	if err != nil {
		slog.Error("failed to get due tasks", "error", err)
		return
	}

	for _, task := range tasks {
		s.execute(ctx, task)
	}
}

func (s *Scheduler) execute(ctx context.Context, task store.ScheduledTask) {
	slog.Info("executing scheduled task", "id", task.ID, "name", task.Name, "agent", task.AgentID)

	meta := map[string]string{
		"sender":  "scheduler",
		"task_id": task.ID,
	}
	if s.mainChatID != 0 {
		meta["chat_id"] = strconv.FormatInt(s.mainChatID, 10)
	}

	err := s.orch.HandleMessage(ctx, task.AgentID, task.Prompt, meta)

	var lastStatus, lastError string
	if err != nil {
		lastStatus = "error"
		lastError = err.Error()
		slog.Error("task execution failed", "id", task.ID, "error", err)
	} else {
		lastStatus = "success"
	}

	// Calculate next run time
	nextRun := schedule.CalculateNextRun(task.Schedule)

	if err := s.store.UpdateTaskRun(task.ID, lastStatus, lastError, nextRun); err != nil {
		slog.Error("failed to update task run", "id", task.ID, "error", err)
	}

	s.publishTaskExecutedEvent(task, lastStatus)

	// Mark one-off tasks as completed when they have no next run
	if nextRun == nil {
		slog.Info("no next run, marking one-off task as completed", "id", task.ID, "name", task.Name)
		if err := s.store.UpdateTaskStatus(task.ID, "completed"); err != nil {
			slog.Error("failed to complete task", "id", task.ID, "error", err)
		}
	}
}

func (s *Scheduler) publishTaskExecutedEvent(task store.ScheduledTask, status string) {
	if s.natsClient == nil {
		return
	}

	event := map[string]any{
		"type":      "task_executed",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"data": map[string]any{
			"id":     task.ID,
			"name":   task.Name,
			"status": status,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	_ = s.natsClient.Publish(natsbus.TopicEventsTaskExecuted, data)
}
