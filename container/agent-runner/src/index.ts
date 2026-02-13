import { query } from "@anthropic-ai/claude-code";
import { NatsBridge } from "./nats-bridge.js";
import { readFileSync } from "fs";

const NATS_URL = process.env.NATS_URL || "nats://localhost:4222";
const GROUP_ID = process.env.GROUP_ID || "default";
const IS_MAIN = process.env.IS_MAIN === "true";
const SESSION_ID = process.env.SESSION_ID || undefined;

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

    const result = await query({
      prompt: text,
      options: {
        cwd,
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
      },
    });

    // Process streaming result
    let fullResponse = "";
    for await (const event of result) {
      if (event.type === "text") {
        const content = (event as { type: string; content: string }).content;
        fullResponse += content;

        // Stream partial output
        await bridge.publishOutput(content, "text");
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

async function handleControl(data: Record<string, unknown>): Promise<void> {
  const command = data.command as string;
  console.log(`[agent] control command: ${command}`);

  switch (command) {
    case "shutdown":
      console.log("[agent] shutting down...");
      await bridge.close();
      process.exit(0);
      break;
    case "ping":
      await bridge.publishOutput("pong", "control");
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
