package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/mtzanidakis/praktor/internal/vault"
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
	case "vault":
		if err := runVault(os.Args[2:]); err != nil {
			slog.Error("vault command failed", "error", err)
			os.Exit(1)
		}
	case "backup":
		if err := runBackup(os.Args[2:]); err != nil {
			slog.Error("backup failed", "error", err)
			os.Exit(1)
		}
	case "restore":
		if err := runRestore(os.Args[2:]); err != nil {
			slog.Error("restore failed", "error", err)
			os.Exit(1)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: praktor <command>\n\nCommands:\n  gateway    Start the Praktor gateway service\n  vault      Manage encrypted secrets\n  backup     Back up all praktor Docker volumes\n  restore    Restore praktor Docker volumes from backup\n  version    Print version\n")
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
	db, err := store.New(config.StorePath)
	if err != nil {
		return fmt.Errorf("init store: %w", err)
	}
	defer db.Close()
	slog.Info("store initialized", "path", config.StorePath)

	// Embedded NATS
	bus, err := natsbus.New(cfg.NATS)
	if err != nil {
		return fmt.Errorf("init nats: %w", err)
	}
	defer bus.Close()
	slog.Info("nats started", "port", config.NATSPort)

	// Agent registry (replaces groups manager)
	reg := registry.New(db, cfg.Agents, cfg.Defaults, config.AgentsBasePath)
	if err := reg.Sync(); err != nil {
		return fmt.Errorf("sync agent registry: %w", err)
	}

	// Container manager
	ctrMgr, err := container.NewManager(bus, cfg.Defaults)
	if err != nil {
		return fmt.Errorf("init container manager: %w", err)
	}

	// Vault
	if cfg.Vault.Passphrase == "" {
		return fmt.Errorf("vault passphrase is required (set PRAKTOR_VAULT_PASSPHRASE or vault.passphrase in config)")
	}
	v := vault.New(cfg.Vault.Passphrase)
	slog.Info("vault initialized")

	// Agent orchestrator
	orch := agent.NewOrchestrator(bus, ctrMgr, db, reg, cfg.Defaults, v)

	// Message router
	rtr := router.New(reg, cfg.Router)
	rtr.SetOrchestrator(orch)

	// Idle reaper
	go orch.StartIdleReaper(ctx)

	// Nix garbage collection
	go orch.StartNixGC(ctx)

	// Swarm coordinator
	swarmCoord := swarm.NewCoordinator(bus, ctrMgr, db, reg, v)
	orch.SetSwarmCoordinator(swarmCoord)

	// Scheduler
	sched := scheduler.New(db, orch, bus, cfg.Scheduler, cfg.Telegram.MainChatID)
	go sched.Start(ctx)

	// Telegram bot
	if cfg.Telegram.Token != "" {
		bot, err := telegram.NewBot(cfg.Telegram, orch, rtr, swarmCoord, reg, bus, db)
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
		srv := web.NewServer(db, bus, orch, reg, swarmCoord, cfg.Web, v)
		go func() {
			if err := srv.Start(ctx); err != nil {
				slog.Error("web server error", "error", err)
			}
		}()
	}

	// Config file watcher — polls mtime every 3s
	reloadCh := make(chan struct{}, 1)
	go watchConfigFile(ctx, config.Path(), reloadCh)

	// Wait for signals or config reload
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	currentCfg := cfg
	for {
		select {
		case sig := <-sigCh:
			if sig == syscall.SIGHUP {
				slog.Info("received SIGHUP, reloading config")
			} else {
				slog.Info("shutting down", "signal", sig)
				cancel()
				ctrMgr.StopAll(context.Background())
				return nil
			}
		case <-reloadCh:
			slog.Info("config file changed, reloading")
		}

		updated, err := reloadConfig(ctx, currentCfg, reg, orch, ctrMgr, rtr, sched)
		if err != nil {
			slog.Error("config reload failed", "error", err)
			continue
		}
		currentCfg = updated
	}
}

// watchConfigFile polls the config file mtime every 3s; when it changes,
// computes a SHA-256 hash to confirm actual content change before signalling.
func watchConfigFile(ctx context.Context, path string, reloadCh chan<- struct{}) {
	info, err := os.Stat(path)
	if err != nil {
		slog.Warn("config watcher: cannot stat file, watcher disabled", "path", path, "error", err)
		return
	}
	lastMod := info.ModTime()
	lastHash, err := hashFile(path)
	if err != nil {
		slog.Warn("config watcher: cannot read file, watcher disabled", "path", path, "error", err)
		return
	}
	slog.Info("config watcher started", "path", path)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			mod := info.ModTime()
			if !mod.After(lastMod) {
				continue
			}
			lastMod = mod

			h, err := hashFile(path)
			if err != nil {
				continue
			}
			if h == lastHash {
				continue
			}
			lastHash = h

			select {
			case reloadCh <- struct{}{}:
			default:
			}
		}
	}
}

func hashFile(path string) ([sha256.Size]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func reloadConfig(
	ctx context.Context,
	oldCfg *config.Config,
	reg *registry.Registry,
	orch *agent.Orchestrator,
	ctrMgr *container.Manager,
	rtr *router.Router,
	sched *scheduler.Scheduler,
) (*config.Config, error) {
	newCfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	diff := config.Diff(oldCfg, newCfg)

	// Warn about non-reloadable changes
	for _, field := range diff.NonReloadable {
		slog.Warn("config field changed but requires restart", "field", field)
	}

	if !diff.HasChanges() {
		slog.Info("config reload: no reloadable changes detected")
		return newCfg, nil
	}

	// Update registry (agents + defaults)
	if len(diff.AgentsAdded) > 0 || len(diff.AgentsRemoved) > 0 || len(diff.AgentsChanged) > 0 || diff.DefaultsChanged {
		if err := reg.Update(newCfg.Agents, newCfg.Defaults); err != nil {
			return nil, fmt.Errorf("update registry: %w", err)
		}
		slog.Info("registry updated",
			"added", diff.AgentsAdded,
			"removed", diff.AgentsRemoved,
			"changed", diff.AgentsChanged,
		)
	}

	// Update orchestrator and container manager defaults
	if diff.DefaultsChanged {
		orch.UpdateDefaults(newCfg.Defaults)
		ctrMgr.UpdateDefaults(newCfg.Defaults)
		slog.Info("defaults updated")
	}

	// Update router default agent
	if diff.RouterChanged {
		rtr.SetDefaultAgent(diff.NewDefaultAgent)
		slog.Info("router default agent updated", "agent", diff.NewDefaultAgent)
	}

	// Update scheduler
	if diff.SchedulerChanged || diff.MainChatIDChanged {
		pollInterval := newCfg.Scheduler.PollInterval
		mainChatID := newCfg.Telegram.MainChatID
		sched.UpdateConfig(pollInterval, mainChatID)
		slog.Info("scheduler config updated", "poll_interval", pollInterval, "main_chat_id", mainChatID)
	}

	// Stop running agents whose config changed (lazy restart on next message)
	for _, agentID := range diff.AgentsChanged {
		if ctrMgr.GetRunning(agentID) != nil {
			slog.Info("stopping agent due to config change", "agent", agentID)
			if err := orch.StopAgent(ctx, agentID); err != nil {
				slog.Error("failed to stop changed agent", "agent", agentID, "error", err)
			}
		}
	}

	// Stop running agents that were removed
	for _, agentID := range diff.AgentsRemoved {
		if ctrMgr.GetRunning(agentID) != nil {
			slog.Info("stopping removed agent", "agent", agentID)
			if err := orch.StopAgent(ctx, agentID); err != nil {
				slog.Error("failed to stop removed agent", "agent", agentID, "error", err)
			}
		}
	}

	slog.Info("config reload complete")
	return newCfg, nil
}
