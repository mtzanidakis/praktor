import { Routes, Route, NavLink } from 'react-router-dom';
import Dashboard from './pages/Dashboard';
import Groups from './pages/Groups';
import Conversations from './pages/Conversations';
import Tasks from './pages/Tasks';
import Swarms from './pages/Swarms';

const navItems = [
  { to: '/', label: 'Dashboard', icon: '\u25A0' },
  { to: '/groups', label: 'Groups', icon: '\u25CB' },
  { to: '/conversations', label: 'Conversations', icon: '\u25AC' },
  { to: '/tasks', label: 'Tasks', icon: '\u25B7' },
  { to: '/swarms', label: 'Swarms', icon: '\u2726' },
];

const styles = {
  layout: {
    display: 'flex',
    minHeight: '100vh',
  } as React.CSSProperties,
  sidebar: {
    width: 220,
    background: '#111111',
    borderRight: '1px solid #1e1e1e',
    padding: '24px 0',
    display: 'flex',
    flexDirection: 'column',
    flexShrink: 0,
  } as React.CSSProperties,
  logo: {
    padding: '0 24px 24px',
    borderBottom: '1px solid #1e1e1e',
    marginBottom: 16,
  } as React.CSSProperties,
  logoTitle: {
    fontSize: 20,
    fontWeight: 700,
    color: '#6366f1',
    letterSpacing: '0.05em',
  } as React.CSSProperties,
  logoSub: {
    fontSize: 11,
    color: '#666',
    marginTop: 2,
  } as React.CSSProperties,
  nav: {
    display: 'flex',
    flexDirection: 'column',
    gap: 2,
    padding: '0 8px',
  } as React.CSSProperties,
  navLink: {
    display: 'flex',
    alignItems: 'center',
    gap: 12,
    padding: '10px 16px',
    borderRadius: 8,
    textDecoration: 'none',
    color: '#888',
    fontSize: 14,
    transition: 'all 0.15s ease',
  } as React.CSSProperties,
  navLinkActive: {
    background: '#6366f1',
    color: '#fff',
  } as React.CSSProperties,
  content: {
    flex: 1,
    padding: 32,
    overflowY: 'auto',
    maxHeight: '100vh',
  } as React.CSSProperties,
};

function App() {
  return (
    <div style={styles.layout}>
      <aside style={styles.sidebar}>
        <div style={styles.logo}>
          <div style={styles.logoTitle}>PRAKTOR</div>
          <div style={styles.logoSub}>Mission Control</div>
        </div>
        <nav style={styles.nav}>
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              style={({ isActive }) => ({
                ...styles.navLink,
                ...(isActive ? styles.navLinkActive : {}),
              })}
            >
              <span style={{ fontSize: 16 }}>{item.icon}</span>
              {item.label}
            </NavLink>
          ))}
        </nav>
      </aside>
      <main style={styles.content}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/groups" element={<Groups />} />
          <Route path="/conversations" element={<Conversations />} />
          <Route path="/tasks" element={<Tasks />} />
          <Route path="/swarms" element={<Swarms />} />
        </Routes>
      </main>
    </div>
  );
}

export default App;
