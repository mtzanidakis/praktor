import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { sendIPC } from "./ipc.js";

const server = new McpServer({
  name: "praktor-profile",
  version: "1.0.0",
});

server.tool(
  "user_profile_read",
  "Read the HUMAN user's profile (USER.md) containing their name, preferences, and personal information. This is NOT your agent identity — your identity is in /workspace/agent/AGENT.md which you can read/edit directly.",
  {},
  async () => {
    const resp = await sendIPC("read_user_md", {});
    if (resp.error) {
      return { content: [{ type: "text" as const, text: `Error: ${resp.error}` }] };
    }
    return {
      content: [{ type: "text" as const, text: resp.content || "(No user profile set)" }],
    };
  }
);

server.tool(
  "user_profile_update",
  "Update the HUMAN user's profile (USER.md). Provide the full markdown content to replace the existing profile. Only use this when the user asks to change THEIR profile. Do NOT use this for your agent identity — edit /workspace/agent/AGENT.md directly instead.",
  {
    content: z.string().describe("Full markdown content for USER.md"),
  },
  async ({ content }) => {
    const resp = await sendIPC("update_user_md", { content });
    if (resp.error) {
      return { content: [{ type: "text" as const, text: `Error: ${resp.error}` }] };
    }
    return {
      content: [{ type: "text" as const, text: "User profile updated successfully." }],
    };
  }
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP profile server error:", err);
  process.exit(1);
});
