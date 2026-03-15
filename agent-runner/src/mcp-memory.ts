import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { DatabaseSync } from "node:sqlite";
import { z } from "zod";

const MEMORY_DB_PATH = "/workspace/agent/memory.db";
const memoryDb = new DatabaseSync(MEMORY_DB_PATH);

// Core table
memoryDb.exec(`
  CREATE TABLE IF NOT EXISTS memories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT UNIQUE NOT NULL,
    content TEXT NOT NULL,
    tags TEXT DEFAULT '',
    access_count INTEGER DEFAULT 0,
    last_accessed INTEGER DEFAULT 0,
    created_at INTEGER DEFAULT (unixepoch()),
    updated_at INTEGER DEFAULT (unixepoch())
  )
`);

// Add columns for existing databases (ignore errors if already present)
for (const col of [
  "ALTER TABLE memories ADD COLUMN access_count INTEGER DEFAULT 0",
  "ALTER TABLE memories ADD COLUMN last_accessed INTEGER DEFAULT 0",
]) {
  try { memoryDb.exec(col); } catch { /* column already exists */ }
}

// FTS5 content-sync table + triggers
memoryDb.exec(`
  CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    key,
    content,
    tags,
    content=memories,
    content_rowid=id
  )
`);

memoryDb.exec(`
  CREATE TRIGGER IF NOT EXISTS memories_fts_insert AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, key, content, tags) VALUES (new.id, new.key, new.content, new.tags);
  END
`);

memoryDb.exec(`
  CREATE TRIGGER IF NOT EXISTS memories_fts_update AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, key, content, tags) VALUES ('delete', old.id, old.key, old.content, old.tags);
    INSERT INTO memories_fts(rowid, key, content, tags) VALUES (new.id, new.key, new.content, new.tags);
  END
`);

memoryDb.exec(`
  CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, key, content, tags) VALUES ('delete', old.id, old.key, old.content, old.tags);
  END
`);

// Populate FTS index for pre-existing memories
memoryDb.exec(`INSERT OR IGNORE INTO memories_fts(rowid, key, content, tags) SELECT id, key, content, tags FROM memories`);

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
  "Search memories by keyword. Uses full-text search with relevance ranking across keys, content, and tags. Returns the most relevant memories first.",
  {
    query: z.string().describe("Search query (supports words, \"phrases\", OR, NOT operators)"),
  },
  async ({ query }) => {
    console.error(`[mcp-memory] recall query=${query}`);

    // Try FTS5 first, fall back to LIKE if the query syntax is invalid
    let rows: Array<{
      key: string; content: string; tags: string;
      access_count: number; created_at: number; updated_at: number;
    }>;

    try {
      const stmt = memoryDb.prepare(
        `SELECT m.key, m.content, m.tags, m.access_count, m.created_at, m.updated_at
         FROM memories_fts f
         JOIN memories m ON m.id = f.rowid
         WHERE memories_fts MATCH ?
         ORDER BY f.rank
         LIMIT 20`
      );
      rows = stmt.all(query) as typeof rows;
    } catch {
      // FTS5 query syntax error — fall back to LIKE
      const pattern = `%${query}%`;
      const stmt = memoryDb.prepare(
        `SELECT key, content, tags, access_count, created_at, updated_at FROM memories
         WHERE key LIKE ? OR content LIKE ? OR tags LIKE ?
         ORDER BY updated_at DESC`
      );
      rows = stmt.all(pattern, pattern, pattern) as typeof rows;
    }

    if (rows.length === 0) {
      return { content: [{ type: "text" as const, text: "No memories found matching that query." }] };
    }

    // Update access tracking for matched memories
    const updateStmt = memoryDb.prepare(
      `UPDATE memories SET access_count = access_count + 1, last_accessed = unixepoch() WHERE key = ?`
    );
    for (const r of rows) {
      updateStmt.run(r.key);
    }

    const result = rows.map((r) =>
      `## ${r.key}${r.tags ? ` [${r.tags}]` : ""}\n${r.content}\n_(updated: ${new Date(r.updated_at * 1000).toISOString()}, accessed: ${r.access_count + 1}x)_`
    ).join("\n\n");
    return { content: [{ type: "text" as const, text: result }] };
  }
);

server.tool(
  "memory_list",
  "List all stored memory keys with their tags, timestamps, and access counts.",
  {},
  async () => {
    console.error(`[mcp-memory] list`);
    const stmt = memoryDb.prepare(
      `SELECT key, tags, access_count, created_at, updated_at FROM memories ORDER BY updated_at DESC`
    );
    const rows = stmt.all() as Array<{
      key: string; tags: string; access_count: number; created_at: number; updated_at: number;
    }>;
    if (rows.length === 0) {
      return { content: [{ type: "text" as const, text: "No memories stored yet." }] };
    }
    const lines = rows.map((r) =>
      `- ${r.key}${r.tags ? ` [${r.tags}]` : ""} (updated: ${new Date(r.updated_at * 1000).toISOString()}, accessed: ${r.access_count}x)`
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
  "Search and delete all memories matching a query. Uses full-text search across keys, content, and tags.",
  {
    query: z.string().describe("Keyword or phrase — all matching memories will be deleted"),
  },
  async ({ query }) => {
    console.error(`[mcp-memory] forget query=${query}`);

    // Try FTS5 first for matching, fall back to LIKE
    let deletedCount: number;
    try {
      // Find matching IDs via FTS5, then delete from main table (triggers handle FTS cleanup)
      const matchStmt = memoryDb.prepare(
        `SELECT m.id FROM memories_fts f JOIN memories m ON m.id = f.rowid WHERE memories_fts MATCH ?`
      );
      const ids = (matchStmt.all(query) as Array<{ id: number }>).map((r) => r.id);
      if (ids.length === 0) {
        return { content: [{ type: "text" as const, text: `No memories found matching "${query}".` }] };
      }
      const deleteStmt = memoryDb.prepare(`DELETE FROM memories WHERE id = ?`);
      for (const id of ids) {
        deleteStmt.run(id);
      }
      deletedCount = ids.length;
    } catch {
      // FTS5 query syntax error — fall back to LIKE
      const pattern = `%${query}%`;
      const stmt = memoryDb.prepare(
        `DELETE FROM memories WHERE key LIKE ? OR content LIKE ? OR tags LIKE ?`
      );
      const result = stmt.run(pattern, pattern, pattern);
      deletedCount = result.changes;
    }

    return {
      content: [{ type: "text" as const, text: `Deleted ${deletedCount} memory(ies) matching "${query}".` }],
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
