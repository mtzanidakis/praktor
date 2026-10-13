import { describe, it, expect, vi, afterEach } from "vitest";
import type { SDKMessage, SDKUserMessage } from "@anthropic-ai/claude-agent-sdk";
import {
  InputQueue,
  SessionRun,
  backgroundCeilingMs,
  buildUserMessage,
  liveBackgroundTasks,
  type SessionRunHooks,
  type TurnOutcome,
} from "../session-run.js";

// FakeQuery stands in for the SDK Query: it consumes the run's input like the
// CLI's stdin and yields whatever events the test emits.
class FakeQuery {
  received: SDKUserMessage[] = [];
  inputEnded = false;
  closed = false;
  private queue: Array<IteratorResult<SDKMessage> | Error> = [];
  private wake: (() => void) | null = null;

  constructor(input: AsyncIterable<SDKUserMessage>) {
    void (async () => {
      for await (const m of input) this.received.push(m);
      this.inputEnded = true;
    })();
  }

  private put(item: IteratorResult<SDKMessage> | Error): void {
    this.queue.push(item);
    const w = this.wake;
    this.wake = null;
    w?.();
  }

  emit(event: Record<string, unknown>): void {
    this.put({ value: event as unknown as SDKMessage, done: false });
  }

  end(): void {
    this.put({ value: undefined, done: true });
  }

  fail(err: Error): void {
    this.put(err);
  }

  close(): void {
    this.closed = true;
    this.end();
  }

  [Symbol.asyncIterator](): AsyncIterator<SDKMessage> {
    return {
      next: async () => {
        while (this.queue.length === 0) {
          await new Promise<void>((resolve) => { this.wake = resolve; });
        }
        const item = this.queue.shift()!;
        if (item instanceof Error) throw item;
        return item;
      },
    };
  }
}

const tick = async (n = 5) => {
  for (let i = 0; i < n; i++) await new Promise<void>((r) => setImmediate(r));
};

const uuidOf = (m: SDKUserMessage) => m.uuid as string;
const result = (text: string, uuids: string[] = [], extra: Record<string, unknown> = {}) => ({
  type: "result",
  subtype: "success",
  result: text,
  session_id: "sess-1",
  terminal_reason: "completed",
  ...(uuids.length ? { user_message_uuid: uuids[uuids.length - 1], user_message_uuids: uuids } : {}),
  ...extra,
});
const bgChanged = (tasks: Array<{ task_id: string; ambient?: boolean }>) => ({
  type: "system",
  subtype: "background_tasks_changed",
  tasks: tasks.map((t) => ({ task_type: "local_bash", description: "", ...t })),
});
const assistantText = (text: string, parent: string | null = null) => ({
  type: "assistant",
  parent_tool_use_id: parent,
  message: { content: [{ type: "text", text }] },
});

function setup(ceilingMs = 0, hooks: Partial<SessionRunHooks> = {}) {
  let fake!: FakeQuery;
  const turns: TurnOutcome[] = [];
  const texts: Array<[string, string | undefined]> = [];
  const bgCounts: number[] = [];
  const run = new SessionRun(
    (input) => (fake = new FakeQuery(input)),
    {
      onTurnEnd: (o) => { turns.push(o); },
      onText: (t, id) => { texts.push([t, id]); },
      onBackgroundChange: (n) => { bgCounts.push(n); },
      ...hooks,
    },
    ceilingMs,
  );
  return { run, turns, texts, bgCounts, fake: () => fake };
}

afterEach(() => {
  vi.useRealTimers();
});

describe("SessionRun", () => {
  it("closes stdin right after the result when nothing runs in the background", async () => {
    const { run, turns, fake } = setup();
    run.push({ text: "hi", msgId: "m1" });
    const done = run.run();
    await tick();
    expect(fake().received).toHaveLength(1);
    fake().emit(result("hello", [uuidOf(fake().received[0])]));
    await tick();
    expect(turns).toEqual([
      expect.objectContaining({ msgId: "m1", result: "hello", followUp: false, background: false, sessionId: "sess-1" }),
    ]);
    expect(fake().inputEnded).toBe(true);
    expect(run.acceptsInput).toBe(false);
    expect(run.push({ text: "late" })).toBe(false);
    fake().end();
    await done;
  });

  it("publishes the reply at once and the follow-up turn after the background job", async () => {
    const { run, turns, bgCounts, fake } = setup();
    run.push({ text: "start a job", msgId: "m1" });
    const done = run.run();
    await tick();
    const u1 = uuidOf(fake().received[0]);

    fake().emit(bgChanged([{ task_id: "b1" }]));
    fake().emit(result("STARTED", [u1]));
    await tick();
    expect(turns).toHaveLength(1);
    expect(turns[0]).toMatchObject({ msgId: "m1", result: "STARTED", background: true, followUp: false });
    expect(run.busy).toBe(false);
    expect(run.background).toBe(1);
    // stdin stays open while the job runs
    expect(fake().inputEnded).toBe(false);
    expect(run.acceptsInput).toBe(true);

    // Job finishes: stdin closes (the CLI still runs the follow-up turn).
    fake().emit(bgChanged([]));
    await tick();
    expect(fake().inputEnded).toBe(true);
    fake().emit({ type: "system", subtype: "init" });
    fake().emit(result("FINISHED", []));
    await tick();
    expect(turns[1]).toMatchObject({ msgId: "m1", result: "FINISHED", followUp: true, background: false });
    expect(bgCounts).toEqual([1, 0]);
    fake().end();
    await done;
  });

  it("pushes a new message into the live process while a job runs", async () => {
    const { run, turns, fake } = setup();
    run.push({ text: "start a job", msgId: "m1" });
    const done = run.run();
    await tick();
    const u1 = uuidOf(fake().received[0]);
    fake().emit(bgChanged([{ task_id: "b1" }]));
    fake().emit(result("STARTED", [u1]));
    await tick();

    expect(run.push({ text: "ping", msgId: "m2" })).toBe(true);
    expect(run.busy).toBe(true);
    await tick();
    expect(fake().received).toHaveLength(2);
    const u2 = uuidOf(fake().received[1]);
    fake().emit(result("PONG", [u2]));
    await tick();
    expect(turns[1]).toMatchObject({ msgId: "m2", result: "PONG", followUp: false, background: true });
    expect(run.busy).toBe(false);

    fake().emit(bgChanged([]));
    fake().emit(result("FINISHED", []));
    await tick();
    // The follow-up goes to the last answered message.
    expect(turns[2]).toMatchObject({ msgId: "m2", followUp: true, background: false });
    fake().end();
    await done;
  });

  it("keeps stdin open while a pushed turn is pending even if the job ends", async () => {
    const { run, fake } = setup();
    run.push({ text: "start", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().emit(bgChanged([{ task_id: "b1" }]));
    fake().emit(result("STARTED", [uuidOf(fake().received[0])]));
    await tick();
    run.push({ text: "next", msgId: "m2" });
    fake().emit(bgChanged([]));
    await tick();
    expect(fake().inputEnded).toBe(false);
    fake().emit(result("ok", [uuidOf(fake().received[1])]));
    await tick();
    expect(fake().inputEnded).toBe(true);
    fake().end();
    await done;
  });

  it("does not count ambient tasks", async () => {
    const { run, turns, fake } = setup();
    run.push({ text: "hi", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().emit(bgChanged([{ task_id: "w1", ambient: true }]));
    fake().emit(result("hello", [uuidOf(fake().received[0])]));
    await tick();
    expect(turns[0].background).toBe(false);
    expect(fake().inputEnded).toBe(true);
    fake().end();
    await done;
  });

  it("streams main-thread text only, attributed to the pending message", async () => {
    const { run, turns, texts, fake } = setup();
    run.push({ text: "hi", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().emit(assistantText("subagent chatter", "toolu_1"));
    fake().emit(assistantText("hello"));
    fake().emit(result("hello", [uuidOf(fake().received[0])]));
    await tick();
    expect(texts).toEqual([["hello", "m1"]]);
    expect(turns[0].streamed).toBe(true);
    fake().end();
    await done;
  });

  it("maps error results to a terminal reason", async () => {
    const { run, turns, fake } = setup();
    run.push({ text: "hi", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().emit({ type: "result", subtype: "error_max_turns", session_id: "sess-2", user_message_uuids: [uuidOf(fake().received[0])] });
    await tick();
    expect(turns[0]).toMatchObject({ msgId: "m1", result: "", terminalReason: "max_turns", sessionId: "sess-2" });
    fake().end();
    await done;
  });

  it("kills the process when background work outlives the ceiling", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const ceilings: Array<[string | undefined, number]> = [];
    const { run, fake } = setup(1000, { onCeiling: (id, n) => { ceilings.push([id, n]); } });
    run.push({ text: "start", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().emit(bgChanged([{ task_id: "b1" }, { task_id: "b2" }]));
    fake().emit(result("STARTED", [uuidOf(fake().received[0])]));
    await tick();

    vi.advanceTimersByTime(999);
    await tick();
    expect(fake().closed).toBe(false);
    vi.advanceTimersByTime(1);
    await tick();
    expect(ceilings).toEqual([["m1", 2]]);
    expect(fake().closed).toBe(true);
    expect(run.killed).toBe(true);
    await done;
  });

  it("does not let the ceiling cut a follow-up turn short", async () => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    const { run, turns, fake } = setup(1000);
    run.push({ text: "start", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().emit(bgChanged([{ task_id: "b1" }]));
    fake().emit(result("STARTED", [uuidOf(fake().received[0])]));
    await tick();
    vi.advanceTimersByTime(900);
    fake().emit(bgChanged([]));
    fake().emit({ type: "system", subtype: "init" });
    await tick();
    vi.advanceTimersByTime(5000);
    await tick();
    expect(fake().closed).toBe(false);
    fake().emit(result("FINISHED", []));
    await tick();
    expect(turns).toHaveLength(2);
    fake().end();
    await done;
  });

  it("kill() ends the process and swallows the resulting stream error", async () => {
    const { run, turns, fake } = setup();
    run.push({ text: "hi", msgId: "m1" });
    const done = run.run();
    await tick();
    run.kill();
    fake().fail(new Error("Query closed before response received"));
    await expect(done).resolves.toBeUndefined();
    expect(fake().closed).toBe(true);
    expect(turns).toHaveLength(0);
    expect(run.pendingMsgIds).toEqual(["m1"]);
  });

  it("propagates a stream error when not killed", async () => {
    const { run, fake } = setup();
    run.push({ text: "hi", msgId: "m1" });
    const done = run.run();
    await tick();
    fake().fail(new Error("boom"));
    await expect(done).rejects.toThrow("boom");
    expect(run.pendingMsgIds).toEqual(["m1"]);
  });
});

describe("liveBackgroundTasks", () => {
  it("counts non-ambient tasks of a background_tasks_changed event", () => {
    expect(liveBackgroundTasks(bgChanged([{ task_id: "a" }, { task_id: "b", ambient: true }]))).toBe(1);
    expect(liveBackgroundTasks(bgChanged([]))).toBe(0);
  });

  it("ignores other events", () => {
    expect(liveBackgroundTasks({ type: "system", subtype: "task_started" })).toBeUndefined();
    expect(liveBackgroundTasks(result("x"))).toBeUndefined();
  });
});

describe("buildUserMessage", () => {
  it("marks verbatim turns as client composed", () => {
    const m = buildUserMessage({ text: "@/etc/passwd /help", verbatim: true }, "u-1");
    expect(m).toMatchObject({ type: "user", uuid: "u-1", client_composed: true, parent_tool_use_id: null });
    expect(m.message).toEqual({ role: "user", content: "@/etc/passwd /help" });
  });

  it("leaves regular turns alone", () => {
    expect(buildUserMessage({ text: "hi" }, "u-2")).not.toHaveProperty("client_composed");
  });
});

describe("InputQueue", () => {
  it("yields pushed items in order and ends on close", async () => {
    const q = new InputQueue<number>();
    q.push(1);
    q.push(2);
    const seen: number[] = [];
    const reader = (async () => { for await (const n of q) seen.push(n); })();
    await tick();
    q.push(3);
    q.close();
    await reader;
    expect(seen).toEqual([1, 2, 3]);
    expect(q.push(4)).toBe(false);
  });
});

describe("backgroundCeilingMs", () => {
  it("defaults to ten minutes", () => {
    expect(backgroundCeilingMs(undefined)).toBe(600_000);
    expect(backgroundCeilingMs("")).toBe(600_000);
    expect(backgroundCeilingMs("abc")).toBe(600_000);
    expect(backgroundCeilingMs("-5")).toBe(600_000);
  });

  it("accepts an explicit value, 0 disabling the ceiling", () => {
    expect(backgroundCeilingMs("1800000")).toBe(1_800_000);
    expect(backgroundCeilingMs("0")).toBe(0);
  });
});
