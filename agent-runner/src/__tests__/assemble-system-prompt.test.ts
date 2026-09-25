import { describe, it, expect } from "vitest";
import { SYSTEM_PROMPT_DYNAMIC_BOUNDARY } from "@anthropic-ai/claude-agent-sdk";
import { assembleSystemPrompt } from "../index.js";

describe("assembleSystemPrompt", () => {
  it("joins static parts into a single block without a boundary", () => {
    expect(assembleSystemPrompt(["A", "B"], [])).toEqual(["A\n\n---\n\nB"]);
  });

  it("puts dynamic parts after the cache boundary", () => {
    expect(assembleSystemPrompt(["A", "B"], ["MEMORY"])).toEqual([
      "A\n\n---\n\nB",
      SYSTEM_PROMPT_DYNAMIC_BOUNDARY,
      "MEMORY",
    ]);
  });

  it("keeps the static prefix identical when only dynamic parts change", () => {
    const a = assembleSystemPrompt(["A", "B"], ["- key1"]);
    const b = assembleSystemPrompt(["A", "B"], ["- key1\n- key2"]);
    expect(a[0]).toBe(b[0]);
    expect(a[2]).not.toBe(b[2]);
  });
});
