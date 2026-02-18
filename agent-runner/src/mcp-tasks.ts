import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";
import { sendIPC } from "./ipc.js";

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
        `Schedule expression.

CRITICAL: All times use LOCAL timezone. NEVER convert to UTC. If user says "9:30" use hour=9 minute=30.

Supported formats:
- Relative delay: "+30s", "+5m", "+2h" (ALWAYS use this for "in X seconds/minutes/hours" requests)
- 5-field cron: "minute hour day month weekday" (e.g. "0 9 * * *" for daily at 9am local)
- 6-field cron with year (one-off): "minute hour day month weekday year" (e.g. "20 10 17 2 * 2026" for Feb 17 2026 at 10:20 local)
- 7-field cron with seconds: "second minute hour day month weekday year" (e.g. "0 30 9 17 2 * 2026" for Feb 17 2026 at 9:30:00 local)
- Preset tags: @yearly, @annually, @monthly, @weekly, @daily, @hourly, @5minutes, @10minutes, @15minutes, @30minutes, @always, @everysecond
- Month names: JAN-DEC, Weekday names: SUN-SAT
- Modifiers: L (last day), W (nearest weekday), # (nth weekday, e.g. 1#2 = second Monday)

IMPORTANT: For relative delays ("in 30 seconds", "in 5 minutes") ALWAYS use the +Ns/+Nm/+Nh format. Use cron only for absolute times and recurring schedules.`
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

async function main(): Promise<void> {
  const transport = new StdioServerTransport();
  await server.connect(transport);
}

main().catch((err) => {
  console.error("MCP tasks server error:", err);
  process.exit(1);
});
