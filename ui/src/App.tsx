import { useState, useEffect, useCallback, lazy, Suspense } from 'react';
import { Routes, Route, NavLink } from 'react-router-dom';

const Dashboard = lazy(() => import('./pages/Dashboard'));
const Agents = lazy(() => import('./pages/Agents'));
const Conversations = lazy(() => import('./pages/Conversations'));
const Tasks = lazy(() => import('./pages/Tasks'));
const Secrets = lazy(() => import('./pages/Secrets'));
const Swarms = lazy(() => import('./pages/Swarms'));
const UserProfile = lazy(() => import('./pages/UserProfile'));

// SVG icon components (16x16)
function IconDashboard() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="1.5" y="1.5" width="5" height="5" rx="1" />
      <rect x="9.5" y="1.5" width="5" height="5" rx="1" />
      <rect x="1.5" y="9.5" width="5" height="5" rx="1" />
      <rect x="9.5" y="9.5" width="5" height="5" rx="1" />
    </svg>
  );
}

function IconAgents() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="5.5" cy="5" r="2.5" />
      <path d="M1 13c0-2.2 1.8-4 4-4h1c2.2 0 4 1.8 4 4" />
      <circle cx="11.5" cy="5.5" r="2" />
      <path d="M11.5 9.5c1.9 0 3.5 1.3 3.5 3" />
    </svg>
  );
}

function IconConversations() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M2 3a1 1 0 011-1h6a1 1 0 011 1v5a1 1 0 01-1 1H5l-2 2V9H3a1 1 0 01-1-1V3z" />
      <path d="M6 11v1a1 1 0 001 1h4l2 2v-2h1a1 1 0 001-1V7a1 1 0 00-1-1h-2" />
    </svg>
  );
}

function IconTasks() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M2.5 4.5l2 2 3.5-4" />
      <line x1="10" y1="4" x2="14" y2="4" />
      <path d="M2.5 10.5l2 2 3.5-4" />
      <line x1="10" y1="10" x2="14" y2="10" />
    </svg>
  );
}

function IconSwarms() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="8" cy="3" r="1.5" />
      <circle cx="3.5" cy="11" r="1.5" />
      <circle cx="12.5" cy="11" r="1.5" />
      <line x1="8" y1="4.5" x2="4.5" y2="9.5" />
      <line x1="8" y1="4.5" x2="11.5" y2="9.5" />
      <line x1="5" y1="11" x2="11" y2="11" />
    </svg>
  );
}

function IconSecrets() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <rect x="3" y="7" width="10" height="7" rx="1.5" />
      <path d="M5 7V5a3 3 0 016 0v2" />
    </svg>
  );
}

function IconUser() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="8" cy="5" r="3" />
      <path d="M2 14c0-2.8 2.7-5 6-5s6 2.2 6 5" />
    </svg>
  );
}

function IconSun() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <circle cx="8" cy="8" r="3" />
      <line x1="8" y1="1" x2="8" y2="2.5" />
      <line x1="8" y1="13.5" x2="8" y2="15" />
      <line x1="1" y1="8" x2="2.5" y2="8" />
      <line x1="13.5" y1="8" x2="15" y2="8" />
      <line x1="3.05" y1="3.05" x2="4.11" y2="4.11" />
      <line x1="11.89" y1="11.89" x2="12.95" y2="12.95" />
      <line x1="3.05" y1="12.95" x2="4.11" y2="11.89" />
      <line x1="11.89" y1="4.11" x2="12.95" y2="3.05" />
    </svg>
  );
}

function IconMoon() {
  return (
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
      <path d="M13.5 8.5a5.5 5.5 0 01-6-6 5.5 5.5 0 106 6z" />
    </svg>
  );
}

const navItems = [
  { to: '/', label: 'Dashboard', Icon: IconDashboard },
  { to: '/agents', label: 'Agents', Icon: IconAgents },
  { to: '/conversations', label: 'Conversations', Icon: IconConversations },
  { to: '/tasks', label: 'Tasks', Icon: IconTasks },
  { to: '/secrets', label: 'Secrets', Icon: IconSecrets },
  { to: '/swarms', label: 'Swarms', Icon: IconSwarms },
  { to: '/user', label: 'User', Icon: IconUser },
];

function App() {
  const [theme, setTheme] = useState<'dark' | 'light'>(() => {
    return (localStorage.getItem('praktor-theme') as 'dark' | 'light') || 'dark';
  });

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('praktor-theme', theme);
  }, [theme]);

  const toggleTheme = useCallback(() => {
    setTheme((t) => (t === 'dark' ? 'light' : 'dark'));
  }, []);

  return (
    <div style={{ display: 'flex', minHeight: '100vh' }}>
      <aside style={{
        width: 232,
        background: 'var(--bg-sidebar)',
        borderRight: '1px solid var(--border)',
        padding: '20px 0',
        display: 'flex',
        flexDirection: 'column',
        flexShrink: 0,
        position: 'fixed',
        top: 0,
        left: 0,
        bottom: 0,
        zIndex: 10,
      }}>
        {/* Logo */}
        <div style={{
          padding: '4px 20px 20px',
          borderBottom: '1px solid var(--border)',
          marginBottom: 12,
          display: 'flex',
          alignItems: 'center',
          gap: 10,
        }}>
          <div style={{
            width: 28,
            height: 28,
            borderRadius: 7,
            background: 'var(--accent)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
          }}>
            <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
              <path d="M7 1l2 4h4l-3.2 2.5L11 12 7 9.2 3 12l1.2-4.5L1 5h4L7 1z" fill="#fff" />
            </svg>
          </div>
          <div>
            <div style={{ fontSize: 16, fontWeight: 700, color: 'var(--text-primary)', letterSpacing: '-0.01em' }}>
              Mission Control
            </div>
            <div style={{ fontSize: 12, color: 'var(--text-tertiary)', marginTop: 1 }}>Praktor</div>
          </div>
        </div>

        {/* Navigation */}
        <nav style={{ display: 'flex', flexDirection: 'column', gap: 1, padding: '0 8px', flex: 1 }}>
          {navItems.map(({ to, label, Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              style={({ isActive }) => ({
                display: 'flex',
                alignItems: 'center',
                gap: 10,
                padding: '8px 12px',
                borderRadius: 7,
                textDecoration: 'none',
                fontSize: 14,
                fontWeight: isActive ? 600 : 500,
                color: isActive ? '#fff' : 'var(--text-secondary)',
                background: isActive ? 'var(--accent)' : 'transparent',
              })}
            >
              <Icon />
              {label}
            </NavLink>
          ))}
        </nav>

        {/* Theme toggle */}
        <div style={{ padding: '12px 8px 4px', borderTop: '1px solid var(--border)' }}>
          <button
            onClick={toggleTheme}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 10,
              width: '100%',
              padding: '8px 12px',
              borderRadius: 7,
              border: 'none',
              background: 'transparent',
              color: 'var(--text-secondary)',
              fontSize: 14,
              fontWeight: 500,
              cursor: 'pointer',
            }}
          >
            {theme === 'dark' ? <IconSun /> : <IconMoon />}
            {theme === 'dark' ? 'Light mode' : 'Dark mode'}
          </button>
        </div>
      </aside>

      <main style={{
        flex: 1,
        marginLeft: 232,
        padding: 32,
        overflowY: 'auto',
        maxHeight: '100vh',
        minHeight: '100vh',
      }}>
        <Suspense fallback={null}>
          <Routes>
            <Route path="/" element={<Dashboard />} />
            <Route path="/user" element={<UserProfile />} />
            <Route path="/agents" element={<Agents />} />
            <Route path="/conversations" element={<Conversations />} />
            <Route path="/tasks" element={<Tasks />} />
            <Route path="/secrets" element={<Secrets />} />
            <Route path="/swarms" element={<Swarms />} />
          </Routes>
        </Suspense>
      </main>
    </div>
  );
}

export default App;
