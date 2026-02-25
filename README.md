<p align="center">
  <img src="ui/public/favicon.svg" width="80" alt="Praktor logo" />
</p>

# Praktor

Personal AI agent assistant. Define named agents with distinct roles and models, message them from Telegram, and monitor everything from Mission Control.

```
Telegram ──> Go Gateway ──> Router ──> Embedded NATS ──> Agent Containers (Docker)
                 |                                            |
             SQLite DB                                Claude Code SDK
                 |
       Mission Control (React SPA)
```

A single Go binary that orchestrates the full loop: receives messages from Telegram, routes them to the right agent, spins up isolated Docker containers running Claude Code, streams responses back, and serves a real-time Mission Control web UI.

## Features

- **Mission Control** — Real-time dashboard with WebSocket updates
- **Telegram I/O** — Chat with your agents from your phone
- **Telegram commands** — `/start`, `/stop`, `/reset`, `/nix`, `/agents`, `/commands`
- **Named agents** — Multiple agents with distinct roles, models, and configurations
- **Smart routing** — `@agent_name` prefix or AI-powered routing via the default agent
- **Per-agent isolation** — Each agent runs in its own Docker container with its own filesystem
- **Persistent memory** — Per-agent SQLite memory database with MCP tools for storing and recalling facts across sessions
- **Agent identity** — Each agent has an `AGENT.md` for personality and expertise, editable from Mission Control or by agents themselves
- **User profile** — Agents know who you are via `USER.md`, editable from Mission Control or by agents themselves
- **Scheduled tasks** — Cron, interval, or one-shot jobs that run agents and deliver results via Telegram
- **Secure vault** — AES-256-GCM encrypted secrets injected as env vars or files at container start, never exposed to the LLM
- **Web & browser access** — Agents can search the web and automate browsers via [playwright-cli](https://github.com/microsoft/playwright-cli)
- **Hot config reload** — Edit `praktor.yaml` and changes apply automatically, no restart needed
- **Nix package manager** — Agents can install packages on demand (Python, ffmpeg, LaTeX, etc.) via MCP tools or the `/nix` Telegram command
- **Agent extensions** — Per-agent MCP servers, plugins, and skills, managed via Mission Control
- **Agent swarms** — Graph-based multi-agent orchestration with fan-out, pipeline, and collaborative patterns
- **Backup & restore** — Back up and restore all Docker volumes as zstd-compressed tarballs via CLI

## Prerequisites

- Docker and Docker Compose
- A Telegram bot token ([create one with @BotFather](https://t.me/BotFather))
- A Claude authentication method: [Anthropic API key](https://console.anthropic.com/) or Claude Code OAuth token (`claude setup-token`)

> **Note on OAuth tokens:** Using Claude Code OAuth tokens with third-party applications must comply with Anthropic's [authentication and credential use policy](https://code.claude.com/docs/en/legal-and-compliance#authentication-and-credential-use). Review the policy before using this method. OAuth token support is deprecated and will be removed in a future version.

## Getting Started

### 1. Clone and Configure

```sh
git clone https://github.com/mtzanidakis/praktor.git
cd praktor
cp config/praktor.example.yaml config/praktor.yaml
cp .env.example .env && chmod 0600 .env
```

Edit `.env` and fill in your credentials (see comments in the file for details). Set `DOCKER_GID` to the group ID of the `docker` group on your host so the non-root container user can access the Docker socket:

```sh
grep docker /etc/group    # look for the docker group GID
```

Edit `config/praktor.yaml` to define your agents:

```yaml
telegram:
  allow_from: []            # Telegram user IDs (empty = allow all)
  main_chat_id: 0           # Chat ID for scheduled task / swarm results

defaults:
  model: "claude-sonnet-4-6"
  max_running: 5
  idle_timeout: 10m

agents:
  general:
    description: "General-purpose assistant for everyday tasks"
    nix_enabled: true
  coder:
    description: "Software engineering specialist"
    model: "claude-opus-4-6"
    nix_enabled: true
    env:
      GITHUB_TOKEN: "secret:github-token"    # Resolved from vault
  researcher:
    description: "Web research and analysis"
    allowed_tools: [WebSearch, WebFetch, Read, Write]

router:
  default_agent: general
```

### 2. Build and Run

```sh
docker compose build agent    # Build the agent image locally
docker compose up -d          # Start the stack (pulls gateway from GHCR)
docker compose logs -f        # Watch logs
```

The agent image must be built locally because it bundles proprietary third-party software that cannot be redistributed. See [Third-Party Notice](#third-party-notice) for details.

Mission Control is available at `http://localhost:8080`.

### 3. Start Chatting

Open Telegram and send a message to your bot. Praktor routes it to the right agent, spins up a container, and responds. Use `@agent_name` to target a specific agent:

```
Hello!                              → routed to default agent
@coder fix the login bug            → routed to coder
@researcher find papers on RAG      → routed to researcher
```

For a secure setup without exposed ports, see [Production Deployment with Tailscale](#production-deployment-with-tailscale).

## Upgrading

Pull the latest code, images, and rebuild the agent:

```sh
./scripts/upgrade.sh
```

Then restart the stack:

```sh
docker compose up -d
```

## Hot Config Reload

The gateway watches `praktor.yaml` for changes and applies them automatically within seconds. No restart required.

| Reloads live | Requires restart |
|---|---|
| Agent definitions, default model/image/max_running/idle_timeout, default router agent, scheduler poll interval, main_chat_id | Telegram token, web port, NATS config, vault passphrase |

When an agent's config changes, its running container is stopped and lazily restarted on the next message. You can also trigger a reload manually:

```sh
docker compose kill -s HUP praktor
```

## Vault (Encrypted Secrets)

Praktor includes an encrypted vault for API keys, tokens, SSH keys, and service account files. Secrets are encrypted at rest with AES-256-GCM (Argon2id key derivation) and injected into containers at start time — never passed through the LLM.

### CLI

```sh
praktor vault set github-token --value "ghp_xxx" --description "GitHub PAT"
praktor vault set ssh-key --file ~/.ssh/id_rsa --description "Deploy key"
praktor vault list
praktor vault get github-token
praktor vault assign github-token --agent coder
praktor vault global github-token --enable
praktor vault delete github-token
```

Secrets can also be managed from the **Secrets** page in Mission Control.

### Usage in Agent Config

Reference secrets with `secret:name` syntax in `env` values or `files`:

```yaml
agents:
  coder:
    env:
      GITHUB_TOKEN: "secret:github-token"        # Injected as env var
    files:
      - secret: gcp-service-account              # Injected as file
        target: /etc/gcp/sa.json
        mode: "0600"
```

## Browser Automation

All agent containers come with [playwright-cli](https://github.com/microsoft/playwright-cli) pre-installed and configured to use system Chromium. Agents can navigate websites, fill forms, take screenshots, and extract data — no setup needed.

The browser session persists across messages within the same agent session and shuts down with the container on idle timeout.

```
@general take a screenshot of https://example.com
@general go to https://example.com/form, fill in the email field, and submit
```

The playwright-cli skill is automatically loaded into every agent's system prompt, teaching it the full command set (`open`, `goto`, `click`, `fill`, `screenshot`, `snapshot`, tabs, etc.).

## Agent Extensions

Extend agents with MCP servers, plugins, and skills — all managed per-agent from Mission Control. Extensions are applied automatically when containers start.

> Extensions require `nix_enabled: true` on the agent. Dependencies are auto-installed via nix.

### MCP Servers

Connect agents to external tools via the Model Context Protocol. Supports **stdio** (local commands) and **HTTP** transports.

In Mission Control, go to the agent's **Extensions** tab, add an MCP server, and configure the JSON:

**HTTP server** (e.g., [Context7](https://context7.com/)):
```json
{
  "type": "http",
  "url": "https://mcp.context7.com/mcp",
  "headers": { "CONTEXT7_API_KEY": "secret:context7-api-key" }
}
```

**Stdio server:**
```json
{
  "type": "stdio",
  "command": "some-tool",
  "args": ["--flag"],
  "env": { "API_KEY": "secret:some-api-key" }
}
```

Secret references (`secret:name`) in `env` and `headers` values are resolved from the vault at container start. Stdio commands that aren't already in `$PATH` are auto-installed via nix.

> MCP servers are injected at runtime via the SDK `query()` call. They won't appear in `claude mcp list` — but agents can use their tools during conversations.

### Plugins

Claude Code marketplace plugins, installed via `claude plugin install` on first container start. Persisted on the agent's home volume so installation is one-time.

Plugin names use the `plugin-name@marketplace` format. Plugins can be temporarily disabled without uninstalling.

**Marketplaces:** Plugins come from marketplaces. The `claude-plugins-official` marketplace is always available, but third-party marketplaces must be registered first. Add marketplace sources (`owner/repo`, git URLs, or URLs to `marketplace.json`) in the Marketplaces tab — they are registered via `claude plugin marketplace add` before plugin installation.

### Skills

Custom instructions written to `~/.claude/skills/{name}/SKILL.md` on container start. Claude Code discovers them automatically.

Each skill has a `description` and a `content` body (the SKILL.md text). Skills can also include additional `files` (base64-encoded) written alongside the SKILL.md in the skill directory — useful for scripts or configs the skill references. Removed skills have their directories cleaned up on the next container start.

### How It Works

Saving extensions in Mission Control stops the running agent container. On the next message:

1. The gateway loads extensions from the DB, resolves any `secret:` references from the vault
2. The resolved extensions are passed to the container as the `AGENT_EXTENSIONS` env var
3. The agent-runner auto-installs missing nix dependencies for stdio MCP server commands
4. MCP servers are merged into the SDK `query()` call; skills are written to disk; plugins are installed/enabled

## Agent Swarms

Swarms let multiple agents collaborate on a task. Define a graph of agents and connections, and Praktor orchestrates execution.

### Orchestration Patterns

| Pattern | Connection | Behavior |
|---|---|---|
| **Fan-out** | None | Agents run in parallel, independently |
| **Pipeline** | A → B | B waits for A and receives A's output as context |
| **Collaborative** | A ↔ B | Agents share a real-time chat channel |

The **lead agent** always runs last and synthesizes all results.

### From Mission Control

The **Swarms** page provides a visual graph editor: add agents, draw connections, toggle between pipeline and collaborative edges, set the lead agent, write prompts, and launch. Running swarms show real-time status updates.

### From Telegram

```
@swarm researcher,writer,reviewer: Write a blog post about AI agents
@swarm researcher>writer>reviewer: Write a blog post about AI agents
@swarm researcher<>writer,reviewer: Write a blog post about AI agents
```

Results are delivered to the originating chat when the swarm completes.

## Nix Package Manager

Agents with `nix_enabled: true` can install any package from [nixpkgs](https://search.nixos.org/packages) on demand — no custom Docker images needed.

When an agent needs a missing tool (e.g. Python, ffmpeg), it will automatically search and install it via nix MCP tools. Installed packages persist across sessions.

### MCP Tools

| Tool | Description |
|---|---|
| `nix_search` | Search nixpkgs for packages |
| `nix_add` | Install a package |
| `nix_list_installed` | List installed packages |
| `nix_remove` | Remove a package |
| `nix_upgrade` | Upgrade all packages |

### Telegram Command

```
/nix search python3          # Search for packages
/nix add python3             # Install a package
/nix list                    # List installed packages
/nix remove python3          # Remove a package
/nix upgrade                 # Upgrade all packages
/nix list @coder             # Target a specific agent
```

## Backup & Restore

Back up all praktor state (SQLite, NATS, agent workspaces, home dirs, nix stores) into a single zstd-compressed tarball, and restore it later.

### Backup

```sh
praktor backup -f /path/to/backup.tar.zst
```

Creates a tarball of all `praktor-*` Docker volumes. Each volume becomes a top-level directory in the archive.

### Restore

```sh
praktor restore -f /path/to/backup.tar.zst              # Fails if volumes exist
praktor restore -f /path/to/backup.tar.zst -overwrite    # Overwrites existing volumes
```

Restores volumes from a backup archive. Without `-overwrite`, refuses to restore if any target volume already exists.

Both commands accept `-image <name>` to override the helper container image (default: `alpine:3`).

### Scheduled Backups

The included `docker-compose.override.yml-prod` configures hourly automated backups using [Ofelia](https://github.com/netresearch/ofelia), a Docker job scheduler. It runs `praktor backup` inside the container and writes to a `./dumps` bind mount. Adjust the schedule or add your own jobs via Ofelia labels.

## Production Deployment with Tailscale

For production, a `docker-compose.override.yml-prod` is included that adds a [tsrp](https://github.com/mtzanidakis/tsrp) (Tailscale Reverse Proxy) sidecar and removes the public port mapping. Mission Control becomes accessible only over your Tailscale network.

```sh
ln -s docker-compose.override.yml-prod docker-compose.override.yml
```

Add the following to your `.env`:

```sh
HOSTNAME=praktor              # Tailscale machine name
TS_AUTHKEY=tskey-auth-...     # Tailscale auth key (https://login.tailscale.com/admin/settings/keys)
```

Then start as usual — the override is picked up automatically:

```sh
docker compose up -d
```

Mission Control will be available at `https://praktor.<your-tailnet>.ts.net`. The tsrp container stores its Tailscale state in `./state/`.

## Development

```sh
go mod download              # Install Go dependencies
make dev                     # Run the gateway locally
make test                    # Run tests
```

Mission Control with hot reload:

```sh
cd ui && npm install && npm run dev    # Vite dev server on :5173, proxies /api to :8080
```

## License

See [LICENSE](LICENSE).

## Third-Party Notice

This project integrates with third-party tools that have their own licenses and terms of service. In particular, [Claude Code](https://github.com/anthropics/claude-code) and the [Claude Agent SDK](https://github.com/anthropics/claude-agent-sdk-typescript) are proprietary software by Anthropic and subject to [Anthropic's Commercial Terms of Service](https://www.anthropic.com/legal/commercial-terms). They are not included in this repository — users must install them at build time and are responsible for complying with Anthropic's terms. Pre-built Docker images containing these components should not be redistributed without Anthropic's permission.
