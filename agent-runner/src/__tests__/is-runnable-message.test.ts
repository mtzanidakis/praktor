import { describe, it, expect } from "vitest";
import { isRunnableMessage } from "../index.js";

describe("isRunnableMessage", () => {
  it("runs a message with text", () => {
    expect(isRunnableMessage({ text: "hi", sender: "user:1" })).toBe(true);
  });

  it("runs a scheduled task with a prompt", () => {
    expect(isRunnableMessage({ text: "Summarise", sender: "scheduler" })).toBe(true);
  });

  it("runs a check-only scheduled task", () => {
    expect(isRunnableMessage({ text: "", sender: "scheduler", check_command: "echo due" })).toBe(true);
  });

  it("drops an empty scheduled task without a check", () => {
    expect(isRunnableMessage({ text: "", sender: "scheduler" })).toBe(false);
  });

  it("drops an empty message with a check from any other sender", () => {
    expect(isRunnableMessage({ text: "", sender: "user:1", check_command: "id" })).toBe(false);
    expect(isRunnableMessage({ sender: "agentmail", check_command: "id" })).toBe(false);
  });

  it("drops a message with no text", () => {
    expect(isRunnableMessage({ sender: "user:1" })).toBe(false);
  });
});
