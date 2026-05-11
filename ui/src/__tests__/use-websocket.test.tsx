import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useWebSocket } from "../hooks/useWebSocket";

// Minimal mock that implements only the surface the hook touches.
class MockWebSocket {
  static OPEN = 1;
  static CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readyState = 0;
  url: string;
  onopen: (() => void) | null = null;
  onmessage: ((evt: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  message(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }

  rawMessage(data: string) {
    this.onmessage?.({ data });
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }
}

describe("useWebSocket", () => {
  const realWebSocket = globalThis.WebSocket;

  beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.instances = [];
    // @ts-expect-error — assign mock onto the global namespace
    globalThis.WebSocket = MockWebSocket;
  });

  afterEach(() => {
    vi.useRealTimers();
    globalThis.WebSocket = realWebSocket;
  });

  it("connects to /api/ws using ws:// for http pages", () => {
    const { unmount } = renderHook(() => useWebSocket());
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(MockWebSocket.instances[0].url).toBe(
      `ws://${window.location.host}/api/ws`
    );
    unmount();
  });

  it("starts in 'connecting' status and moves to 'connected' on open", () => {
    const { result } = renderHook(() => useWebSocket());
    expect(result.current.status).toBe("connecting");

    act(() => {
      MockWebSocket.instances[0].open();
    });
    expect(result.current.status).toBe("connected");
  });

  it("appends parsed events on incoming messages", () => {
    const { result } = renderHook(() => useWebSocket());
    act(() => MockWebSocket.instances[0].open());

    act(() => {
      MockWebSocket.instances[0].message({
        type: "agent_started",
        agent_id: "foo",
        data: { hello: "world" },
        timestamp: "2026-05-11T07:30:00Z",
      });
    });

    expect(result.current.events).toHaveLength(1);
    expect(result.current.events[0]).toMatchObject({
      type: "agent_started",
      agent_id: "foo",
    });
  });

  it("silently drops malformed JSON messages", () => {
    const { result } = renderHook(() => useWebSocket());
    act(() => MockWebSocket.instances[0].open());

    act(() => {
      MockWebSocket.instances[0].rawMessage("{not json");
    });

    expect(result.current.events).toHaveLength(0);
  });

  it("trims the event buffer to the last 500 entries", () => {
    const { result } = renderHook(() => useWebSocket());
    act(() => MockWebSocket.instances[0].open());

    act(() => {
      for (let i = 0; i < 510; i++) {
        MockWebSocket.instances[0].message({
          type: "tick",
          data: i,
          timestamp: "",
        });
      }
    });

    expect(result.current.events).toHaveLength(501);
    // Oldest entries trimmed: first remaining should be index >= 9
    expect(result.current.events[0]).toMatchObject({ data: 9 });
    expect(result.current.events[result.current.events.length - 1]).toMatchObject({ data: 509 });
  });

  it("clearEvents resets the buffer", () => {
    const { result } = renderHook(() => useWebSocket());
    act(() => MockWebSocket.instances[0].open());
    act(() =>
      MockWebSocket.instances[0].message({
        type: "x",
        data: 1,
        timestamp: "",
      })
    );
    expect(result.current.events).toHaveLength(1);

    act(() => result.current.clearEvents());
    expect(result.current.events).toHaveLength(0);
  });

  it("reconnects after the socket closes", () => {
    renderHook(() => useWebSocket());
    expect(MockWebSocket.instances).toHaveLength(1);

    act(() => MockWebSocket.instances[0].close());

    // Reconnect is scheduled via setTimeout(3000).
    act(() => {
      vi.advanceTimersByTime(3000);
    });

    expect(MockWebSocket.instances).toHaveLength(2);
  });

});
