// Cheap gate for scheduled tasks. A task with a `check_command` runs that
// command in the container before any Claude query:
//   - empty stdout             → the task ends silently (no tokens, no message)
//   - stdout, empty prompt     → stdout is delivered verbatim (no Claude)
//   - stdout and a prompt      → Claude runs the prompt with stdout appended
//                                inside <check_output>
// A failing command stays silent, except that CHECK_FAILURE_ALERT consecutive
// failures produce one warning so a broken check can't go unnoticed forever.
// Failure counts are kept per task id.
// When the agent may not run shell commands (no Bash in allowed_tools) the
// check is ignored: the prompt runs as a plain task, or, if it is empty, the
// task ends silently.

import { execFile } from "child_process";
import { mkdirSync, readFileSync, rmSync, writeFileSync } from "fs";
import { join } from "path";

const CHECK_TIMEOUT_MS = 120_000;
const CHECK_MAX_OUTPUT = 1 << 20;
export const CHECK_FAILURE_ALERT = 4;

export type TaskCheckOutcome =
  | { action: "query"; prompt: string }
  | { action: "publish"; content: string };

export interface CheckResult {
  ok: boolean;
  stdout: string;
  stderr: string;
}

export type CheckRunner = (command: string, signal?: AbortSignal) => Promise<CheckResult>;

export const runShell: CheckRunner = (command, signal) =>
  new Promise((resolve) => {
    execFile(
      "bash",
      ["-c", command],
      {
        cwd: "/workspace/agent",
        timeout: CHECK_TIMEOUT_MS,
        maxBuffer: CHECK_MAX_OUTPUT,
        signal,
      },
      (err, stdout, stderr) => {
        resolve({ ok: !err, stdout: String(stdout), stderr: String(stderr || err?.message || "") });
      }
    );
  });

export interface TaskCheckOptions {
  command: string;
  prompt: string;
  taskId?: string;
  stateDir: string;
  // False when this agent may not run shell commands; the gateway rejects
  // such tasks too, this is defense in depth.
  allowed?: boolean;
  signal?: AbortSignal;
  run?: CheckRunner;
}

// Task ids are UUIDs; anything else is reduced to a safe file name.
function failureFile(stateDir: string, taskId: string | undefined): string {
  const key = (taskId || "unknown").replace(/[^A-Za-z0-9._-]/g, "_").slice(0, 64);
  return join(stateDir, `${key}.failures`);
}

export async function evaluateTaskCheck(opts: TaskCheckOptions): Promise<TaskCheckOutcome> {
  const { command, prompt, taskId, stateDir, signal } = opts;
  if (opts.allowed === false) {
    console.warn(`[task] ignoring check_command: Bash is not in this agent's allowed_tools`);
    return prompt.trim() ? { action: "query", prompt } : { action: "publish", content: "" };
  }
  const run = opts.run ?? runShell;
  const file = failureFile(stateDir, taskId);
  const res = await run(command, signal);

  if (!res.ok) {
    let failures = 1;
    try {
      failures = parseInt(readFileSync(file, "utf8"), 10) + 1 || 1;
    } catch {
      /* first failure */
    }
    try {
      mkdirSync(stateDir, { recursive: true });
      writeFileSync(file, String(failures));
    } catch {
      /* best effort */
    }
    const detail = res.stderr.trim().slice(-300);
    console.error(`[task] check failed (${failures} in a row): ${command}: ${detail}`);
    if (failures === CHECK_FAILURE_ALERT) {
      return {
        action: "publish",
        content: `⚠️ Scheduled check has failed ${failures} times in a row: ${command}\n${detail}`,
      };
    }
    return { action: "publish", content: "" };
  }

  rmSync(file, { force: true });
  const out = res.stdout.trim();
  if (!out) return { action: "publish", content: "" };
  if (!prompt.trim()) return { action: "publish", content: out };
  return { action: "query", prompt: `${prompt}\n\n<check_output>\n${out}\n</check_output>` };
}
