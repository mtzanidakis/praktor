import { useState, useEffect, useRef } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';

interface Agent {
  id: string;
  name: string;
}

interface Message {
  id: string;
  role: string;
  text: string;
  time: string;
}

const card: React.CSSProperties = {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  boxShadow: 'var(--shadow)',
};

function Conversations() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [selectedAgentId, setSelectedAgentId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [loadingMessages, setLoadingMessages] = useState(false);
  const { events, status: wsStatus } = useWebSocket();
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetch('/api/agents/definitions')
      .then((res) => res.json())
      .then((data) => {
        const a = Array.isArray(data) ? data : [];
        setAgents(a);
        if (a.length > 0 && !selectedAgentId) {
          setSelectedAgentId(a[0].id);
        }
      })
      .catch(() => {});
  }, [selectedAgentId]);

  useEffect(() => {
    if (!selectedAgentId) return;
    setLoadingMessages(true);
    fetch(`/api/agents/definitions/${selectedAgentId}/messages`)
      .then((res) => res.json())
      .then((data) => setMessages(Array.isArray(data) ? data : []))
      .catch(() => setMessages([]))
      .finally(() => setLoadingMessages(false));
  }, [selectedAgentId]);

  useEffect(() => {
    if (!selectedAgentId) return;
    const relevant = events.filter(
      (e) => e.agent_id === selectedAgentId && e.type === 'message'
    );
    if (relevant.length === 0) return;
    const latest = relevant[relevant.length - 1];
    const msg = latest.data as Message;
    if (msg && msg.id) {
      setMessages((prev) => {
        if (prev.some((m) => m.id === msg.id)) return prev;
        return [...prev, msg];
      });
    }
  }, [events, selectedAgentId]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const selectedAgent = agents.find((a) => a.id === selectedAgentId);

  const wsColor = wsStatus === 'connected' ? 'var(--green)' : wsStatus === 'connecting' ? 'var(--amber)' : 'var(--red)';

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 26, fontWeight: 700, color: 'var(--text-primary)' }}>Conversations</h1>
        <div style={{ display: 'flex', alignItems: 'center', fontSize: 13, color: 'var(--text-tertiary)', gap: 6 }}>
          <span style={{
            width: 7,
            height: 7,
            borderRadius: '50%',
            background: wsColor,
            display: 'inline-block',
          }} />
          {wsStatus}
        </div>
      </div>

      <div style={{ display: 'flex', gap: 16, height: 'calc(100vh - 140px)' }}>
        {/* Agent list */}
        <div style={{ ...card, width: 200, padding: 6, overflowY: 'auto', flexShrink: 0 }}>
          {agents.map((agent) => (
            <div
              key={agent.id}
              data-hover={selectedAgentId !== agent.id ? '' : undefined}
              onClick={() => setSelectedAgentId(agent.id)}
              style={{
                padding: '8px 12px',
                borderRadius: 7,
                cursor: 'pointer',
                fontSize: 14,
                fontWeight: selectedAgentId === agent.id ? 600 : 400,
                background: selectedAgentId === agent.id ? 'var(--accent)' : 'transparent',
                color: selectedAgentId === agent.id ? '#fff' : 'var(--text-secondary)',
                marginBottom: 1,
              }}
            >
              {agent.name}
            </div>
          ))}
          {agents.length === 0 && (
            <div style={{ padding: 12, color: 'var(--text-tertiary)', fontSize: 13 }}>No agents</div>
          )}
        </div>

        {/* Messages */}
        <div style={{ ...card, flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{
            padding: '14px 20px',
            borderBottom: '1px solid var(--border)',
            fontWeight: 600,
            fontSize: 15,
            color: 'var(--text-primary)',
          }}>
            {selectedAgent?.name ?? 'Select an agent'}
          </div>
          <div style={{ flex: 1, overflowY: 'auto', padding: 20, display: 'flex', flexDirection: 'column', gap: 8 }}>
            {loadingMessages && <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>Loading...</div>}
            {!loadingMessages && messages.length === 0 && (
              <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>No messages yet</div>
            )}
            {messages.map((msg) => {
              const isAssistant = msg.role === 'assistant';
              return (
                <div
                  key={msg.id}
                  style={{
                    alignSelf: isAssistant ? 'flex-start' : 'flex-end',
                    maxWidth: '75%',
                    padding: '10px 14px',
                    borderRadius: 10,
                    background: isAssistant ? 'var(--accent-muted)' : 'var(--bg-elevated)',
                    borderLeft: isAssistant ? '3px solid var(--accent)' : 'none',
                    fontSize: 14,
                  }}
                >
                  <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 4 }}>
                    <span style={{ color: isAssistant ? 'var(--accent)' : 'var(--text-secondary)', fontWeight: 600 }}>{msg.role}</span>
                    {msg.time && <span style={{ marginLeft: 8 }}>{msg.time}</span>}
                  </div>
                  <div style={{ color: 'var(--text-primary)', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{msg.text}</div>
                </div>
              );
            })}
            <div ref={messagesEndRef} />
          </div>
        </div>
      </div>
    </div>
  );
}

export default Conversations;
