import { connect, Msg, NatsConnection, Subscription, StringCodec } from "nats";

const sc = StringCodec();

export class NatsBridge {
  private conn: NatsConnection | null = null;
  private subscriptions: Subscription[] = [];

  constructor(
    private url: string,
    private agentId: string
  ) {}

  async connect(): Promise<void> {
    this.conn = await connect({ servers: this.url });
    console.log(`[nats] connected to ${this.url}`);
  }

  async flush(): Promise<void> {
    if (!this.conn) throw new Error("Not connected to NATS");
    await this.conn.flush();
  }

  async publish(topic: string, data: Record<string, unknown>): Promise<void> {
    if (!this.conn) throw new Error("Not connected to NATS");
    this.conn.publish(topic, sc.encode(JSON.stringify(data)));
  }

  async publishOutput(content: string, type: string = "text"): Promise<void> {
    await this.publish(`agent.${this.agentId}.output`, { type, content });
  }

  async publishResult(content: string): Promise<void> {
    await this.publish(`agent.${this.agentId}.output`, {
      type: "result",
      content,
    });
  }

  async publishReady(): Promise<void> {
    await this.publish(`agent.${this.agentId}.ready`, { status: "ready" });
  }

  async publishIPC(command: string, payload: unknown): Promise<void> {
    await this.publish(`host.ipc.${this.agentId}`, {
      type: command,
      payload,
    });
  }

  subscribe(
    topic: string,
    handler: (data: Record<string, unknown>, msg: Msg) => void
  ): void {
    if (!this.conn) throw new Error("Not connected to NATS");

    const sub = this.conn.subscribe(topic);
    this.subscriptions.push(sub);

    (async () => {
      for await (const msg of sub) {
        try {
          const data = JSON.parse(sc.decode(msg.data));
          handler(data, msg);
        } catch (err) {
          console.error(`[nats] failed to parse message on ${topic}:`, err);
        }
      }
    })();
  }

  subscribeInput(handler: (data: Record<string, unknown>) => void): void {
    this.subscribe(`agent.${this.agentId}.input`, (data) => handler(data));
  }

  subscribeControl(
    handler: (data: Record<string, unknown>, msg: Msg) => void
  ): void {
    this.subscribe(`agent.${this.agentId}.control`, handler);
  }

  subscribeRoute(
    handler: (data: Record<string, unknown>, msg: Msg) => void
  ): void {
    this.subscribe(`agent.${this.agentId}.route`, handler);
  }

  async close(): Promise<void> {
    for (const sub of this.subscriptions) {
      sub.unsubscribe();
    }
    if (this.conn) {
      await this.conn.drain();
    }
  }
}
