import { useState, useEffect } from 'react';

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

const badge = (color: string, bg: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 600,
  background: bg,
  color,
});

function Agents() {
  const [agents, setAgents] = useState<Agent[]>([]);
  const [selected, setSelected] = useState<Agent | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch('/api/agents/definitions')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setAgents(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  return (
    <div>
      <h1 style={{ fontSize: 26, fontWeight: 700, marginBottom: 28, color: 'var(--text-primary)' }}>Agents</h1>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16, cursor: 'default' }}>
          Failed to load agents: {error}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 }}>
        {agents.map((agent) => (
          <div
            key={agent.id}
            style={{
              ...card,
              borderColor: selected?.id === agent.id ? 'var(--accent)' : 'var(--border)',
            }}
            onClick={() => setSelected(selected?.id === agent.id ? null : agent)}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <span style={{ fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>{agent.name}</span>
              {agent.agent_status && (
                <span style={badge(
                  agent.agent_status === 'running' ? 'var(--green)' : 'var(--text-secondary)',
                  agent.agent_status === 'running' ? 'var(--green-muted)' : 'var(--accent-muted)',
                )}>
                  {agent.agent_status}
                </span>
              )}
            </div>
            {agent.description && (
              <div style={{ fontSize: 13, color: 'var(--text-tertiary)', marginBottom: 4 }}>{agent.description}</div>
            )}
            {agent.model && (
              <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 4 }}>Model: {agent.model}</div>
            )}
            <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, color: 'var(--text-tertiary)' }}>
              <span>{agent.message_count ?? 0} messages</span>
              {agent.last_active && <span>{agent.last_active}</span>}
            </div>
          </div>
        ))}
      </div>

      {agents.length === 0 && !error && (
        <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>No agents found</div>
      )}

      {selected && (
        <div style={{ marginTop: 28 }}>
          <div style={{ ...card, cursor: 'default' }}>
            <h2 style={{ fontSize: 20, fontWeight: 600, marginBottom: 16, color: 'var(--accent)' }}>
              {selected.name}
            </h2>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, fontSize: 14 }}>
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
        </div>
      )}
    </div>
  );
}

export default Agents;
