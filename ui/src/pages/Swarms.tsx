import { useState, useEffect, useCallback } from 'react';

interface SwarmAgent {
  role: string;
  status: string;
}

interface Swarm {
  id: string;
  name: string;
  status: string;
  objective?: string;
  agents?: SwarmAgent[];
  created_at?: string;
  completed_at?: string;
}

interface SwarmForm {
  name: string;
  objective: string;
  agent_roles: string;
}

const emptyForm: SwarmForm = { name: '', objective: '', agent_roles: '' };

const card: React.CSSProperties = {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: 20,
  boxShadow: 'var(--shadow)',
};

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 12px',
  borderRadius: 7,
  border: '1px solid var(--border)',
  background: 'var(--bg-input)',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
};

const btnPrimary: React.CSSProperties = {
  padding: '8px 20px',
  borderRadius: 7,
  border: 'none',
  background: 'var(--accent)',
  color: '#fff',
  fontSize: 13,
  fontWeight: 600,
  cursor: 'pointer',
};

const statusColors: Record<string, { color: string; bg: string }> = {
  running: { color: 'var(--green)', bg: 'var(--green-muted)' },
  completed: { color: 'var(--accent)', bg: 'var(--accent-muted)' },
  failed: { color: 'var(--red)', bg: 'var(--red-muted)' },
  pending: { color: 'var(--amber)', bg: 'var(--amber-muted)' },
};

function Swarms() {
  const [swarms, setSwarms] = useState<Swarm[]>([]);
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState<SwarmForm>(emptyForm);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const fetchSwarms = useCallback(() => {
    fetch('/api/swarms')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setSwarms(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  useEffect(() => {
    fetchSwarms();
    const interval = setInterval(fetchSwarms, 5000);
    return () => clearInterval(interval);
  }, [fetchSwarms]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      const body = {
        name: form.name,
        objective: form.objective,
        agent_roles: form.agent_roles.split(',').map((r) => r.trim()).filter(Boolean),
      };
      const res = await fetch('/api/swarms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setForm(emptyForm);
      setShowForm(false);
      fetchSwarms();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 24, fontWeight: 700, color: 'var(--text-primary)' }}>Swarms</h1>
        <button
          style={btnPrimary}
          onClick={() => { setForm(emptyForm); setShowForm(!showForm); }}
        >
          {showForm ? 'Cancel' : '+ New Swarm'}
        </button>
      </div>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleCreate} style={{ ...card, marginBottom: 20 }}>
          <h3 style={{ fontSize: 15, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' }}>Launch New Swarm</h3>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div>
              <label style={{ fontSize: 12, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Name</label>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Research Team"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 12, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Objective</label>
              <textarea
                style={{ ...inputStyle, minHeight: 80, resize: 'vertical' }}
                value={form.objective}
                onChange={(e) => setForm({ ...form, objective: e.target.value })}
                placeholder="Describe what this swarm should accomplish..."
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 12, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>
                Agent Roles (comma-separated)
              </label>
              <input
                style={inputStyle}
                value={form.agent_roles}
                onChange={(e) => setForm({ ...form, agent_roles: e.target.value })}
                placeholder="researcher, writer, reviewer"
              />
            </div>
            <button type="submit" style={{ ...btnPrimary, alignSelf: 'flex-start' }}>
              Launch Swarm
            </button>
          </div>
        </form>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {swarms.map((swarm) => {
          const isExpanded = expanded === swarm.id;
          const sc = statusColors[swarm.status] ?? { color: 'var(--text-tertiary)', bg: 'var(--accent-muted)' };
          return (
            <div key={swarm.id} style={card}>
              <div
                style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', cursor: 'pointer' }}
                onClick={() => setExpanded(isExpanded ? null : swarm.id)}
              >
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
                    <span style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)' }}>{swarm.name}</span>
                    <span
                      style={{
                        fontSize: 11,
                        padding: '2px 8px',
                        borderRadius: 999,
                        background: sc.bg,
                        color: sc.color,
                        fontWeight: 600,
                      }}
                    >
                      {swarm.status}
                    </span>
                  </div>
                  {swarm.objective && (
                    <div style={{ fontSize: 12, color: 'var(--text-secondary)', maxWidth: 600 }}>
                      {swarm.objective.length > 120 ? swarm.objective.slice(0, 120) + '...' : swarm.objective}
                    </div>
                  )}
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 6, display: 'flex', gap: 16 }}>
                    {swarm.created_at && <span>Created: {swarm.created_at}</span>}
                    {swarm.completed_at && <span>Completed: {swarm.completed_at}</span>}
                    {swarm.agents && <span>{swarm.agents.length} agent(s)</span>}
                  </div>
                </div>
                <span style={{
                  color: 'var(--text-tertiary)',
                  fontSize: 14,
                  transform: isExpanded ? 'rotate(90deg)' : 'none',
                  transition: 'transform 0.15s',
                }}>
                  {'\u25B6'}
                </span>
              </div>

              {isExpanded && swarm.agents && swarm.agents.length > 0 && (
                <div style={{ marginTop: 16, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
                  <h4 style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 10 }}>Agents</h4>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 8 }}>
                    {swarm.agents.map((agent, i) => {
                      const ac = statusColors[agent.status] ?? { color: 'var(--text-tertiary)', bg: 'var(--accent-muted)' };
                      return (
                        <div
                          key={i}
                          style={{
                            padding: '10px 14px',
                            background: 'var(--bg-elevated)',
                            borderRadius: 8,
                            fontSize: 12,
                            display: 'flex',
                            justifyContent: 'space-between',
                            alignItems: 'center',
                          }}
                        >
                          <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{agent.role}</span>
                          <span
                            style={{
                              fontSize: 11,
                              padding: '1px 6px',
                              borderRadius: 999,
                              background: ac.bg,
                              color: ac.color,
                            }}
                          >
                            {agent.status}
                          </span>
                        </div>
                      );
                    })}
                  </div>
                </div>
              )}
            </div>
          );
        })}
        {swarms.length === 0 && !error && (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 13 }}>No swarm runs yet</div>
        )}
      </div>
    </div>
  );
}

export default Swarms;
