# Praktor

Personal AI Agent Assistant.

**Do NOT commit or push unless explicitly asked.**

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
cmd/praktor/main.go              # CLI: `gateway`, `vault`, and `version` subcommands
cmd/ptask/main.go                # Task management CLI (Go, runs inside agent containers)
internal/
  config/                        # YAML config + env var overrides
  extensions/                    # Agent extension types (MCP servers, plugins, skills, settings)
  store/                         # SQLite (modernc.org/sqlite, pure Go) - agents, messages, tasks, swarms, secrets
  vault/                         # AES-256-GCM encryption with Argon2id key derivation
  natsbus/                       # Embedded NATS server + client helpers + topic naming
  container/                     # Docker container lifecycle, image building, volume mounts
  agent/                         # Message orchestrator, per-agent queue, session tracking
  registry/                      # Agent registry - syncs YAML config to DB, resolves agent config
  router/                        # Message router - @prefix parsing, smart routing via default agent
  telegram/                      # Telegram bot (telego), long-polling, message chunking
  scheduler/                     # Cron/interval/relative delay task polling (adhocore/gronx)
  swarm/                         # Graph-based swarm orchestration (DAG execution, collaborative chat)
  web/                           # HTTP server, REST API, WebSocket hub, embedded SPA
Dockerfile                       # Gateway image (multi-stage: UI + Go + scratch)
Dockerfile.agent                 # Agent image (multi-stage: Go + esbuild + alpine)
agent-runner/src/                # TypeScript: NATS bridge + Claude Code SDK + MCP servers (bundled with esbuild)
  index.ts                       # Main entrypoint: agent lifecycle, message handling, MCP server registration
  extensions.ts                  # Apply agent extensions on startup (MCP servers, plugins, skills, settings)
  nats-bridge.ts                 # NATS pub/sub wrapper for agent ↔ host communication
  ipc.ts                         # Shared NATS IPC helper (sendIPC + IPCResponse)
  mcp-tasks.ts                   # MCP server: scheduled_task_create/list/delete
  mcp-profile.ts                 # MCP server: user_profile_read/update
  mcp-memory.ts                  # MCP server: memory_store/recall/list/delete/forget
  mcp-swarm.ts                   # MCP server: swarm_chat_send (conditional on SWARM_CHAT_TOPIC)
  mcp-nix.ts                     # MCP server: nix_search/add/list_installed/remove/upgrade
ui/                              # React/Vite SPA (dark theme, indigo accent)
  src/pages/                     # Dashboard, Agents, Conversations, Tasks, Secrets, Swarms
  src/components/SwarmGraph.tsx   # SVG-based visual graph editor for swarm topology
  src/hooks/useWebSocket.ts      # Real-time WebSocket event hook
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

**IMPORTANT: Never run `node`, `npm`, or `npx` directly on the host.** Always run them inside a Docker container. For the UI:

```sh
docker run --rm -v $(pwd)/ui:/app -w /app node:24-alpine sh -c "npm install && npm run build"
docker run --rm -v $(pwd)/ui:/app -w /app node:24-alpine npm run dev
```

For agent-runner or any other Node.js tooling, use the same pattern with the appropriate volume mount.

## Configuration

Loaded from YAML (default: `config/praktor.yaml`, override with `PRAKTOR_CONFIG` env var). Environment variables take precedence over YAML values:

| Env Var | Config Key | Purpose |
|---------|-----------|---------|
| `PRAKTOR_TELEGRAM_TOKEN` | `telegram.token` | Telegram bot token |
| `ANTHROPIC_API_KEY` | `defaults.anthropic_api_key` | Anthropic API key for agents |
| `CLAUDE_CODE_OAUTH_TOKEN` | `defaults.oauth_token` | Claude Code OAuth token |
| `PRAKTOR_WEB_PASSWORD` | `web.auth` | Basic auth password for web UI |
| `PRAKTOR_WEB_PORT` | `web.port` | Web UI port (default: 8080) |
| `PRAKTOR_AGENT_MODEL` | `defaults.model` | Override default Claude model |
| `PRAKTOR_VAULT_PASSPHRASE` | `vault.passphrase` | Encryption passphrase for secrets vault |

Hardcoded paths (not configurable): `data/praktor.db` (SQLite), `data/agents` (agent workspaces).

The `telegram.main_chat_id` setting specifies which Telegram chat receives scheduled task results and swarm results launched from Mission Control.

### Agent Definitions

Agents are defined in the `agents` map in YAML config. Each agent has:
- `description` - Used for smart routing
- `model` - Override default model
- `image` - Override default container image
- `workspace` - Volume suffix (defaults to agent name)
- `env` - Per-agent environment variables (supports `secret:name` references resolved from vault)
- `files` - Secret files injected into container at start (`secret`, `target`, `mode`)
- `allowed_tools` - Restrict Claude tools
- `claude_md` - Relative path to agent-specific CLAUDE.md
- `nix_enabled` - Enable nix package manager in agent container (starts nix-daemon)

The `router.default_agent` must reference an existing agent.

### Agent Extensions

Extensions are stored per-agent in the DB (not YAML config) and managed via the REST API + Mission Control UI. They allow adding MCP servers, plugins, skills, and Claude Code settings to individual agents. Extensions require `nix_enabled: true` on the agent.

**Key files:**
- `internal/extensions/types.go` — `AgentExtensions`, `MCPServerConfig`, `MarketplaceConfig`, `PluginConfig`, `SkillConfig`
- `internal/agent/extensions.go` — Loads extensions from DB, resolves secrets, passes as `AGENT_EXTENSIONS` env var
- `internal/web/api_extensions.go` — GET/PUT `/api/agents/definitions/{id}/extensions`
- `agent-runner/src/extensions.ts` — Applies extensions at container startup (nix deps, MCP servers, skills, settings, plugins)
- `ui/src/components/AgentExtensions.tsx` — UI component with tabs for each extension type

**Extension types:**
- `marketplaces` — Plugin marketplace sources registered via `claude plugin marketplace add` before plugin installation. `MarketplaceConfig`: `Source string` (required: `owner/repo`, git URL, or URL), `Name string` (optional override, derived from source if omitted).
- `mcp_servers` — Merged into the `query()` SDK call alongside built-in MCP servers. Supports `secret:name` env/header references.
- `plugins` — Installed via `claude plugin install` on container start. Requires marketplace to be registered first (except `claude-plugins-official` which is built-in). Persisted on home volume.
- `skills` — Written to `~/.claude/skills/{name}/SKILL.md` on container start.
- `settings` — Deep-merged into `~/.claude/settings.json` on container start.

Updating extensions via PUT stops the running agent container so it picks up changes on the next message.

### Hot Config Reload

The gateway watches the config file for changes (mtime polled every 3s, SHA-256 hash verified on mtime change). When a change is detected, it automatically reloads without restarting the gateway process. SIGHUP also triggers a reload.

**Reloadable:** Agent definitions (all fields), defaults (model, image, max_running, idle_timeout), router.default_agent, scheduler poll_interval, telegram main_chat_id.

**Not reloadable** (warning logged): telegram.token, web.port, nats.data_dir, vault.passphrase.

Running agents whose config changed are stopped and lazily restarted on the next message. Added agents become routable immediately. Removed agents are stopped.

Key implementation files: `internal/config/diff.go` (config diffing), `cmd/praktor/main.go` (`watchConfigFile`, `reloadConfig`).

## NATS Topics

```
agent.{agentID}.input           # Host → Container: user messages
agent.{agentID}.output          # Container → Host: agent responses (text, result)
agent.{agentID}.control         # Host → Container: shutdown, ping
agent.{agentID}.route           # Host → Container: routing classification queries
host.ipc.{agentID}              # Container → Host: IPC commands
swarm.{swarmID}.chat.{groupID}  # Inter-agent collaborative chat within a swarm group
swarm.{swarmID}.*               # Other inter-agent swarm communication
events.swarm.{swarmID}          # Swarm lifecycle events (started, agent_started, tier_completed, completed, failed)
events.>                        # System events (broadcast to WebSocket clients)
```

## REST API

```
GET            /api/agents/definitions              # List agent definitions
GET            /api/agents/definitions/{id}          # Agent details
GET            /api/agents/definitions/{id}/messages # Message history
GET            /api/agents                           # Active agent containers
GET/POST       /api/tasks                            # List/create scheduled tasks
PUT/DELETE     /api/tasks/{id}                       # Update/delete task
DELETE         /api/tasks/completed                  # Delete all completed tasks
GET/POST       /api/secrets                          # List/create secrets
GET/PUT/DELETE /api/secrets/{id}                     # Get/update/delete secret
GET/PUT        /api/agents/definitions/{id}/secrets  # List/set agent secret assignments
POST/DELETE    /api/agents/definitions/{id}/secrets/{secretId}  # Add/remove agent secret
GET/POST       /api/swarms                           # List/create swarm runs
GET/DELETE     /api/swarms/{id}                      # Swarm status / delete
GET/PUT        /api/agents/definitions/{id}/agent-md   # Read/update per-agent AGENT.md
GET/PUT        /api/agents/definitions/{id}/extensions # Read/update agent extensions (MCP servers, plugins, skills, settings)
GET/PUT        /api/user-profile                      # Read/update USER.md
GET            /api/status                           # System health
WS             /api/ws                               # WebSocket for real-time events
```

## Container Mount Strategy

All containers use Docker named volumes (no host path dependencies):

| Volume | Container Path | Mode | Purpose |
|--------|---------------|------|---------|
| `praktor-wk-{workspace}` | `/workspace/agent` | rw | Agent workspace |
| `praktor-global` | `/workspace/global` | ro | Global instructions |
| `praktor-home-{workspace}` | `/home/praktor` | rw | Agent home directory |

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

## Swarm Orchestration

Swarms are graph-based: agents are nodes, connections ("synapses") define execution patterns, and a lead agent synthesizes results.

**Three orchestration patterns** defined by synapse types:
- **No connection** → Fan-out: agents run in parallel, independently
- **A → B directed** → Pipeline: B waits for A, receives A's output as context
- **A ↔ B bidirectional** → Collaborative: agents share a real-time NATS chat channel

The lead agent always runs last and receives all prior results for synthesis.

**Key files:**
- `internal/swarm/types.go` — `SwarmRequest`, `SwarmAgent`, `Synapse`, `AgentResult`
- `internal/swarm/graph.go` — `BuildPlan()`: topological sort, collab group detection (union-find), cycle detection, tier assignment
- `internal/swarm/graph_test.go` — Unit tests for all graph topologies
- `internal/swarm/coordinator.go` — Tier-based DAG execution, secret resolution, event publishing, swarm membership tracking
- `ui/src/components/SwarmGraph.tsx` — SVG visual graph editor (drag nodes, draw edges, toggle direction, edit mode with initialData)
- `ui/src/pages/Swarms.tsx` — Create/list views with edit, delete, and replay buttons per swarm

**Execution flow:**
1. `BuildPlan()` analyzes the graph → produces ordered `ExecutionTier`s, `CollabGroups`, and `PipelineInputs`
2. Coordinator executes tier-by-tier; within each tier, agents run in parallel via WaitGroup
3. Pipeline agents receive predecessor outputs prepended to their prompt
4. Collaborative agents get `SWARM_CHAT_TOPIC` env var → agent-runner subscribes to chat, buffers messages, and provides `swarm_chat_send` MCP tool
5. Lead agent (last tier) receives all previous results in a synthesis prompt

**Telegram syntax** (`@swarm` prefix):
- `@swarm agent1,agent2,agent3: task` → fan-out, first agent = lead
- `@swarm agent1>agent2>agent3: task` → pipeline, last agent = lead
- `@swarm agent1<>agent2,agent3: task` → agent1↔agent2 collaborative + agent3 independent

**API:** `POST /api/swarms` accepts `SwarmRequest` with `agents`, `synapses`, `lead_agent`, `task`, and `name`. Graph is validated via `BuildPlan()` before execution; returns 400 on cycles or unknown roles. `DELETE /api/swarms/{id}` removes a swarm run.

**Result delivery:** Swarms launched from Telegram deliver results to the originating chat. Swarms launched from Mission Control deliver results to `telegram.main_chat_id`.

**WebSocket events:** `swarm_started`, `swarm_agent_started`, `swarm_agent_completed`, `swarm_tier_completed`, `swarm_completed`, `swarm_failed` — published on `events.swarm.{swarmID}`.

**DB columns:** `swarm_runs` table includes `name`, `synapses` (JSON), `lead_agent` (added via ALTER TABLE migrations that ignore duplicate column errors).

## SQLite Schema

Tables: `agents`, `messages` (with agent_id index), `scheduled_tasks` (with status+next_run index), `agent_sessions`, `swarm_runs`, `secrets`, `agent_secrets`. Migrations run automatically on startup.

## MCP Server Convention

Each MCP tool domain lives in its own file under `agent-runner/src/mcp-*.ts`. To add a new MCP server:

1. Create `agent-runner/src/mcp-{domain}.ts` with its own `McpServer` instance + `StdioServerTransport`
2. If it needs NATS IPC, import `sendIPC` from `./ipc.js`
3. Add an esbuild entry in `Dockerfile.agent` to bundle it as `out/mcp-{domain}.js`
4. Register it in `agent-runner/src/index.ts` under `mcpServers` with `command: "node", args: ["/app/mcp-{domain}.js"]`
5. The `allowedTools` wildcard `"mcp__praktor-*"` covers all `praktor-*` named servers automatically

## What it supports

- Telegram I/O - Message Claude from your phone
- Named agents - Multiple agents with distinct roles, models, and configurations
- Smart routing - `@agent_name` prefix or AI-powered routing via default agent
- Isolated agent context - Each agent has its own CLAUDE.md memory, isolated filesystem, and runs in its own container sandbox
- Persistent memory - SQLite-backed per-agent memory (`/workspace/agent/memory.db`) with MCP tools (memory_store, memory_recall, memory_list, memory_delete, memory_forget). Existing memory keys are listed in the system prompt so agents know what's stored.
- Scheduled tasks - Cron/interval/relative delay (+30s, +5m, +2h)/one-shot jobs that run Claude and deliver results
- Web access - Agents can use WebSearch and WebFetch tools
- Nix package manager - Agents with `nix_enabled: true` can install packages on demand via MCP tools (nix_search, nix_add, nix_list_installed, nix_remove, nix_upgrade). When nix-daemon is detected, the system prompt instructs agents to auto-install missing tools. The `/nix` Telegram command provides direct user control over agent packages.
- Browser control - Chromium available in agent containers
- Container isolation - Agents sandboxed in Docker containers with NATS communication
- Agent swarms - Graph-based orchestration: fan-out (parallel), pipeline (sequential with context passing), and collaborative (real-time chat) execution patterns. Visual graph editor in Mission Control, `@swarm` Telegram integration
- Secure vault - AES-256-GCM encrypted secrets, injected as env vars or files at container start (never exposed to LLM)
- Hot config reload - Config file changes are detected automatically (file polling every 3s) or via SIGHUP; only affected agents are restarted
- Mission Control UI - Real-time dashboard with WebSocket updates
- Telegram slash commands — registered via `SetMyCommands` with `th.CommandEqual()` predicates. Agent resolved from arg or last-used agent for the chat (`/start` falls back to default agent). **When adding a new command, also add it to the `/commands` handler output and the list below.**
  - `/agents` — List available agents (id, description, status, model, messages)
  - `/commands` — Show available commands
  - `/start [agent]` — Say hello to an agent
  - `/stop [agent]` — Abort the active run and drain the message queue (container stays running)
  - `/reset [agent]` — Clear session context for a fresh conversation
  - `/nix <action> [package] [@agent]` — Manage nix packages (search, add, list, remove, upgrade)
