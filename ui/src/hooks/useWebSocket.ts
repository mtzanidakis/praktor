import { useState, useEffect, useRef, useCallback } from 'react';

interface WsEvent {
  type: string;
  group_id?: string;
  data: unknown;
  timestamp: string;
}

type ConnectionStatus = 'connecting' | 'connected' | 'disconnected';

export function useWebSocket() {
  const [events, setEvents] = useState<WsEvent[]>([]);
  const [status, setStatus] = useState<ConnectionStatus>('disconnected');
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout>>();

  const connect = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) return;

    setStatus('connecting');
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/ws`);
    wsRef.current = ws;

    ws.onopen = () => {
      setStatus('connected');
    };

    ws.onmessage = (evt) => {
      try {
        const event: WsEvent = JSON.parse(evt.data);
        setEvents((prev) => [...prev.slice(-500), event]);
      } catch {
        // ignore malformed messages
      }
    };

    ws.onclose = () => {
      setStatus('disconnected');
      wsRef.current = null;
      reconnectTimer.current = setTimeout(connect, 3000);
    };

    ws.onerror = () => {
      ws.close();
    };
  }, []);

  useEffect(() => {
    connect();
    return () => {
      clearTimeout(reconnectTimer.current);
      wsRef.current?.close();
    };
  }, [connect]);

  const clearEvents = useCallback(() => setEvents([]), []);

  return { events, status, clearEvents };
}
