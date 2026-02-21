import { query } from "@anthropic-ai/claude-agent-sdk";
import { NatsBridge } from "./nats-bridge.js";
import { applyExtensions } from "./extensions.js";
import { readFileSync, readdirSync, mkdirSync, writeFileSync, rmSync, existsSync } from "fs";
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
const SESSION_ID = process.env.SESSION_ID || undefined;
const CLAUDE_MODEL = process.env.CLAUDE_MODEL || undefined;
const ALLOWED_TOOLS_ENV = process.env.ALLOWED_TOOLS || "";
const SWARM_CHAT_TOPIC = process.env.SWARM_CHAT_TOPIC || "";
const SWARM_ROLE = process.env.SWARM_ROLE || "";

let bridge: NatsBridge;
let isProcessing = false;
let hasSession = false;
let currentQueryIter: AsyncIterator<unknown> | null = null;
let abortPending = false;
let extensionMcpServers: Record<string, { type: string; command?: string; args?: string[]; url?: string; env?: Record<string, string>; headers?: Record<string, string> }> = {};

// Swarm collaborative chat buffer
interface ChatMessage {
  from: string;
  content: string;
  timestamp: number;
}
const chatHistory: ChatMessage[] = [];

function parseAllowedTools(): string[] | undefined {
  if (!ALLOWED_TOOLS_ENV) return undefined;
  const tools = ALLOWED_TOOLS_ENV.split(",").map((t) => t.trim()).filter(Boolean);
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
      const rows = db.prepare(
        "SELECT key, tags FROM memories ORDER BY updated_at DESC"
      ).all() as Array<{ key: string; tags: string }>;
      db.close();

      if (rows.length > 0) {
        memorySection += `\n\nYou currently have ${rows.length} stored memories:\n`;
        memorySection += rows
          .map((r) => `- ${r.key}${r.tags ? ` [${r.tags}]` : ""}`)
          .join("\n");
        memorySection += "\n\nCall memory_recall with a relevant keyword to retrieve full content before answering.";
      }
    }
    parts.push(memorySection);
  } catch (err) {
    console.warn("[agent] could not load memory keys:", err);
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

async function handleMessage(data: Record<string, unknown>): Promise<void> {
  const text = data.text as string;
  if (!text) return;

  if (abortPending) {
    abortPending = false;
    console.log("[agent] discarding message received after abort");
    return;
  }

  if (isProcessing) {
    console.log("[agent] already processing, queuing message");
    return;
  }

  isProcessing = true;
  console.log(`[agent] processing message for agent ${AGENT_ID}: ${text.substring(0, 100)}...`);

  try {
    const systemPrompt = loadSystemPrompt();
    const cwd = "/workspace/agent";

    // Prepend swarm chat context if in collaborative mode
    let augmentedText = text;
    if (SWARM_CHAT_TOPIC && chatHistory.length > 0) {
      const chatContext = chatHistory
        .map((m) => `[${m.from}]: ${m.content}`)
        .join("\n");
      augmentedText = `## Collaborative Chat History\n\n${chatContext}\n\n---\n\n${text}`;
      console.log(`[agent] prepended ${chatHistory.length} chat messages to prompt`);
    }

    console.log(`[agent] starting claude query, cwd=${cwd}`);

    const configuredTools = parseAllowedTools();
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

    const result = query({
      prompt: augmentedText,
      options: {
        model: CLAUDE_MODEL,
        cwd,
        pathToClaudeCodeExecutable: "/usr/local/bin/claude",
        systemPrompt: systemPrompt || undefined,
        continue: hasSession,
        allowedTools,
        mcpServers: {
          "praktor-tasks": {
            type: "stdio",
            command: "node",
            args: ["/app/mcp-tasks.js"],
            env: { NATS_URL, AGENT_ID },
          },
          "praktor-profile": {
            type: "stdio",
            command: "node",
            args: ["/app/mcp-profile.js"],
            env: { NATS_URL, AGENT_ID },
          },
          "praktor-memory": {
            type: "stdio",
            command: "node",
            args: ["/app/mcp-memory.js"],
            env: {},
          },
          "praktor-nix": {
            type: "stdio",
            command: "node",
            args: ["/app/mcp-nix.js"],
            env: {},
          },
          ...(SWARM_CHAT_TOPIC ? {
            "praktor-swarm": {
              type: "stdio",
              command: "node",
              args: ["/app/mcp-swarm.js"],
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
    });

    // Process streaming result
    let fullResponse = "";
    const iter = result[Symbol.asyncIterator]();
    currentQueryIter = iter;
    try {
      for await (const event of { [Symbol.asyncIterator]: () => iter }) {
        console.log(`[agent] event: type=${event.type}${"subtype" in event ? ` subtype=${event.subtype}` : ""}`);
        if (event.type === "result" && event.subtype === "success") {
          fullResponse = event.result;
        } else if (event.type === "assistant") {
          for (const block of event.message.content) {
            if (block.type === "text") {
              await bridge.publishOutput(block.text, "text");
            } else if (block.type === "tool_use" || block.type === "server_tool_use") {
              console.log(`[agent] tool: ${block.name}`);
            }
          }
        }
      }
    } catch (streamErr) {
      // Claude Code native binary may exit with code 1 after streaming
      // a successful result. If we already have the result, treat it as
      // a non-fatal warning rather than a failure.
      if (fullResponse) {
        console.warn(`[agent] claude process exited with error after successful result, ignoring:`, streamErr);
      } else {
        throw streamErr;
      }
    }

    // Send final result
    if (fullResponse) {
      await bridge.publishResult(fullResponse);
    }

    hasSession = true;
    console.log(`[agent] completed processing for agent ${AGENT_ID}`);
  } catch (err) {
    if ((err as Error)?.message === "aborted") {
      console.log("[agent] query aborted by user");
      await bridge.publishResult("Aborted.");
      hasSession = true;
      return;
    }
    console.error(`[agent] error processing message:`, err);
    const errorMsg = err instanceof Error ? err.message : String(err);
    await bridge.publishResult(`Error: ${errorMsg}`);
  } finally {
    currentQueryIter = null;
    isProcessing = false;
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
      abortPending = true;
      if (currentQueryIter) {
        currentQueryIter.return?.(undefined);
        currentQueryIter = null;
      }
      // Kill any running claude processes as backstop
      try { execSync("pkill -f /usr/local/bin/claude", { timeout: 3000 }); } catch { /* ignore */ }
      isProcessing = false;
      msg.respond(new TextEncoder().encode(JSON.stringify({ status: "ok" })));
      console.log("[agent] run aborted");
      break;
    case "clear_session":
      console.log("[agent] clearing session...");
      hasSession = false;
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
