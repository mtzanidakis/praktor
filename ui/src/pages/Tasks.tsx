import { useState, useEffect, useCallback, useRef } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';

interface Task {
  id: string;
  name: string;
  schedule: string;
  schedule_display?: string;
  agent_id?: string;
  agent_name?: string;
  prompt?: string;
  enabled: boolean;
  status: string;
  last_run?: string;
  next_run?: string;
}

interface TaskForm {
  name: string;
  schedule: string;
  agent_id: string;
  prompt: string;
  enabled: boolean;
}

interface Agent {
  id: string;
  name: string;
}

const emptyForm: TaskForm = { name: '', schedule: '', agent_id: '', prompt: '', enabled: true };

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

/** Extract user-friendly schedule string from schedule JSON for editing. */
function parseScheduleForEdit(scheduleJSON: string): string {
  try {
    const s = JSON.parse(scheduleJSON);
    if (s.kind === 'cron' && s.cron_expr) return s.cron_expr;
    if (s.kind === 'interval' && s.interval_ms > 0) {
      const ms = s.interval_ms;
      if (ms % 3600000 === 0) return `+${ms / 3600000}h`;
      if (ms % 60000 === 0) return `+${ms / 60000}m`;
      return `+${ms / 1000}s`;
    }
    if (s.kind === 'once' && s.at_ms) {
      const d = new Date(s.at_ms);
      return d.toLocaleString();
    }
  } catch { /* not JSON */ }
  return scheduleJSON;
}

function Tasks() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [agents, setAgents] = useState<Agent[]>([]);
  const [form, setForm] = useState<TaskForm>(emptyForm);
  const [editing, setEditing] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const { events } = useWebSocket();
  const debounceRef = useRef<ReturnType<typeof setTimeout>>();

  const fetchTasks = useCallback(() => {
    fetch('/api/tasks')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setTasks(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  const fetchAgents = useCallback(() => {
    fetch('/api/agents/definitions')
      .then((res) => res.json())
      .then((data) => setAgents(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetchTasks();
    fetchAgents();
  }, [fetchTasks, fetchAgents]);

  // Re-fetch on relevant WebSocket events (debounced)
  useEffect(() => {
    if (events.length === 0) return;
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(fetchTasks, 500);
  }, [events.length, fetchTasks]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      const url = editing ? `/api/tasks/${editing}` : '/api/tasks';
      const method = editing ? 'PUT' : 'POST';
      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(form),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => null);
        throw new Error(body?.error || `HTTP ${res.status}`);
      }
      setForm(emptyForm);
      setEditing(null);
      setShowForm(false);
      fetchTasks();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this task?')) return;
    try {
      const res = await fetch(`/api/tasks/${id}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      fetchTasks();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleEdit = (task: Task) => {
    setForm({
      name: task.name,
      schedule: parseScheduleForEdit(task.schedule),
      agent_id: task.agent_id ?? '',
      prompt: task.prompt ?? '',
      enabled: task.enabled,
    });
    setEditing(task.id);
    setShowForm(true);
  };

  const handleToggle = async (task: Task) => {
    if (task.status === 'completed') return;
    try {
      const res = await fetch(`/api/tasks/${task.id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: !task.enabled }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      fetchTasks();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleDeleteCompleted = async () => {
    if (!confirm('Delete all completed tasks?')) return;
    try {
      const res = await fetch('/api/tasks/completed', { method: 'DELETE' });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      fetchTasks();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>Scheduled Tasks</h1>
        <div style={{ display: 'flex', gap: 8 }}>
          {tasks.some((t) => t.status === 'completed') && (
            <button style={btnDanger} onClick={handleDeleteCompleted}>
              Delete completed
            </button>
          )}
          <button
            style={btnPrimary}
            onClick={() => { setForm(emptyForm); setEditing(null); setShowForm(!showForm); }}
          >
            {showForm ? 'Cancel' : '+ New Task'}
          </button>
        </div>
      </div>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} style={{ ...card, marginBottom: 20 }}>
          <h3 style={{ fontSize: 18, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' }}>
            {editing ? 'Edit Task' : 'Create Task'}
          </h3>
          <div className="form-grid-2col" style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
            <div>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Name</label>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Daily summary"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Schedule (cron, +5m, +2h)</label>
              <input
                style={inputStyle}
                value={form.schedule}
                onChange={(e) => setForm({ ...form, schedule: e.target.value })}
                placeholder="0 9 * * *"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Agent</label>
              <select
                style={inputStyle}
                value={form.agent_id}
                onChange={(e) => setForm({ ...form, agent_id: e.target.value })}
              >
                <option value="">Select an agent...</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </select>
            </div>
            <div style={{ display: 'flex', alignItems: 'flex-end' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 16, color: 'var(--text-secondary)', cursor: 'pointer' }}>
                <input
                  type="checkbox"
                  checked={form.enabled}
                  onChange={(e) => setForm({ ...form, enabled: e.target.checked })}
                />
                Enabled
              </label>
            </div>
          </div>
          <div style={{ marginBottom: 16 }}>
            <label style={{ fontSize: 15, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Prompt</label>
            <textarea
              style={{ ...inputStyle, minHeight: 80, resize: 'vertical' }}
              value={form.prompt}
              onChange={(e) => setForm({ ...form, prompt: e.target.value })}
              placeholder="What should the agent do?"
            />
          </div>
          <button type="submit" style={btnPrimary}>
            {editing ? 'Update Task' : 'Create Task'}
          </button>
        </form>
      )}

      <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
        {tasks.map((task) => (
          <div key={task.id} style={card}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
                  <span style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>{task.name}</span>
                  <span
                    style={{
                      ...badge(
                        task.status === 'active' ? 'var(--green)' : 'var(--text-secondary)',
                        task.status === 'active' ? 'var(--green-muted)' : 'var(--accent-muted)',
                      ),
                      cursor: task.status === 'completed' ? 'default' : 'pointer',
                    }}
                    onClick={() => handleToggle(task)}
                  >
                    {task.status}
                  </span>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 8, fontSize: 15, color: 'var(--text-secondary)' }}>
                  <span>{task.schedule_display || task.schedule}</span>
                  {task.agent_id && (
                    <span style={badge('var(--accent)', 'var(--accent-muted)')}>
                      {task.agent_name || task.agent_id}
                    </span>
                  )}
                </div>

                {task.prompt && (
                  <div style={{ fontSize: 15, color: 'var(--text-tertiary)', marginBottom: 8, maxWidth: 600 }}>
                    {task.prompt.length > 120 ? task.prompt.slice(0, 120) + '...' : task.prompt}
                  </div>
                )}

                <div style={{ fontSize: 14, color: 'var(--text-muted)', display: 'flex', gap: 16 }}>
                  {task.last_run && <span>Last run: {task.last_run}</span>}
                  {task.next_run && <span>Next run: {task.next_run}</span>}
                </div>
              </div>

              <div style={{ display: 'flex', gap: 6, flexShrink: 0, marginLeft: 16 }}>
                <button
                  style={{ ...btnDanger, color: 'var(--text-secondary)' }}
                  onClick={() => handleEdit(task)}
                >
                  Edit
                </button>
                <button style={btnDanger} onClick={() => handleDelete(task.id)}>
                  Delete
                </button>
              </div>
            </div>
          </div>
        ))}
        {tasks.length === 0 && !error && (
          <div style={{ color: 'var(--text-tertiary)', fontSize: 16 }}>No scheduled tasks</div>
        )}
      </div>
    </div>
  );
}

export default Tasks;
