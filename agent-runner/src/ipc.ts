import { connect, StringCodec } from "nats";

const sc = StringCodec();

const NATS_URL = process.env.NATS_URL || "nats://localhost:4222";
const AGENT_ID = process.env.AGENT_ID || process.env.GROUP_ID || "default";

export interface IPCResponse {
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

export async function sendIPC(
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
