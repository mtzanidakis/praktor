package scheduler

import (
	"context"
	"log/slog"
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
}

func New(s *store.Store, orch *agent.Orchestrator, cfg config.SchedulerConfig) *Scheduler {
	return &Scheduler{
		store:        s,
		orch:         orch,
		pollInterval: cfg.PollInterval,
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
	slog.Info("executing scheduled task", "id", task.ID, "name", task.Name, "group", task.GroupID)

	meta := map[string]string{
		"sender":  "scheduler",
		"task_id": task.ID,
	}

	err := s.orch.HandleMessage(ctx, task.GroupID, task.Prompt, meta)

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
}
