import { query } from "@anthropic-ai/claude-agent-sdk";
import { NatsBridge } from "./nats-bridge.js";
import { readFileSync, mkdirSync, writeFileSync, rmSync } from "fs";

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

let bridge: NatsBridge;
let isProcessing = false;
let hasSession = false;

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

function loadSystemPrompt(): string {
  const parts: string[] = [];

  // Include global instructions in system prompt as well (belt and suspenders)
  try {
    const global = readFileSync("/workspace/global/CLAUDE.md", "utf-8");
    parts.push(global);
  } catch {
    // Global instructions not available
  }

  return parts.join("\n\n---\n\n");
}

async function handleMessage(data: Record<string, unknown>): Promise<void> {
  const text = data.text as string;
  if (!text) return;

  if (isProcessing) {
    console.log("[agent] already processing, queuing message");
    return;
  }

  isProcessing = true;
  console.log(`[agent] processing message for agent ${AGENT_ID}: ${text.substring(0, 100)}...`);

  try {
    const systemPrompt = loadSystemPrompt();
    const cwd = "/workspace/agent";

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
    ];

    const result = query({
      prompt: text,
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
            args: ["/app/mcp-server.js"],
            env: {
              NATS_URL,
              AGENT_ID,
            },
          },
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
    try {
      for await (const event of result) {
        console.log(`[agent] event: type=${event.type}${"subtype" in event ? ` subtype=${event.subtype}` : ""}`);
        if (event.type === "result" && event.subtype === "success") {
          fullResponse = event.result;
        } else if (event.type === "assistant") {
          // Extract text blocks from assistant messages
          for (const block of event.message.content) {
            if (block.type === "text") {
              await bridge.publishOutput(block.text, "text");
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
    console.error(`[agent] error processing message:`, err);
    const errorMsg = err instanceof Error ? err.message : String(err);
    await bridge.publishResult(`Error: ${errorMsg}`);
  } finally {
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

  console.log(`[agent] routing query: ${text.substring(0, 100)}...`);

  try {
    const systemPrompt = loadSystemPrompt();
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
  }
}

async function main(): Promise<void> {
  console.log(`[agent] starting for agent ${AGENT_ID}`);
  console.log(`[agent] NATS URL: ${NATS_URL}`);

  installGlobalInstructions();

  // Clean up Claude Code internal files that accumulate over time
  for (const dir of ["/home/praktor/.claude/debug", "/home/praktor/.claude/todos"]) {
    try { rmSync(dir, { recursive: true, force: true }); } catch { /* ignore */ }
  }

  bridge = new NatsBridge(NATS_URL, AGENT_ID);
  await bridge.connect();

  bridge.subscribeInput(handleMessage);
  bridge.subscribeControl(handleControl);
  bridge.subscribeRoute(handleRoute);

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
