import { randomUUID } from "crypto";
import type { SDKMessage, SDKUserMessage } from "@anthropic-ai/claude-agent-sdk";

// A SessionRun is one Claude Code process driven in streaming-input mode.
//
// Since Claude Code 2.1.292 a query no longer kills background work (Bash
// run_in_background, background subagents) shortly after the turn's result:
// the process stays alive until that work settles, and each completion starts
// a follow-up turn with its own `result`. The run therefore:
//
// - reports every turn's result as it arrives, so the reply to the user's
//   message is not held back until background jobs finish, and the follow-up
//   turns' output reaches the user too;
// - keeps stdin open while background work is live, so a new user message is
//   pushed into the same process (one CLI per session, never two writing the
//   same transcript) instead of waiting behind the job;
// - closes stdin as soon as no turn is pending and no background task is live.
//   A closed-input CLI still waits for work it already started and runs the
//   follow-up turn before it exits;
// - is bounded: if nothing happens on the main thread for ceilingMs after the
//   last result, the process (and its background work) is killed.

export class InputQueue<T> implements AsyncIterable<T> {
  private items: T[] = [];
  private wake: (() => void) | null = null;
  private closedFlag = false;

  push(item: T): boolean {
    if (this.closedFlag) return false;
    this.items.push(item);
    this.notify();
    return true;
  }

  close(): void {
    this.closedFlag = true;
    this.notify();
  }

  get closed(): boolean {
    return this.closedFlag;
  }

  private notify(): void {
    const w = this.wake;
    this.wake = null;
    w?.();
  }

  async *[Symbol.asyncIterator](): AsyncIterator<T> {
    for (;;) {
      if (this.items.length > 0) {
        yield this.items.shift()!;
        continue;
      }
      if (this.closedFlag) return;
      await new Promise<void>((resolve) => { this.wake = resolve; });
    }
  }
}

// liveBackgroundTasks returns the number of live background tasks a
// background_tasks_changed event reports (a level signal with replace
// semantics), or undefined for any other event. Ambient tasks (housekeeping,
// watchers) are not activity and are not counted.
export function liveBackgroundTasks(event: unknown): number | undefined {
  const e = event as { type?: string; subtype?: string; tasks?: Array<{ ambient?: boolean }> };
  if (e.type !== "system" || e.subtype !== "background_tasks_changed" || !Array.isArray(e.tasks)) return undefined;
  return e.tasks.filter((t) => !t.ambient).length;
}

export interface UserTurn {
  text: string;
  msgId?: string;
  // Deliver the text as written: no @path expansion or slash-command dispatch.
  verbatim?: boolean;
}

export interface TurnOutcome {
  // The user message this turn answers. A follow-up turn (started by a
  // background completion) is attributed to the last answered message.
  msgId?: string;
  followUp: boolean;
  // Final assistant text of a successful turn ("" otherwise).
  result: string;
  terminalReason?: string;
  sessionId?: string;
  // Main-thread assistant text was streamed during this turn.
  streamed: boolean;
  // Background tasks still live when the turn ended: more results may follow.
  background: boolean;
}

export interface SessionRunHooks {
  onEvent?(event: SDKMessage): void;
  onText?(text: string, msgId: string | undefined): void | Promise<void>;
  onTurnEnd(outcome: TurnOutcome): void | Promise<void>;
  onBackgroundChange?(count: number): void;
  // The run outlived its last result by the ceiling; it is killed right
  // after this returns. background is the number of live background tasks.
  onCeiling?(msgId: string | undefined, background: number): void | Promise<void>;
}

export interface RunnableQuery {
  [Symbol.asyncIterator](): AsyncIterator<SDKMessage>;
  close(): void;
}

export type StartQuery = (prompt: AsyncIterable<SDKUserMessage>) => RunnableQuery;

export function buildUserMessage(turn: UserTurn, uuid: string): SDKUserMessage {
  return {
    type: "user",
    message: { role: "user", content: turn.text },
    parent_tool_use_id: null,
    uuid: uuid as SDKUserMessage["uuid"],
    ...(turn.verbatim ? { client_composed: true as const } : {}),
  };
}

export class SessionRun {
  private readonly input = new InputQueue<SDKUserMessage>();
  // uuid → msgId of pushed user turns whose result has not arrived yet.
  private readonly awaiting = new Map<string, string | undefined>();
  private lastAnswered: string | undefined;
  private streamed = false;
  private ceilingTimer: ReturnType<typeof setTimeout> | undefined;
  private query: RunnableQuery | null = null;
  private killedFlag = false;
  background = 0;

  constructor(
    private readonly start: StartQuery,
    private readonly hooks: SessionRunHooks,
    private readonly ceilingMs: number,
  ) {}

  // push queues a user turn. Returns false once stdin is closed: the caller
  // must wait for the run to end and start a new one.
  push(turn: UserTurn): boolean {
    if (this.input.closed) return false;
    const uuid = randomUUID();
    this.awaiting.set(uuid, turn.msgId);
    if (this.lastAnswered === undefined) this.lastAnswered = turn.msgId;
    this.clearCeiling();
    return this.input.push(buildUserMessage(turn, uuid));
  }

  // acceptsInput reports whether push would hand the turn to this process.
  get acceptsInput(): boolean {
    return !this.input.closed && !this.killedFlag;
  }

  // busy reports whether a pushed user turn is still waiting for its result.
  get busy(): boolean {
    return this.awaiting.size > 0;
  }

  get killed(): boolean {
    return this.killedFlag;
  }

  // pendingMsgIds lists the user turns that never got a result.
  get pendingMsgIds(): Array<string | undefined> {
    return [...this.awaiting.values()];
  }

  get currentStreamed(): boolean {
    return this.streamed;
  }

  // kill ends the process and every background task it runs.
  kill(): void {
    this.killedFlag = true;
    this.clearCeiling();
    this.input.close();
    try { this.query?.close(); } catch { /* ignore */ }
  }

  // run consumes the process's events until it exits. It rejects when the
  // event stream fails (unless the run was killed).
  async run(): Promise<void> {
    this.query = this.start(this.input);
    if (this.killedFlag) { try { this.query.close(); } catch { /* ignore */ } return; }
    const iter = this.query[Symbol.asyncIterator]();
    try {
      for (;;) {
        const next = await iter.next();
        if (next.done) break;
        await this.handle(next.value);
      }
    } catch (err) {
      if (!this.killedFlag) throw err;
    } finally {
      this.clearCeiling();
      this.input.close();
      if (this.background !== 0) {
        this.background = 0;
        this.hooks.onBackgroundChange?.(0);
      }
    }
  }

  private currentMsgId(): string | undefined {
    const first = this.awaiting.values().next();
    return first.done ? this.lastAnswered : first.value;
  }

  private async handle(event: SDKMessage): Promise<void> {
    if (this.killedFlag) return;
    this.hooks.onEvent?.(event);

    const live = liveBackgroundTasks(event);
    if (live !== undefined) {
      if (live !== this.background) {
        this.background = live;
        this.hooks.onBackgroundChange?.(live);
      }
      this.maybeCloseInput();
      return;
    }

    const e = event as Record<string, unknown>;
    if (event.type === "system" && e.subtype === "init") {
      // A turn is starting (a pushed message or a background follow-up).
      this.clearCeiling();
      return;
    }

    if (event.type === "assistant") {
      // Subagent messages carry their parent tool_use id; only main-thread
      // text is the agent talking to the user.
      if (event.parent_tool_use_id) return;
      this.clearCeiling();
      for (const block of event.message.content) {
        if (block.type === "text") {
          this.streamed = true;
          await this.hooks.onText?.(block.text, this.currentMsgId());
        }
      }
      return;
    }

    if (event.type === "result") {
      const uuids = (e.user_message_uuids as string[] | undefined)
        ?? (e.user_message_uuid ? [e.user_message_uuid as string] : []);
      let msgId: string | undefined;
      let matched = false;
      for (const u of uuids) {
        if (this.awaiting.has(u)) {
          msgId = this.awaiting.get(u);
          this.awaiting.delete(u);
          matched = true;
        }
      }
      if (matched) this.lastAnswered = msgId;
      else msgId = this.lastAnswered;

      let terminalReason = e.terminal_reason as string | undefined;
      let result = "";
      if (event.subtype === "success") {
        result = event.result;
      } else if (typeof event.subtype === "string" && event.subtype.startsWith("error")) {
        // SDK uses subtypes like "error_max_turns", "error_blocking_limit"
        terminalReason = terminalReason || event.subtype.replace(/^error_?/, "") || event.subtype;
      }

      const outcome: TurnOutcome = {
        msgId,
        followUp: !matched,
        result,
        terminalReason,
        sessionId: e.session_id as string | undefined,
        streamed: this.streamed,
        background: this.background > 0,
      };
      this.streamed = false;
      await this.hooks.onTurnEnd(outcome);
      this.maybeCloseInput();
      // Normally the process exits right after this (stdin closed, nothing
      // live) and run() clears the timer. Otherwise it bounds the wait for
      // background work, or for a closed-input CLI that stays up for some
      // other reason (e.g. a scheduled wakeup).
      this.armCeiling();
    }
  }

  private maybeCloseInput(): void {
    if (this.input.closed || this.awaiting.size > 0 || this.background > 0) return;
    this.input.close();
  }

  private armCeiling(): void {
    this.clearCeiling();
    if (this.ceilingMs <= 0 || this.killedFlag) return;
    this.ceilingTimer = setTimeout(() => {
      this.ceilingTimer = undefined;
      void (async () => {
        try {
          await this.hooks.onCeiling?.(this.lastAnswered, this.background);
        } finally {
          this.kill();
        }
      })();
    }, this.ceilingMs);
    this.ceilingTimer.unref?.();
  }

  private clearCeiling(): void {
    if (this.ceilingTimer) clearTimeout(this.ceilingTimer);
    this.ceilingTimer = undefined;
  }
}

// backgroundCeilingMs reads how long a run may wait for background work after
// its last result. It honours the same variable (and default) Claude Code uses
// for one-shot runs; 0 disables the ceiling.
export function backgroundCeilingMs(env: string | undefined): number {
  if (env === undefined || env.trim() === "") return 600_000;
  const n = Number(env);
  return Number.isFinite(n) && n >= 0 ? n : 600_000;
}
