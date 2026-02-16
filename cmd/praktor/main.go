package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/mtzanidakis/praktor/internal/agent"
	"github.com/mtzanidakis/praktor/internal/config"
	"github.com/mtzanidakis/praktor/internal/container"
	"github.com/mtzanidakis/praktor/internal/natsbus"
	"github.com/mtzanidakis/praktor/internal/registry"
	"github.com/mtzanidakis/praktor/internal/router"
	"github.com/mtzanidakis/praktor/internal/scheduler"
	"github.com/mtzanidakis/praktor/internal/store"
	"github.com/mtzanidakis/praktor/internal/swarm"
	"github.com/mtzanidakis/praktor/internal/telegram"
	"github.com/mtzanidakis/praktor/internal/web"
)

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "version":
		fmt.Printf("praktor %s\n", version)
	case "gateway":
		if err := runGateway(); err != nil {
			slog.Error("gateway failed", "error", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: praktor <command>\n\nCommands:\n  gateway    Start the Praktor gateway service\n  version    Print version\n")
}

func runGateway() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	slog.Info("starting praktor gateway", "version", version)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SQLite store
	db, err := store.New(cfg.Store)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer db.Close()
	slog.Info("store initialized", "path", cfg.Store.Path)

	// Embedded NATS
	bus, err := natsbus.New(cfg.NATS)
	if err != nil {
		return fmt.Errorf("init nats: %w", err)
	}
	defer bus.Close()
	slog.Info("nats started", "port", cfg.NATS.Port)

	// Agent registry (replaces groups manager)
	reg := registry.New(db, cfg.Agents, cfg.Defaults, cfg.Defaults.BasePath)
	if err := reg.Sync(); err != nil {
		return fmt.Errorf("sync agent registry: %w", err)
	}

	// Container manager
	ctrMgr, err := container.NewManager(bus, cfg.Defaults)
	if err != nil {
		return fmt.Errorf("init container manager: %w", err)
	}

	// Agent orchestrator
	orch := agent.NewOrchestrator(bus, ctrMgr, db, reg, cfg.Defaults)

	// Message router
	rtr := router.New(reg, cfg.Router)
	rtr.SetOrchestrator(orch)

	// Idle reaper
	go orch.StartIdleReaper(ctx)

	// Swarm coordinator
	swarmCoord := swarm.NewCoordinator(bus, ctrMgr, db)

	// Scheduler
	sched := scheduler.New(db, orch, cfg.Scheduler)
	go sched.Start(ctx)
	slog.Info("scheduler started")

	// Telegram bot
	if cfg.Telegram.Token != "" {
		bot, err := telegram.NewBot(cfg.Telegram, orch, rtr)
		if err != nil {
			return fmt.Errorf("init telegram bot: %w", err)
		}
		go bot.Start(ctx)
		slog.Info("telegram bot started")
	} else {
		slog.Warn("telegram token not set, bot disabled")
	}

	// Web UI
	if cfg.Web.Enabled {
		srv := web.NewServer(db, bus, orch, swarmCoord, cfg.Web)
		go func() {
			if err := srv.Start(ctx); err != nil {
				slog.Error("web server error", "error", err)
			}
		}()
		slog.Info("web server started", "port", cfg.Web.Port)
	}

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("shutting down", "signal", sig)
	cancel()

	// Cleanup
	ctrMgr.StopAll(context.Background())
	return nil
}
