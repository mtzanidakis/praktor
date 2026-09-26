import { describe, it, expect, beforeEach } from "vitest";
import { mkdtempSync, readdirSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import { CHECK_FAILURE_ALERT, evaluateTaskCheck, type CheckRunner } from "../task-check.js";

const ok = (stdout: string): CheckRunner => async () => ({ ok: true, stdout, stderr: "" });
const fail: CheckRunner = async () => ({ ok: false, stdout: "", stderr: "boom" });

describe("evaluateTaskCheck", () => {
  let stateDir: string;
  beforeEach(() => {
    stateDir = mkdtempSync(join(tmpdir(), "task-check-"));
  });

  const evaluate = (over: Partial<Parameters<typeof evaluateTaskCheck>[0]>) =>
    evaluateTaskCheck({ command: "check", prompt: "body", taskId: "t-1", stateDir, ...over });

  it("stays silent when the check prints nothing", async () => {
    expect(await evaluate({ run: ok("  \n") })).toEqual({ action: "publish", content: "" });
  });

  it("delivers output verbatim when the prompt is empty", async () => {
    expect(await evaluate({ prompt: "", run: ok("⏰ Dentist\n") })).toEqual({
      action: "publish",
      content: "⏰ Dentist",
    });
  });

  it("hands output to Claude when there is a prompt", async () => {
    expect(await evaluate({ run: ok('{"uid":"1"}') })).toEqual({
      action: "query",
      prompt: 'body\n\n<check_output>\n{"uid":"1"}\n</check_output>',
    });
  });

  it("alerts once after repeated failures and resets on success", async () => {
    const outcomes = [];
    for (let i = 0; i < CHECK_FAILURE_ALERT + 1; i++) outcomes.push(await evaluate({ run: fail }));
    const alerts = outcomes.filter((o) => o.action === "publish" && o.content !== "");
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toMatchObject({ content: expect.stringContaining("failed 4 times") });

    await evaluate({ run: ok("") });
    for (let i = 0; i < CHECK_FAILURE_ALERT - 1; i++) {
      expect(await evaluate({ run: fail })).toEqual({ action: "publish", content: "" });
    }
  });

  it("counts failures per task, not per command", async () => {
    for (let i = 0; i < CHECK_FAILURE_ALERT - 1; i++) {
      await evaluate({ taskId: "task-a", run: fail });
    }
    // A different task running the same command starts from zero.
    expect(await evaluate({ taskId: "task-b", run: fail })).toEqual({
      action: "publish",
      content: "",
    });
    expect(readdirSync(stateDir).sort()).toEqual(["task-a.failures", "task-b.failures"]);
    const alert = await evaluate({ taskId: "task-a", run: fail });
    expect(alert).toMatchObject({ action: "publish", content: expect.stringContaining("⚠️") });
  });

  it("keeps a task id safe to use as a file name", async () => {
    await evaluate({ taskId: "../../etc/passwd", run: fail });
    expect(readdirSync(stateDir)).toEqual([".._.._etc_passwd.failures"]);
  });

  it("passes the abort signal to the runner", async () => {
    const controller = new AbortController();
    let seen: AbortSignal | undefined;
    const run: CheckRunner = async (_cmd, signal) => {
      seen = signal;
      return { ok: true, stdout: "out", stderr: "" };
    };
    await evaluate({ prompt: "", signal: controller.signal, run });
    expect(seen).toBe(controller.signal);
  });

  describe("when the agent may not run shell commands", () => {
    const neverRun: CheckRunner = async () => {
      throw new Error("check must not run");
    };

    it("runs the prompt without the check", async () => {
      expect(await evaluate({ allowed: false, run: neverRun })).toEqual({
        action: "query",
        prompt: "body",
      });
    });

    it("ends silently when the prompt is empty", async () => {
      expect(await evaluate({ allowed: false, prompt: " ", run: neverRun })).toEqual({
        action: "publish",
        content: "",
      });
    });
  });
});
