import { describe, it, expect } from "vitest";
import {
  collectDependencies,
  deriveMarketplaceName,
  parseNames,
  type AgentExtensions,
} from "../extensions.js";

describe("collectDependencies", () => {
  it("returns empty array for empty extensions", () => {
    expect(collectDependencies({})).toEqual([]);
  });

  it("collects stdio MCP server commands", () => {
    const ext: AgentExtensions = {
      mcp_servers: {
        myserver: { type: "stdio", command: "uvx" },
      },
    };
    expect(collectDependencies(ext)).toEqual(["uvx"]);
  });

  it("ignores http MCP servers", () => {
    const ext: AgentExtensions = {
      mcp_servers: {
        remote: { type: "http", url: "https://example.com" },
      },
    };
    expect(collectDependencies(ext)).toEqual([]);
  });

  it("collects plugin requires", () => {
    const ext: AgentExtensions = {
      plugins: [
        { name: "my-plugin", requires: ["python3", "git"] },
      ],
    };
    const deps = collectDependencies(ext);
    expect(deps).toContain("python3");
    expect(deps).toContain("git");
  });

  it("collects skill requires", () => {
    const ext: AgentExtensions = {
      skills: {
        myskill: {
          description: "A skill",
          content: "# Skill",
          requires: ["jq"],
        },
      },
    };
    expect(collectDependencies(ext)).toEqual(["jq"]);
  });

  it("deduplicates dependencies across types", () => {
    const ext: AgentExtensions = {
      mcp_servers: {
        s1: { type: "stdio", command: "python3" },
      },
      plugins: [
        { name: "p1", requires: ["python3", "git"] },
      ],
      skills: {
        sk1: {
          description: "test",
          content: "test",
          requires: ["git"],
        },
      },
    };
    const deps = collectDependencies(ext);
    expect(deps).toHaveLength(2);
    expect(deps).toContain("python3");
    expect(deps).toContain("git");
  });

  it("handles plugins with no requires field", () => {
    const ext: AgentExtensions = {
      plugins: [{ name: "simple-plugin" }],
    };
    expect(collectDependencies(ext)).toEqual([]);
  });

  it("handles skills with no requires field", () => {
    const ext: AgentExtensions = {
      skills: {
        basic: { description: "basic", content: "# Basic" },
      },
    };
    expect(collectDependencies(ext)).toEqual([]);
  });
});

describe("deriveMarketplaceName", () => {
  it("handles owner/repo format", () => {
    expect(deriveMarketplaceName("owner/repo")).toBe("owner-repo");
  });

  it("strips https protocol and normalizes separators", () => {
    expect(deriveMarketplaceName("https://github.com/owner/repo")).toBe(
      "github-com-owner-repo"
    );
  });

  it("strips http protocol", () => {
    expect(deriveMarketplaceName("http://example.com/plugins")).toBe(
      "example-com-plugins"
    );
  });

  it("removes trailing dashes", () => {
    expect(deriveMarketplaceName("https://example.com/")).toBe(
      "example-com"
    );
  });
});

describe("parseNames", () => {
  it("extracts names from CLI output with markers", () => {
    const output = `Installed marketplaces:
  ❯ claude-plugins-official
  ❯ my-marketplace`;
    expect(parseNames(output)).toEqual([
      "claude-plugins-official",
      "my-marketplace",
    ]);
  });

  it("returns empty array when no markers found", () => {
    expect(parseNames("No plugins installed.")).toEqual([]);
  });

  it("returns empty array for empty input", () => {
    expect(parseNames("")).toEqual([]);
  });

  it("extracts name before any parenthetical", () => {
    const output = "  ❯ my-plugin (disabled)";
    // parseNames grabs \S+ after ❯, so it gets "my-plugin"
    expect(parseNames(output)).toEqual(["my-plugin"]);
  });
});
