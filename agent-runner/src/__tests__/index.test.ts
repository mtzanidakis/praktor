import { describe, it, expect } from "vitest";
import { parseAllowedTools } from "../index.js";

describe("parseAllowedTools", () => {
  it("returns undefined for empty string", () => {
    expect(parseAllowedTools("")).toBeUndefined();
  });

  it("parses a single tool", () => {
    expect(parseAllowedTools("Bash")).toEqual(["Bash"]);
  });

  it("parses comma-separated tools", () => {
    expect(parseAllowedTools("Bash,Read,Write")).toEqual([
      "Bash",
      "Read",
      "Write",
    ]);
  });

  it("trims whitespace around tool names", () => {
    expect(parseAllowedTools(" Bash , Read , Write ")).toEqual([
      "Bash",
      "Read",
      "Write",
    ]);
  });

  it("filters out empty entries from trailing commas", () => {
    expect(parseAllowedTools("Bash,,Read,")).toEqual(["Bash", "Read"]);
  });

  it("returns undefined for whitespace-only string", () => {
    expect(parseAllowedTools("  ,  ,  ")).toBeUndefined();
  });
});
