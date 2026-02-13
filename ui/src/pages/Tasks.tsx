import { useState, useEffect, useCallback } from 'react';

interface Task {
  id: string;
  name: string;
  schedule: string;
  group_id?: string;
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

const emptyForm: TaskForm = { name: '', schedule: '', group_id: '', prompt: '', enabled: true };

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

const btnDanger: React.CSSProperties = {
  padding: '6px 14px',
  borderRadius: 6,
  border: '1px solid #7f1d1d',
  background: 'transparent',
  color: '#f87171',
  fontSize: 13,
  cursor: 'pointer',
};

function Tasks() {
  const [tasks, setTasks] = useState<Task[]>([]);
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

  useEffect(() => {
    fetchTasks();
  }, [fetchTasks]);

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
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
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
      schedule: task.schedule,
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
        body: JSON.stringify({ ...task, enabled: !task.enabled }),
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      fetchTasks();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 32 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700 }}>Scheduled Tasks</h1>
        <button
          style={btnPrimary}
          onClick={() => { setForm(emptyForm); setEditing(null); setShowForm(!showForm); }}
        >
          {showForm ? 'Cancel' : '+ New Task'}
        </button>
      </div>

      {error && (
        <div style={{ ...card, color: '#f87171', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {showForm && (
        <form onSubmit={handleSubmit} style={{ ...card, marginBottom: 24 }}>
          <h3 style={{ fontSize: 16, fontWeight: 600, marginBottom: 16 }}>
            {editing ? 'Edit Task' : 'Create Task'}
          </h3>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 12 }}>
            <div>
              <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>Name</label>
              <input
                style={inputStyle}
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                placeholder="Daily summary"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>Schedule (cron)</label>
              <input
                style={inputStyle}
                value={form.schedule}
                onChange={(e) => setForm({ ...form, schedule: e.target.value })}
                placeholder="0 9 * * *"
                required
              />
            </div>
            <div>
              <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>Group ID</label>
              <input
                style={inputStyle}
                value={form.group_id}
                onChange={(e) => setForm({ ...form, group_id: e.target.value })}
                placeholder="main"
              />
            </div>
            <div style={{ display: 'flex', alignItems: 'flex-end' }}>
              <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 14, color: '#aaa', cursor: 'pointer' }}>
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
            <label style={{ fontSize: 12, color: '#666', display: 'block', marginBottom: 4 }}>Prompt</label>
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

      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {tasks.map((task) => (
          <div key={task.id} style={card}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
              <div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
                  <span style={{ fontSize: 16, fontWeight: 600 }}>{task.name}</span>
                  <span
                    style={{
                      fontSize: 11,
                      padding: '2px 8px',
                      borderRadius: 999,
                      background: task.enabled ? '#22c55e22' : '#66666622',
                      color: task.enabled ? '#22c55e' : '#666',
                      fontWeight: 600,
                      cursor: 'pointer',
                    }}
                    onClick={() => handleToggle(task)}
                  >
                    {task.enabled ? 'active' : 'paused'}
                  </span>
                </div>
                <div style={{ fontSize: 13, color: '#666' }}>
                  <span style={{ fontFamily: 'monospace', color: '#888' }}>{task.schedule}</span>
                  {task.group_id && <span style={{ marginLeft: 12 }}>Group: {task.group_id}</span>}
                </div>
                {task.prompt && (
                  <div style={{ fontSize: 13, color: '#555', marginTop: 6, maxWidth: 500 }}>
                    {task.prompt.length > 100 ? task.prompt.slice(0, 100) + '...' : task.prompt}
                  </div>
                )}
                <div style={{ fontSize: 12, color: '#444', marginTop: 6, display: 'flex', gap: 16 }}>
                  {task.last_run && <span>Last: {task.last_run}</span>}
                  {task.next_run && <span>Next: {task.next_run}</span>}
                </div>
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  style={{ ...btnDanger, color: '#888', borderColor: '#333' }}
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
          <div style={{ color: '#555', fontSize: 14 }}>No scheduled tasks</div>
        )}
      </div>
    </div>
  );
}

export default Tasks;
