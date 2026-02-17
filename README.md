# Praktor

Personal AI agent assistant. Define named agents with distinct roles and models, message them from Telegram with smart routing, and monitor everything from Mission Control.

```
Telegram ──> Go Gateway ──> Router ──> Embedded NATS ──> Agent Containers (Docker)
                 |                                            |
             SQLite DB                                Claude Code SDK
                 |
       Mission Control (React SPA)
```

Praktor is a single Go binary that orchestrates the full loop: it receives messages from a Telegram bot, routes them to the right agent, spins up isolated Docker containers running Claude Code, streams responses back, and serves a Mission Control web UI for monitoring. Each agent gets its own sandboxed container with persistent memory via per-agent `CLAUDE.md` files.

## Features

- **Telegram I/O** - Chat with your agents from your phone
- **Named agents** - Define multiple agents with distinct roles, models, and configurations
- **Smart routing** - `@agent_name` prefix or AI-powered routing via the default agent
- **Per-agent isolation** - Each agent runs in its own Docker container with its own filesystem and memory (Docker named volumes)
- **Agent identity** - Each agent has an `AGENT.md` file with personality, vibe, and expertise — editable from Mission Control or by agents themselves
- **User profile** - Agents know who you are via `USER.md` — editable from Mission Control or by agents themselves
- **Scheduled tasks** - Cron, interval, or one-shot jobs that run agents and deliver results via Telegram
- **Secure vault** - AES-256-GCM encrypted secrets, injected as env vars or files at container start (never exposed to LLM)
- **Hot config reload** - Edit `praktor.yaml` and changes apply automatically — no restart needed
- **Agent swarms** - Spin up teams of specialized agents that collaborate on complex tasks
- **Web & browser access** - Agents can search the web and control Chromium
- **Mission Control** - Real-time dashboard with WebSocket updates

## Prerequisites

- Docker and Docker Compose
- A Telegram bot token
- A Claude authentication method (API key or Claude subscription)

## Getting Started

### 1. Create a Telegram Bot

Open Telegram and message [@BotFather](https://t.me/BotFather):

1. Send `/newbot`
2. Choose a display name (e.g. "Praktor")
3. Choose a username (e.g. `my_praktor_bot`)
4. BotFather will reply with your **bot token** - save it

Optionally, find your Telegram user ID (message [@userinfobot](https://t.me/userinfobot)) to restrict who can use the bot.

### 2. Set Up Claude Authentication

Praktor supports two authentication methods. Pick one:

**Option A: Anthropic API Key (pay-per-use)**

1. Go to [console.anthropic.com](https://console.anthropic.com/)
2. Create an account and add billing
3. Go to **API Keys** and create a new key
4. Save the key (starts with `sk-ant-`)

**Option B: Claude Code OAuth Token (use your Claude Pro/Team/Max subscription)**

1. Install Claude Code: `npm install -g @anthropic-ai/claude-code`
2. Run `claude setup-token` and copy the token it gives you

### 3. Configure Praktor

```sh
git clone https://github.com/mtzanidakis/praktor.git
cd praktor
cp config/praktor.example.yaml config/praktor.yaml
```

Create a `.env` file in the project root:

```sh
# Telegram
PRAKTOR_TELEGRAM_TOKEN=your-bot-token-from-botfather

# Claude auth (pick one)
ANTHROPIC_API_KEY=sk-ant-your-api-key
# OR
CLAUDE_CODE_OAUTH_TOKEN=your-oauth-token

# Mission Control password (optional)
PRAKTOR_WEB_PASSWORD=your-secret-password

# Vault passphrase for encrypted secrets (required)
PRAKTOR_VAULT_PASSPHRASE=your-vault-passphrase
```

Edit `config/praktor.yaml` to configure your agents:

```yaml
telegram:
  allow_from: []            # Telegram user IDs allowed to use the bot (empty = allow all)
  main_chat_id: 0           # Your chat ID for scheduled task results

defaults:
  model: "claude-sonnet-4-6"
  max_running: 5            # Max concurrent agent containers
  idle_timeout: 10m         # Stop idle containers after this duration

agents:
  general:
    description: "General-purpose assistant for everyday tasks"
  coder:
    description: "Software engineering specialist"
    model: "claude-opus-4-6"
    env:
      GITHUB_TOKEN: "${GITHUB_TOKEN}"
  researcher:
    description: "Web research and analysis"
    allowed_tools: [WebSearch, WebFetch, Read, Write]

router:
  default_agent: general    # Agent used for routing and as fallback
```

### 4. Build and Run

Build the container images and start the stack:

```sh
# Build both container images (gateway + agent)
make containers

# Start Praktor
docker compose up -d
```

Mission Control is available at `http://localhost:8080`.

Data is stored in Docker named volumes (`praktor-data`, `praktor-global`) — no host directory bind mounts needed. Both gateway and agent containers run as non-root user `praktor` (uid 10321).

Open Telegram and send a message to your bot. Praktor will route it to the right agent, spin up a container, and respond. Use `@coder fix the bug` to explicitly target an agent.

### 5. Verify

Check that everything is running:

```sh
# Service health
curl http://localhost:8080/api/status

# View logs
docker compose logs -f praktor
```

## Hot Config Reload

The gateway watches `praktor.yaml` for changes (mtime polled every 3s, SHA-256 verified on change) and automatically applies them — no restart required. Edit your config file and save; changes take effect within a few seconds.

**What reloads live:** agent definitions, default model/image/max_running/idle_timeout, default router agent, scheduler poll interval, telegram main_chat_id.

**What requires a restart:** telegram token, web port, NATS config, vault passphrase (a warning is logged if these change).

When an agent's config changes (model, env, image, etc.), its running container is stopped. The next message to that agent starts a fresh container with the new configuration. New agents become routable immediately; removed agents are stopped.

You can also trigger a reload manually with SIGHUP:

```sh
docker compose kill -s HUP praktor
```

## Vault (Encrypted Secrets)

Praktor includes an encrypted vault for storing secrets (API keys, tokens, SSH keys, service account files). Secrets are encrypted at rest with AES-256-GCM using a key derived from your passphrase via Argon2id.

Secrets are injected into agent containers at start time — as environment variables or files — and never pass through the LLM conversation.

### Setup

Set the `PRAKTOR_VAULT_PASSPHRASE` environment variable (or `vault.passphrase` in YAML config). The gateway will not start without it.

### CLI Usage

```sh
# Store a string secret
praktor vault set github-token --value "ghp_xxxxxxxxxxxx"

# Store a file secret
praktor vault set ssh-key --file ~/.ssh/id_rsa

# List all secrets (metadata only)
praktor vault list

# Retrieve and decrypt a secret
praktor vault get github-token

# Assign a secret to a specific agent
praktor vault assign github-token --agent coder

# Make a secret globally available to all agents
praktor vault global github-token --enable

# Delete a secret
praktor vault delete github-token
```

### Mission Control

The Secrets page in Mission Control lets you create, edit, delete secrets and manage agent assignments through the web UI. Secret values are never displayed — only metadata is shown.

### Agent Config

Reference vault secrets in agent definitions using `secret:name` syntax:

```yaml
agents:
  coder:
    description: "Software engineering specialist"
    env:
      EDITOR: vim                                # Regular env var
      GITHUB_TOKEN: "secret:github-token"        # Resolved from vault
    files:
      - secret: gcp-service-account              # Secret name in vault
        target: /etc/gcp/sa.json                  # Path inside container
        mode: "0600"                              # File permissions
```

Regular env vars are passed through as-is. Values prefixed with `secret:` are resolved from the vault and injected as plain env vars. File secrets are copied into the container before it starts. The agent uses `$GITHUB_TOKEN` or reads `/etc/gcp/sa.json` directly — secret values never enter the LLM context.

## Development

Run the gateway locally (without Docker) for development:

```sh
# Install Go dependencies
go mod download

# Run the gateway
make dev

# Run tests
make test
```

Mission Control can be developed separately with hot reload:

```sh
cd ui
npm install
npm run dev    # Starts Vite dev server on :5173, proxies /api to :8080
```

## License

See [LICENSE](LICENSE).
