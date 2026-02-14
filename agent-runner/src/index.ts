import { query } from "@anthropic-ai/claude-agent-sdk";
import { NatsBridge } from "./nats-bridge.js";
import { readFileSync } from "fs";

const NATS_URL = process.env.NATS_URL || "nats://localhost:4222";
const GROUP_ID = process.env.GROUP_ID || "default";
const IS_MAIN = process.env.IS_MAIN === "true";
const SESSION_ID = process.env.SESSION_ID || undefined;
const CLAUDE_MODEL = process.env.CLAUDE_MODEL || undefined;

let bridge: NatsBridge;
let isProcessing = false;

async function loadSystemPrompt(): Promise<string> {
  const parts: string[] = [];

  // Load global CLAUDE.md
  try {
    const global = readFileSync("/workspace/global/CLAUDE.md", "utf-8");
    parts.push(global);
  } catch {
    // Global instructions not available
  }

  // Load group-specific CLAUDE.md
  try {
    const group = readFileSync("/workspace/group/CLAUDE.md", "utf-8");
    parts.push(group);
  } catch {
    // Group instructions not available
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
  console.log(`[agent] processing message for group ${GROUP_ID}: ${text.substring(0, 100)}...`);

  try {
    const systemPrompt = await loadSystemPrompt();
    const cwd = IS_MAIN ? "/workspace/project" : "/workspace/group";

    console.log(`[agent] starting claude query, cwd=${cwd}`);

    const result = query({
      prompt: text,
      options: {
        model: CLAUDE_MODEL,
        cwd,
        pathToClaudeCodeExecutable: "/app/node_modules/.bin/claude",
        systemPrompt: systemPrompt || undefined,
        allowedTools: [
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
        ],
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

    console.log(`[agent] completed processing for group ${GROUP_ID}`);
  } catch (err) {
    console.error(`[agent] error processing message:`, err);
    const errorMsg = err instanceof Error ? err.message : String(err);
    await bridge.publishResult(`Error: ${errorMsg}`);
  } finally {
    isProcessing = false;
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
  console.log(`[agent] starting for group ${GROUP_ID} (main: ${IS_MAIN})`);
  console.log(`[agent] NATS URL: ${NATS_URL}`);

  bridge = new NatsBridge(NATS_URL, GROUP_ID);
  await bridge.connect();

  bridge.subscribeInput(handleMessage);
  bridge.subscribeControl(handleControl);

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
