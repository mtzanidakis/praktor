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
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: 24,
  boxShadow: 'var(--shadow)',
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
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24, color: 'var(--text-primary)' }}>Dashboard</h1>
        <div style={{ ...card, color: 'var(--red-light)' }}>
          Failed to load status: {error}
        </div>
      </div>
    );
  }

  if (!status) {
    return (
      <div>
        <h1 style={{ fontSize: 24, fontWeight: 700, marginBottom: 24, color: 'var(--text-primary)' }}>Dashboard</h1>
        <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>Loading...</div>
      </div>
    );
  }

  const stats = [
    { label: 'Active Agents', value: status.active_agents ?? 0, color: 'var(--green)', bg: 'var(--green-muted)' },
    { label: 'Groups', value: status.groups_count ?? 0, color: 'var(--accent)', bg: 'var(--accent-muted)' },
    { label: 'Pending Tasks', value: status.pending_tasks ?? 0, color: 'var(--amber)', bg: 'var(--amber-muted)' },
  ];

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Dashboard</h1>
        {status.version && (
          <span style={{
            fontSize: 12,
            color: 'var(--text-tertiary)',
            background: 'var(--bg-elevated)',
            padding: '4px 12px',
            borderRadius: 6,
            border: '1px solid var(--border)',
          }}>
            v{status.version}
          </span>
        )}
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 16, marginBottom: 28 }}>
        {stats.map((s) => (
          <div key={s.label} style={card}>
            <div style={{
              fontSize: 36,
              fontWeight: 700,
              color: s.color,
              lineHeight: 1,
            }}>{s.value}</div>
            <div style={{ fontSize: 13, color: 'var(--text-secondary)', marginTop: 6 }}>{s.label}</div>
          </div>
        ))}
      </div>

      {status.uptime && (
        <div style={{ ...card, marginBottom: 20, display: 'flex', alignItems: 'center', gap: 12 }}>
          <div style={{
            width: 8,
            height: 8,
            borderRadius: '50%',
            background: 'var(--green)',
            flexShrink: 0,
          }} />
          <div>
            <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginBottom: 2 }}>Uptime</div>
            <div style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>{status.uptime}</div>
          </div>
        </div>
      )}

      <div style={card}>
        <h2 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' }}>Recent Messages</h2>
        {(!status.recent_messages || status.recent_messages.length === 0) ? (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 13 }}>No recent messages</div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
            {status.recent_messages.map((msg) => (
              <div key={msg.id} style={{
                padding: '10px 14px',
                background: 'var(--bg-elevated)',
                borderRadius: 8,
                fontSize: 13,
              }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 4, alignItems: 'center' }}>
                  <span>
                    <span style={{ color: 'var(--accent)', fontWeight: 600 }}>{msg.role}</span>
                    <span style={{ color: 'var(--text-muted)', margin: '0 6px' }}>in</span>
                    <span style={{ color: 'var(--text-secondary)' }}>{msg.group}</span>
                  </span>
                  <span style={{ color: 'var(--text-muted)', fontSize: 11 }}>{msg.time}</span>
                </div>
                <div style={{ color: 'var(--text-primary)' }}>{msg.text}</div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

export default Dashboard;
