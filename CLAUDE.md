# Praktor

Personal AI Agent Assistant.

## Quick Context

Go 1.26 service (`github.com/mtzanidakis/praktor`) that connects to Telegram, routes messages to Claude Code (Agent SDK) running in isolated Docker containers, and provides a Mission Control Web UI. Single binary deployment via Docker Compose.

## Architecture

```
Telegram ←→ Go Gateway ←→ Embedded NATS ←→ Agent Containers (Docker)
                ↕                                  ↕
            SQLite DB                     Claude Code SDK
                ↕
         Web UI (React SPA)
```

The gateway binary runs all core services: Telegram bot, NATS message bus, agent orchestrator, scheduler, swarm coordinator, and HTTP/WebSocket server. Agent containers are spawned on demand via the Docker API and communicate with the host over NATS pub/sub.

## Project Structure

```
cmd/praktor/main.go              # CLI: `gateway` and `version` subcommands
cmd/ptask/main.go                # Task management CLI (Go, runs inside agent containers)
internal/
  config/                        # YAML config + env var overrides
  store/                         # SQLite (modernc.org/sqlite, pure Go) - groups, messages, tasks, swarms
  natsbus/                       # Embedded NATS server + client helpers + topic naming
  container/                     # Docker container lifecycle, image building, volume mounts
  agent/                         # Message orchestrator, per-group queue, session tracking
  telegram/                      # Telegram bot (telego), long-polling, message chunking
  scheduler/                     # Cron/interval task polling (adhocore/gronx)
  swarm/                         # Multi-container swarm coordination
  web/                           # HTTP server, REST API, WebSocket hub, embedded SPA
  groups/                        # Group registration, CLAUDE.md management
Dockerfile                       # Gateway image (multi-stage: UI + Go + scratch)
Dockerfile.agent                 # Agent image (multi-stage: Go + esbuild + alpine)
agent-runner/src/                # TypeScript entrypoint: NATS bridge + Claude Code SDK (bundled with esbuild)
ui/                              # React/Vite SPA (dark theme, indigo accent)
  src/pages/                     # Dashboard, Groups, Conversations, Tasks, Swarms
  src/hooks/useWebSocket.ts      # Real-time WebSocket event hook
groups/global/CLAUDE.md          # Global agent instructions (seeded to praktor-global volume)
config/praktor.example.yaml      # Example configuration
```

## Key Commands

```sh
go run ./cmd/praktor version           # Print version
go run ./cmd/praktor gateway           # Start the gateway (needs config)
CGO_ENABLED=0 go build ./cmd/praktor   # Build static binary
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
| `ANTHROPIC_API_KEY` | `agent.anthropic_api_key` | Anthropic API key for agents |
| `CLAUDE_CODE_OAUTH_TOKEN` | `agent.oauth_token` | Claude Code OAuth token |
| `PRAKTOR_WEB_PASSWORD` | `web.auth` | Basic auth password for web UI |
| `PRAKTOR_WEB_PORT` | `web.port` | Web UI port (default: 8080) |
| `PRAKTOR_NATS_PORT` | `nats.port` | NATS port (default: 4222) |
| `PRAKTOR_STORE_PATH` | `store.path` | SQLite DB path (default: `data/praktor.db`) |
| `PRAKTOR_GROUPS_BASE` | `groups.base_path` | Groups directory (default: `data/groups`) |
| `PRAKTOR_MAIN_CHAT_ID` | `groups.main_chat_id` | Telegram chat ID for admin channel |

## NATS Topics

```
agent.{groupID}.input      # Host → Container: user messages
agent.{groupID}.output     # Container → Host: agent responses (text, result)
agent.{groupID}.control    # Host → Container: shutdown, ping
host.ipc.{groupID}         # Container → Host: IPC commands
swarm.{swarmID}.*          # Inter-agent swarm communication
events.>                   # System events (broadcast to WebSocket clients)
```

## REST API

```
GET/POST       /api/groups              # List/create groups
GET            /api/groups/{id}         # Group details
GET            /api/groups/{id}/messages # Message history
GET            /api/agents              # Active agent containers
POST           /api/agents/{groupID}/stop # Stop an agent
GET/POST       /api/tasks               # List/create scheduled tasks
PUT/DELETE     /api/tasks/{id}          # Update/delete task
GET/POST       /api/swarms              # List/create swarm runs
GET            /api/swarms/{id}         # Swarm status
GET            /api/status              # System health
WS             /api/ws                  # WebSocket for real-time events
```

## Container Mount Strategy

All containers use Docker named volumes (no host path dependencies):

| Volume | Container Path | Mode | Purpose |
|--------|---------------|------|---------|
| `praktor-wk-{folder}` | `/workspace/group` | rw | Group workspace |
| `praktor-global` | `/workspace/global` | ro | Global instructions |
| `praktor-sess-{folder}` | `/home/praktor/.claude` | rw | Claude session data |

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

Tables: `groups`, `messages` (with group_id index), `scheduled_tasks` (with status+next_run index), `agent_sessions`, `swarm_runs`. Migrations run automatically on startup.

## What it supports

- Telegram I/O - Message Claude from your phone
- Isolated group context - Each group has its own CLAUDE.md memory, isolated filesystem, and runs in its own container sandbox
- Main channel - Private channel for admin control; other groups are completely isolated
- Scheduled tasks - Cron/interval/one-shot jobs that run Claude and deliver results
- Web access - Agents can use WebSearch and WebFetch tools
- Browser control - Chromium available in agent containers
- Container isolation - Agents sandboxed in Docker containers with NATS communication
- Agent swarms - Spin up teams of specialized agents that collaborate via NATS
- Mission Control UI - Real-time dashboard with WebSocket updates
