import { describe, it, expect } from "vitest";
import { decideTaskFinalResponse } from "../index.js";

describe("decideTaskFinalResponse", () => {
  it("uses the result text when claude produced one", () => {
    expect(
      decideTaskFinalResponse({
        result: "Here is your summary.",
        hasStreamedOutput: false,
        hasFileSent: false,
      })
    ).toEqual({ content: "Here is your summary.", warn: false });
  });

  it("prefers result over streamed/file signals", () => {
    expect(
      decideTaskFinalResponse({
        result: "final answer",
        hasStreamedOutput: true,
        hasFileSent: true,
      })
    ).toEqual({ content: "final answer", warn: false });
  });

  it("falls back to [response was streamed] when only streamed text occurred", () => {
    expect(
      decideTaskFinalResponse({
        result: "",
        hasStreamedOutput: true,
        hasFileSent: false,
      })
    ).toEqual({ content: "[response was streamed]", warn: false });
  });

  it("publishes empty content (no marker) when only a file was sent", () => {
    // Regression: a scheduled task that delivered output via file_send used to
    // append '⚠️ Task completed with no output.' even though the user already
    // received the image. file_send alone should suppress the warning.
    expect(
      decideTaskFinalResponse({
        result: "",
        hasStreamedOutput: false,
        hasFileSent: true,
      })
    ).toEqual({ content: "", warn: false });
  });

  it("prefers streamed marker over file-only suppression when both happened", () => {
    expect(
      decideTaskFinalResponse({
        result: "",
        hasStreamedOutput: true,
        hasFileSent: true,
      })
    ).toEqual({ content: "[response was streamed]", warn: false });
  });

  it("publishes empty content with warn=true when nothing user-visible happened", () => {
    // Empty content is dropped by the gateway before Telegram (orchestrator
    // skips listener forwarding for empty results), so silent task completions
    // surface only as an internal warn log — no noisy '⚠️' message to the user.
    expect(
      decideTaskFinalResponse({
        result: "",
        hasStreamedOutput: false,
        hasFileSent: false,
      })
    ).toEqual({ content: "", warn: true });
  });
});
