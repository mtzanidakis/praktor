import { useState, useEffect, useCallback } from 'react';
import { useWebSocket } from '../hooks/useWebSocket';
import SwarmGraph, { type SwarmLaunchData } from '../components/SwarmGraph';

interface SwarmAgentResult {
  role: string;
  status: string;
  output?: string;
  error?: string;
}

interface SwarmSynapse {
  from: string;
  to: string;
  bidirectional: boolean;
}

interface Swarm {
  id: string;
  name: string;
  lead_agent: string;
  status: string;
  task: string;
  agents?: Array<{ agent_id: string; role: string; prompt: string; workspace: string }>;
  synapses?: SwarmSynapse[];
  results?: SwarmAgentResult[];
  started_at?: string;
  completed_at?: string;
}

const card: React.CSSProperties = {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: 20,
  boxShadow: 'var(--shadow)',
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

const btnSmall: React.CSSProperties = {
  padding: '4px 12px',
  borderRadius: 6,
  border: '1px solid var(--border)',
  background: 'transparent',
  color: 'var(--text-secondary)',
  fontSize: 14,
  cursor: 'pointer',
};

const statusColors: Record<string, { color: string; bg: string }> = {
  running: { color: 'var(--green)', bg: 'var(--green-muted)' },
  completed: { color: 'var(--accent)', bg: 'var(--accent-muted)' },
  failed: { color: 'var(--red)', bg: 'var(--red-muted)' },
  pending: { color: 'var(--amber)', bg: 'var(--amber-muted)' },
};

function swarmToLaunchData(swarm: Swarm): SwarmLaunchData {
  return {
    name: swarm.name || 'Swarm',
    task: swarm.task,
    lead_agent: swarm.lead_agent,
    agents: (swarm.agents || []).map((a) => ({
      agent_id: a.agent_id,
      role: a.role,
      prompt: a.prompt || '',
      workspace: a.workspace || a.agent_id,
    })),
    synapses: (swarm.synapses || []).map((s) => ({
      from: s.from,
      to: s.to,
      bidirectional: s.bidirectional,
    })),
  };
}

function Swarms() {
  const [swarms, setSwarms] = useState<Swarm[]>([]);
  const [view, setView] = useState<'list' | 'create' | 'edit'>('list');
  const [editData, setEditData] = useState<SwarmLaunchData | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { events } = useWebSocket();

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
  }, [fetchSwarms]);

  // React to WebSocket swarm events
  useEffect(() => {
    const latest = events[events.length - 1];
    if (!latest) return;
    const t = latest.type as string;
    if (t.startsWith('swarm_')) {
      fetchSwarms();
    }
  }, [events, fetchSwarms]);

  const launchSwarm = async (data: SwarmLaunchData) => {
    setError(null);
    try {
      const res = await fetch('/api/swarms', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      setView('list');
      setEditData(null);
      fetchSwarms();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const replaySwarm = (swarm: Swarm) => {
    launchSwarm(swarmToLaunchData(swarm));
  };

  const editSwarm = (swarm: Swarm) => {
    setEditData(swarmToLaunchData(swarm));
    setView('edit');
  };

  const deleteSwarm = async (id: string) => {
    setError(null);
    try {
      const res = await fetch(`/api/swarms/${id}`, { method: 'DELETE' });
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
        throw new Error(body.error || `HTTP ${res.status}`);
      }
      fetchSwarms();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unknown error');
    }
  };

  const handleEditLaunch = (data: SwarmLaunchData) => {
    launchSwarm(data);
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: 28 }}>
        <h1 style={{ fontSize: 28, fontWeight: 700, color: 'var(--text-primary)' }}>Swarms</h1>
        <button
          style={btnPrimary}
          onClick={() => {
            if (view === 'list') {
              setEditData(null);
              setView('create');
            } else {
              setView('list');
              setEditData(null);
            }
          }}
        >
          {view === 'list' ? '+ New Swarm' : 'Back to List'}
        </button>
      </div>

      {error && (
        <div style={{ ...card, color: 'var(--red-light)', marginBottom: 16 }}>
          {error}
        </div>
      )}

      {view === 'create' ? (
        <SwarmGraph onLaunch={launchSwarm} />
      ) : view === 'edit' && editData ? (
        <SwarmGraph
          onLaunch={handleEditLaunch}
          initialData={editData}
          launchLabel="Save & Launch"
        />
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          {swarms.map((swarm) => {
            const isExpanded = expanded === swarm.id;
            const sc = statusColors[swarm.status] ?? { color: 'var(--text-tertiary)', bg: 'var(--accent-muted)' };
            const agents = swarm.agents || [];
            const results = swarm.results || [];
            const synapses = swarm.synapses || [];

            return (
              <div key={swarm.id} style={card}>
                <div
                  style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', cursor: 'pointer' }}
                  onClick={() => setExpanded(isExpanded ? null : swarm.id)}
                >
                  <div style={{ flex: 1 }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
                      <span style={{ fontSize: 18, fontWeight: 600, color: 'var(--text-primary)' }}>
                        {swarm.name || 'Swarm'}
                      </span>
                      <span
                        style={{
                          fontSize: 14,
                          padding: '2px 8px',
                          borderRadius: 999,
                          background: sc.bg,
                          color: sc.color,
                          fontWeight: 600,
                        }}
                      >
                        {swarm.status}
                      </span>
                      {swarm.lead_agent && (
                        <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>
                          Lead: {swarm.lead_agent}
                        </span>
                      )}
                    </div>
                    <div style={{ fontSize: 15, color: 'var(--text-secondary)', maxWidth: 600 }}>
                      {swarm.task.length > 120 ? swarm.task.slice(0, 120) + '...' : swarm.task}
                    </div>
                    <div style={{ fontSize: 14, color: 'var(--text-muted)', marginTop: 6, display: 'flex', gap: 16 }}>
                      {agents.length > 0 && <span>{agents.length} agent(s)</span>}
                      {synapses.length > 0 && <span>{synapses.length} connection(s)</span>}
                      {swarm.started_at && <span>Started: {swarm.started_at}</span>}
                      {swarm.completed_at && <span>Completed: {swarm.completed_at}</span>}
                    </div>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6, marginLeft: 12 }}>
                    {swarm.status !== 'running' && (
                      <>
                        <button
                          style={btnSmall}
                          title="Replay"
                          onClick={(e) => { e.stopPropagation(); replaySwarm(swarm); }}
                        >
                          Replay
                        </button>
                        <button
                          style={btnSmall}
                          title="Edit"
                          onClick={(e) => { e.stopPropagation(); editSwarm(swarm); }}
                        >
                          Edit
                        </button>
                        <button
                          style={{ ...btnSmall, color: 'var(--red)', borderColor: 'var(--red)' }}
                          title="Delete"
                          onClick={(e) => { e.stopPropagation(); deleteSwarm(swarm.id); }}
                        >
                          Delete
                        </button>
                      </>
                    )}
                    <span style={{
                      color: 'var(--text-tertiary)',
                      fontSize: 17,
                      transform: isExpanded ? 'rotate(90deg)' : 'none',
                      transition: 'transform 0.15s',
                      marginLeft: 4,
                    }}>
                      {'\u25B6'}
                    </span>
                  </div>
                </div>

                {isExpanded && (
                  <div style={{ marginTop: 16, paddingTop: 16, borderTop: '1px solid var(--border)' }}>
                    {/* Mini topology */}
                    {agents.length > 0 && (
                      <MiniTopology agents={agents} synapses={synapses} results={results} leadAgent={swarm.lead_agent} />
                    )}

                    {/* Results */}
                    {results.length > 0 && (
                      <div style={{ marginTop: 16 }}>
                        <h4 style={{ fontSize: 16, fontWeight: 600, color: 'var(--text-secondary)', marginBottom: 10 }}>
                          Results
                        </h4>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                          {results.map((r, i) => {
                            const rc = statusColors[r.status] ?? { color: 'var(--text-tertiary)', bg: 'var(--accent-muted)' };
                            return (
                              <div key={i} style={{ padding: '10px 14px', background: 'var(--bg-elevated)', borderRadius: 8 }}>
                                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
                                  <span style={{ fontWeight: 600, fontSize: 15, color: 'var(--text-primary)' }}>{r.role}</span>
                                  <span style={{ fontSize: 13, padding: '1px 6px', borderRadius: 999, background: rc.bg, color: rc.color }}>
                                    {r.status}
                                  </span>
                                </div>
                                {r.output && (
                                  <pre style={{
                                    fontSize: 14,
                                    color: 'var(--text-secondary)',
                                    whiteSpace: 'pre-wrap',
                                    wordBreak: 'break-word',
                                    maxHeight: 200,
                                    overflowY: 'auto',
                                    margin: 0,
                                  }}>
                                    {r.output}
                                  </pre>
                                )}
                                {r.error && (
                                  <div style={{ fontSize: 14, color: 'var(--red)' }}>{r.error}</div>
                                )}
                              </div>
                            );
                          })}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
          {swarms.length === 0 && !error && (
            <div style={{ color: 'var(--text-tertiary)', fontSize: 16 }}>No swarm runs yet</div>
          )}
        </div>
      )}
    </div>
  );
}

/* ── Mini read-only graph visualization ── */
function MiniTopology({
  agents,
  synapses,
  results,
  leadAgent,
}: {
  agents: Array<{ role: string }>;
  synapses: SwarmSynapse[];
  results: SwarmAgentResult[];
  leadAgent: string;
}) {
  const resultMap = new Map(results.map((r) => [r.role, r.status]));
  const nodeW = 100;
  const nodeH = 36;
  const padding = 20;

  // Simple grid layout
  const cols = Math.min(agents.length, 4);
  const nodes = agents.map((a, i) => ({
    role: a.role,
    x: padding + (i % cols) * (nodeW + 40),
    y: padding + Math.floor(i / cols) * (nodeH + 30),
  }));

  const svgW = padding * 2 + cols * (nodeW + 40);
  const svgH = padding * 2 + Math.ceil(agents.length / cols) * (nodeH + 30);

  return (
    <svg width={Math.min(svgW, 600)} height={Math.min(svgH, 200)} style={{ display: 'block', marginBottom: 8 }}>
      <defs>
        <marker id="mini-arrow" markerWidth="6" markerHeight="5" refX="6" refY="2.5" orient="auto">
          <path d="M0,0 L6,2.5 L0,5" fill="var(--text-muted)" />
        </marker>
      </defs>

      {/* Edges */}
      {synapses.map((s, i) => {
        const from = nodes.find((n) => n.role === s.from);
        const to = nodes.find((n) => n.role === s.to);
        if (!from || !to) return null;
        return (
          <line
            key={`e-${i}`}
            x1={from.x + nodeW / 2} y1={from.y + nodeH / 2}
            x2={to.x + nodeW / 2} y2={to.y + nodeH / 2}
            stroke="var(--text-muted)"
            strokeWidth={1}
            markerEnd={s.bidirectional ? undefined : 'url(#mini-arrow)'}
            strokeDasharray={s.bidirectional ? '4,3' : undefined}
          />
        );
      })}

      {/* Nodes */}
      {nodes.map((n) => {
        const status = resultMap.get(n.role);
        const sc = status ? (statusColors[status] ?? { color: 'var(--text-tertiary)', bg: 'var(--accent-muted)' }) : { color: 'var(--text-tertiary)', bg: 'var(--bg-elevated)' };
        const isLead = n.role === leadAgent;
        return (
          <g key={n.role}>
            <rect
              x={n.x} y={n.y} width={nodeW} height={nodeH} rx={6}
              fill={sc.bg}
              stroke={isLead ? '#f59e0b' : 'var(--border)'}
              strokeWidth={isLead ? 2 : 1}
            />
            <text
              x={n.x + nodeW / 2} y={n.y + nodeH / 2 + 4}
              textAnchor="middle" fontSize={13} fontWeight={600}
              fill={sc.color}
            >
              {n.role.length > 12 ? n.role.slice(0, 10) + '..' : n.role}
            </text>
          </g>
        );
      })}
    </svg>
  );
}

export default Swarms;
