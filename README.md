# Praktor

Personal AI agent assistant. Message Claude from Telegram, get responses powered by Claude Code running in isolated Docker containers, and monitor everything from a web dashboard.

```
Telegram ──> Go Gateway ──> Embedded NATS ──> Agent Containers (Docker)
                 |                                     |
             SQLite DB                         Claude Code SDK
                 |
          Web UI (React SPA)
```

Praktor is a single Go binary that orchestrates the full loop: it receives messages from a Telegram bot, spins up isolated Docker containers running Claude Code, streams responses back, and serves a Mission Control web UI for monitoring. Each Telegram group gets its own sandboxed container with persistent memory via per-group `CLAUDE.md` files.

## Features

- **Telegram I/O** - Chat with Claude from your phone
- **Per-group isolation** - Each group runs in its own Docker container with its own filesystem and memory
- **Admin channel** - Your private Telegram chat has elevated access to the full project
- **Scheduled tasks** - Cron, interval, or one-shot jobs that run Claude and deliver results
- **Agent swarms** - Spin up teams of specialized agents that collaborate on complex tasks
- **Mission Control** - Real-time web dashboard with WebSocket updates
- **Web & browser access** - Agents can search the web and control Chromium

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
2. Run `claude` and complete the OAuth login flow in your browser
3. After login, extract the token:
   ```sh
   jq -rcM .claudeAiOauth.accessToken ~/.claude/.credentials.json
   ```
4. Save the `accessToken` value

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

# Web dashboard password (optional)
PRAKTOR_WEB_PASSWORD=your-secret-password
```

Edit `config/praktor.yaml` to customize settings. Key options:

```yaml
telegram:
  allow_from: []          # List of Telegram user IDs allowed to use the bot
                          # Empty = allow everyone (fine for personal use)

agent:
  max_containers: 5       # Max concurrent agent containers
  idle_timeout: 30m       # Stop idle containers after this duration

groups:
  main_chat_id: ""        # Your Telegram user ID for admin channel
```

To set your admin channel, put your Telegram user ID in `main_chat_id`. The admin channel gets read-write access to the full project directory.

### 4. Build and Run

Build the agent container image first, then start the stack:

```sh
# Build both container images
make containers

# Start Praktor
docker compose up -d
```

The web dashboard is available at `http://localhost:8080`.

Open Telegram and send a message to your bot. Praktor will spin up an agent container and respond.

### 5. Verify

Check that everything is running:

```sh
# Service health
curl http://localhost:8080/api/status

# View logs
docker compose logs -f praktor
```

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

The web UI can be developed separately with hot reload:

```sh
cd ui
npm install
npm run dev    # Starts Vite dev server on :5173, proxies /api to :8080
```

## License

See [LICENSE](LICENSE).
