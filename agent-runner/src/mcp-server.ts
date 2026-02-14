import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { connect, StringCodec } from "nats";
import { z } from "zod";

const sc = StringCodec();
const NATS_URL = process.env.NATS_URL || "nats://localhost:4222";
const GROUP_ID = process.env.GROUP_ID || "default";

interface IPCResponse {
  ok?: boolean;
  error?: string;
  id?: string;
  tasks?: Array<{
    id: string;
    name: string;
    schedule: string;
    prompt: string;
    status: string;
  }>;
}

async function sendIPC(
  type: string,
  payload: Record<string, unknown>
): Promise<IPCResponse> {
  const conn = await connect({ servers: NATS_URL });
  const topic = `host.ipc.${GROUP_ID}`;
  const data = sc.encode(JSON.stringify({ type, payload }));
  const resp = await conn.request(topic, data, { timeout: 10000 });
  const result: IPCResponse = JSON.parse(sc.decode(resp.data));
  await conn.drain();
  return result;
}

const server = new McpServer({
  name: "praktor-tasks",
  version: "1.0.0",
});

server.tool(
  "scheduled_task_create",
  "Create a recurring scheduled task. The prompt is sent verbatim to an agent each time the task fires — write it as an instruction (e.g. 'Reply with: Hello!').",
  {
    name: z.string().describe("Task name"),
    schedule: z
      .string()
      .describe(
        'Cron expression (e.g. "* * * * *" for every minute, "0 9 * * *" for daily at 9am) or JSON schedule'
      ),
    prompt: z
      .string()
      .describe(
        "Instruction sent to the agent when the task fires. Write as a directive, e.g. 'Reply to the user with: Hello!'"
      ),
  },
  async ({ name, schedule, prompt }) => {
    const resp = await sendIPC("create_task", { name, schedule, prompt });
    if (resp.error) {
      return { content: [{ type: "text" as const, text: `Error: ${resp.error}` }] };
    }
    return {
      content: [
        { type: "text" as const, text: `Task created successfully. ID: ${resp.id}` },
      ],
    };
  }
);

server.tool(
  "scheduled_task_list",
  "List all scheduled tasks for this group.",
  {},
  async () => {
    const resp = await sendIPC("list_tasks", {});
    if (resp.error) {
      return { content: [{ type: "text" as const, text: `Error: ${resp.error}` }] };
    }
    if (!resp.tasks || resp.tasks.length === 0) {
      return {
        content: [{ type: "text" as const, text: "No scheduled tasks found." }],
      };
    }
    const lines = resp.tasks.map(
      (t) => `- ${t.id} [${t.status}] "${t.name}" schedule=${t.schedule}`
    );
    return { content: [{ type: "text" as const, text: lines.join("\n") }] };
  }
);

server.tool(
  "scheduled_task_delete",
  "Delete a scheduled task by ID.",
  {
    id: z.string().describe("Task ID to delete"),
  },
  async ({ id }) => {
    const resp = await sendIPC("delete_task", { id });
    if (resp.error) {
      return { content: [{ type: "text" as const, text: `Error: ${resp.error}` }] };
    }
    return {
      content: [{ type: "text" as const, text: "Task deleted successfully." }],
    };
  }
);

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP server error:", err);
  process.exit(1);
});
