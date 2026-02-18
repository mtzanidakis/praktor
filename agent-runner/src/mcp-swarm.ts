import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { sendIPC } from "./ipc.js";

const SWARM_CHAT_TOPIC = process.env.SWARM_CHAT_TOPIC || "";

const server = new McpServer({
  name: "praktor-swarm",
  version: "1.0.0",
});

if (SWARM_CHAT_TOPIC) {
  server.tool(
    "swarm_chat_send",
    "Send a message to your collaborative peers in the swarm. Use this to share findings, ask questions, or coordinate with other agents working on the same task.",
    {
      message: z.string().describe("The message to send to peer agents"),
    },
    async ({ message }) => {
      const resp = await sendIPC("swarm_message", { content: message });
      if (resp.error) {
        return {
          content: [
            { type: "text" as const, text: `Error: ${resp.error}` },
          ],
        };
      }
      return {
        content: [
          {
            type: "text" as const,
            text: "Message sent to collaborative peers.",
          },
        ],
      };
    }
  );
}

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP swarm server error:", err);
  process.exit(1);
});
