import { describe, it, expect } from "vitest";
import { FileSendTracker } from "../index.js";

const call = (id: string, name = "mcp__praktor-file__file_send") => ({
  type: "assistant",
  message: { content: [{ type: "tool_use", id, name, input: {} }] },
});

const result = (id: string, isError = false) => ({
  type: "user",
  message: { content: [{ type: "tool_result", tool_use_id: id, content: "x", is_error: isError }] },
});

describe("FileSendTracker", () => {
  it("does not count a call until its result arrives", () => {
    const t = new FileSendTracker();
    t.observe(call("a"));
    expect(t.sent).toBe(false);
    t.observe(result("a"));
    expect(t.sent).toBe(true);
  });

  it("does not count a failed send", () => {
    const t = new FileSendTracker();
    t.observe(call("a"));
    t.observe(result("a", true));
    expect(t.sent).toBe(false);
  });

  it("counts a later success after a failed attempt", () => {
    const t = new FileSendTracker();
    t.observe(call("a"));
    t.observe(result("a", true));
    t.observe(call("b"));
    t.observe(result("b"));
    expect(t.sent).toBe(true);
  });

  it("ignores results of other tools", () => {
    const t = new FileSendTracker();
    t.observe(call("a", "Bash"));
    t.observe(result("a"));
    expect(t.sent).toBe(false);
  });

  it("ignores events without content blocks", () => {
    const t = new FileSendTracker();
    t.observe({ type: "user", message: { content: "plain prompt" } });
    t.observe({ type: "system", subtype: "init" });
    expect(t.sent).toBe(false);
  });
});
