import { describe, it, expect } from "vitest";
import { resolveRouteModel } from "../index.js";

describe("resolveRouteModel", () => {
  it("uses the model sent by the gateway", () => {
    expect(resolveRouteModel({ text: "hi", model: "claude-haiku-5-5" }, "claude-opus-5-5")).toBe(
      "claude-haiku-5-5"
    );
  });

  it("falls back to the agent model when the payload has none (older gateway)", () => {
    expect(resolveRouteModel({ text: "hi" }, "claude-opus-5-5")).toBe("claude-opus-5-5");
  });

  it("falls back on an empty or blank model", () => {
    expect(resolveRouteModel({ text: "hi", model: "" }, "claude-opus-5-5")).toBe("claude-opus-5-5");
    expect(resolveRouteModel({ text: "hi", model: "  " }, "claude-opus-5-5")).toBe("claude-opus-5-5");
  });

  it("ignores a non-string model", () => {
    expect(resolveRouteModel({ text: "hi", model: 42 }, "claude-opus-5-5")).toBe("claude-opus-5-5");
  });

  it("returns undefined when neither is set (SDK default)", () => {
    expect(resolveRouteModel({ text: "hi" }, undefined)).toBeUndefined();
  });
});
