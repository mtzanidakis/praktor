import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { DatabaseSync } from "node:sqlite";
import { z } from "zod";

const MEMORY_DB_PATH = "/workspace/agent/memory.db";
const memoryDb = new DatabaseSync(MEMORY_DB_PATH);
memoryDb.exec(`
  CREATE TABLE IF NOT EXISTS memories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT UNIQUE NOT NULL,
    content TEXT NOT NULL,
    tags TEXT DEFAULT '',
    created_at INTEGER DEFAULT (unixepoch()),
    updated_at INTEGER DEFAULT (unixepoch())
  )
`);

const server = new McpServer({
  name: "praktor-memory",
  version: "1.0.0",
});

server.tool(
  "memory_store",
  "Store a memory with a short descriptive key. Use this to remember facts, preferences, decisions, or anything worth recalling later. If the key already exists, the content is updated.",
  {
    key: z.string().describe("Short descriptive key (e.g. 'pet-cat-name', 'project-stack')"),
    content: z.string().describe("The content to remember"),
    tags: z.string().optional().describe("Comma-separated tags for categorization (e.g. 'personal, pets')"),
  },
  async ({ key, content, tags }) => {
    console.error(`[mcp-memory] store key=${key} tags=${tags || ""}`);
    const stmt = memoryDb.prepare(
      `INSERT INTO memories (key, content, tags)
       VALUES (?, ?, ?)
       ON CONFLICT(key) DO UPDATE SET content=excluded.content, tags=excluded.tags, updated_at=unixepoch()`
    );
    stmt.run(key, content, tags || "");
    return {
      content: [{ type: "text" as const, text: `Memory stored: ${key}` }],
    };
  }
);

server.tool(
  "memory_recall",
  "Search memories by keyword. Searches across keys, content, and tags. Returns full content of all matching memories.",
  {
    query: z.string().describe("Keyword or phrase to search for"),
  },
  async ({ query }) => {
    console.error(`[mcp-memory] recall query=${query}`);
    const pattern = `%${query}%`;
    const stmt = memoryDb.prepare(
      `SELECT key, content, tags, created_at, updated_at FROM memories
       WHERE key LIKE ? OR content LIKE ? OR tags LIKE ?
       ORDER BY updated_at DESC`
    );
    const rows = stmt.all(pattern, pattern, pattern) as Array<{
      key: string; content: string; tags: string; created_at: number; updated_at: number;
    }>;
    if (rows.length === 0) {
      return { content: [{ type: "text" as const, text: "No memories found matching that query." }] };
    }
    const result = rows.map((r) =>
      `## ${r.key}${r.tags ? ` [${r.tags}]` : ""}\n${r.content}\n_(updated: ${new Date(r.updated_at * 1000).toISOString()})_`
    ).join("\n\n");
    return { content: [{ type: "text" as const, text: result }] };
  }
);

server.tool(
  "memory_list",
  "List all stored memory keys with their tags and timestamps.",
  {},
  async () => {
    console.error(`[mcp-memory] list`);
    const stmt = memoryDb.prepare(
      `SELECT key, tags, created_at, updated_at FROM memories ORDER BY updated_at DESC`
    );
    const rows = stmt.all() as Array<{
      key: string; tags: string; created_at: number; updated_at: number;
    }>;
    if (rows.length === 0) {
      return { content: [{ type: "text" as const, text: "No memories stored yet." }] };
    }
    const lines = rows.map((r) =>
      `- ${r.key}${r.tags ? ` [${r.tags}]` : ""} (updated: ${new Date(r.updated_at * 1000).toISOString()})`
    );
    return { content: [{ type: "text" as const, text: lines.join("\n") }] };
  }
);

server.tool(
  "memory_delete",
  "Delete a specific memory by its exact key.",
  {
    key: z.string().describe("Exact key of the memory to delete"),
  },
  async ({ key }) => {
    console.error(`[mcp-memory] delete key=${key}`);
    const stmt = memoryDb.prepare(`DELETE FROM memories WHERE key = ?`);
    const result = stmt.run(key);
    if (result.changes === 0) {
      return { content: [{ type: "text" as const, text: `No memory found with key: ${key}` }] };
    }
    return { content: [{ type: "text" as const, text: `Memory deleted: ${key}` }] };
  }
);

server.tool(
  "memory_forget",
  "Search and delete all memories matching a query. Searches across keys, content, and tags.",
  {
    query: z.string().describe("Keyword or phrase — all matching memories will be deleted"),
  },
  async ({ query }) => {
    console.error(`[mcp-memory] forget query=${query}`);
    const pattern = `%${query}%`;
    const stmt = memoryDb.prepare(
      `DELETE FROM memories WHERE key LIKE ? OR content LIKE ? OR tags LIKE ?`
    );
    const result = stmt.run(pattern, pattern, pattern);
    return {
      content: [{ type: "text" as const, text: `Deleted ${result.changes} memory(ies) matching "${query}".` }],
    };
  }
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP memory server error:", err);
  process.exit(1);
});
