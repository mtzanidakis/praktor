package scheduler

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/schedule"
	"github.com/mtzanidakis/praktor/internal/store"
)

type Scheduler struct {
	store        *store.Store
	orch         *agent.Orchestrator
	pollInterval time.Duration
	mainChatID   int64
}

func New(s *store.Store, orch *agent.Orchestrator, cfg config.SchedulerConfig, mainChatID int64) *Scheduler {
	return &Scheduler{
		store:        s,
		orch:         orch,
		pollInterval: cfg.PollInterval,
		mainChatID:   mainChatID,
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

	// Auto-pause one-off tasks that have no next run
	if nextRun == nil {
		slog.Info("no next run, pausing one-off task", "id", task.ID, "name", task.Name)
		if err := s.store.UpdateTaskStatus(task.ID, "paused"); err != nil {
			slog.Error("failed to pause completed task", "id", task.ID, "error", err)
		}
	}
}
