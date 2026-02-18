import { useState, useEffect, useRef, useCallback } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';

interface Agent {
  id: string;
  name: string;
  description?: string;
  model?: string;
  image?: string;
  workspace?: string;
  agent_status?: string;
  message_count?: number;
  last_active?: string;
}

const card: React.CSSProperties = {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: 20,
  cursor: 'pointer',
  boxShadow: 'var(--shadow)',
};


function Agents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [selected, setSelected] = useState<Agent | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [agentMd, setAgentMd] = useState('');
  const [agentMdSaved, setAgentMdSaved] = useState(false);
  const [agentMdLoading, setAgentMdLoading] = useState(false);
  const [confirmStop, setConfirmStop] = useState<string | null>(null);
  const [starting, setStarting] = useState<string | null>(null);
  const { events } = useWebSocket();
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  const fetchAgents = useCallback(() => {
    fetch('/api/agents/definitions')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setAgents(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  useEffect(() => {
    fetchAgents();
  }, [fetchAgents]);

  // Re-fetch on relevant WebSocket events (debounced)
  useEffect(() => {
    if (events.length === 0) return;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(fetchAgents, 500);
  }, [events.length, fetchAgents]);

  useEffect(() => {
    if (!selected) return;
    setAgentMdLoading(true);
    fetch(`/api/agents/definitions/${selected.id}/agent-md`)
      .then((res) => res.json())
      .then((data) => setAgentMd(data.content || ''))
      .catch(() => setAgentMd(''))
      .finally(() => setAgentMdLoading(false));
  }, [selected?.id]);

  const startAgent = async (agentId: string) => {
    try {
      setStarting(agentId);
      const res = await fetch(`/api/agents/${agentId}/start`, { method: 'POST' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      fetchAgents();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start agent');
    } finally {
      setStarting(null);
    }
  };

  const stopAgent = async (agentId: string) => {
    try {
      const res = await fetch(`/api/agents/${agentId}/stop`, { method: 'POST' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setConfirmStop(null);
      fetchAgents();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to stop agent');
    }
  };

  const saveAgentMd = () => {
    if (!selected) return;
    fetch(`/api/agents/definitions/${selected.id}/agent-md`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content: agentMd }),
    }).then(() => {
      setAgentMdSaved(true);
      setTimeout(() => setAgentMdSaved(false), 2000);
    });
  };

  return (
    <div>
      <h1 style={{ fontSize: 28, fontWeight: 700, marginBottom: 28, color: 'var(--text-primary)' }}>Agents</h1>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16, cursor: 'default' }}>
          Failed to load agents: {error}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 }}>
        {agents.map((agent) => (
          <div
            key={agent.id}
            data-hover
            style={{
              ...card,
              borderColor: selected?.id === agent.id ? 'var(--accent)' : 'var(--border)',
            }}
            onClick={() => setSelected(selected?.id === agent.id ? null : agent)}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <span style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>{agent.name}</span>
              <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
                <button
                  data-agent-start={agent.agent_status !== 'running' ? '' : undefined}
                  onClick={agent.agent_status !== 'running' ? (e) => { e.stopPropagation(); startAgent(agent.id); } : undefined}
                  disabled={agent.agent_status === 'running' || starting === agent.id}
                  style={{
                    background: 'none',
                    border: 'none',
                    fontSize: 16,
                    cursor: agent.agent_status !== 'running' ? 'pointer' : 'default',
                    color: agent.agent_status === 'running' ? 'var(--green)' : 'var(--text-muted)',
                    padding: '2px 6px',
                    borderRadius: 4,
                    lineHeight: 1,
                  }}
                >
                  ▶
                </button>
                {confirmStop === agent.id ? (
                  <span style={{ display: 'flex', gap: 4 }}>
                    <button
                      onClick={(e) => { e.stopPropagation(); stopAgent(agent.id); }}
                      style={{
                        padding: '2px 10px',
                        borderRadius: 6,
                        border: '1px solid var(--red)',
                        background: 'var(--red-muted)',
                        color: 'var(--red)',
                        fontSize: 13,
                        fontWeight: 600,
                        cursor: 'pointer',
                      }}
                    >
                      confirm
                    </button>
                    <button
                      onClick={(e) => { e.stopPropagation(); setConfirmStop(null); }}
                      style={{
                        padding: '2px 10px',
                        borderRadius: 6,
                        border: '1px solid var(--border)',
                        background: 'transparent',
                        color: 'var(--text-secondary)',
                        fontSize: 13,
                        cursor: 'pointer',
                      }}
                    >
                      cancel
                    </button>
                  </span>
                ) : (
                  <button
                    data-agent-stop={agent.agent_status === 'running' ? '' : undefined}
                    onClick={agent.agent_status === 'running' ? (e) => { e.stopPropagation(); setConfirmStop(agent.id); } : undefined}
                    disabled={agent.agent_status !== 'running'}
                    style={{
                      background: 'none',
                      border: 'none',
                      fontSize: 16,
                      cursor: agent.agent_status === 'running' ? 'pointer' : 'default',
                      color: 'var(--text-muted)',
                      padding: '2px 6px',
                      borderRadius: 4,
                      lineHeight: 1,
                    }}
                  >
                    ■
                  </button>
                )}
              </div>
            </div>
            {agent.description && (
              <div style={{ fontSize: 15, color: 'var(--text-tertiary)', marginBottom: 4 }}>{agent.description}</div>
            )}
            {agent.model && (
              <div style={{ fontSize: 14, color: 'var(--text-muted)', marginBottom: 4 }}>Model: {agent.model}</div>
            )}
            <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 15, color: 'var(--text-tertiary)' }}>
              <span>{agent.message_count ?? 0} messages</span>
              {agent.last_active && <span>{agent.last_active}</span>}
            </div>
          </div>
        ))}
      </div>

      {agents.length === 0 && !error && (
        <div style={{ color: 'var(--text-tertiary)', fontSize: 16 }}>No agents found</div>
      )}

      {selected && (
        <div style={{ marginTop: 28 }}>
          <div style={{ ...card, cursor: 'default' }}>
            <h2 style={{ fontSize: 22, fontWeight: 600, marginBottom: 16, color: 'var(--accent)' }}>
              {selected.name}
            </h2>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, fontSize: 16 }}>
              <div>
                <span style={{ color: 'var(--text-tertiary)' }}>ID: </span>
                <span style={{ fontFamily: 'monospace', color: 'var(--text-secondary)' }}>{selected.id}</span>
              </div>
              {selected.description && (
                <div>
                  <span style={{ color: 'var(--text-tertiary)' }}>Description: </span>
                  <span style={{ color: 'var(--text-primary)' }}>{selected.description}</span>
                </div>
              )}
              {selected.model && (
                <div>
                  <span style={{ color: 'var(--text-tertiary)' }}>Model: </span>
                  <span style={{ color: 'var(--text-primary)' }}>{selected.model}</span>
                </div>
              )}
              {selected.workspace && (
                <div>
                  <span style={{ color: 'var(--text-tertiary)' }}>Workspace: </span>
                  <span style={{ color: 'var(--text-primary)' }}>{selected.workspace}</span>
                </div>
              )}
              <div>
                <span style={{ color: 'var(--text-tertiary)' }}>Agent Status: </span>
                <span style={{ color: 'var(--text-primary)' }}>{selected.agent_status ?? 'unknown'}</span>
              </div>
              <div>
                <span style={{ color: 'var(--text-tertiary)' }}>Messages: </span>
                <span style={{ color: 'var(--text-primary)' }}>{selected.message_count ?? 0}</span>
              </div>
              {selected.last_active && (
                <div>
                  <span style={{ color: 'var(--text-tertiary)' }}>Last Active: </span>
                  <span style={{ color: 'var(--text-primary)' }}>{selected.last_active}</span>
                </div>
              )}
            </div>

          </div>

          <div style={{ ...card, cursor: 'default', marginTop: 16 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
              <div>
                <h3 style={{ fontSize: 20, fontWeight: 600, margin: 0, color: 'var(--text-primary)' }}>
                  Agent Identity
                </h3>
                <p style={{ fontSize: 15, color: 'var(--text-secondary)', margin: '4px 0 0' }}>
                  Personal identity and instructions for this agent via AGENT.md
                </p>
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                {agentMdSaved && (
                  <span style={{ color: 'var(--green)', fontSize: 15, fontWeight: 500 }}>Saved</span>
                )}
                {!agentMdLoading && (
                  <button
                    onClick={saveAgentMd}
                    style={{
                      padding: '6px 18px',
                      background: 'var(--accent)',
                      color: '#fff',
                      border: 'none',
                      borderRadius: 6,
                      cursor: 'pointer',
                      fontSize: 15,
                      fontWeight: 600,
                    }}
                  >
                    Save
                  </button>
                )}
              </div>
            </div>
            {agentMdLoading ? (
              <div style={{ color: 'var(--text-tertiary)', fontSize: 15 }}>Loading...</div>
            ) : (
              <textarea
                value={agentMd}
                onChange={(e) => setAgentMd(e.target.value)}
                style={{
                  width: '100%',
                  minHeight: 180,
                  fontFamily: 'monospace',
                  fontSize: 15,
                  background: 'var(--bg-main)',
                  color: 'var(--text-primary)',
                  border: '1px solid var(--border)',
                  borderRadius: 6,
                  padding: 12,
                  resize: 'vertical',
                  boxSizing: 'border-box',
                }}
              />
            )}
          </div>
        </div>
      )}
    </div>
  );
}

export default Agents;
