import { query } from "@anthropic-ai/claude-agent-sdk";
import { NatsBridge } from "./nats-bridge.js";
import { applyExtensions } from "./extensions.js";
import { readFileSync, readdirSync, mkdirSync, writeFileSync, rmSync, symlinkSync, existsSync, lstatSync, readlinkSync, unlinkSync } from "fs";
import { join } from "path";
import { execSync } from "child_process";
import { DatabaseSync } from "node:sqlite";

// Patch console to prepend timestamps matching gateway format (YYYY/MM/DD HH:MM:SS)
const origLog = console.log;
const origWarn = console.warn;
const origError = console.error;
function ts(): string {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}
console.log = (...args: unknown[]) => origLog(ts(), ...args);
console.warn = (...args: unknown[]) => origWarn(ts(), ...args);
console.error = (...args: unknown[]) => origError(ts(), ...args);

const NATS_URL = process.env.NATS_URL || "nats://localhost:4222";
const AGENT_ID = process.env.AGENT_ID || process.env.GROUP_ID || "default";
const CLAUDE_MODEL = process.env.CLAUDE_MODEL || undefined;
const ALLOWED_TOOLS_ENV = process.env.ALLOWED_TOOLS || "";
const SWARM_CHAT_TOPIC = process.env.SWARM_CHAT_TOPIC || "";
const SWARM_ROLE = process.env.SWARM_ROLE || "";

let bridge: NatsBridge;
let isProcessing = false;
let lastSessionId: string | undefined;
let currentQueryIter: AsyncIterator<unknown> | null = null;
let aborted = false;
let extensionMcpServers: Record<string, { type: string; command?: string; args?: string[]; url?: string; env?: Record<string, string>; headers?: Record<string, string> }> = {};
const pendingMessages: Array<Record<string, unknown>> = [];

// Parallel task execution
const MAX_PARALLEL_TASKS = parseInt(process.env.MAX_PARALLEL_TASKS || "3", 10);
let activeTaskCount = 0;
const activeQueries = new Map<string, AsyncIterator<unknown>>();
const pendingTasks: Array<Record<string, unknown>> = [];

// Swarm collaborative chat buffer
interface ChatMessage {
  from: string;
  content: string;
  timestamp: number;
}
const chatHistory: ChatMessage[] = [];

export function parseAllowedTools(env: string): string[] | undefined {
  if (!env) return undefined;
  const tools = env.split(",").map((t) => t.trim()).filter(Boolean);
  return tools.length > 0 ? tools : undefined;
}

function installGlobalInstructions(): void {
  // Write global instructions to ~/.claude/CLAUDE.md (user-level).
  // Claude Code automatically loads both user-level and project-level CLAUDE.md,
  // so we only need to write the global one here. The per-agent CLAUDE.md in
  // /workspace/agent/ is loaded automatically as the project-level file.
  try {
    const global = readFileSync("/workspace/global/CLAUDE.md", "utf-8");
    const userClaudeDir = "/home/praktor/.claude";
    mkdirSync(userClaudeDir, { recursive: true });
    writeFileSync(`${userClaudeDir}/CLAUDE.md`, global);
    console.log(`[agent] installed global instructions to ${userClaudeDir}/CLAUDE.md`);
  } catch (err) {
    console.warn("[agent] could not install global instructions:", err);
  }
}

const agentMdTemplate = `# Agent Identity

## Name
(Agent display name)

## Vibe
(Personality, communication style)

## Expertise
(Areas of specialization)
`;

function ensureAgentMd(): void {
  const path = "/workspace/agent/AGENT.md";
  if (!existsSync(path)) {
    try {
      writeFileSync(path, agentMdTemplate);
      console.log("[agent] created AGENT.md template");
    } catch (err) {
      console.warn("[agent] could not create AGENT.md:", err);
    }
  }
}

function setupAgentBrowser(): void {
  const skillSource = "/usr/local/lib/node_modules/agent-browser/skills/agent-browser";
  const configSource = "/usr/local/share/agent-browser/config.json";
  if (!existsSync(skillSource)) return; // agent-browser not installed

  try {
    const skillsDir = "/home/praktor/.claude/skills";
    mkdirSync(skillsDir, { recursive: true });

    // Remove stale playwright-cli symlink from previous image versions
    const staleLink = join(skillsDir, "playwright-cli");
    try {
      if (lstatSync(staleLink).isSymbolicLink() && readlinkSync(staleLink) === "/opt/playwright-cli/skill") {
        unlinkSync(staleLink);
        console.log("[agent] removed stale playwright-cli skill symlink");
      }
    } catch { /* doesn't exist */ }

    // Force-update skill symlink
    const skillLink = join(skillsDir, "agent-browser");
    try { unlinkSync(skillLink); } catch { /* doesn't exist */ }
    symlinkSync(skillSource, skillLink);

    // Force-update config symlink
    const configDir = "/home/praktor/.agent-browser";
    mkdirSync(configDir, { recursive: true });
    const configLink = join(configDir, "config.json");
    try { unlinkSync(configLink); } catch { /* doesn't exist */ }
    symlinkSync(configSource, configLink);

    console.log("[agent] agent-browser configured");
  } catch (err) {
    console.warn("[agent] could not configure agent-browser:", err);
  }
}

function loadSystemPrompt(includeIdentity = true): string {
  const parts: string[] = [];

  // User profile (loaded before global instructions so agents know the user)
  try {
    const user = readFileSync("/workspace/global/USER.md", "utf-8");
    parts.push(user);
  } catch {
    // User profile not available
  }

  // Agent identity (excluded for routing queries to avoid personality bleed)
  if (includeIdentity) {
    try {
      const agent = readFileSync("/workspace/agent/AGENT.md", "utf-8");
      parts.push(
        "The following is your agent identity. " +
        "This is stored at /workspace/agent/AGENT.md and you can update it " +
        "anytime using the Edit or Write tool (e.g. to set your name, vibe, or expertise).\n\n" +
        agent
      );
    } catch {
      // Agent identity not available
    }
  }

  // Include global instructions in system prompt as well (belt and suspenders)
  try {
    const global = readFileSync("/workspace/global/CLAUDE.md", "utf-8");
    parts.push(global);
  } catch {
    // Global instructions not available
  }

  // Nix package manager: detect nix-daemon and inform agent
  try {
    execSync("pgrep -l nix-daemon", { timeout: 5000 });
    parts.push(
      "NIX PACKAGE MANAGER — You have the nix package manager available.\n" +
      "- When a task requires a tool or language not present in the container, use nix to install it.\n" +
      "- Use the `nix_search` MCP tool to find packages, and `nix_add` to install them.\n" +
      "- Use `nix_list_installed` to see what's already installed.\n" +
      "- Example: if asked to run a Python script and python is missing, install it with nix_add(package: \"python3\") first.\n" +
      "- Always check if a command exists before installing (e.g. `which python3`)."
    );
  } catch {
    // nix-daemon not running, skip
  }

  // Messaging: explain how agent responses reach the user
  parts.push(
    "MESSAGING — Your text responses are automatically delivered to the user via Telegram.\n" +
    "- To send a message, simply reply with text — no special tool is needed.\n" +
    "- The file_send tool is ONLY for sending binary files (images, PDFs, etc.), NOT for text messages. NEVER create .txt files to deliver text content.\n" +
    "- When executing scheduled tasks, your text reply IS the notification the user receives.\n" +
    "- Keep scheduled task replies short and direct — the user sees them as Telegram messages."
  );

  // Telegram formatting: instruct agent to use Telegram-compatible Markdown
  parts.push(
    "TELEGRAM FORMATTING — Your messages are rendered in Telegram, which only supports MarkdownV1.\n" +
    "- Bold: *text* (single asterisks, NOT **double**)\n" +
    "- Italic: _text_\n" +
    "- Inline code: `code`\n" +
    "- Code blocks: ```code```\n" +
    "- Links: [text](url)\n" +
    "- DO NOT use: # headers, - bullet lists, --- horizontal rules, ![]()" +
    " image embeds — these render as raw text in Telegram.\n" +
    "- Instead of headers, use *bold text* on its own line.\n" +
    "- Instead of bullet lists with - or *, use • (bullet character) or numbered lists."
  );

  // Security: prevent agents from revealing secret values
  parts.push(
    "SECURITY — MANDATORY RULES:\n" +
    "- NEVER reveal, print, or include the values of environment variables that contain secrets, tokens, API keys, passwords, or credentials.\n" +
    "- NEVER read or output the contents of secret files (e.g. service account JSON files, SSH keys, certificates).\n" +
    "- If the user asks for a secret value, respond with [REDACTED] in place of the value and explain that secrets cannot be disclosed.\n" +
    "- You may confirm that a secret or env var EXISTS, but must NEVER show its value — always use [REDACTED] as placeholder."
  );

  // Memory: list existing keys so the agent knows what's stored
  try {
    const MEMORY_DB_PATH = "/workspace/agent/memory.db";
    let memorySection =
      "MEMORY — You have persistent memory via MCP tools (memory_store, memory_recall, memory_forget, memory_delete, memory_list).\n" +
      "- To remember: call memory_store with a short key and content\n" +
      "- To recall: call memory_recall with a keyword to search\n" +
      "- To forget: call memory_forget with a search query";

    if (existsSync(MEMORY_DB_PATH)) {
      const db = new DatabaseSync(MEMORY_DB_PATH);
      // access_count may not exist yet on older databases
      let rows: Array<{ key: string; tags: string; access_count?: number }>;
      try {
        rows = db.prepare(
          "SELECT key, tags, access_count FROM memories ORDER BY updated_at DESC"
        ).all() as typeof rows;
      } catch {
        rows = db.prepare(
          "SELECT key, tags FROM memories ORDER BY updated_at DESC"
        ).all() as typeof rows;
      }
      db.close();

      if (rows.length > 0) {
        memorySection += `\n\nYou currently have ${rows.length} stored memories:\n`;
        memorySection += rows
          .map((r) => {
            let line = `- ${r.key}`;
            if (r.tags) line += ` [${r.tags}]`;
            if (r.access_count) line += ` (${r.access_count}x)`;
            return line;
          })
          .join("\n");
        memorySection += "\n\nCall memory_recall with a relevant keyword to retrieve full content before answering.";
        memorySection += " memory_recall uses hybrid search combining keyword matching with semantic similarity — use natural language queries for best results.";
      }
    }
    parts.push(memorySection);
  } catch (err) {
    console.warn("[agent] could not load memory keys:", err);
  }

  // agent-browser: inform agent it's pre-installed with system chromium
  if (existsSync("/usr/local/lib/node_modules/agent-browser")) {
    parts.push(
      "AGENT-BROWSER — Pre-installed and configured. Do NOT install browsers via npm, npx, nix, or any other method.\n" +
      "- `agent-browser` is already in PATH and ready to use.\n" +
      "- It is configured to use the system Chromium at `/usr/bin/chromium-browser`.\n" +
      "- Run `agent-browser open <url>` to start a browser session, then `agent-browser snapshot -i` to see the page.\n" +
      "- The browser persists across messages. Reuse the existing session.\n" +
      "- When executing a scheduled task, ALWAYS run `agent-browser close` when done to free resources."
    );
  }

  // Skills: load installed SKILL.md files into prompt
  const skillsDir = "/home/praktor/.claude/skills";
  try {
    if (existsSync(skillsDir)) {
      const entries = readdirSync(skillsDir, { withFileTypes: true });
      for (const entry of entries) {
        if (!entry.isDirectory()) continue;
        const skillMd = join(skillsDir, entry.name, "SKILL.md");
        try {
          const content = readFileSync(skillMd, "utf-8");
          parts.push(`SKILL: ${entry.name}\n\n${content}`);
        } catch {
          // SKILL.md not found in this directory, skip
        }
      }
    }
  } catch {
    // skills directory not accessible, skip
  }

  return parts.join("\n\n---\n\n");
}

function buildQueryOptions(prompt: string, sessionId?: string) {
  const systemPrompt = loadSystemPrompt();
  const cwd = "/workspace/agent";
  const configuredTools = parseAllowedTools(ALLOWED_TOOLS_ENV);
  const allowedTools = configuredTools || [
    "Bash",
    "Read",
    "Write",
    "Edit",
    "Glob",
    "Grep",
    "WebSearch",
    "WebFetch",
    "Task",
    "TaskOutput",
    "mcp__praktor-*",
  ];

  return {
    prompt,
    options: {
      model: CLAUDE_MODEL,
      cwd,
      pathToClaudeCodeExecutable: "/usr/local/bin/claude",
      systemPrompt: systemPrompt || undefined,
      ...(sessionId ? { resume: sessionId } : {}),
      allowedTools,
      mcpServers: {
        "praktor-tasks": {
          type: "stdio",
          command: "node",
          args: ["/app/mcp-tasks.mjs"],
          env: { NATS_URL, AGENT_ID },
        },
        "praktor-profile": {
          type: "stdio",
          command: "node",
          args: ["/app/mcp-profile.mjs"],
          env: { NATS_URL, AGENT_ID },
        },
        "praktor-memory": {
          type: "stdio",
          command: "node",
          args: ["/app/mcp-memory.mjs"],
          env: {},
        },
        "praktor-nix": {
          type: "stdio",
          command: "node",
          args: ["/app/mcp-nix.mjs"],
          env: {},
        },
        "praktor-file": {
          type: "stdio",
          command: "node",
          args: ["/app/mcp-file.mjs"],
          env: { NATS_URL, AGENT_ID },
        },
        "praktor-history": {
          type: "stdio",
          command: "node",
          args: ["/app/mcp-history.mjs"],
          env: { NATS_URL, AGENT_ID },
        },
        ...(SWARM_CHAT_TOPIC ? {
          "praktor-swarm": {
            type: "stdio",
            command: "node",
            args: ["/app/mcp-swarm.mjs"],
            env: { NATS_URL, AGENT_ID, SWARM_CHAT_TOPIC },
          },
        } : {}),
        ...extensionMcpServers,
      },
      permissionMode: "bypassPermissions",
      allowDangerouslySkipPermissions: true,
      stderr: (data: string) => {
        console.error(`[claude-stderr] ${data.trimEnd()}`);
      },
    },
  };
}

// Execute a scheduled task in parallel (fresh session, no resume)
async function executeTask(data: Record<string, unknown>): Promise<void> {
  const text = data.text as string;
  const msgId = data.msg_id as string | undefined;
  console.log(`[task] executing parallel task: ${text.substring(0, 100)}...`);

  try {
    const opts = buildQueryOptions(text);
    const result = query(opts);

    let fullResponse = "";
    const iter = result[Symbol.asyncIterator]();
    if (msgId) activeQueries.set(msgId, iter);

    try {
      for await (const event of { [Symbol.asyncIterator]: () => iter }) {
        if (event.type === "result" && event.subtype === "success") {
          fullResponse = event.result;
        } else if (event.type === "assistant") {
          for (const block of event.message.content) {
            if (block.type === "text") {
              await bridge.publishOutput(block.text, "text", msgId);
            } else if (block.type === "tool_use" || block.type === "server_tool_use") {
              console.log(`[task] tool: ${block.name}`);
            }
          }
        }
      }
    } catch (streamErr) {
      if (fullResponse) {
        console.warn(`[task] claude process exited with error after successful result, ignoring:`, streamErr);
      } else {
        throw streamErr;
      }
    }

    if (fullResponse && !aborted) {
      await bridge.publishResult(fullResponse, msgId);
    }
    console.log(`[task] completed`);
  } catch (err) {
    if (aborted) {
      console.log("[task] aborted");
      return;
    }
    const errorMsg = err instanceof Error ? err.message : String(err);
    console.error(`[task] error:`, err);
    await bridge.publishResult(`Error: ${errorMsg}`, msgId);
  } finally {
    if (msgId) activeQueries.delete(msgId);
    activeTaskCount--;
    // Dequeue next pending task
    if (pendingTasks.length > 0) {
      const next = pendingTasks.shift()!;
      activeTaskCount++;
      console.log(`[task] dequeuing next task (${pendingTasks.length} remaining)`);
      executeTask(next);
    }
  }
}

async function handleMessage(data: Record<string, unknown>): Promise<void> {
  const text = data.text as string;
  if (!text) return;

  const sender = data.sender as string | undefined;
  const msgId = data.msg_id as string | undefined;

  // Scheduled tasks run in parallel with fresh sessions
  if (sender === "scheduler") {
    if (activeTaskCount >= MAX_PARALLEL_TASKS) {
      pendingTasks.push(data);
      console.log(`[task] at capacity (${activeTaskCount}/${MAX_PARALLEL_TASKS}), queued (${pendingTasks.length} pending)`);
      return;
    }
    activeTaskCount++;
    executeTask(data);
    return;
  }

  // Regular messages: sequential with session continuity
  if (isProcessing) {
    pendingMessages.push(data);
    console.log(`[agent] already processing, queued message (${pendingMessages.length} pending)`);
    return;
  }

  isProcessing = true;
  aborted = false;
  console.log(`[agent] processing message for agent ${AGENT_ID}: ${text.substring(0, 100)}...`);

  try {
    // Prepend swarm chat context if in collaborative mode
    let augmentedText = text;
    if (SWARM_CHAT_TOPIC && chatHistory.length > 0) {
      const chatContext = chatHistory
        .map((m) => `[${m.from}]: ${m.content}`)
        .join("\n");
      augmentedText = `## Collaborative Chat History\n\n${chatContext}\n\n---\n\n${text}`;
      console.log(`[agent] prepended ${chatHistory.length} chat messages to prompt`);
    }

    console.log(`[agent] starting claude query`);

    const opts = buildQueryOptions(augmentedText, lastSessionId);
    const result = query(opts);

    // Process streaming result
    let fullResponse = "";
    const iter = result[Symbol.asyncIterator]();
    currentQueryIter = iter;
    try {
      for await (const event of { [Symbol.asyncIterator]: () => iter }) {
        console.log(`[agent] event: type=${event.type}${"subtype" in event ? ` subtype=${event.subtype}` : ""}`);
        if (event.type === "result" && event.subtype === "success") {
          fullResponse = event.result;
          lastSessionId = event.session_id;
        } else if (event.type === "assistant") {
          for (const block of event.message.content) {
            if (block.type === "text") {
              await bridge.publishOutput(block.text, "text", msgId);
            } else if (block.type === "tool_use" || block.type === "server_tool_use") {
              console.log(`[agent] tool: ${block.name}`);
            }
          }
        }
      }
    } catch (streamErr) {
      if (fullResponse) {
        console.warn(`[agent] claude process exited with error after successful result, ignoring:`, streamErr);
      } else {
        throw streamErr;
      }
    }

    // Send final result (skip if aborted — orchestrator already notified the user)
    if (fullResponse && !aborted) {
      await bridge.publishResult(fullResponse, msgId);
    }

    console.log(`[agent] completed processing for agent ${AGENT_ID} (session=${lastSessionId})`);
  } catch (err) {
    if (aborted) {
      console.log("[agent] query aborted by user");
      return;
    }
    const errorMsg = err instanceof Error ? err.message : String(err);
    console.error(`[agent] error processing message:`, err);
    await bridge.publishResult(`Error: ${errorMsg}`, msgId);
  } finally {
    currentQueryIter = null;
    isProcessing = false;

    // Process next queued message if any
    if (pendingMessages.length > 0) {
      const next = pendingMessages.shift()!;
      console.log(`[agent] dequeuing next message (${pendingMessages.length} remaining)`);
      handleMessage(next);
    }
  }
}

async function handleRoute(
  data: Record<string, unknown>,
  msg: import("nats").Msg
): Promise<void> {
  const text = data.text as string;
  if (!text) {
    msg.respond(new TextEncoder().encode(JSON.stringify({ agent: AGENT_ID })));
    return;
  }

  // If already processing a regular message, skip the routing query to avoid
  // concurrent Claude Code processes interfering via shared session state.
  if (isProcessing) {
    console.log("[agent] busy processing, returning default agent for routing");
    msg.respond(new TextEncoder().encode(JSON.stringify({ agent: AGENT_ID })));
    return;
  }

  console.log("[agent] routing query");

  try {
    const systemPrompt = loadSystemPrompt(false);
    const cwd = "/workspace/agent";

    // Build agent descriptions from environment if available
    const agentDescsEnv = process.env.AGENT_DESCRIPTIONS || "";
    let routingPrompt = `You are a message router. Given the user message below, respond with ONLY the name of the most appropriate agent to handle it. Do not include any other text.\n\n`;
    if (agentDescsEnv) {
      routingPrompt += `Available agents:\n${agentDescsEnv}\n\n`;
    }
    routingPrompt += `User message: ${text}`;

    const result = query({
      prompt: routingPrompt,
      options: {
        model: CLAUDE_MODEL,
        cwd,
        pathToClaudeCodeExecutable: "/usr/local/bin/claude",
        systemPrompt: systemPrompt || undefined,
        allowedTools: [],
        permissionMode: "bypassPermissions",
        allowDangerouslySkipPermissions: true,
      },
    });

    let agentName = "";
    for await (const event of result) {
      if (event.type === "result" && event.subtype === "success") {
        agentName = event.result.trim();
      }
    }

    msg.respond(new TextEncoder().encode(JSON.stringify({ agent: agentName })));
  } catch (err) {
    console.error(`[agent] routing error:`, err);
    msg.respond(new TextEncoder().encode(JSON.stringify({ agent: AGENT_ID })));
  }
}

async function handleControl(
  data: Record<string, unknown>,
  msg: import("nats").Msg
): Promise<void> {
  const command = data.command as string;

  switch (command) {
    case "shutdown":
      console.log("[agent] shutting down...");
      await bridge.close();
      process.exit(0);
      break;
    case "ping":
      msg.respond(new TextEncoder().encode(JSON.stringify({ status: "ok" })));
      break;
    case "abort":
      console.log("[agent] aborting current run...");
      aborted = true;
      // Abort conversational query
      if (currentQueryIter) {
        currentQueryIter.return?.(undefined);
        currentQueryIter = null;
      }
      // Abort all parallel task queries
      for (const [id, iter] of activeQueries) {
        iter.return?.(undefined);
      }
      activeQueries.clear();
      activeTaskCount = 0;
      // Kill any running claude processes as backstop
      try { execSync("pkill -f /usr/local/bin/claude", { timeout: 3000 }); } catch { /* ignore */ }
      // Drain all queues
      if (pendingMessages.length > 0) {
        console.log(`[agent] discarding ${pendingMessages.length} queued message(s)`);
        pendingMessages.length = 0;
      }
      if (pendingTasks.length > 0) {
        console.log(`[agent] discarding ${pendingTasks.length} queued task(s)`);
        pendingTasks.length = 0;
      }
      isProcessing = false;
      msg.respond(new TextEncoder().encode(JSON.stringify({ status: "ok" })));
      console.log("[agent] run aborted");
      break;
    case "clear_session":
      console.log("[agent] clearing session...");
      lastSessionId = undefined;
      for (const dir of [
        "/home/praktor/.claude/projects",
        "/home/praktor/.claude/sessions",
        "/home/praktor/.claude/debug",
        "/home/praktor/.claude/todos",
      ]) {
        try { rmSync(dir, { recursive: true, force: true }); } catch { /* ignore */ }
      }
      msg.respond(new TextEncoder().encode(JSON.stringify({ status: "ok" })));
      console.log("[agent] session cleared");
      break;
    default:
      console.warn(`[agent] unknown control command: ${command}`);
      msg.respond(new TextEncoder().encode(JSON.stringify({ error: `unknown command: ${command}` })));
      break;
  }
}

async function main(): Promise<void> {
  console.log(`[agent] starting for agent ${AGENT_ID}`);
  console.log(`[agent] NATS URL: ${NATS_URL}`);

  installGlobalInstructions();
  ensureAgentMd();
  setupAgentBrowser();

  // Apply agent extensions (MCP servers, plugins, skills, settings)
  const extResult = await applyExtensions();
  extensionMcpServers = extResult.mcpServers;

  // Clean up Claude Code internal files that accumulate over time
  for (const dir of ["/home/praktor/.claude/debug", "/home/praktor/.claude/todos"]) {
    try { rmSync(dir, { recursive: true, force: true }); } catch { /* ignore */ }
  }

  bridge = new NatsBridge(NATS_URL, AGENT_ID);
  await bridge.connect();

  // Report any extension errors via NATS
  if (extResult.errors.length > 0) {
    const errMsg = `Extension errors:\n${extResult.errors.map((e) => `- ${e}`).join("\n")}`;
    console.error(`[extensions] ${errMsg}`);
    await bridge.publishOutput(errMsg, "text");
  }

  bridge.subscribeInput(handleMessage);
  bridge.subscribeControl(handleControl);
  bridge.subscribeRoute(handleRoute);

  // Subscribe to swarm collaborative chat if in swarm mode
  if (SWARM_CHAT_TOPIC) {
    console.log(`[agent] swarm mode: subscribing to chat topic ${SWARM_CHAT_TOPIC}`);
    bridge.subscribeSwarmChat(SWARM_CHAT_TOPIC, (msg) => {
      // Don't echo own messages
      if (msg.from === AGENT_ID) return;
      chatHistory.push({
        from: msg.from,
        content: msg.content,
        timestamp: Date.now(),
      });
      console.log(`[agent] swarm chat from ${msg.from}: ${msg.content.substring(0, 80)}...`);
    });
  }

  // Flush to ensure subscriptions are registered with NATS server
  await bridge.flush();

  await bridge.publishReady();
  console.log(`[agent] ready and listening for messages`);

  // Keep process alive
  process.on("SIGTERM", async () => {
    console.log("[agent] SIGTERM received, shutting down...");
    await bridge.close();
    process.exit(0);
  });

  process.on("SIGINT", async () => {
    console.log("[agent] SIGINT received, shutting down...");
    await bridge.close();
    process.exit(0);
  });
}

main().catch((err) => {
  console.error("[agent] fatal error:", err);
  process.exit(1);
});
