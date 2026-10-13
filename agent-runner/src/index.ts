import { query, startup, SYSTEM_PROMPT_DYNAMIC_BOUNDARY, type McpServerConfig, type WarmQuery } from "@anthropic-ai/claude-agent-sdk";
import { NatsBridge } from "./nats-bridge.js";
import { applyExtensions } from "./extensions.js";
import { evaluateTaskCheck } from "./task-check.js";
import { SessionRun, backgroundCeilingMs, type SessionRunHooks, type StartQuery, type UserTurn } from "./session-run.js";
import { readFileSync, readdirSync, mkdirSync, writeFileSync, rmSync, symlinkSync, existsSync, lstatSync, readlinkSync, unlinkSync } from "fs";
import { join } from "path";
import { execSync } from "child_process";
import { pathToFileURL } from "url";
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
const MAX_TURNS = parseInt(process.env.MAX_TURNS || "200", 10);
const SWARM_CHAT_TOPIC = process.env.SWARM_CHAT_TOPIC || "";
const SWARM_ROLE = process.env.SWARM_ROLE || "";
// agent-browser is driven through its typed MCP server (v0.28.0+).
// AGENT_BROWSER_MCP selects the tool profile passed to `agent-browser mcp
// --tools` (default "core"; composable, e.g. "core,network,react").
// See `agent-browser mcp --help`.
const AGENT_BROWSER_INSTALLED = existsSync("/usr/local/bin/agent-browser");
const AGENT_BROWSER_MCP_TOOLS = (process.env.AGENT_BROWSER_MCP || "core").trim() || "core";
// Claude Code (2.1.275+) syncs the skills and plugins enabled on the claude.ai
// account behind CLAUDE_CODE_OAUTH_TOKEN into every session. Agents get their
// skills and plugins from per-agent extensions only, so opt out of both.
const CLAUDE_SETTINGS = { syncClaudeAiSkills: false, syncClaudeAiPlugins: false };

let bridge: NatsBridge;
let isProcessing = false;
let lastSessionId: string | undefined;
// The Claude Code process of the conversational session. It outlives the
// turn's result while background work runs (see session-run.ts), and new
// messages are pushed into it meanwhile.
let activeRun: SessionRun | null = null;
let aborted = false;
// How long a run may outlive its last result waiting for background work.
const BACKGROUND_CEILING_MS = backgroundCeilingMs(process.env.CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS);
// Live background tasks per run, from the SDK's background_tasks_changed level
// signal. Keyed by run so counts never leak across runs — the entry is deleted
// when the run ends.
export const backgroundTasksByQuery = new Map<string, number>();
let taskKeyCounter = 0;
export function setBg(key: string, n: number): void {
  if (n > 0) backgroundTasksByQuery.set(key, n);
  else backgroundTasksByQuery.delete(key);
}
export function totalBgTasks(): number {
  let total = 0;
  for (const v of backgroundTasksByQuery.values()) total += v;
  return total;
}
let extensionMcpServers: Record<string, McpServerConfig> = {};
const pendingMessages: Array<Record<string, unknown>> = [];

// Pre-warmed subprocess for the next regular message, so the CLI spawn +
// initialize handshake does not add latency to the first assistant token.
// Not used for the swarm collaborative path (short-lived) or parallel
// scheduled tasks (multiple concurrent subprocesses).
let warmHandle: WarmQuery | null = null;
let warmForSessionId: string | undefined;
let warming = false;
function rewarm(): void {
  // Never warm while a run is alive: the spare would resume a session the
  // live process is still writing to.
  if (SWARM_CHAT_TOPIC || activeRun) return;
  const target = lastSessionId;
  if ((warmHandle || warming) && warmForSessionId === target) return;
  const prev = warmHandle;
  warmHandle = null;
  if (prev) { try { prev.close(); } catch { /* ignore */ } }
  warmForSessionId = target;
  warming = true;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  startup({ options: buildRunOptions(target) as any })
    .then((w) => {
      // Discard if the session changed while warming, or a run started
      // meanwhile (it resumes the session itself).
      if (warmForSessionId !== target || activeRun || warmHandle) { try { w.close(); } catch { /* ignore */ } return; }
      warmHandle = w;
    })
    .catch((err) => {
      console.warn("[agent] pre-warm failed:", err instanceof Error ? err.message : err);
    })
    .finally(() => {
      if (warmForSessionId === target) warming = false;
    });
}

// Parallel task execution
const MAX_PARALLEL_TASKS = parseInt(process.env.MAX_PARALLEL_TASKS || "3", 10);
let activeTaskCount = 0;
// Running scheduled-task runs, so /stop kills them (and their background work).
const activeTaskRuns = new Set<SessionRun>();
// Running task check commands, so /stop kills them too.
const activeChecks = new Set<AbortController>();
// A check command is shell execution: agents whose allowed_tools exclude Bash
// must not get one even if a check_command reaches them (defense in depth; the
// gateway refuses to store one in the first place).
const CHECKS_ALLOWED = (() => {
  const tools = parseAllowedTools(ALLOWED_TOOLS_ENV);
  return !tools || tools.includes("Bash");
})();
const TASK_CHECK_STATE_DIR = "/workspace/agent/.state/task-checks";
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
  // agent-browser is driven via its typed MCP server, so no usage-guide skill
  // is injected — only the config symlink (chromium path) is needed.
  const configSource = "/usr/local/share/agent-browser/config.json";
  if (!AGENT_BROWSER_INSTALLED) return;

  try {
    const skillsDir = "/home/praktor/.claude/skills";
    mkdirSync(skillsDir, { recursive: true });

    // Remove stale skill symlinks from previous image versions (the old
    // playwright-cli / agent-browser links, and the `core` usage-guide skill
    // that earlier versions injected before the switch to the MCP server).
    for (const [name, target] of [
      ["playwright-cli", "/opt/playwright-cli/skill"],
      ["agent-browser", "/usr/local/share/agent-browser/skills/agent-browser"],
      ["core", "/usr/local/share/agent-browser/skills/core"],
    ] as const) {
      const staleLink = join(skillsDir, name);
      try {
        if (lstatSync(staleLink).isSymbolicLink() && readlinkSync(staleLink) === target) {
          unlinkSync(staleLink);
          console.log(`[agent] removed stale ${name} skill symlink`);
        }
      } catch { /* doesn't exist */ }
    }

    // Force-update config symlink
    const configDir = "/home/praktor/.agent-browser";
    mkdirSync(configDir, { recursive: true });
    const configLink = join(configDir, "config.json");
    try { unlinkSync(configLink); } catch { /* doesn't exist */ }
    symlinkSync(configSource, configLink);

    console.log("[agent] agent-browser configured (MCP)");
  } catch (err) {
    console.warn("[agent] could not configure agent-browser:", err);
  }
}

function setupAgentMail(): void {
  const skillsDir = "/home/praktor/.claude/skills";

  // agentmail-cli 1.x ships one generated skill per API resource, so the
  // image holds a directory of them instead of a single SKILL.md. Drop the
  // symlink the pre-1.x images left behind on the home volume.
  try { unlinkSync(join(skillsDir, "agentmail-cli")); } catch { /* not present */ }

  const skillSource = "/opt/agentmail-skills";
  if (!process.env.AGENTMAIL_API_KEY || !existsSync(skillSource)) return;

  try {
    mkdirSync(skillsDir, { recursive: true });

    const names = readdirSync(skillSource, { withFileTypes: true })
      .filter((e) => e.isDirectory())
      .map((e) => e.name);

    for (const name of names) {
      const skillLink = join(skillsDir, name);
      try { unlinkSync(skillLink); } catch { /* doesn't exist */ }
      symlinkSync(join(skillSource, name), skillLink);
    }

    console.log(`[agent] agentmail skills configured (${names.join(", ")})`);
  } catch (err) {
    console.warn("[agent] could not configure agentmail skills:", err);
  }
}

const SYSTEM_PROMPT_SEPARATOR = "\n\n---\n\n";

// Joins the system prompt into cache-aware blocks. Everything before
// SYSTEM_PROMPT_DYNAMIC_BOUNDARY is eligible for cross-session prompt caching
// (fresh-session scheduled tasks reuse it); the dynamic suffix holds what
// changes within a session, so updating it doesn't invalidate the prefix.
export function assembleSystemPrompt(staticParts: string[], dynamicParts: string[]): string[] {
  const blocks = [staticParts.join(SYSTEM_PROMPT_SEPARATOR)];
  if (dynamicParts.length > 0) {
    blocks.push(SYSTEM_PROMPT_DYNAMIC_BOUNDARY, dynamicParts.join(SYSTEM_PROMPT_SEPARATOR));
  }
  return blocks;
}

function loadSystemPrompt(includeIdentity = true): string[] {
  const parts: string[] = [];
  // Parts that change mid-session (stored memory keys) go after the cache
  // boundary.
  const dynamicParts: string[] = [];

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

  // Background work: follow-up turns are delivered like any other reply
  const ceilingMinutes = Math.round(BACKGROUND_CEILING_MS / 60000);
  parts.push(
    "BACKGROUND TASKS — Bash commands run with run_in_background and background subagents keep running after your reply.\n" +
    "- When one finishes you get a new turn; your text reply in that turn is sent to the user as a new Telegram message, so report the outcome briefly.\n" +
    "- The user can keep messaging you while background work runs.\n" +
    (ceilingMinutes > 0
      ? `- Background work still running ${ceilingMinutes} minutes after your last reply is stopped. Do not start long-lived servers or watchers in the background.`
      : "- Do not start long-lived servers or watchers in the background.")
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
    "- NEVER include secrets, tokens, API keys, passwords, or credentials in emails. The same redaction rules apply to email as to Telegram.\n" +
    "- If the user asks for a secret value, respond with [REDACTED] in place of the value and explain that secrets cannot be disclosed.\n" +
    "- You may confirm that a secret or env var EXISTS, but must NEVER show its value — always use [REDACTED] as placeholder."
  );

  // Conversation history: instruct agent to search when context is missing
  parts.push(
    "CONVERSATION HISTORY — You have access to the full conversation history via the history_search MCP tool.\n" +
    "- When the user references a previous conversation, topic, or decision you don't have in your current context, ALWAYS search history before saying you don't know.\n" +
    "- Use history_search with relevant keywords from the user's message to find past discussions.\n" +
    "- This is especially important after a session restart — your conversation memory resets but the full history is preserved and searchable.\n" +
    "- NEVER say you don't have information or context without searching history first."
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
    dynamicParts.push(memorySection);
  } catch (err) {
    console.warn("[agent] could not load memory keys:", err);
  }

  // agent-browser: inform agent it's pre-installed (system chromium) and
  // exposed through its typed MCP tools.
  if (AGENT_BROWSER_INSTALLED) {
    parts.push(
      "AGENT-BROWSER — Pre-installed, exposed as typed MCP tools. Do NOT install browsers via npm, npx, nix, or any other method.\n" +
      "- Browser automation is available as `mcp__agent-browser__agent_browser_*` tools (configured to use the system Chromium).\n" +
      "- Use `agent_browser_open` to start a session, then `agent_browser_snapshot` to see the page.\n" +
      "- When re-checking a page you already looked at (polling, scheduled checks), pass `delta: true` to `agent_browser_snapshot` to get only what changed, and `ifChanged: true` to `agent_browser_screenshot` to skip unchanged captures.\n" +
      "- Do NOT call the `agent-browser` CLI via Bash — use the MCP tools instead.\n" +
      "- The browser persists across messages. Reuse the existing session.\n" +
      "- When executing a scheduled task, ALWAYS call `agent_browser_close` when done to free resources."
    );
  }

  // AgentMail: inbox-locked restrictions when configured
  if (process.env.AGENTMAIL_API_KEY && process.env.AGENTMAIL_INBOX_ID) {
    parts.push(
      "AGENTMAIL — MANDATORY RULES:\n" +
      `- Your inbox ID is: ${process.env.AGENTMAIL_INBOX_ID}. You MUST use ONLY this inbox ID for ALL agentmail operations.\n` +
      "- NEVER use, access, list, or reference any other inbox ID, even if the user asks.\n" +
      "- NEVER create new inboxes.\n" +
      "- NEVER use pods, webhooks, or domains commands. These are admin-only operations.\n" +
      "- NEVER include secrets, tokens, API keys, passwords, or credentials in emails.\n" +
      "- The same secret redaction rules that apply to Telegram apply to email — use [REDACTED] for any secret values.\n" +
      "- EMAIL FORMATTING: Emails are NOT Telegram messages. Do NOT use Telegram Markdown formatting or escape characters in emails. " +
      "Write emails in plain text with natural punctuation. No backslash escaping, no *bold*, no `code` — just normal text."
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

  return assembleSystemPrompt(parts, dynamicParts);
}

export function inferTerminalReason(errorMsg: string): string | undefined {
  if (/maximum number of turns/i.test(errorMsg)) return "max_turns";
  if (/blocking.*limit/i.test(errorMsg)) return "blocking_limit";
  if (/abort/i.test(errorMsg)) return "aborted_tools";
  return undefined;
}

// Prompts assembled from text the user did not type (inbound email) are
// delivered verbatim — no `@path` expansion or slash-command dispatch — per
// message, via UserTurn.verbatim (the SDK's client_composed flag, which is
// what the verbatimPrompts option sets on every message). Trade-off: the CLI
// also skips its turn-start attachments (skill listings, nested CLAUDE.md)
// until the turn's first tool call.
function buildRunOptions(sessionId?: string) {
  const systemPrompt = loadSystemPrompt();
  const cwd = "/workspace/agent";
  const tools = parseAllowedTools(ALLOWED_TOOLS_ENV);
  // If tools are restricted, make sure the agent-browser MCP tools are
  // allowlisted (the praktor-* wildcard wouldn't cover them).
  if (tools && AGENT_BROWSER_INSTALLED && !tools.includes("mcp__agent-browser__*")) {
    tools.push("mcp__agent-browser__*");
  }

  // Annotated const so contextual typing narrows `type: "stdio"` on each
  // entry to the literal "stdio" expected by McpStdioServerConfig (a plain
  // inline literal would widen to `string` and fail to match the SDK type).
  const mcpServers: Record<string, McpServerConfig> = {
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
    ...extensionMcpServers,
  };
  if (SWARM_CHAT_TOPIC) {
    mcpServers["praktor-swarm"] = {
      type: "stdio",
      command: "node",
      args: ["/app/mcp-swarm.mjs"],
      env: { NATS_URL, AGENT_ID, SWARM_CHAT_TOPIC },
    };
  }
  // agent-browser typed MCP server (v0.28.0+). Tools surface as
  // mcp__agent-browser__*; profile selected by AGENT_BROWSER_MCP (default core).
  if (AGENT_BROWSER_INSTALLED) {
    mcpServers["agent-browser"] = {
      type: "stdio",
      command: "/usr/local/bin/agent-browser",
      args: ["mcp", "--tools", AGENT_BROWSER_MCP_TOOLS],
      env: {},
    };
  }

  return {
      model: CLAUDE_MODEL,
      cwd,
      pathToClaudeCodeExecutable: "/usr/local/bin/claude",
      // snapshot: false — the SDK records the system prompt on a conversation's
      // first request and replays it on every resume by default. This prompt is
      // rebuilt per message (stored memory keys, USER.md, installed skills), so
      // recording it would freeze that list for the life of the session.
      systemPrompt: { type: "custom" as const, prompt: systemPrompt, snapshot: false },
      ...(sessionId ? { resume: sessionId } : {}),
      ...(tools ? { tools } : {}),
      maxTurns: MAX_TURNS,
      mcpServers,
      settings: CLAUDE_SETTINGS,
      permissionMode: "bypassPermissions" as const,
      allowDangerouslySkipPermissions: true,
      stderr: (data: string) => {
        console.error(`[claude-stderr] ${data.trimEnd()}`);
      },
  };
}

// Execute a scheduled task in parallel (fresh session, no resume)
interface TaskResponseSignals {
  result: string;
  hasStreamedOutput: boolean;
  hasFileSent: boolean;
}

interface TaskResponseDecision {
  content: string;
  warn: boolean;
}

// Decides what to publish as a task's final result message. Silent
// completions publish empty content (the gateway drops it before Telegram)
// and only surface as an internal warn log — the user-facing marker was
// noisy and unhelpful.
export function decideTaskFinalResponse(
  signals: TaskResponseSignals
): TaskResponseDecision {
  if (signals.result) {
    return { content: signals.result, warn: false };
  }
  if (signals.hasStreamedOutput) {
    return { content: "[response was streamed]", warn: false };
  }
  if (signals.hasFileSent) {
    return { content: "", warn: false };
  }
  return { content: "", warn: true };
}

const FILE_SEND_TOOL = "mcp__praktor-file__file_send";

// Tracks file_send calls in a task's event stream. A file counts as sent only
// when the call's tool_result comes back without an error, so a rejected send
// (missing file, over the size limit) isn't reported as a reply.
export class FileSendTracker {
  private pending = new Set<string>();
  sent = false;

  observe(event: unknown): void {
    const e = event as { type?: string; message?: { content?: unknown } };
    const blocks = e.message?.content;
    if (!Array.isArray(blocks)) return;
    for (const block of blocks as Array<Record<string, unknown>>) {
      if (e.type === "assistant" && block.type === "tool_use" && block.name === FILE_SEND_TOOL) {
        this.pending.add(block.id as string);
      } else if (e.type === "user" && block.type === "tool_result" && this.pending.has(block.tool_use_id as string)) {
        this.pending.delete(block.tool_use_id as string);
        if (!block.is_error) this.sent = true;
      }
    }
  }
}

function logTools(prefix: string, event: unknown): void {
  const e = event as { type?: string; parent_tool_use_id?: string | null; message?: { content?: unknown } };
  if (e.type !== "assistant" || e.parent_tool_use_id || !Array.isArray(e.message?.content)) return;
  for (const block of e.message.content as Array<{ type?: string; name?: string }>) {
    if (block.type === "tool_use" || block.type === "server_tool_use") {
      console.log(`${prefix} tool: ${block.name}`);
    }
  }
}

// backgroundTimeoutNotice is published when a run is killed with background
// work still running after the ceiling, so the user knows the job is gone.
async function publishBackgroundTimeout(prefix: string, msgId: string | undefined, background: number): Promise<void> {
  const minutes = Math.round(BACKGROUND_CEILING_MS / 60000);
  if (background === 0) {
    console.warn(`${prefix} run still alive ${minutes} min after its last result, stopping it`);
    return;
  }
  console.warn(`${prefix} ${background} background task(s) still running ${minutes} min after the last result, stopping them`);
  await bridge.publishResult(
    `${background} background task(s) were still running ${minutes} min after my last reply and were stopped.`,
    msgId,
    "background_timeout",
  );
}

async function executeTask(data: Record<string, unknown>): Promise<void> {
  const text = data.text as string;
  const msgId = data.msg_id as string | undefined;
  const taskId = data.task_id as string | undefined;
  const checkCommand = (data.check_command as string | undefined) || "";
  const bgKey = msgId ?? `__task-${++taskKeyCounter}`;
  console.log(`[task] executing parallel task: ${text.substring(0, 100)}...`);

  const fileSends = new FileSendTracker();
  let fileSentReported = false;
  let delivered = false;
  let run: SessionRun | undefined;

  try {
    let prompt = text;
    if (checkCommand) {
      const controller = new AbortController();
      activeChecks.add(controller);
      let gate;
      try {
        gate = await evaluateTaskCheck({
          command: checkCommand,
          prompt: text,
          taskId,
          stateDir: TASK_CHECK_STATE_DIR,
          allowed: CHECKS_ALLOWED,
          signal: controller.signal,
        });
      } finally {
        activeChecks.delete(controller);
      }
      if (gate.action === "publish") {
        if (!aborted) await bridge.publishResult(gate.content, msgId);
        console.log(`[task] check: ${gate.content ? "delivered output" : "nothing to do"}`);
        return;
      }
      prompt = gate.prompt;
    }

    // Fresh session per task. Each turn's result (the task's reply, then any
    // follow-up after a background job completes) is published as it arrives.
    const hooks: SessionRunHooks = {
      onEvent: (event) => {
        fileSends.observe(event);
        logTools("[task]", event);
      },
      onText: (blockText, id) => bridge.publishOutput(blockText, "text", id),
      onBackgroundChange: (n) => setBg(bgKey, n),
      onTurnEnd: async (outcome) => {
        // A file counts toward the turn it was sent in.
        const fileSent = fileSends.sent && !fileSentReported;
        if (fileSent) fileSentReported = true;
        const decision = decideTaskFinalResponse({
          result: outcome.result,
          hasStreamedOutput: outcome.streamed,
          hasFileSent: fileSent,
        });
        if (decision.warn && !outcome.followUp) {
          console.warn(`[task] completed with no output (msg_id=${msgId}, terminal=${outcome.terminalReason ?? "none"})`);
        }
        await bridge.publishResult(decision.content, msgId, outcome.terminalReason, fileSent, outcome.background);
        delivered = true;
        const label = outcome.followUp ? "follow-up turn completed" : "completed";
        const extra = [
          outcome.terminalReason && outcome.terminalReason !== "completed" ? `terminal_reason: ${outcome.terminalReason}` : "",
          outcome.background ? "background work still running" : "",
        ].filter(Boolean).join(", ");
        console.log(`[task] ${label}${extra ? ` (${extra})` : ""}`);
      },
      onCeiling: (id, background) => publishBackgroundTimeout("[task]", id ?? msgId, background),
    };
    const taskRun = new SessionRun(
      (input) => query({ prompt: input, options: buildRunOptions() }),
      hooks,
      BACKGROUND_CEILING_MS,
    );
    run = taskRun;
    activeTaskRuns.add(taskRun);
    taskRun.push({ text: prompt, msgId });

    try {
      await taskRun.run();
    } catch (streamErr) {
      if (taskRun.killed) throw streamErr;
      if (delivered || taskRun.currentStreamed || fileSends.sent) {
        console.warn(`[task] claude process exited with error after output, ignoring:`, streamErr);
      } else {
        throw streamErr;
      }
    }

    if (!delivered && !taskRun.killed) {
      // The process ended without a result for the task's turn.
      const fileSent = fileSends.sent && !fileSentReported;
      const decision = decideTaskFinalResponse({
        result: "",
        hasStreamedOutput: taskRun.currentStreamed,
        hasFileSent: fileSent,
      });
      if (decision.warn) {
        console.warn(`[task] completed with no output (msg_id=${msgId}, terminal=none)`);
      }
      await bridge.publishResult(decision.content, msgId, undefined, fileSent);
    }
  } catch (err) {
    if (aborted || run?.killed) {
      console.log("[task] aborted");
      return;
    }
    const errorMsg = err instanceof Error ? err.message : String(err);
    console.error(`[task] error:`, err);
    await bridge.publishResult(`Error: ${errorMsg}`, msgId, inferTerminalReason(errorMsg));
  } finally {
    if (run) activeTaskRuns.delete(run);
    backgroundTasksByQuery.delete(bgKey);
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

// isRunnableMessage reports whether an input message has work to do. A
// message needs text, except a check-only scheduled task: it has no prompt,
// and its check_command is the work.
export function isRunnableMessage(data: Record<string, unknown>): boolean {
  if (data.text) return true;
  return data.sender === "scheduler" && !!data.check_command;
}

async function handleMessage(data: Record<string, unknown>): Promise<void> {
  if (!isRunnableMessage(data)) return;

  const sender = data.sender as string | undefined;

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

  // Regular messages: sequential with session continuity. A turn in progress,
  // or a run that is only finishing up, queues the message.
  if (isProcessing || (activeRun && !activeRun.acceptsInput)) {
    pendingMessages.push(data);
    console.log(`[agent] already processing, queued message (${pendingMessages.length} pending)`);
    return;
  }

  isProcessing = true;
  aborted = false;
  const turn = userTurn(data);
  console.log(`[agent] processing message for agent ${AGENT_ID}: ${turn.text.substring(0, 100)}...`);

  if (activeRun) {
    // The previous turn's background work is still running in this
    // session's process: hand the message to it instead of starting a second
    // process on the same session.
    console.log(`[agent] background work still running, sending message to the live session`);
    activeRun.push(turn);
    return;
  }

  startRun(turn);
}

const REGULAR_BG_KEY = "__regular";

// userTurn builds the turn for a regular message.
function userTurn(data: Record<string, unknown>): UserTurn {
  let text = (data.text as string | undefined) ?? "";
  // Prepend swarm chat context if in collaborative mode
  if (SWARM_CHAT_TOPIC && chatHistory.length > 0) {
    const chatContext = chatHistory
      .map((m) => `[${m.from}]: ${m.content}`)
      .join("\n");
    text = `## Collaborative Chat History\n\n${chatContext}\n\n---\n\n${text}`;
    console.log(`[agent] prepended ${chatHistory.length} chat messages to prompt`);
  }
  return {
    text,
    msgId: data.msg_id as string | undefined,
    // Inbound email carries text from arbitrary senders: deliver it verbatim.
    verbatim: data.sender === "agentmail",
  };
}

// startRun starts a Claude Code process for the conversational session and
// sends it the turn. The process lives until no turn is pending and no
// background work is running.
function startRun(turn: UserTurn): void {
  // Use the pre-warmed subprocess if available and fresh for the current
  // session; otherwise spawn a new one. The warm path skips the CLI
  // spawn + initialize handshake latency on the first token.
  const start: StartQuery = (input) => {
    if (warmHandle && warmForSessionId === lastSessionId && !SWARM_CHAT_TOPIC) {
      console.log(`[agent] starting claude query (warm)`);
      const handle = warmHandle;
      warmHandle = null;
      try {
        return handle.query(input);
      } catch (warmErr) {
        console.warn("[agent] warm query failed, falling back:", warmErr instanceof Error ? warmErr.message : warmErr);
        try { handle.close(); } catch { /* ignore */ }
      }
    }
    console.log(`[agent] starting claude query`);
    return query({ prompt: input, options: buildRunOptions(lastSessionId) });
  };

  const hooks: SessionRunHooks = {
    onEvent: (event) => {
      console.log(`[agent] event: type=${event.type}${"subtype" in event ? ` subtype=${event.subtype}` : ""}`);
      logTools("[agent]", event);
    },
    onText: (text, msgId) => bridge.publishOutput(text, "text", msgId),
    onBackgroundChange: (n) => setBg(REGULAR_BG_KEY, n),
    onTurnEnd: async (outcome) => {
      if (outcome.sessionId) lastSessionId = outcome.sessionId;
      let content = outcome.result;
      if (!content && outcome.streamed) content = "[response was streamed]";
      if (content || outcome.terminalReason) {
        await bridge.publishResult(content, outcome.msgId, outcome.terminalReason, false, outcome.background);
      } else {
        // Interactive path: keep silence (user might have just sent "thanks"),
        // but log so silent failures are visible in container logs.
        console.warn(`[agent] completed with no output (msg_id=${outcome.msgId})`);
      }
      const extra = [
        outcome.terminalReason && outcome.terminalReason !== "completed" ? `terminal_reason=${outcome.terminalReason}` : "",
        outcome.background ? `background_tasks=${run.background}` : "",
      ].filter(Boolean).join(", ");
      console.log(`[agent] ${outcome.followUp ? "follow-up turn completed" : "completed processing"} for agent ${AGENT_ID} (session=${lastSessionId}${extra ? `, ${extra}` : ""})`);

      isProcessing = run.busy;
      // While background work keeps the process alive, queued messages go to
      // it. Otherwise the process is about to exit and the next message starts
      // a fresh one (with a freshly built system prompt).
      if (!run.busy && run.background > 0 && pendingMessages.length > 0) {
        const next = pendingMessages.shift()!;
        console.log(`[agent] dequeuing next message into the live session (${pendingMessages.length} remaining)`);
        isProcessing = true;
        run.push(userTurn(next));
      }
    },
    onCeiling: (msgId, background) => publishBackgroundTimeout("[agent]", msgId, background),
  };

  const run = new SessionRun(start, hooks, BACKGROUND_CEILING_MS);
  activeRun = run;
  run.push(turn);

  run.run()
    .catch(async (err) => {
      if (run.killed || aborted) {
        console.log("[agent] query aborted by user");
        return;
      }
      const pending = run.pendingMsgIds;
      if (pending.length === 0) {
        console.warn(`[agent] claude process exited with error after output, ignoring:`, err);
        return;
      }
      const errorMsg = err instanceof Error ? err.message : String(err);
      console.error(`[agent] error processing message:`, err);
      for (const [i, msgId] of pending.entries()) {
        if (i === 0 && run.currentStreamed) {
          await bridge.publishResult("[response was streamed]", msgId);
        } else {
          await bridge.publishResult(`Error: ${errorMsg}`, msgId, inferTerminalReason(errorMsg));
        }
      }
    })
    .catch((err) => console.error("[agent] failed to report run error:", err))
    .finally(() => {
      if (activeRun !== run) return; // aborted; a newer run may be active
      activeRun = null;
      isProcessing = false;
      backgroundTasksByQuery.delete(REGULAR_BG_KEY);

      // Pre-warm the subprocess for the next regular message.
      rewarm();

      // Process next queued message if any
      if (pendingMessages.length > 0) {
        const next = pendingMessages.shift()!;
        console.log(`[agent] dequeuing next message (${pendingMessages.length} remaining)`);
        handleMessage(next);
      }
    });
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
        systemPrompt,
        tools: [],
        settings: CLAUDE_SETTINGS,
        // The routing prompt embeds the user's message; never expand `@path`
        // mentions or dispatch slash commands from it.
        verbatimPrompts: true,
        permissionMode: "bypassPermissions" as const,
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
      if (warmHandle) { try { warmHandle.close(); } catch { /* ignore */ } warmHandle = null; }
      await bridge.close();
      process.exit(0);
      break;
    case "ping":
      msg.respond(new TextEncoder().encode(JSON.stringify({
        status: "ok",
        processing: isProcessing,
        pending_messages: pendingMessages.length,
        active_tasks: activeTaskCount,
        background_tasks: totalBgTasks(),
      })));
      break;
    case "abort":
      console.log("[agent] aborting current run...");
      aborted = true;
      // Kill the conversational process, background work included.
      if (activeRun) {
        activeRun.kill();
        activeRun = null;
      }
      // Abort running task check commands
      for (const controller of activeChecks) {
        controller.abort();
      }
      activeChecks.clear();
      // Kill all parallel task runs. Each one's finally releases its slot
      // (activeTaskCount) once its process is gone.
      for (const run of activeTaskRuns) {
        run.kill();
      }
      activeTaskRuns.clear();
      backgroundTasksByQuery.clear();
      // Discard the pre-warmed subprocess; it will be recreated below.
      if (warmHandle) { try { warmHandle.close(); } catch { /* ignore */ } warmHandle = null; }
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
      // Re-warm for the next message.
      rewarm();
      break;
    case "clear_session":
      console.log("[agent] clearing session...");
      lastSessionId = undefined;
      // A process kept alive only by background work belongs to the old
      // session: its follow-up turns would land in the transcript being
      // deleted. Stop it (a turn in progress is left to finish, as before).
      if (activeRun && !activeRun.busy) {
        console.log("[agent] stopping background work of the old session");
        activeRun.kill();
        activeRun = null;
        backgroundTasksByQuery.delete(REGULAR_BG_KEY);
      }
      // The warm handle was prepared for the old session; discard it.
      if (warmHandle) { try { warmHandle.close(); } catch { /* ignore */ } warmHandle = null; }
      for (const dir of [
        "/home/praktor/.claude/projects",
        "/home/praktor/.claude/sessions",
        "/home/praktor/.claude/debug",
        "/home/praktor/.claude/todos",
      ]) {
        try { rmSync(dir, { recursive: true, force: true }); } catch { /* ignore */ }
      }
      rewarm();
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
  setupAgentMail();

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

  // Pre-warm a claude subprocess so the first message skips the spawn +
  // initialize handshake latency. Fire-and-forget; falls back transparently.
  rewarm();

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

// Only auto-run main() when this file is the process entrypoint — not when
// it's imported by vitest. Otherwise main()'s NATS connect fails in test
// environments and process.exit(1) bubbles up as an unhandled rejection.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((err) => {
    console.error("[agent] fatal error:", err);
    process.exit(1);
  });
}
