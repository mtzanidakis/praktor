import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { connect, StringCodec } from "nats";
import { z } from "zod";

const sc = StringCodec();
const NATS_URL = process.env.NATS_URL || "nats://localhost:4222";
const AGENT_ID = process.env.AGENT_ID || process.env.GROUP_ID || "default";

interface IPCResponse {
  ok?: boolean;
  error?: string;
  id?: string;
  content?: string;
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
  const topic = `host.ipc.${AGENT_ID}`;
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
  "Create a scheduled task (recurring or one-off). The prompt is sent verbatim to an agent each time the task fires — write it as an instruction (e.g. 'Reply with: Hello!'). One-off tasks are automatically paused after execution.",
  {
    name: z.string().describe("Task name"),
    schedule: z
      .string()
      .describe(
        `Cron expression or preset tag. All times are in the system's LOCAL timezone (set via TZ env var) — do NOT convert to UTC.

Supported formats:
- 5-field cron: "minute hour day month weekday" (e.g. "0 9 * * *" for daily at 9am, "*/5 * * * *" for every 5 min)
- 6-field cron with year (one-off): "minute hour day month weekday year" (e.g. "20 10 17 2 * 2026" for Feb 17 2026 at 10:20)
- Preset tags: @yearly, @monthly, @weekly, @daily, @hourly, @5minutes, @10minutes, @15minutes, @30minutes
- Month names: JAN, FEB, MAR, APR, MAY, JUN, JUL, AUG, SEP, OCT, NOV, DEC
- Weekday names: SUN, MON, TUE, WED, THU, FRI, SAT
- Modifiers: L (last day), W (nearest weekday), # (nth weekday, e.g. 1#2 = second Monday)
- JSON schedule: {"kind":"interval","interval_ms":60000} or {"kind":"once","at_ms":1739793600000}`
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
  "List all scheduled tasks for this agent.",
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
  console.error("MCP server error:", err);
  process.exit(1);
});
