import { useState, useEffect } from 'react';

interface Group {
  id: string;
  name: string;
  type?: string;
  agent_status?: string;
  message_count?: number;
  last_active?: string;
}

const card: React.CSSProperties = {
  background: '#141414',
  border: '1px solid #1e1e1e',
  borderRadius: 12,
  padding: 20,
  cursor: 'pointer',
  transition: 'border-color 0.15s ease',
};

const badge = (color: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 600,
  background: `${color}22`,
  color,
});

function Groups() {
  const [groups, setGroups] = useState<Group[]>([]);
  const [selected, setSelected] = useState<Group | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch('/api/groups')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setGroups(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  return (
    <div>
      <h1 style={{ fontSize: 28, fontWeight: 700, marginBottom: 32 }}>Groups</h1>

      {error && (
        <div style={{ ...card, color: '#f87171', marginBottom: 16, cursor: 'default' }}>
          Failed to load groups: {error}
        </div>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 }}>
        {groups.map((group) => (
          <div
            key={group.id}
            style={{
              ...card,
              borderColor: selected?.id === group.id ? '#6366f1' : '#1e1e1e',
            }}
            onClick={() => setSelected(selected?.id === group.id ? null : group)}
          >
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <span style={{ fontSize: 16, fontWeight: 600 }}>{group.name}</span>
              {group.agent_status && (
                <span style={badge(group.agent_status === 'running' ? '#22c55e' : '#888')}>
                  {group.agent_status}
                </span>
              )}
            </div>
            {group.type && (
              <div style={{ fontSize: 12, color: '#666', marginBottom: 4 }}>Type: {group.type}</div>
            )}
            <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13, color: '#555' }}>
              <span>{group.message_count ?? 0} messages</span>
              {group.last_active && <span>{group.last_active}</span>}
            </div>
          </div>
        ))}
      </div>

      {groups.length === 0 && !error && (
        <div style={{ color: '#555', fontSize: 14 }}>No groups found</div>
      )}

      {selected && (
        <div style={{ marginTop: 32 }}>
          <div style={{ ...card, cursor: 'default' }}>
            <h2 style={{ fontSize: 20, fontWeight: 600, marginBottom: 16, color: '#6366f1' }}>
              {selected.name}
            </h2>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, fontSize: 14 }}>
              <div>
                <span style={{ color: '#666' }}>ID: </span>
                <span style={{ fontFamily: 'monospace', color: '#aaa' }}>{selected.id}</span>
              </div>
              <div>
                <span style={{ color: '#666' }}>Type: </span>
                <span>{selected.type ?? 'unknown'}</span>
              </div>
              <div>
                <span style={{ color: '#666' }}>Agent Status: </span>
                <span>{selected.agent_status ?? 'unknown'}</span>
              </div>
              <div>
                <span style={{ color: '#666' }}>Messages: </span>
                <span>{selected.message_count ?? 0}</span>
              </div>
              {selected.last_active && (
                <div>
                  <span style={{ color: '#666' }}>Last Active: </span>
                  <span>{selected.last_active}</span>
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default Groups;
