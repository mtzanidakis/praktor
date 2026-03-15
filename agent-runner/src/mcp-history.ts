import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { sendIPC } from "./ipc.js";

const server = new McpServer({
  name: "praktor-history",
  version: "1.0.0",
});

server.tool(
  "history_search",
  "Search your conversation history with the user. Uses full-text search across all past messages (both user and assistant). Returns matching messages ranked by relevance.",
  {
    query: z.string().describe("Search query (supports FTS5 syntax: words, phrases in quotes, OR, NOT)"),
    limit: z.number().optional().describe("Max results to return (default: 20)"),
  },
  async ({ query, limit }) => {
    console.error(`[mcp-history] search query=${query} limit=${limit || 20}`);
    const resp = await sendIPC("search_history", { query, limit: limit || 20 });
    if (resp.error) {
      return { content: [{ type: "text" as const, text: `Search error: ${resp.error}` }] };
    }
    const messages = (resp as any).messages as Array<{
      sender: string; content: string; created_at: string;
    }>;
    if (!messages || messages.length === 0) {
      return { content: [{ type: "text" as const, text: "No messages found matching that query." }] };
    }
    const result = messages.map((m) => {
      const role = m.sender === "agent" ? "Assistant" : "User";
      return `**${role}** (${m.created_at}):\n${m.content}`;
    }).join("\n\n---\n\n");
    return { content: [{ type: "text" as const, text: result }] };
  }
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP history server error:", err);
  process.exit(1);
});
