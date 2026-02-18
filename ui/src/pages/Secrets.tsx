import { useState, useEffect, useCallback, useRef } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';

interface Secret {
  id: string;
  name: string;
  description: string;
  kind: string;
  filename?: string;
  global: boolean;
  agent_ids: string[];
  created_at: string;
  updated_at: string;
}

interface Agent {
  id: string;
  name: string;
}

interface SecretForm {
  name: string;
  description: string;
  kind: string;
  filename: string;
  value: string;
  global: boolean;
  agent_ids: string[];
}

const emptyForm: SecretForm = { name: '', description: '', kind: 'string', filename: '', value: '', global: false, agent_ids: [] };

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
  fontSize: 16,
  outline: 'none',
};

const btnPrimary: React.CSSProperties = {
  padding: '8px 20px',
  borderRadius: 7,
  border: 'none',
  background: 'var(--accent)',
  color: '#fff',
  fontSize: 16,
  fontWeight: 600,
  cursor: 'pointer',
};

const btnDanger: React.CSSProperties = {
  padding: '6px 14px',
  borderRadius: 6,
  border: '1px solid var(--border)',
  background: 'transparent',
  color: 'var(--red-light)',
  fontSize: 15,
  cursor: 'pointer',
};

const badge = (color: string, bg: string): React.CSSProperties => ({
  display: 'inline-block',
  padding: '2px 10px',
  borderRadius: 999,
  fontSize: 14,
  fontWeight: 600,
  background: bg,
  color,
});

function Secrets() {
  const [secrets, setSecrets] = useState<Secret[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [form, setForm] = useState<SecretForm>(emptyForm);
  const [editing, setEditing] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { events } = useWebSocket();
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  const fetchSecrets = useCallback(() => {
    fetch('/api/secrets')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setSecrets(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  const fetchAgents = useCallback(() => {
    fetch('/api/agents/definitions')
      .then((res) => res.json())
      .then((data) => setAgents(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetchSecrets();
    fetchAgents();
    // Poll for changes from external sources (CLI, etc.)
    const interval = setInterval(fetchSecrets, 5000);
    return () => clearInterval(interval);
  }, [fetchSecrets, fetchAgents]);

  // Re-fetch immediately on WebSocket secret events (debounced)
  useEffect(() => {
    if (events.length === 0) return;
    const last = events[events.length - 1];
    if (typeof last.type === 'string' && last.type.startsWith('events.secret.')) {
      clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(fetchSecrets, 500);
    }
  }, [events.length, fetchSecrets]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      const url = editing ? `/api/secrets/${editing}` : '/api/secrets';
      const method = editing ? 'PUT' : 'POST';

      const body: Record<string, unknown> = {
        name: form.name,
        description: form.description,
        kind: form.kind,
        global: form.global,
        agent_ids: form.agent_ids,
      };

      if (form.kind === 'file') {
        body.filename = form.filename;
      }

      // Only send value if creating or if value was provided (not empty)
      if (!editing || form.value) {
        body.value = form.value;
      }

      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => null);
        throw new Error(data?.error || `HTTP ${res.status}`);
      }
      setForm(emptyForm);
      setEditing(null);
      setShowForm(false);
      fetchSecrets();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this secret?')) return;
    try {
      const res = await fetch(`/api/secrets/${id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      fetchSecrets();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleEdit = (secret: Secret) => {
    setForm({
      name: secret.name,
      description: secret.description || '',
      kind: secret.kind,
      filename: secret.filename || '',
      value: '',
      global: secret.global,
      agent_ids: secret.agent_ids || [],
    });
    setEditing(secret.id);
    setShowForm(true);
  };

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;
    setForm((f) => ({ ...f, filename: file.name }));
    const reader = new FileReader();
    reader.onload = () => {
      setForm((f) => ({ ...f, value: reader.result as string }));
    };
    reader.readAsText(file);
  };

  const toggleAgent = (agentId: string) => {
    setForm((f) => ({
      ...f,
      agent_ids: f.agent_ids.includes(agentId)
        ? f.agent_ids.filter((id) => id !== agentId)
        : [...f.agent_ids, agentId],
    }));
  };

  const agentNameMap = agents.reduce<Record<string, string>>((acc, a) => {
    acc[a.id] = a.name;
    return acc;
  }, {});

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>Secrets</h1>
        <button
          style={btnPrimary}
          onClick={() => { setForm(emptyForm); setEditing(null); setShowForm(!showForm); }}
        >
          {showForm ? 'Cancel' : '+ New Secret'}
        </button>
      </div>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} style={{ ...card, marginBottom: 20 }}>
          <h3 style={{ fontSize: 18, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' }}>
            {editing ? 'Edit Secret' : 'Create Secret'}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
            <div>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Name</label>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="github-token"
                required
                disabled={!!editing}
              />
            </div>
            <div>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Kind</label>
              <select
                style={inputStyle}
                value={form.kind}
                onChange={(e) => setForm({ ...form, kind: e.target.value })}
              >
                <option value="string">String</option>
                <option value="file">File</option>
              </select>
            </div>
            <div style={{ gridColumn: '1 / -1' }}>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Description</label>
              <input
                style={inputStyle}
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
                placeholder="Optional description"
              />
            </div>
          </div>

          <div style={{ marginBottom: 12 }}>
            <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>
              Value {editing && '(leave empty to keep current)'}
            </label>
            {form.kind === 'file' ? (
              <input
                type="file"
                onChange={handleFileChange}
                style={{ fontSize: 16, color: 'var(--text-primary)' }}
              />
            ) : (
              <textarea
                style={{ ...inputStyle, minHeight: 80, resize: 'vertical' }}
                value={form.value}
                onChange={(e) => setForm({ ...form, value: e.target.value })}
                placeholder="Secret value"
                required={!editing}
              />
            )}
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 20, marginBottom: 16 }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 16, color: 'var(--text-secondary)', cursor: 'pointer' }}>
              <input
                type="checkbox"
                checked={form.global}
                onChange={(e) => setForm({ ...form, global: e.target.checked })}
              />
              Global (accessible by all agents)
            </label>
          </div>

          {agents.length > 0 && !form.global && (
            <div style={{ marginBottom: 16 }}>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 8 }}>
                Assign to agents
              </label>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {agents.map((a) => (
                  <label
                    key={a.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 6,
                      fontSize: 15,
                      color: 'var(--text-secondary)',
                      cursor: 'pointer',
                      padding: '4px 10px',
                      borderRadius: 6,
                      border: '1px solid var(--border)',
                      background: form.agent_ids.includes(a.id) ? 'var(--accent-muted)' : 'transparent',
                    }}
                  >
                    <input
                      type="checkbox"
                      checked={form.agent_ids.includes(a.id)}
                      onChange={() => toggleAgent(a.id)}
                      style={{ display: 'none' }}
                    />
                    {a.name}
                  </label>
                ))}
              </div>
            </div>
          )}

          <button type="submit" style={btnPrimary}>
            {editing ? 'Update Secret' : 'Create Secret'}
          </button>
        </form>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {secrets.map((secret) => (
          <div key={secret.id} style={card}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
                  <span style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>{secret.name}</span>
                  <span style={badge(
                    secret.kind === 'string' ? 'var(--accent)' : 'var(--amber)',
                    secret.kind === 'string' ? 'var(--accent-muted)' : 'var(--amber-muted)',
                  )}>
                    {secret.kind}
                  </span>
                  {secret.global && (
                    <span style={badge('var(--green)', 'var(--green-muted)')}>
                      global
                    </span>
                  )}
                </div>

                {secret.description && (
                  <div style={{ fontSize: 15, color: 'var(--text-tertiary)', marginBottom: 8 }}>
                    {secret.description}
                  </div>
                )}

                <div style={{ fontSize: 15, color: 'var(--text-muted)', marginBottom: 4, fontFamily: 'monospace' }}>
                  {'*'.repeat(12)}
                </div>

                {secret.agent_ids && secret.agent_ids.length > 0 && (
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', marginTop: 8 }}>
                    {secret.agent_ids.map((id) => (
                      <span key={id} style={{
                        fontSize: 13,
                        padding: '1px 8px',
                        borderRadius: 4,
                        background: 'var(--bg-elevated)',
                        color: 'var(--text-secondary)',
                      }}>
                        {agentNameMap[id] || id}
                      </span>
                    ))}
                  </div>
                )}
              </div>

              <div style={{ display: 'flex', gap: 6, flexShrink: 0, marginLeft: 16 }}>
                <button
                  style={{ ...btnDanger, color: 'var(--text-secondary)' }}
                  onClick={() => handleEdit(secret)}
                >
                  Edit
                </button>
                <button style={btnDanger} onClick={() => handleDelete(secret.id)}>
                  Delete
                </button>
              </div>
            </div>
          </div>
        ))}
        {secrets.length === 0 && !error && (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 16 }}>No secrets stored</div>
        )}
      </div>
    </div>
  );
}

export default Secrets;
