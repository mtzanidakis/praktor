import { useState, useRef, useCallback, useEffect } from 'react';

/* ── Types ── */
export interface GraphNode {
  id: string;       // agent definition ID
  role: string;     // display label
  x: number;
  y: number;
  isLead: boolean;
  prompt: string;
}
export interface GraphEdge {
  from: string;     // node id
  to: string;
  bidirectional: boolean;
}
interface AgentDef {
  id: string;
  name: string;
  description: string;
}
export interface SwarmLaunchData {
  name: string;
  task: string;
  lead_agent: string;
  agents: { agent_id: string; role: string; prompt: string; workspace: string }[];
  synapses: { from: string; to: string; bidirectional: boolean }[];
}
interface Props {
  onLaunch: (data: SwarmLaunchData) => void;
  initialData?: SwarmLaunchData;
  launchLabel?: string;
}

/* ── Constants ── */
const NODE_W = 160;
const NODE_H = 64;
const HANDLE_R = 7;

/* ── Styles ── */
const card: React.CSSProperties = {
  background: 'var(--bg-card)',
  border: '1px solid var(--border)',
  borderRadius: 10,
  padding: 16,
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
  boxSizing: 'border-box',
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
const btnSecondary: React.CSSProperties = {
  padding: '6px 14px',
  borderRadius: 7,
  border: '1px solid var(--border)',
  background: 'transparent',
  color: 'var(--text-secondary)',
  fontSize: 15,
  cursor: 'pointer',
};

export default function SwarmGraph({ onLaunch, initialData, launchLabel }: Props) {
  const [agents, setAgents] = useState<AgentDef[]>([]);
  const [nodes, setNodes] = useState<GraphNode[]>([]);
  const [edges, setEdges] = useState<GraphEdge[]>([]);
  const [selectedNode, setSelectedNode] = useState<string | null>(null);
  const [selectedEdge, setSelectedEdge] = useState<number | null>(null);
  const [name, setName] = useState('');
  const [task, setTask] = useState('');
  const [initialized, setInitialized] = useState(false);

  // Drag state
  const [dragging, setDragging] = useState<string | null>(null);
  const dragOffset = useRef({ x: 0, y: 0 });

  // Edge drawing state
  const [drawingFrom, setDrawingFrom] = useState<string | null>(null);
  const [drawMouse, setDrawMouse] = useState({ x: 0, y: 0 });

  const svgRef = useRef<SVGSVGElement>(null);

  useEffect(() => {
    fetch('/api/agents/definitions')
      .then((r) => r.json())
      .then((data) => setAgents(Array.isArray(data) ? data : []))
      .catch(() => {});
  }, []);

  // Initialize from initialData once agents are loaded
  useEffect(() => {
    if (initialized || !initialData || agents.length === 0) return;
    setName(initialData.name || '');
    setTask(initialData.task || '');
    // Build role→agent_id map from initial data
    const roleToId = new Map(initialData.agents.map((a) => [a.role, a.agent_id]));
    const initNodes: GraphNode[] = initialData.agents.map((a, i) => {
      const col = i % 3;
      const row = Math.floor(i / 3);
      return {
        id: a.agent_id,
        role: a.role,
        x: 80 + col * 220,
        y: 60 + row * 120,
        isLead: a.role === initialData.lead_agent,
        prompt: a.prompt || '',
      };
    });
    setNodes(initNodes);
    // Map synapse roles to node IDs
    const initEdges: GraphEdge[] = (initialData.synapses || []).map((s) => ({
      from: roleToId.get(s.from) || s.from,
      to: roleToId.get(s.to) || s.to,
      bidirectional: s.bidirectional,
    }));
    setEdges(initEdges);
    setInitialized(true);
  }, [agents, initialData, initialized]);

  const getSVGPoint = useCallback((e: React.MouseEvent | MouseEvent) => {
    const svg = svgRef.current;
    if (!svg) return { x: 0, y: 0 };
    const rect = svg.getBoundingClientRect();
    return { x: e.clientX - rect.left, y: e.clientY - rect.top };
  }, []);

  /* ── Add agent node ── */
  const addNode = useCallback((agent: AgentDef) => {
    if (nodes.find((n) => n.id === agent.id)) return;
    const col = nodes.length % 3;
    const row = Math.floor(nodes.length / 3);
    setNodes((prev) => [
      ...prev,
      {
        id: agent.id,
        role: agent.name,
        x: 80 + col * 220,
        y: 60 + row * 120,
        isLead: prev.length === 0,
        prompt: '',
      },
    ]);
    setSelectedNode(agent.id);
  }, [nodes]);

  /* ── Node drag ── */
  const startDrag = useCallback((e: React.MouseEvent, nodeId: string) => {
    e.stopPropagation();
    const node = nodes.find((n) => n.id === nodeId);
    if (!node) return;
    const pt = getSVGPoint(e);
    dragOffset.current = { x: pt.x - node.x, y: pt.y - node.y };
    setDragging(nodeId);
  }, [nodes, getSVGPoint]);

  const onMouseMove = useCallback((e: React.MouseEvent) => {
    const pt = getSVGPoint(e);
    if (dragging) {
      setNodes((prev) =>
        prev.map((n) =>
          n.id === dragging
            ? { ...n, x: pt.x - dragOffset.current.x, y: pt.y - dragOffset.current.y }
            : n
        )
      );
    }
    if (drawingFrom) {
      setDrawMouse(pt);
    }
  }, [dragging, drawingFrom, getSVGPoint]);

  const onMouseUp = useCallback(() => {
    setDragging(null);
    setDrawingFrom(null);
  }, []);

  /* ── Edge handles ── */
  const startEdge = useCallback((e: React.MouseEvent, nodeId: string) => {
    e.stopPropagation();
    setDrawingFrom(nodeId);
    setDrawMouse(getSVGPoint(e));
  }, [getSVGPoint]);

  const finishEdge = useCallback((nodeId: string) => {
    if (drawingFrom && drawingFrom !== nodeId) {
      // Check if edge already exists
      const exists = edges.some(
        (ed) =>
          (ed.from === drawingFrom && ed.to === nodeId) ||
          (ed.from === nodeId && ed.to === drawingFrom)
      );
      if (!exists) {
        setEdges((prev) => [
          ...prev,
          { from: drawingFrom, to: nodeId, bidirectional: false },
        ]);
      }
    }
    setDrawingFrom(null);
  }, [drawingFrom, edges]);

  /* ── Remove ── */
  const removeNode = useCallback((nodeId: string) => {
    setNodes((prev) => {
      const remaining = prev.filter((n) => n.id !== nodeId);
      // If removed node was lead, assign lead to first remaining
      if (remaining.length > 0 && !remaining.some((n) => n.isLead)) {
        remaining[0].isLead = true;
      }
      return remaining;
    });
    setEdges((prev) => prev.filter((e) => e.from !== nodeId && e.to !== nodeId));
    if (selectedNode === nodeId) setSelectedNode(null);
  }, [selectedNode]);

  const removeEdge = useCallback((idx: number) => {
    setEdges((prev) => prev.filter((_, i) => i !== idx));
    setSelectedEdge(null);
  }, []);

  const toggleEdgeDirection = useCallback((idx: number) => {
    setEdges((prev) =>
      prev.map((e, i) => (i === idx ? { ...e, bidirectional: !e.bidirectional } : e))
    );
  }, []);

  const setLead = useCallback((nodeId: string) => {
    setNodes((prev) =>
      prev.map((n) => ({ ...n, isLead: n.id === nodeId }))
    );
  }, []);

  const updatePrompt = useCallback((nodeId: string, prompt: string) => {
    setNodes((prev) =>
      prev.map((n) => (n.id === nodeId ? { ...n, prompt } : n))
    );
  }, []);

  /* ── Launch ── */
  const handleLaunch = useCallback(() => {
    const leadNode = nodes.find((n) => n.isLead);
    if (!leadNode || !task.trim()) return;
    onLaunch({
      name: name || 'Swarm',
      task,
      lead_agent: leadNode.role,
      agents: nodes.map((n) => ({
        agent_id: n.id,
        role: n.role,
        prompt: n.prompt,
        workspace: n.id,
      })),
      synapses: edges.map((e) => {
        const fromNode = nodes.find((n) => n.id === e.from);
        const toNode = nodes.find((n) => n.id === e.to);
        return {
          from: fromNode?.role || e.from,
          to: toNode?.role || e.to,
          bidirectional: e.bidirectional,
        };
      }),
    });
  }, [nodes, edges, name, task, onLaunch]);

  /* ── Helper: get edge path ── */
  const getEdgePath = (from: GraphNode, to: GraphNode) => {
    const fx = from.x + NODE_W / 2;
    const fy = from.y + NODE_H / 2;
    const tx = to.x + NODE_W / 2;
    const ty = to.y + NODE_H / 2;
    return { fx, fy, tx, ty };
  };

  const selectedNodeObj = nodes.find((n) => n.id === selectedNode);
  const selectedEdgeObj = selectedEdge !== null ? edges[selectedEdge] : null;

  return (
    <div className="conversations-layout" style={{ display: 'flex', gap: 16, height: 'calc(100vh - 180px)', minHeight: 500 }}>
      {/* Left: Agent palette */}
      <div className="conversations-agents" style={{ ...card, width: 200, overflowY: 'auto', flexShrink: 0 }}>
        <h3 style={{ fontSize: 16, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 12 }}>
          Agents
        </h3>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
          {agents.map((a) => {
            const added = nodes.some((n) => n.id === a.id);
            return (
              <button
                key={a.id}
                onClick={() => addNode(a)}
                disabled={added}
                style={{
                  textAlign: 'left',
                  padding: '8px 10px',
                  borderRadius: 7,
                  border: '1px solid var(--border)',
                  background: added ? 'var(--bg-elevated)' : 'var(--bg-input)',
                  color: added ? 'var(--text-muted)' : 'var(--text-primary)',
                  cursor: added ? 'default' : 'pointer',
                  fontSize: 15,
                  opacity: added ? 0.5 : 1,
                }}
              >
                <div style={{ fontWeight: 600 }}>{a.name}</div>
                {a.description && (
                  <div style={{ fontSize: 13, color: 'var(--text-tertiary)', marginTop: 2 }}>
                    {a.description.length > 60 ? a.description.slice(0, 60) + '...' : a.description}
                  </div>
                )}
              </button>
            );
          })}
          {agents.length === 0 && (
            <div style={{ fontSize: 14, color: 'var(--text-muted)' }}>No agents defined</div>
          )}
        </div>
      </div>

      {/* Center: SVG Canvas */}
      <div style={{ ...card, flex: 1, padding: 0, overflow: 'hidden', position: 'relative' }}>
        <svg
          ref={svgRef}
          width="100%"
          height="100%"
          style={{ display: 'block', cursor: dragging ? 'grabbing' : 'default' }}
          onMouseMove={onMouseMove}
          onMouseUp={onMouseUp}
          onClick={() => { setSelectedNode(null); setSelectedEdge(null); }}
        >
          <defs>
            <marker id="arrow" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto">
              <path d="M0,0 L8,3 L0,6" fill="var(--text-tertiary)" />
            </marker>
            <marker id="arrow-selected" markerWidth="8" markerHeight="6" refX="8" refY="3" orient="auto">
              <path d="M0,0 L8,3 L0,6" fill="var(--accent)" />
            </marker>
          </defs>

          {/* Edges */}
          {edges.map((edge, i) => {
            const fromNode = nodes.find((n) => n.id === edge.from);
            const toNode = nodes.find((n) => n.id === edge.to);
            if (!fromNode || !toNode) return null;
            const { fx, fy, tx, ty } = getEdgePath(fromNode, toNode);
            const isSelected = selectedEdge === i;
            const color = isSelected ? 'var(--accent)' : 'var(--text-tertiary)';
            const markerEnd = edge.bidirectional ? undefined : `url(#arrow${isSelected ? '-selected' : ''})`;
            const markerStart = edge.bidirectional ? `url(#arrow${isSelected ? '-selected' : ''})` : undefined;
            return (
              <g key={`edge-${i}`}>
                {/* Wider invisible hitbox */}
                <line
                  x1={fx} y1={fy} x2={tx} y2={ty}
                  stroke="transparent" strokeWidth={16}
                  style={{ cursor: 'pointer' }}
                  onClick={(e) => { e.stopPropagation(); setSelectedEdge(i); setSelectedNode(null); }}
                />
                <line
                  x1={fx} y1={fy} x2={tx} y2={ty}
                  stroke={color}
                  strokeWidth={isSelected ? 2.5 : 1.5}
                  markerEnd={markerEnd}
                  markerStart={markerStart}
                  strokeDasharray={edge.bidirectional ? '6,4' : undefined}
                  style={{ pointerEvents: 'none' }}
                />
                {edge.bidirectional && (
                  <text
                    x={(fx + tx) / 2} y={(fy + ty) / 2 - 8}
                    textAnchor="middle" fontSize={12} fill={color}
                    style={{ pointerEvents: 'none' }}
                  >
                    collab
                  </text>
                )}
              </g>
            );
          })}

          {/* Drawing edge */}
          {drawingFrom && (() => {
            const fromNode = nodes.find((n) => n.id === drawingFrom);
            if (!fromNode) return null;
            return (
              <line
                x1={fromNode.x + NODE_W / 2}
                y1={fromNode.y + NODE_H / 2}
                x2={drawMouse.x}
                y2={drawMouse.y}
                stroke="var(--accent)"
                strokeWidth={2}
                strokeDasharray="6,4"
                style={{ pointerEvents: 'none' }}
              />
            );
          })()}

          {/* Nodes */}
          {nodes.map((node) => {
            const isSelected = selectedNode === node.id;
            return (
              <g key={node.id}>
                {/* Node body */}
                <rect
                  x={node.x} y={node.y}
                  width={NODE_W} height={NODE_H}
                  rx={10}
                  fill="var(--bg-elevated)"
                  stroke={node.isLead ? '#f59e0b' : isSelected ? 'var(--accent)' : 'var(--border)'}
                  strokeWidth={node.isLead || isSelected ? 2.5 : 1}
                  style={{ cursor: 'grab' }}
                  onMouseDown={(e) => startDrag(e, node.id)}
                  onClick={(e) => { e.stopPropagation(); setSelectedNode(node.id); setSelectedEdge(null); }}
                  onMouseUp={() => finishEdge(node.id)}
                />
                {/* Name */}
                <text
                  x={node.x + NODE_W / 2} y={node.y + 26}
                  textAnchor="middle"
                  fontSize={15} fontWeight={600}
                  fill="var(--text-primary)"
                  style={{ pointerEvents: 'none' }}
                >
                  {node.role.length > 16 ? node.role.slice(0, 14) + '..' : node.role}
                </text>
                {/* Agent ID */}
                <text
                  x={node.x + NODE_W / 2} y={node.y + 44}
                  textAnchor="middle"
                  fontSize={12}
                  fill="var(--text-muted)"
                  style={{ pointerEvents: 'none' }}
                >
                  {node.id}
                </text>
                {/* Lead crown */}
                {node.isLead && (
                  <text
                    x={node.x + NODE_W - 12} y={node.y + 16}
                    fontSize={16} style={{ pointerEvents: 'none' }}
                  >
                    {'★'}
                  </text>
                )}
                {/* Connection handle (right side) */}
                <circle
                  cx={node.x + NODE_W} cy={node.y + NODE_H / 2}
                  r={HANDLE_R}
                  fill="var(--bg-card)"
                  stroke="var(--accent)"
                  strokeWidth={1.5}
                  style={{ cursor: 'crosshair' }}
                  onMouseDown={(e) => startEdge(e, node.id)}
                />
                {/* Connection handle (left side) */}
                <circle
                  cx={node.x} cy={node.y + NODE_H / 2}
                  r={HANDLE_R}
                  fill="var(--bg-card)"
                  stroke="var(--accent)"
                  strokeWidth={1.5}
                  style={{ cursor: 'crosshair' }}
                  onMouseDown={(e) => startEdge(e, node.id)}
                  onMouseUp={() => finishEdge(node.id)}
                />
              </g>
            );
          })}

          {/* Empty state */}
          {nodes.length === 0 && (
            <text x="50%" y="50%" textAnchor="middle" fontSize={16} fill="var(--text-muted)">
              Click agents in the palette to add them to the canvas
            </text>
          )}
        </svg>
      </div>

      {/* Right: Properties panel */}
      <div style={{ ...card, width: 260, overflowY: 'auto', flexShrink: 0, display: 'flex', flexDirection: 'column', gap: 16 }}>
        <div>
          <label style={{ fontSize: 14, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>
            Swarm Name
          </label>
          <input
            style={inputStyle}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Research Team"
          />
        </div>
        <div>
          <label style={{ fontSize: 14, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>
            Task
          </label>
          <textarea
            style={{ ...inputStyle, minHeight: 80, resize: 'vertical' }}
            value={task}
            onChange={(e) => setTask(e.target.value)}
            placeholder="Describe what this swarm should accomplish..."
          />
        </div>

        {/* Selected node properties */}
        {selectedNodeObj && (
          <div style={{ borderTop: '1px solid var(--border)', paddingTop: 12 }}>
            <h4 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8 }}>
              {selectedNodeObj.role}
            </h4>
            <div style={{ display: 'flex', gap: 6, marginBottom: 10 }}>
              <button
                style={{
                  ...btnSecondary,
                  background: selectedNodeObj.isLead ? '#f59e0b22' : undefined,
                  borderColor: selectedNodeObj.isLead ? '#f59e0b' : undefined,
                  color: selectedNodeObj.isLead ? '#f59e0b' : undefined,
                }}
                onClick={() => setLead(selectedNodeObj.id)}
              >
                {'★'} {selectedNodeObj.isLead ? 'Lead' : 'Set Lead'}
              </button>
              <button
                style={{ ...btnSecondary, color: 'var(--red)' }}
                onClick={() => removeNode(selectedNodeObj.id)}
              >
                Remove
              </button>
            </div>
            <label style={{ fontSize: 14, color: 'var(--text-tertiary)', display: 'block', marginBottom: 4 }}>
              Agent Prompt
            </label>
            <textarea
              style={{ ...inputStyle, minHeight: 60, resize: 'vertical' }}
              value={selectedNodeObj.prompt}
              onChange={(e) => updatePrompt(selectedNodeObj.id, e.target.value)}
              placeholder="Per-agent instructions..."
            />
          </div>
        )}

        {/* Selected edge properties */}
        {selectedEdgeObj && (
          <div style={{ borderTop: '1px solid var(--border)', paddingTop: 12 }}>
            <h4 style={{ fontSize: 15, fontWeight: 600, color: 'var(--text-primary)', marginBottom: 8 }}>
              Connection
            </h4>
            <div style={{ fontSize: 14, color: 'var(--text-secondary)', marginBottom: 8 }}>
              {nodes.find((n) => n.id === selectedEdgeObj.from)?.role}
              {selectedEdgeObj.bidirectional ? ' ↔ ' : ' → '}
              {nodes.find((n) => n.id === selectedEdgeObj.to)?.role}
            </div>
            <div style={{ display: 'flex', gap: 6 }}>
              <button
                style={{
                  ...btnSecondary,
                  background: selectedEdgeObj.bidirectional ? 'var(--accent-muted)' : undefined,
                }}
                onClick={() => toggleEdgeDirection(selectedEdge!)}
              >
                {selectedEdgeObj.bidirectional ? 'Collaborative ↔' : 'Pipeline →'}
              </button>
              <button
                style={{ ...btnSecondary, color: 'var(--red)' }}
                onClick={() => removeEdge(selectedEdge!)}
              >
                Delete
              </button>
            </div>
          </div>
        )}

        {/* Legend */}
        <div style={{ borderTop: '1px solid var(--border)', paddingTop: 12, marginTop: 'auto' }}>
          <div style={{ fontSize: 13, color: 'var(--text-muted)', lineHeight: 1.8 }}>
            <div>{'→'} Pipeline: B waits for A</div>
            <div>{'↔'} Collaborative: shared chat</div>
            <div>No connection: parallel</div>
            <div>{'★'} Lead: synthesizes results</div>
          </div>
        </div>

        {/* Launch button */}
        <button
          style={{
            ...btnPrimary,
            width: '100%',
            padding: '10px 20px',
            opacity: nodes.length < 2 || !task.trim() ? 0.5 : 1,
          }}
          disabled={nodes.length < 2 || !task.trim()}
          onClick={handleLaunch}
        >
          {launchLabel || `Launch Swarm (${nodes.length} agents)`}
        </button>
      </div>
    </div>
  );
}
