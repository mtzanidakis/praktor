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
  background: '#141414',
  border: '1px solid #1e1e1e',
  borderRadius: 12,
  padding: 20,
};

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 12px',
  borderRadius: 8,
  border: '1px solid #2a2a2a',
  background: '#0a0a0a',
  color: '#e0e0e0',
  fontSize: 14,
  outline: 'none',
};

const btnPrimary: React.CSSProperties = {
  padding: '8px 20px',
  borderRadius: 8,
  border: 'none',
  background: '#6366f1',
  color: '#fff',
  fontSize: 14,
  fontWeight: 600,
  cursor: 'pointer',
};

const statusColors: Record<string, string> = {
  running: '#22c55e',
  completed: '#6366f1',
  failed: '#ef4444',
  pending: '#eab308',
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
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 32 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700 }}>Swarms</h1>
        <button
          style={btnPrimary}
          onClick={() => { setForm(emptyForm); setShowForm(!showForm); }}
        >
          {showForm ? 'Cancel' : '+ New Swarm'}
        </button>
      </div>

      {error && (
        <div style={{ ...card, color: '#f87171', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleCreate} style={{ ...card, marginBottom: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>Launch New Swarm</h3>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <div>
              <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>Name</label>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Research Team"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>Objective</label>
              <textarea
                style={{ ...inputStyle, minHeight: 80, resize: 'vertical' }}
                value={form.objective}
                onChange={(e) => setForm({ ...form, objective: e.target.value })}
                placeholder="Describe what this swarm should accomplish..."
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>
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

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {swarms.map((swarm) => {
          const isExpanded = expanded === swarm.id;
          const color = statusColors[swarm.status] ?? '#666';
          return (
            <div key={swarm.id} style={card}>
              <div
                style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', cursor: 'pointer' }}
                onClick={() => setExpanded(isExpanded ? null : swarm.id)}
              >
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
                    <span style={{ fontSize: 16, fontWeight: 600 }}>{swarm.name}</span>
                    <span
                      style={{
                        fontSize: 11,
                        padding: '2px 8px',
                        borderRadius: 999,
                        background: `${color}22`,
                        color,
                        fontWeight: 600,
                      }}
                    >
                      {swarm.status}
                    </span>
                  </div>
                  {swarm.objective && (
                    <div style={{ fontSize: 13, color: '#888', maxWidth: 600 }}>
                      {swarm.objective.length > 120 ? swarm.objective.slice(0, 120) + '...' : swarm.objective}
                    </div>
                  )}
                  <div style={{ fontSize: 12, color: '#444', marginTop: 6, display: 'flex', gap: 16 }}>
                    {swarm.created_at && <span>Created: {swarm.created_at}</span>}
                    {swarm.completed_at && <span>Completed: {swarm.completed_at}</span>}
                    {swarm.agents && <span>{swarm.agents.length} agent(s)</span>}
                  </div>
                </div>
                <span style={{ color: '#555', fontSize: 18, transform: isExpanded ? 'rotate(90deg)' : 'none', transition: 'transform 0.15s' }}>
                  {'\u25B6'}
                </span>
              </div>

              {isExpanded && swarm.agents && swarm.agents.length > 0 && (
                <div style={{ marginTop: 16, paddingTop: 16, borderTop: '1px solid #1e1e1e' }}>
                  <h4 style={{ fontSize: 14, fontWeight: 600, color: '#888', marginBottom: 10 }}>Agents</h4>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 8 }}>
                    {swarm.agents.map((agent, i) => {
                      const agentColor = statusColors[agent.status] ?? '#666';
                      return (
                        <div
                          key={i}
                          style={{
                            padding: '10px 14px',
                            background: '#1a1a1a',
                            borderRadius: 8,
                            fontSize: 13,
                            display: 'flex',
                            justifyContent: 'space-between',
                            alignItems: 'center',
                          }}
                        >
                          <span style={{ fontWeight: 600 }}>{agent.role}</span>
                          <span
                            style={{
                              fontSize: 11,
                              padding: '1px 6px',
                              borderRadius: 999,
                              background: `${agentColor}22`,
                              color: agentColor,
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
          <div style={{ color: '#555', fontSize: 14 }}>No swarm runs yet</div>
        )}
      </div>
    </div>
  );
}

export default Swarms;
