import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { createRequire } from "node:module";
import { DatabaseSync } from "node:sqlite";
import { z } from "zod";

// Use CJS require for @huggingface/transformers — its ESM exports don't resolve on Node 24 Alpine
const _require = createRequire(import.meta.url);

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
  "ALTER TABLE memories ADD COLUMN embedding BLOB",
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

// --- Embedding support ---

type Pipeline = (text: string, opts?: { pooling: string; normalize: boolean }) => Promise<{ data: Float32Array }>;
let embeddingPipeline: Pipeline | null = null;
let embeddingFailed = false;

async function getEmbeddingPipeline(): Promise<Pipeline | null> {
  if (embeddingFailed) return null;
  if (embeddingPipeline) return embeddingPipeline;

  try {
    const { pipeline, env } = _require("@huggingface/transformers");
    env.cacheDir = process.env.TRANSFORMERS_CACHE || "/opt/models";
    embeddingPipeline = await pipeline("feature-extraction", "Xenova/all-MiniLM-L6-v2", {
      dtype: "q8",
      cache_dir: env.cacheDir,
      local_files_only: true,
    }) as unknown as Pipeline;
    console.error("[mcp-memory] embedding model loaded");
    return embeddingPipeline;
  } catch (err) {
    console.error("[mcp-memory] embedding model failed to load, falling back to FTS-only:", err);
    embeddingFailed = true;
    return null;
  }
}

async function computeEmbedding(text: string): Promise<Float32Array | null> {
  const pipe = await getEmbeddingPipeline();
  if (!pipe) return null;
  const result = await pipe(text, { pooling: "mean", normalize: true });
  return result.data;
}

function cosineSimilarity(a: Float32Array, b: Float32Array): number {
  let dot = 0;
  for (let i = 0; i < a.length; i++) {
    dot += a[i] * b[i];
  }
  return dot; // vectors are already normalized
}

function storeEmbeddingAsync(key: string, content: string, tags: string): void {
  const text = `${key} ${tags} ${content}`;
  computeEmbedding(text).then((embedding) => {
    if (!embedding) return;
    const stmt = memoryDb.prepare(`UPDATE memories SET embedding = ? WHERE key = ?`);
    stmt.run(Buffer.from(embedding.buffer), key);
    console.error(`[mcp-memory] embedding stored for key=${key}`);
  }).catch((err) => {
    console.error(`[mcp-memory] embedding failed for key=${key}:`, err);
  });
}

// --- MCP Server ---

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

    // Compute and store embedding async (don't block response)
    storeEmbeddingAsync(key, content, tags || "");

    return {
      content: [{ type: "text" as const, text: `Memory stored: ${key}` }],
    };
  }
);

server.tool(
  "memory_recall",
  "Search memories by keyword or natural language. Uses hybrid search combining full-text keyword matching with semantic similarity for best results. Returns the most relevant memories first.",
  {
    query: z.string().describe("Search query — use natural language for semantic search, or keywords/phrases for exact matching"),
  },
  async ({ query }) => {
    console.error(`[mcp-memory] recall query=${query}`);

    type MemoryRow = {
      id: number; key: string; content: string; tags: string;
      access_count: number; created_at: number; updated_at: number;
      embedding: Buffer | null;
    };

    // 1. FTS5 keyword search
    let ftsResults: Array<{ key: string; rank: number }> = [];
    try {
      const stmt = memoryDb.prepare(
        `SELECT m.key, f.rank
         FROM memories_fts f
         JOIN memories m ON m.id = f.rowid
         WHERE memories_fts MATCH ?
         ORDER BY f.rank`
      );
      ftsResults = stmt.all(query) as typeof ftsResults;
    } catch {
      // FTS5 query syntax error — try LIKE fallback for FTS ranking
      const pattern = `%${query}%`;
      const stmt = memoryDb.prepare(
        `SELECT key, 0 as rank FROM memories
         WHERE key LIKE ? OR content LIKE ? OR tags LIKE ?
         ORDER BY updated_at DESC`
      );
      ftsResults = stmt.all(pattern, pattern, pattern) as typeof ftsResults;
    }

    // 2. Vector search
    let vecResults: Array<{ key: string; similarity: number }> = [];
    const queryEmbedding = await computeEmbedding(query);
    if (queryEmbedding) {
      const allRows = memoryDb.prepare(
        `SELECT key, embedding FROM memories WHERE embedding IS NOT NULL`
      ).all() as Array<{ key: string; embedding: Buffer }>;

      for (const row of allRows) {
        const emb = new Float32Array(row.embedding.buffer, row.embedding.byteOffset, row.embedding.byteLength / 4);
        const sim = cosineSimilarity(queryEmbedding, emb);
        vecResults.push({ key: row.key, similarity: sim });
      }
      vecResults.sort((a, b) => b.similarity - a.similarity);
    }

    // 3. Reciprocal Rank Fusion (k=60)
    const K = 60;
    const scores = new Map<string, number>();

    for (let i = 0; i < ftsResults.length; i++) {
      const key = ftsResults[i].key;
      scores.set(key, (scores.get(key) || 0) + 1 / (K + i + 1));
    }
    for (let i = 0; i < vecResults.length; i++) {
      const key = vecResults[i].key;
      scores.set(key, (scores.get(key) || 0) + 1 / (K + i + 1));
    }

    // If no results from either source
    if (scores.size === 0) {
      return { content: [{ type: "text" as const, text: "No memories found matching that query." }] };
    }

    // Sort by RRF score descending
    const rankedKeys = [...scores.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 20)
      .map(([key]) => key);

    // Fetch full rows for ranked keys
    const placeholders = rankedKeys.map(() => "?").join(",");
    const rows = memoryDb.prepare(
      `SELECT key, content, tags, access_count, created_at, updated_at
       FROM memories WHERE key IN (${placeholders})`
    ).all(...rankedKeys) as Array<{
      key: string; content: string; tags: string;
      access_count: number; created_at: number; updated_at: number;
    }>;

    // Maintain RRF order
    const rowMap = new Map(rows.map((r) => [r.key, r]));
    const orderedRows = rankedKeys.map((k) => rowMap.get(k)).filter(Boolean) as typeof rows;

    // Update access tracking
    const updateStmt = memoryDb.prepare(
      `UPDATE memories SET access_count = access_count + 1, last_accessed = unixepoch() WHERE key = ?`
    );
    for (const r of orderedRows) {
      updateStmt.run(r.key);
    }

    const result = orderedRows.map((r) =>
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

// Lazy backfill: compute embeddings for memories missing them
async function backfillEmbeddings(): Promise<void> {
  const rows = memoryDb.prepare(
    `SELECT key, content, tags FROM memories WHERE embedding IS NULL`
  ).all() as Array<{ key: string; content: string; tags: string }>;

  if (rows.length === 0) return;

  console.error(`[mcp-memory] backfilling embeddings for ${rows.length} memories`);
  const pipe = await getEmbeddingPipeline();
  if (!pipe) return;

  const updateStmt = memoryDb.prepare(`UPDATE memories SET embedding = ? WHERE key = ?`);
  for (const row of rows) {
    try {
      const text = `${row.key} ${row.tags} ${row.content}`;
      const embedding = await computeEmbedding(text);
      if (embedding) {
        updateStmt.run(Buffer.from(embedding.buffer), row.key);
      }
    } catch (err) {
      console.error(`[mcp-memory] backfill failed for key=${row.key}:`, err);
    }
  }
  console.error(`[mcp-memory] backfill complete`);
}

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);

  // Start backfill after 2s delay to avoid blocking startup
  setTimeout(() => backfillEmbeddings().catch((err) =>
    console.error("[mcp-memory] backfill error:", err)
  ), 2000);
}

main().catch((err) => {
  console.error("MCP memory server error:", err);
  process.exit(1);
});
