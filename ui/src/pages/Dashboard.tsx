import { useState, useEffect } from 'react';

interface StatusData {
  version?: string;
  uptime?: string;
  active_agents?: number;
  groups_count?: number;
  pending_tasks?: number;
  recent_messages?: { id: string; group: string; role: string; text: string; time: string }[];
}

const card: React.CSSProperties = {
  background: '#141414',
  border: '1px solid #1e1e1e',
  borderRadius: 12,
  padding: 24,
};

const statValue: React.CSSProperties = {
  fontSize: 36,
  fontWeight: 700,
  color: '#6366f1',
};

const statLabel: React.CSSProperties = {
  fontSize: 13,
  color: '#666',
  marginTop: 4,
};

function Dashboard() {
  const [status, setStatus] = useState<StatusData | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch('/api/status')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then(setStatus)
      .catch((err) => setError(err.message));
  }, []);

  if (error) {
    return (
      <div>
        <h1 style={{ fontSize: 28, fontWeight: 700, marginBottom: 24 }}>Dashboard</h1>
        <div style={{ ...card, color: '#f87171' }}>
          Failed to load status: {error}
        </div>
      </div>
    );
  }

  if (!status) {
    return (
      <div>
        <h1 style={{ fontSize: 28, fontWeight: 700, marginBottom: 24 }}>Dashboard</h1>
        <div style={{ color: '#666' }}>Loading...</div>
      </div>
    );
  }

  const stats = [
    { label: 'Active Agents', value: status.active_agents ?? 0 },
    { label: 'Groups', value: status.groups_count ?? 0 },
    { label: 'Pending Tasks', value: status.pending_tasks ?? 0 },
  ];

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 32 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700 }}>Dashboard</h1>
        {status.version && (
          <span style={{ fontSize: 12, color: '#666', background: '#1a1a1a', padding: '4px 12px', borderRadius: 6 }}>
            v{status.version}
          </span>
        )}
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16, marginBottom: 32 }}>
        {stats.map((s) => (
          <div key={s.label} style={card}>
            <div style={statValue}>{s.value}</div>
            <div style={statLabel}>{s.label}</div>
          </div>
        ))}
      </div>

      {status.uptime && (
        <div style={{ ...card, marginBottom: 24 }}>
          <div style={{ fontSize: 13, color: '#666', marginBottom: 4 }}>Uptime</div>
          <div style={{ fontSize: 16 }}>{status.uptime}</div>
        </div>
      )}

      <div style={card}>
        <h2 style={{ fontSize: 18, fontWeight: 600, marginBottom: 16 }}>Recent Messages</h2>
        {(!status.recent_messages || status.recent_messages.length === 0) ? (
          <div style={{ color: '#555', fontSize: 14 }}>No recent messages</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {status.recent_messages.map((msg) => (
              <div key={msg.id} style={{ padding: '10px 14px', background: '#1a1a1a', borderRadius: 8, fontSize: 14 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4 }}>
                  <span>
                    <span style={{ color: '#6366f1', fontWeight: 600 }}>{msg.role}</span>
                    <span style={{ color: '#555', margin: '0 8px' }}>in</span>
                    <span style={{ color: '#888' }}>{msg.group}</span>
                  </span>
                  <span style={{ color: '#555', fontSize: 12 }}>{msg.time}</span>
                </div>
                <div style={{ color: '#ccc' }}>{msg.text}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

export default Dashboard;
