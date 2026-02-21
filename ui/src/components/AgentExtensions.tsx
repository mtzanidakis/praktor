import { useState, useEffect } from 'react';

interface MCPServerConfig {
  type: 'stdio' | 'http';
  command?: string;
  args?: string[];
  url?: string;
  env?: Record<string, string>;
  headers?: Record<string, string>;
}

interface MarketplaceConfig {
  source: string;
  name?: string;
}

interface PluginConfig {
  name: string;
  disabled?: boolean;
  requires?: string[];
}

interface SkillConfig {
  description: string;
  content: string;
  requires?: string[];
}

interface PluginStatus {
  name: string;
  enabled: boolean;
}

interface ExtensionStatus {
  marketplaces?: string[];
  plugins?: PluginStatus[];
}

interface AgentExtensions {
  mcp_servers?: Record<string, MCPServerConfig>;
  marketplaces?: MarketplaceConfig[];
  plugins?: PluginConfig[];
  skills?: Record<string, SkillConfig>;
  settings?: Record<string, unknown>;
  _status?: ExtensionStatus;
}

const card: React.CSSProperties = {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: 20,
  boxShadow: 'var(--shadow)',
};

const tabStyle = (active: boolean): React.CSSProperties => ({
  padding: '8px 16px',
  background: active ? 'var(--accent)' : 'transparent',
  color: active ? '#fff' : 'var(--text-secondary)',
  border: active ? 'none' : '1px solid var(--border)',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 14,
  fontWeight: 600,
});

const inputStyle: React.CSSProperties = {
  width: '100%',
  padding: '8px 12px',
  background: 'var(--bg-input)',
  color: 'var(--text-primary)',
  border: '1px solid var(--border)',
  borderRadius: 6,
  fontSize: 14,
  boxSizing: 'border-box',
};


const textareaStyle: React.CSSProperties = {
  ...inputStyle,
  fontFamily: 'monospace',
  minHeight: 120,
  resize: 'vertical',
};

const btnPrimary: React.CSSProperties = {
  padding: '6px 18px',
  background: 'var(--accent)',
  color: '#fff',
  border: 'none',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 14,
  fontWeight: 600,
};

const btnDanger: React.CSSProperties = {
  padding: '4px 12px',
  background: 'var(--red-muted, #3a1515)',
  color: 'var(--red-light, #f87171)',
  border: '1px solid var(--red-light, #f87171)',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
};

const btnSmall: React.CSSProperties = {
  padding: '4px 12px',
  background: 'var(--accent-muted)',
  color: 'var(--accent)',
  border: '1px solid var(--accent)',
  borderRadius: 6,
  cursor: 'pointer',
  fontSize: 13,
};

const installedBadge: React.CSSProperties = {
  display: 'inline-block',
  padding: '1px 8px',
  borderRadius: 999,
  fontSize: 12,
  fontWeight: 600,
  background: 'rgba(34, 197, 94, 0.15)',
  color: '#4ade80',
  marginLeft: 8,
};

const disabledBadge: React.CSSProperties = {
  ...installedBadge,
  background: 'rgba(251, 191, 36, 0.15)',
  color: '#fbbf24',
};

// MCP Servers tab
function MCPServersTab({
  servers,
  onChange,
}: {
  servers: Record<string, MCPServerConfig>;
  onChange: (servers: Record<string, MCPServerConfig>) => void;
}) {
  const [newName, setNewName] = useState('');
  const [newType, setNewType] = useState<'' | 'stdio' | 'http'>('');
  const [editing, setEditing] = useState<string | null>(null);
  const [editJSON, setEditJSON] = useState('');

  const addServer = () => {
    if (!newName.trim() || !newType) return;
    const base: MCPServerConfig =
      newType === 'stdio'
        ? { type: 'stdio', command: '', args: [], env: {} }
        : { type: newType, url: '', headers: {} };
    onChange({ ...servers, [newName.trim()]: base });
    setNewName('');
    setEditing(newName.trim());
    setEditJSON(JSON.stringify(base, null, 2));
  };

  const removeServer = (name: string) => {
    const next = { ...servers };
    delete next[name];
    onChange(next);
    if (editing === name) setEditing(null);
  };

  const saveEdit = () => {
    if (!editing) return;
    try {
      const parsed = JSON.parse(editJSON);
      onChange({ ...servers, [editing]: parsed });
      setEditing(null);
    } catch {
      alert('Invalid JSON');
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center' }}>
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="Server name"
          style={{ ...inputStyle, width: 200 }}
          onKeyDown={(e) => e.key === 'Enter' && addServer()}
        />
        <select
          value={newType}
          onChange={(e) => setNewType(e.target.value as 'stdio' | 'http')}
          style={{ ...inputStyle, width: 130 }}
        >
          <option value="" disabled>Transport</option>
          <option value="http">http</option>
          <option value="stdio">stdio</option>
        </select>
        <button style={btnSmall} onClick={addServer}>
          Add
        </button>
      </div>

      {Object.entries(servers).map(([name, srv]) => (
        <div
          key={name}
          style={{
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 12,
            marginBottom: 12,
            background: 'var(--bg-input)',
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
            <div>
              <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{name}</span>
              <span
                style={{
                  marginLeft: 8,
                  padding: '1px 8px',
                  borderRadius: 999,
                  fontSize: 12,
                  background: 'var(--accent-muted)',
                  color: 'var(--accent)',
                }}
              >
                {srv.type}
              </span>
            </div>
            <div style={{ display: 'flex', gap: 6 }}>
              <button
                style={btnSmall}
                onClick={() => {
                  setEditing(editing === name ? null : name);
                  setEditJSON(JSON.stringify(srv, null, 2));
                }}
              >
                {editing === name ? 'Cancel' : 'Edit'}
              </button>
              <button style={btnDanger} onClick={() => removeServer(name)}>
                Remove
              </button>
            </div>
          </div>
          {srv.type === 'stdio' && !editing && (
            <div style={{ fontSize: 13, color: 'var(--text-secondary)', fontFamily: 'monospace' }}>
              {srv.command} {(srv.args || []).join(' ')}
            </div>
          )}
          {srv.type === 'http' && !editing && (
            <div style={{ fontSize: 13, color: 'var(--text-secondary)', fontFamily: 'monospace' }}>{srv.url}</div>
          )}
          {editing === name && (
            <div style={{ marginTop: 8 }}>
              <textarea
                value={editJSON}
                onChange={(e) => setEditJSON(e.target.value)}
                style={{ ...textareaStyle, minHeight: 150 }}
              />
              <button style={{ ...btnPrimary, marginTop: 8 }} onClick={saveEdit}>
                Apply
              </button>
            </div>
          )}
        </div>
      ))}

      {Object.keys(servers).length === 0 && (
        <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>No MCP servers configured</div>
      )}
    </div>
  );
}

// Plugins tab (with marketplaces section)
function PluginsTab({
  marketplaces,
  plugins,
  status,
  onChangeMarketplaces,
  onChangePlugins,
}: {
  marketplaces: MarketplaceConfig[];
  plugins: PluginConfig[];
  status?: ExtensionStatus;
  onChangeMarketplaces: (marketplaces: MarketplaceConfig[]) => void;
  onChangePlugins: (plugins: PluginConfig[]) => void;
}) {
  const [newSource, setNewSource] = useState('');
  const [newPlugin, setNewPlugin] = useState('');

  const addMarketplace = () => {
    if (!newSource.trim()) return;
    onChangeMarketplaces([...marketplaces, { source: newSource.trim() }]);
    setNewSource('');
  };

  const removeMarketplace = (idx: number) => {
    onChangeMarketplaces(marketplaces.filter((_, i) => i !== idx));
  };

  const deriveName = (source: string): string => {
    return source.replace(/^https?:\/\//, '').replace(/[/.:]+/g, '-').replace(/-+$/, '');
  };

  const addPlugin = () => {
    if (!newPlugin.trim()) return;
    onChangePlugins([...plugins, { name: newPlugin.trim() }]);
    setNewPlugin('');
  };

  const removePlugin = (idx: number) => {
    onChangePlugins(plugins.filter((_, i) => i !== idx));
  };

  return (
    <div>
      <h4 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: '0 0 8px' }}>Marketplaces</h4>
      <p style={{ fontSize: 13, color: 'var(--text-secondary)', margin: '0 0 12px' }}>
        Add marketplace sources (e.g., owner/repo) before installing their plugins. The <code style={{ fontSize: 12 }}>claude-plugins-official</code> marketplace is always available.
      </p>

      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input
          value={newSource}
          onChange={(e) => setNewSource(e.target.value)}
          placeholder="owner/repo or https://example.com/marketplace.json"
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => e.key === 'Enter' && addMarketplace()}
        />
        <button style={btnSmall} onClick={addMarketplace}>
          Add
        </button>
      </div>

      {marketplaces.map((m, i) => {
        const isInstalled = status?.marketplaces?.some(
          (line) => line.includes(m.source) || line.includes(m.name || deriveName(m.source))
        );
        return (
          <div
            key={i}
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              padding: '8px 12px',
              border: '1px solid var(--border)',
              borderRadius: 6,
              marginBottom: 8,
              background: 'var(--bg-input)',
            }}
          >
            <div>
              <span style={{ fontFamily: 'monospace', fontSize: 14, color: 'var(--text-primary)' }}>{m.source}</span>
              <span style={{ marginLeft: 8, fontSize: 12, color: 'var(--text-tertiary)' }}>
                ({m.name || deriveName(m.source)})
              </span>
              {isInstalled && <span style={installedBadge}>Registered</span>}
            </div>
            <button style={btnDanger} onClick={() => removeMarketplace(i)}>
              Remove
            </button>
          </div>
        );
      })}

      {marketplaces.length === 0 && (
        <div style={{ color: 'var(--text-tertiary)', fontSize: 14, marginBottom: 16 }}>No additional marketplaces configured</div>
      )}

      <h4 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', margin: '20px 0 8px' }}>Plugins</h4>

      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input
          value={newPlugin}
          onChange={(e) => setNewPlugin(e.target.value)}
          placeholder="plugin-name@marketplace"
          style={{ ...inputStyle, flex: 1 }}
          onKeyDown={(e) => e.key === 'Enter' && addPlugin()}
        />
        <button style={btnSmall} onClick={addPlugin}>
          Add
        </button>
      </div>

      {plugins.map((p, i) => {
        const pluginBase = p.name.split('@')[0];
        const pluginStatus = status?.plugins?.find(
          (ps) => ps?.name && (ps.name === p.name || ps.name === pluginBase || ps.name.startsWith(pluginBase + '@'))
        );
        return (
          <div
            key={i}
            style={{
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              padding: '8px 12px',
              border: '1px solid var(--border)',
              borderRadius: 6,
              marginBottom: 8,
              background: 'var(--bg-input)',
              opacity: p.disabled ? 0.6 : 1,
            }}
          >
            <div>
              <span style={{ fontFamily: 'monospace', fontSize: 14, color: 'var(--text-primary)' }}>{p.name}</span>
              {pluginStatus && !p.disabled && <span style={installedBadge}>Installed</span>}
              {p.disabled && <span style={disabledBadge}>Disabled</span>}
            </div>
            <div style={{ display: 'flex', gap: 6 }}>
              <button
                style={btnSmall}
                onClick={() => {
                  const updated = [...plugins];
                  updated[i] = { ...p, disabled: !p.disabled };
                  onChangePlugins(updated);
                }}
              >
                {p.disabled ? 'Enable' : 'Disable'}
              </button>
              <button style={btnDanger} onClick={() => removePlugin(i)}>
                Remove
              </button>
            </div>
          </div>
        );
      })}

      {plugins.length === 0 && (
        <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>No plugins configured</div>
      )}
    </div>
  );
}

// Skills tab
function SkillsTab({
  skills,
  onChange,
}: {
  skills: Record<string, SkillConfig>;
  onChange: (skills: Record<string, SkillConfig>) => void;
}) {
  const [newName, setNewName] = useState('');
  const [editing, setEditing] = useState<string | null>(null);
  const [editDesc, setEditDesc] = useState('');
  const [editContent, setEditContent] = useState('');

  const addSkill = () => {
    if (!newName.trim()) return;
    const name = newName.trim().replace(/[^a-zA-Z0-9_-]/g, '-');
    onChange({ ...skills, [name]: { description: '', content: '' } });
    setNewName('');
    setEditing(name);
    setEditDesc('');
    setEditContent('');
  };

  const removeSkill = (name: string) => {
    const next = { ...skills };
    delete next[name];
    onChange(next);
    if (editing === name) setEditing(null);
  };

  const saveEdit = () => {
    if (!editing) return;
    onChange({ ...skills, [editing]: { description: editDesc, content: editContent } });
    setEditing(null);
  };

  return (
    <div>
      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input
          value={newName}
          onChange={(e) => setNewName(e.target.value)}
          placeholder="Skill name (e.g. code-review)"
          style={{ ...inputStyle, width: 300 }}
          onKeyDown={(e) => e.key === 'Enter' && addSkill()}
        />
        <button style={btnSmall} onClick={addSkill}>
          Add
        </button>
      </div>

      {Object.entries(skills).map(([name, skill]) => (
        <div
          key={name}
          style={{
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 12,
            marginBottom: 12,
            background: 'var(--bg-input)',
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
            <span style={{ fontWeight: 600, color: 'var(--text-primary)' }}>{name}</span>
            <div style={{ display: 'flex', gap: 6 }}>
              <button
                style={btnSmall}
                onClick={() => {
                  setEditing(editing === name ? null : name);
                  setEditDesc(skill.description);
                  setEditContent(skill.content);
                }}
              >
                {editing === name ? 'Cancel' : 'Edit'}
              </button>
              <button style={btnDanger} onClick={() => removeSkill(name)}>
                Remove
              </button>
            </div>
          </div>
          {editing !== name && skill.description && (
            <div style={{ fontSize: 13, color: 'var(--text-secondary)' }}>{skill.description}</div>
          )}
          {editing === name && (
            <div style={{ marginTop: 8 }}>
              <input
                value={editDesc}
                onChange={(e) => setEditDesc(e.target.value)}
                placeholder="Description"
                style={{ ...inputStyle, marginBottom: 8 }}
              />
              <textarea
                value={editContent}
                onChange={(e) => setEditContent(e.target.value)}
                placeholder="Skill content (SKILL.md body)"
                style={textareaStyle}
              />
              <button style={{ ...btnPrimary, marginTop: 8 }} onClick={saveEdit}>
                Apply
              </button>
            </div>
          )}
        </div>
      ))}

      {Object.keys(skills).length === 0 && (
        <div style={{ color: 'var(--text-tertiary)', fontSize: 14 }}>No skills configured</div>
      )}
    </div>
  );
}

// Settings tab (raw JSON editor)
function SettingsTab({
  settings,
  onChange,
}: {
  settings: Record<string, unknown>;
  onChange: (settings: Record<string, unknown>) => void;
}) {
  const [json, setJson] = useState(JSON.stringify(settings, null, 2));
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setJson(JSON.stringify(settings, null, 2));
  }, [settings]);

  const apply = () => {
    try {
      const parsed = JSON.parse(json);
      setError(null);
      onChange(parsed);
    } catch (err) {
      setError(String(err));
    }
  };

  return (
    <div>
      <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginTop: 0, marginBottom: 12 }}>
        Raw Claude Code settings JSON. These are merged into ~/.claude/settings.json on container start.
      </p>
      <textarea
        value={json}
        onChange={(e) => setJson(e.target.value)}
        style={{ ...textareaStyle, minHeight: 200 }}
      />
      {error && <div style={{ color: 'var(--red-light)', fontSize: 13, marginTop: 4 }}>{error}</div>}
      <button style={{ ...btnPrimary, marginTop: 8 }} onClick={apply}>
        Apply
      </button>
    </div>
  );
}

// Main component
export default function AgentExtensionsPanel({ agentId }: { agentId: string }) {
  const [ext, setExt] = useState<AgentExtensions>({});
  const [tab, setTab] = useState<'mcp' | 'plugins' | 'skills' | 'settings'>('mcp');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setLoading(true);
    setError(null);
    fetch(`/api/agents/definitions/${agentId}/extensions`)
      .then((res) => res.json())
      .then((data) => setExt(data))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, [agentId]);

  const save = () => {
    setSaving(true);
    setError(null);
    // Strip _status (read-only runtime data) before saving
    const { _status, ...payload } = ext;
    fetch(`/api/agents/definitions/${agentId}/extensions`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
      .then(async (res) => {
        if (!res.ok) {
          const data = await res.json().catch(() => ({}));
          throw new Error(data.error || `HTTP ${res.status}`);
        }
        setSaved(true);
        setTimeout(() => setSaved(false), 2000);
      })
      .catch((err) => setError(err.message))
      .finally(() => setSaving(false));
  };

  if (loading) return <div style={{ color: 'var(--text-tertiary)', fontSize: 15 }}>Loading extensions...</div>;

  return (
    <div style={card}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 16 }}>
        <div>
          <h3 style={{ fontSize: 20, fontWeight: 600, margin: 0, color: 'var(--text-primary)' }}>Extensions</h3>
          <p style={{ fontSize: 15, color: 'var(--text-secondary)', margin: '4px 0 0' }}>
            MCP servers, plugins, skills, and Claude Code settings
          </p>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          {saved && <span style={{ color: 'var(--green)', fontSize: 15, fontWeight: 500 }}>Saved</span>}
          <button onClick={save} disabled={saving} style={btnPrimary}>
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>

      {error && (
        <div
          style={{
            padding: '8px 12px',
            background: 'var(--red-muted, #3a1515)',
            border: '1px solid var(--red-light, #f87171)',
            borderRadius: 6,
            color: 'var(--red-light, #f87171)',
            fontSize: 14,
            marginBottom: 16,
          }}
        >
          {error}
        </div>
      )}

      <div style={{ display: 'flex', gap: 8, marginBottom: 20 }}>
        {(['mcp', 'plugins', 'skills', 'settings'] as const).map((t) => (
          <button key={t} style={tabStyle(tab === t)} onClick={() => setTab(t)}>
            {t === 'mcp' ? 'MCP Servers' : t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>

      {tab === 'mcp' && (
        <MCPServersTab
          servers={ext.mcp_servers || {}}
          onChange={(servers) => setExt({ ...ext, mcp_servers: servers })}
        />
      )}
      {tab === 'plugins' && (
        <PluginsTab
          marketplaces={ext.marketplaces || []}
          plugins={ext.plugins || []}
          status={ext._status}
          onChangeMarketplaces={(marketplaces) => setExt({ ...ext, marketplaces })}
          onChangePlugins={(plugins) => setExt({ ...ext, plugins })}
        />
      )}
      {tab === 'skills' && (
        <SkillsTab skills={ext.skills || {}} onChange={(skills) => setExt({ ...ext, skills })} />
      )}
      {tab === 'settings' && (
        <SettingsTab settings={ext.settings || {}} onChange={(settings) => setExt({ ...ext, settings })} />
      )}
    </div>
  );
}
