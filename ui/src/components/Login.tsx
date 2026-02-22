import { useState, FormEvent } from 'react';

interface LoginProps {
  onLogin: () => void;
}

export default function Login({ onLogin }: LoginProps) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);

    try {
      const res = await fetch('/api/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password }),
      });

      if (res.ok) {
        onLogin();
      } else {
        setError('Invalid password');
        setPassword('');
      }
    } catch {
      setError('Connection failed');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div style={{
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      minHeight: '100vh',
      background: 'var(--bg-primary)',
    }}>
      <form onSubmit={handleSubmit} style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 16,
        width: 320,
        padding: 32,
        background: 'var(--bg-secondary)',
        borderRadius: 12,
        border: '1px solid var(--border)',
      }}>
        {/* Logo */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8 }}>
          <div style={{
            width: 32,
            height: 32,
            borderRadius: 8,
            background: 'var(--accent)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
          }}>
            <svg width="18" height="18" viewBox="0 0 128 128">
              <polygon fill="#fff" points="0,8 124,4 128,28 4,32"/>
              <polygon fill="#fff" points="14,40 42,38 28,122 0,124"/>
              <polygon fill="#fff" points="72,36 100,34 86,118 58,120"/>
            </svg>
          </div>
          <div style={{ fontSize: 20, fontWeight: 700, color: 'var(--text-primary)' }}>
            Mission Control
          </div>
        </div>

        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Password"
          autoFocus
          style={{
            padding: '10px 12px',
            borderRadius: 8,
            border: '1px solid var(--border)',
            background: 'var(--bg-primary)',
            color: 'var(--text-primary)',
            fontSize: 14,
            outline: 'none',
          }}
        />

        {error && (
          <div style={{ color: '#ef4444', fontSize: 13 }}>{error}</div>
        )}

        <button
          type="submit"
          disabled={loading || !password}
          style={{
            padding: '10px 12px',
            borderRadius: 8,
            border: 'none',
            background: 'var(--accent)',
            color: '#fff',
            fontSize: 14,
            fontWeight: 600,
            cursor: loading || !password ? 'not-allowed' : 'pointer',
            opacity: loading || !password ? 0.6 : 1,
          }}
        >
          {loading ? 'Signing in...' : 'Sign in'}
        </button>
      </form>
    </div>
  );
}
