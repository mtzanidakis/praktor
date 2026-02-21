import { describe, it, expect } from "vitest";
import { parseProfileList, parseSearchResults } from "../mcp-nix.js";

describe("parseProfileList", () => {
  it("formats valid JSON with packages and versions", () => {
    const hash1 = "a".repeat(32);
    const hash2 = "b".repeat(32);
    const input = JSON.stringify({
      elements: {
        ripgrep: {
          storePaths: [`/nix/store/${hash1}-ripgrep-14.1.0`],
        },
        neovim: {
          storePaths: [`/nix/store/${hash2}-neovim-0.10.0`],
        },
      },
    });
    const result = parseProfileList(input);
    expect(result).toContain("ripgrep: 14.1.0");
    expect(result).toContain("neovim: 0.10.0");
  });

  it("returns 'No packages installed.' for empty elements", () => {
    const input = JSON.stringify({ elements: {} });
    expect(parseProfileList(input)).toBe("No packages installed.");
  });

  it("handles missing elements key", () => {
    const input = JSON.stringify({});
    expect(parseProfileList(input)).toBe("No packages installed.");
  });

  it("falls back to raw string on malformed JSON", () => {
    const raw = "not json at all";
    expect(parseProfileList(raw)).toBe(raw);
  });

  it("shows 'unknown' when storePaths is empty", () => {
    const input = JSON.stringify({
      elements: {
        curl: { storePaths: [] },
      },
    });
    expect(parseProfileList(input)).toBe("curl: unknown");
  });

  it("extracts version from store path correctly", () => {
    // 32-char hash + dash prefix
    const hash = "a".repeat(32);
    const input = JSON.stringify({
      elements: {
        python3: {
          storePaths: [`/nix/store/${hash}-python3-3.12.1`],
        },
      },
    });
    expect(parseProfileList(input)).toBe("python3: 3.12.1");
  });
});

describe("parseSearchResults", () => {
  it("formats valid JSON search results", () => {
    const input = JSON.stringify({
      "nixpkgs.ripgrep": {
        pname: "ripgrep",
        version: "14.1.0",
        description: "Fast search tool",
      },
    });
    const result = parseSearchResults(input);
    expect(result).toBe("ripgrep 14.1.0\nFast search tool");
  });

  it("deduplicates entries with same pname/version/description", () => {
    const input = JSON.stringify({
      "nixpkgs.ripgrep": {
        pname: "ripgrep",
        version: "14.1.0",
        description: "Fast search tool",
      },
      "nixpkgs.x86_64-linux.ripgrep": {
        pname: "ripgrep",
        version: "14.1.0",
        description: "Fast search tool",
      },
    });
    const result = parseSearchResults(input);
    expect(result).toBe("ripgrep 14.1.0\nFast search tool");
  });

  it("returns 'No results found.' for empty object", () => {
    expect(parseSearchResults("{}")).toBe("No results found.");
  });

  it("falls back to raw string on malformed JSON", () => {
    const raw = "invalid json";
    expect(parseSearchResults(raw)).toBe(raw);
  });

  it("formats multiple distinct results", () => {
    const input = JSON.stringify({
      "nixpkgs.python3": {
        pname: "python3",
        version: "3.12.1",
        description: "Python interpreter",
      },
      "nixpkgs.python2": {
        pname: "python2",
        version: "2.7.18",
        description: "Legacy Python",
      },
    });
    const result = parseSearchResults(input);
    expect(result).toContain("python3 3.12.1\nPython interpreter");
    expect(result).toContain("python2 2.7.18\nLegacy Python");
  });
});
