import { connect, StringCodec } from "nats";

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

function parseArgs(args: string[]): Record<string, string> {
  const result: Record<string, string> = {};
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg.startsWith("--") && i + 1 < args.length) {
      result[arg.slice(2)] = args[++i];
    }
  }
  return result;
}

async function main(): Promise<void> {
  const [command, ...rest] = process.argv.slice(2);

  if (!command) {
    console.log("Usage:");
    console.log(
      '  node task-cli.js create --name "..." --schedule "..." --prompt "..."'
    );
    console.log("  node task-cli.js list");
    console.log('  node task-cli.js delete --id "..."');
    process.exit(1);
  }

  switch (command) {
    case "create": {
      const args = parseArgs(rest);
      if (!args.name || !args.schedule || !args.prompt) {
        console.error("Error: --name, --schedule, and --prompt are required");
        process.exit(1);
      }
      const resp = await sendIPC("create_task", {
        name: args.name,
        schedule: args.schedule,
        prompt: args.prompt,
      });
      if (resp.error) {
        console.error(`Error: ${resp.error}`);
        process.exit(1);
      }
      console.log(`Task created: ${resp.id}`);
      break;
    }

    case "list": {
      const resp = await sendIPC("list_tasks", {});
      if (resp.error) {
        console.error(`Error: ${resp.error}`);
        process.exit(1);
      }
      if (!resp.tasks || resp.tasks.length === 0) {
        console.log("No tasks found.");
      } else {
        for (const t of resp.tasks) {
          console.log(`  ${t.id}  ${t.status}  ${t.name}  [${t.schedule}]`);
        }
      }
      break;
    }

    case "delete": {
      const args = parseArgs(rest);
      if (!args.id) {
        console.error("Error: --id is required");
        process.exit(1);
      }
      const resp = await sendIPC("delete_task", { id: args.id });
      if (resp.error) {
        console.error(`Error: ${resp.error}`);
        process.exit(1);
      }
      console.log("Task deleted.");
      break;
    }

    default:
      console.error(`Unknown command: ${command}`);
      process.exit(1);
  }
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
