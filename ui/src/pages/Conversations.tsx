import { useState, useEffect, useRef } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';

interface Group {
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
  background: '#141414',
  border: '1px solid #1e1e1e',
  borderRadius: 12,
};

function Conversations() {
  const [groups, setGroups] = useState<Group[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [loadingMessages, setLoadingMessages] = useState(false);
  const { events, status: wsStatus } = useWebSocket();
  const messagesEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    fetch('/api/groups')
      .then((res) => res.json())
      .then((data) => {
        const g = Array.isArray(data) ? data : [];
        setGroups(g);
        if (g.length > 0 && !selectedGroupId) {
          setSelectedGroupId(g[0].id);
        }
      })
      .catch(() => {});
  }, [selectedGroupId]);

  useEffect(() => {
    if (!selectedGroupId) return;
    setLoadingMessages(true);
    fetch(`/api/groups/${selectedGroupId}/messages`)
      .then((res) => res.json())
      .then((data) => setMessages(Array.isArray(data) ? data : []))
      .catch(() => setMessages([]))
      .finally(() => setLoadingMessages(false));
  }, [selectedGroupId]);

  // Append live WebSocket events for the selected group
  useEffect(() => {
    if (!selectedGroupId) return;
    const relevant = events.filter(
      (e) => e.group_id === selectedGroupId && e.type === 'message'
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
  }, [events, selectedGroupId]);

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  const selectedGroup = groups.find((g) => g.id === selectedGroupId);

  const wsIndicator: React.CSSProperties = {
    width: 8,
    height: 8,
    borderRadius: '50%',
    background: wsStatus === 'connected' ? '#22c55e' : wsStatus === 'connecting' ? '#eab308' : '#ef4444',
    display: 'inline-block',
    marginRight: 6,
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 32 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700 }}>Conversations</h1>
        <div style={{ display: 'flex', alignItems: 'center', fontSize: 12, color: '#666' }}>
          <span style={wsIndicator} />
          {wsStatus}
        </div>
      </div>

      <div style={{ display: 'flex', gap: 16, height: 'calc(100vh - 140px)' }}>
        {/* Group list */}
        <div style={{ ...card, width: 220, padding: 8, overflowY: 'auto', flexShrink: 0 }}>
          {groups.map((group) => (
            <div
              key={group.id}
              onClick={() => setSelectedGroupId(group.id)}
              style={{
                padding: '10px 14px',
                borderRadius: 8,
                cursor: 'pointer',
                fontSize: 14,
                background: selectedGroupId === group.id ? '#6366f1' : 'transparent',
                color: selectedGroupId === group.id ? '#fff' : '#888',
                marginBottom: 2,
              }}
            >
              {group.name}
            </div>
          ))}
          {groups.length === 0 && (
            <div style={{ padding: 14, color: '#555', fontSize: 13 }}>No groups</div>
          )}
        </div>

        {/* Messages */}
        <div style={{ ...card, flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <div style={{ padding: '16px 20px', borderBottom: '1px solid #1e1e1e', fontWeight: 600 }}>
            {selectedGroup?.name ?? 'Select a group'}
          </div>
          <div style={{ flex: 1, overflowY: 'auto', padding: 20, display: 'flex', flexDirection: 'column', gap: 8 }}>
            {loadingMessages && <div style={{ color: '#666', fontSize: 14 }}>Loading...</div>}
            {!loadingMessages && messages.length === 0 && (
              <div style={{ color: '#555', fontSize: 14 }}>No messages yet</div>
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
                    borderRadius: 12,
                    background: isAssistant ? '#1a1a2e' : '#1e1e1e',
                    borderLeft: isAssistant ? '3px solid #6366f1' : 'none',
                    fontSize: 14,
                  }}
                >
                  <div style={{ fontSize: 11, color: '#666', marginBottom: 4 }}>
                    <span style={{ color: isAssistant ? '#6366f1' : '#888', fontWeight: 600 }}>{msg.role}</span>
                    {msg.time && <span style={{ marginLeft: 8 }}>{msg.time}</span>}
                  </div>
                  <div style={{ color: '#ddd', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{msg.text}</div>
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
