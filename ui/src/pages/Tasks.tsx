import { useState, useEffect, useCallback } from 'react';

interface Task {
  id: string;
  name: string;
  schedule: string;
  schedule_display?: string;
  group_id?: string;
  group_name?: string;
  prompt?: string;
  enabled: boolean;
  last_run?: string;
  next_run?: string;
}

interface TaskForm {
  name: string;
  schedule: string;
  group_id: string;
  prompt: string;
  enabled: boolean;
}

interface Group {
  id: string;
  name: string;
}

const emptyForm: TaskForm = { name: '', schedule: '', group_id: '', prompt: '', enabled: true };

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
  fontSize: 14,
  outline: 'none',
};

const btnPrimary: React.CSSProperties = {
  padding: '8px 20px',
  borderRadius: 7,
  border: 'none',
  background: 'var(--accent)',
  color: '#fff',
  fontSize: 14,
  fontWeight: 600,
  cursor: 'pointer',
};

const btnDanger: React.CSSProperties = {
  padding: '6px 14px',
  borderRadius: 6,
  border: '1px solid var(--border)',
  background: 'transparent',
  color: 'var(--red-light)',
  fontSize: 13,
  cursor: 'pointer',
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

/** Extract the cron expression from schedule JSON for editing. */
function parseCronFromSchedule(scheduleJSON: string): string {
  try {
    const s = JSON.parse(scheduleJSON);
    if (s.kind === 'cron' && s.cron_expr) return s.cron_expr;
  } catch { /* not JSON */ }
  return scheduleJSON;
}

function Tasks() {
  const [tasks, setTasks] = useState<Task[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [form, setForm] = useState<TaskForm>(emptyForm);
  const [editing, setEditing] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchTasks = useCallback(() => {
    fetch('/api/tasks')
      .then((res) => {
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        return res.json();
      })
      .then((data) => setTasks(Array.isArray(data) ? data : []))
      .catch((err) => setError(err.message));
  }, []);

  const fetchGroups = useCallback(() => {
    fetch('/api/groups')
      .then((res) => res.json())
      .then((data) => setGroups(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, []);

  useEffect(() => {
    fetchTasks();
    fetchGroups();
  }, [fetchTasks, fetchGroups]);

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
      schedule: parseCronFromSchedule(task.schedule),
      group_id: task.group_id ?? '',
      prompt: task.prompt ?? '',
      enabled: task.enabled,
    });
    setEditing(task.id);
    setShowForm(true);
  };

  const handleToggle = async (task: Task) => {
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

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 26, fontWeight: 700, color: 'var(--text-primary)' }}>Scheduled Tasks</h1>
        <button
          style={btnPrimary}
          onClick={() => { setForm(emptyForm); setEditing(null); setShowForm(!showForm); }}
        >
          {showForm ? 'Cancel' : '+ New Task'}
        </button>
      </div>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} style={{ ...card, marginBottom: 20 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16, color: 'var(--text-primary)' }}>
            {editing ? 'Edit Task' : 'Create Task'}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
            <div>
              <label style={{ fontSize: 13, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Name</label>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Daily summary"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 13, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Schedule (cron)</label>
              <input
                style={inputStyle}
                value={form.schedule}
                onChange={(e) => setForm({ ...form, schedule: e.target.value })}
                placeholder="0 9 * * *"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 13, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Group</label>
              <select
                style={inputStyle}
                value={form.group_id}
                onChange={(e) => setForm({ ...form, group_id: e.target.value })}
              >
                <option value="">Select a group...</option>
                {groups.map((g) => (
                  <option key={g.id} value={g.id}>{g.name}</option>
                ))}
              </select>
            </div>
            <div style={{ display: 'flex', alignItems: 'flex-end' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 14, color: 'var(--text-secondary)', cursor: 'pointer' }}>
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
            <label style={{ fontSize: 13, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>Prompt</label>
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
                  <span style={{ fontSize: 16, fontWeight: 600, color: 'var(--text-primary)' }}>{task.name}</span>
                  <span
                    style={{
                      ...badge(
                        task.enabled ? 'var(--green)' : 'var(--text-secondary)',
                        task.enabled ? 'var(--green-muted)' : 'var(--accent-muted)',
                      ),
                      cursor: 'pointer',
                    }}
                    onClick={() => handleToggle(task)}
                  >
                    {task.enabled ? 'active' : 'paused'}
                  </span>
                </div>

                <div style={{ display: 'flex', alignItems: 'center', gap: 14, marginBottom: 8, fontSize: 13, color: 'var(--text-secondary)' }}>
                  <span>{task.schedule_display || task.schedule}</span>
                  {task.group_id && (
                    <span style={badge('var(--accent)', 'var(--accent-muted)')}>
                      {task.group_name || task.group_id}
                    </span>
                  )}
                </div>

                {task.prompt && (
                  <div style={{ fontSize: 13, color: 'var(--text-tertiary)', marginBottom: 8, maxWidth: 600 }}>
                    {task.prompt.length > 120 ? task.prompt.slice(0, 120) + '...' : task.prompt}
                  </div>
                )}

                <div style={{ fontSize: 12, color: 'var(--text-muted)', display: 'flex', gap: 16 }}>
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
          <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>No scheduled tasks</div>
        )}
      </div>
    </div>
  );
}

export default Tasks;
