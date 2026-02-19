import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { execFileSync } from "node:child_process";
import { z } from "zod";

const server = new McpServer({
  name: "praktor-nix",
  version: "1.0.0",
});

function nixAvailable(): boolean {
  try {
    execFileSync("pgrep", ["-l", "nix-daemon"], { timeout: 5000 });
    return true;
  } catch {
    return false;
  }
}

function nixUnavailableResult() {
  return {
    content: [
      {
        type: "text" as const,
        text: "Nix is not available: nix-daemon is not running. This agent does not have nix_enabled set to true.",
      },
    ],
  };
}

function parseProfileList(stdout: string): string {
  try {
    const data = JSON.parse(stdout);
    const elements = data.elements || {};
    const lines: string[] = [];
    for (const [name, elem] of Object.entries<any>(elements)) {
      const storePaths: string[] = elem.storePaths || [];
      const versions = storePaths.map((p: string) => {
        // Store path format: /nix/store/<32-char hash>-<name>-<version>
        const basename = p.split("/").pop() || "";
        const afterHash = basename.substring(33); // skip hash + dash
        const prefix = name + "-";
        return afterHash.startsWith(prefix) ? afterHash.substring(prefix.length) : afterHash;
      });
      lines.push(`${name}: ${versions.length > 0 ? versions.join(", ") : "unknown"}`);
    }
    return lines.length > 0 ? lines.join("\n") : "No packages installed.";
  } catch {
    return stdout;
  }
}

function parseSearchResults(stdout: string): string {
  try {
    const data = JSON.parse(stdout);
    const seen = new Set<string>();
    const results: string[] = [];
    for (const entry of Object.values<any>(data)) {
      const key = `${entry.pname}\t${entry.version}\t${entry.description}`;
      if (seen.has(key)) continue;
      seen.add(key);
      results.push(`${entry.pname} ${entry.version}\n${entry.description}`);
    }
    return results.length > 0 ? results.join("\n\n") : "No results found.";
  } catch {
    return stdout;
  }
}

server.tool(
  "nix_search",
  "Search for packages in the nixpkgs repository.",
  {
    query: z.string().describe("Package name or keyword to search for"),
  },
  async ({ query }) => {
    console.error(`[mcp-nix] search query=${query}`);
    if (!nixAvailable()) return nixUnavailableResult();
    try {
      const stdout = execFileSync("nix", ["search", "--json", "--quiet", "nixpkgs", query], {
        timeout: 60000,
        encoding: "utf-8",
      });
      return { content: [{ type: "text" as const, text: parseSearchResults(stdout) }] };
    } catch (err: any) {
      const output = err.stderr || err.stdout || err.message;
      return { content: [{ type: "text" as const, text: `Search failed: ${output}` }] };
    }
  }
);

server.tool(
  "nix_add",
  "Install one or more packages from nixpkgs into the agent's nix profile.",
  {
    packages: z.string().describe("Space-separated package names (e.g. 'neovim ripgrep python3')"),
  },
  async ({ packages }) => {
    const pkgs = packages.split(/\s+/).filter(Boolean);
    console.error(`[mcp-nix] add packages=${pkgs.join(",")}`);
    if (!nixAvailable()) return nixUnavailableResult();
    try {
      const args = ["profile", "add", ...pkgs.map((p) => `nixpkgs#${p}`)];
      const stdout = execFileSync("nix", args, {
        timeout: 120000,
        encoding: "utf-8",
      });
      return { content: [{ type: "text" as const, text: stdout || `Installed: ${pkgs.join(", ")}` }] };
    } catch (err: any) {
      const output = err.stderr || err.stdout || err.message;
      return { content: [{ type: "text" as const, text: `Install failed: ${output}` }] };
    }
  }
);

server.tool(
  "nix_list_installed",
  "List all packages installed in the agent's nix profile.",
  {},
  async () => {
    console.error(`[mcp-nix] list_installed`);
    if (!nixAvailable()) return nixUnavailableResult();
    try {
      const stdout = execFileSync("nix", ["profile", "list", "--json"], {
        timeout: 10000,
        encoding: "utf-8",
      });
      return { content: [{ type: "text" as const, text: parseProfileList(stdout) }] };
    } catch (err: any) {
      const output = err.stderr || err.stdout || err.message;
      return { content: [{ type: "text" as const, text: `List failed: ${output}` }] };
    }
  }
);

server.tool(
  "nix_remove",
  "Remove one or more packages from the agent's nix profile.",
  {
    packages: z.string().describe("Space-separated package names to remove (e.g. 'neovim ripgrep')"),
  },
  async ({ packages }) => {
    const pkgs = packages.split(/\s+/).filter(Boolean);
    console.error(`[mcp-nix] remove packages=${pkgs.join(",")}`);
    if (!nixAvailable()) return nixUnavailableResult();
    try {
      const args = ["profile", "remove", ...pkgs];
      const stdout = execFileSync("nix", args, {
        timeout: 30000,
        encoding: "utf-8",
      });
      return { content: [{ type: "text" as const, text: stdout || `Removed: ${pkgs.join(", ")}` }] };
    } catch (err: any) {
      const output = err.stderr || err.stdout || err.message;
      return { content: [{ type: "text" as const, text: `Remove failed: ${output}` }] };
    }
  }
);

server.tool(
  "nix_upgrade",
  "Upgrade all packages in the agent's nix profile to their latest versions.",
  {},
  async () => {
    console.error(`[mcp-nix] upgrade`);
    if (!nixAvailable()) return nixUnavailableResult();
    try {
      const stdout = execFileSync("nix", ["profile", "upgrade", "--all"], {
        timeout: 120000,
        encoding: "utf-8",
      });
      return { content: [{ type: "text" as const, text: stdout || "All packages upgraded successfully." }] };
    } catch (err: any) {
      const output = err.stderr || err.stdout || err.message;
      return { content: [{ type: "text" as const, text: `Upgrade failed: ${output}` }] };
    }
  }
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP nix server error:", err);
  process.exit(1);
});
