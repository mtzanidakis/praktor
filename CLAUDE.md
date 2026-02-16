# Praktor

Personal AI Agent Assistant.

## Quick Context

Go 1.26 service (`github.com/mtzanidakis/praktor`) that connects to Telegram, routes messages to named agents running Claude Code (Agent SDK) in isolated Docker containers, and provides a Mission Control Web UI. Single binary deployment via Docker Compose.

## Architecture

```
Telegram ←→ Go Gateway ←→ Router ←→ Embedded NATS ←→ Agent Containers (Docker)
                ↕                                          ↕
            SQLite DB                             Claude Code SDK
                ↕
         Web UI (React SPA)
```

The gateway binary runs all core services: Telegram bot, message router, NATS message bus, agent orchestrator, scheduler, swarm coordinator, and HTTP/WebSocket server. Agent containers are spawned on demand via the Docker API and communicate with the host over NATS pub/sub.

**Named Agents:** Multiple agents are defined in YAML config, each with its own description, model, image, env vars, secrets, allowed tools, and workspace. Messages are routed to agents via `@agent_name` prefix or smart routing through the default agent's container.

## Project Structure

```
cmd/praktor/main.go              # CLI: `gateway` and `version` subcommands
cmd/ptask/main.go                # Task management CLI (Go, runs inside agent containers)
internal/
  config/                        # YAML config + env var overrides
  store/                         # SQLite (modernc.org/sqlite, pure Go) - agents, messages, tasks, swarms
  natsbus/                       # Embedded NATS server + client helpers + topic naming
  container/                     # Docker container lifecycle, image building, volume mounts
  agent/                         # Message orchestrator, per-agent queue, session tracking
  registry/                      # Agent registry - syncs YAML config to DB, resolves agent config
  router/                        # Message router - @prefix parsing, smart routing via default agent
  telegram/                      # Telegram bot (telego), long-polling, message chunking
  scheduler/                     # Cron/interval task polling (adhocore/gronx)
  swarm/                         # Multi-container swarm coordination
  web/                           # HTTP server, REST API, WebSocket hub, embedded SPA
Dockerfile                       # Gateway image (multi-stage: UI + Go + scratch)
Dockerfile.agent                 # Agent image (multi-stage: Go + esbuild + alpine)
agent-runner/src/                # TypeScript entrypoint: NATS bridge + Claude Code SDK (bundled with esbuild)
ui/                              # React/Vite SPA (dark theme, indigo accent)
  src/pages/                     # Dashboard, Agents, Conversations, Tasks, Swarms
  src/hooks/useWebSocket.ts      # Real-time WebSocket event hook
agents/global/CLAUDE.md          # Global agent instructions (seeded to praktor-global volume)
config/praktor.example.yaml      # Example configuration
```

## Key Commands

```sh
go run ./cmd/praktor version           # Print version
go run ./cmd/praktor gateway           # Start the gateway (needs config)
CGO_ENABLED=0 go build ./cmd/praktor   # Build static binary
CGO_ENABLED=0 go build ./cmd/ptask     # Build ptask CLI
CGO_ENABLED=0 go test ./internal/...   # Run all tests
make containers                        # Build both Docker images (gateway + agent)
docker compose up                      # Run full stack
```

Note: On this system, binaries must be built with `CGO_ENABLED=0` due to the nix dynamic linker. The `modernc.org/sqlite` driver is pure Go and does not require CGO.

## Configuration

Loaded from YAML (default: `config/praktor.yaml`, override with `PRAKTOR_CONFIG` env var). Environment variables take precedence over YAML values:

| Env Var | Config Key | Purpose |
|---------|-----------|---------|
| `PRAKTOR_TELEGRAM_TOKEN` | `telegram.token` | Telegram bot token |
| `ANTHROPIC_API_KEY` | `defaults.anthropic_api_key` | Anthropic API key for agents |
| `CLAUDE_CODE_OAUTH_TOKEN` | `defaults.oauth_token` | Claude Code OAuth token |
| `PRAKTOR_WEB_PASSWORD` | `web.auth` | Basic auth password for web UI |
| `PRAKTOR_WEB_PORT` | `web.port` | Web UI port (default: 8080) |
| `PRAKTOR_NATS_PORT` | `nats.port` | NATS port (default: 4222) |
| `PRAKTOR_AGENT_MODEL` | `defaults.model` | Override default Claude model |

Hardcoded paths (not configurable): `data/praktor.db` (SQLite), `data/agents` (agent workspaces).

The `telegram.main_chat_id` setting specifies which Telegram chat receives scheduled task results.

### Agent Definitions

Agents are defined in the `agents` map in YAML config. Each agent has:
- `description` - Used for smart routing
- `model` - Override default model
- `image` - Override default container image
- `workspace` - Volume suffix (defaults to agent name)
- `env` - Per-agent environment variables
- `secrets` - Host env var names to forward
- `allowed_tools` - Restrict Claude tools
- `claude_md` - Relative path to agent-specific CLAUDE.md

The `router.default_agent` must reference an existing agent.

## NATS Topics

```
agent.{agentID}.input      # Host → Container: user messages
agent.{agentID}.output     # Container → Host: agent responses (text, result)
agent.{agentID}.control    # Host → Container: shutdown, ping
agent.{agentID}.route      # Host → Container: routing classification queries
host.ipc.{agentID}         # Container → Host: IPC commands
swarm.{swarmID}.*          # Inter-agent swarm communication
events.>                   # System events (broadcast to WebSocket clients)
```

## REST API

```
GET            /api/agents/definitions              # List agent definitions
GET            /api/agents/definitions/{id}          # Agent details
GET            /api/agents/definitions/{id}/messages # Message history
GET            /api/agents                           # Active agent containers
POST           /api/agents/{agentID}/stop            # Stop an agent
GET/POST       /api/tasks                            # List/create scheduled tasks
PUT/DELETE     /api/tasks/{id}                       # Update/delete task
GET/POST       /api/swarms                           # List/create swarm runs
GET            /api/swarms/{id}                      # Swarm status
GET            /api/status                           # System health
WS             /api/ws                               # WebSocket for real-time events
```

## Container Mount Strategy

All containers use Docker named volumes (no host path dependencies):

| Volume | Container Path | Mode | Purpose |
|--------|---------------|------|---------|
| `praktor-wk-{workspace}` | `/workspace/agent` | rw | Agent workspace |
| `praktor-global` | `/workspace/global` | ro | Global instructions |
| `praktor-sess-{workspace}` | `/home/praktor/.claude` | rw | Claude session data |

The gateway uses `praktor-data` for SQLite/NATS and `praktor-global` for global instructions. Both gateway and agents run as non-root user `praktor` (uid 10321).

## Go Dependencies

- `mymmrac/telego` - Telegram bot
- `docker/docker` - Docker SDK for container management
- `nats-io/nats-server/v2` - Embedded NATS server
- `nats-io/nats.go` - NATS client
- `modernc.org/sqlite` - Pure-Go SQLite (no CGO)
- `gorilla/websocket` - WebSocket connections
- `google/uuid` - UUID generation
- `adhocore/gronx` - Cron expression parsing
- `gopkg.in/yaml.v3` - YAML config parsing

## SQLite Schema

Tables: `agents`, `messages` (with agent_id index), `scheduled_tasks` (with status+next_run index), `agent_sessions`, `swarm_runs`. Migrations run automatically on startup.

## What it supports

- Telegram I/O - Message Claude from your phone
- Named agents - Multiple agents with distinct roles, models, and configurations
- Smart routing - `@agent_name` prefix or AI-powered routing via default agent
- Isolated agent context - Each agent has its own CLAUDE.md memory, isolated filesystem, and runs in its own container sandbox
- Scheduled tasks - Cron/interval/one-shot jobs that run Claude and deliver results
- Web access - Agents can use WebSearch and WebFetch tools
- Browser control - Chromium available in agent containers
- Container isolation - Agents sandboxed in Docker containers with NATS communication
- Agent swarms - Spin up teams of specialized agents that collaborate via NATS
- Mission Control UI - Real-time dashboard with WebSocket updates
