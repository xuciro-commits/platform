// Editable presentation of registered semantic assets. Graph and NodeCanvas
// share the UI kit's frame, viewport and edge language; the owning capability
// applies edits to its definition. This canvas executes no business logic.
import { ConnectionLineType, Handle, MarkerType, Position, ReactFlow, useEdgesState, useNodesState, useUpdateNodeInternals, type Connection, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useEffect, useRef, useState } from "react";
import { cn } from "../lib/cn";
import { t } from "../i18n";
import { CanvasFrame, CanvasFurniture, CanvasRefit, fitting } from "./CanvasFrame";

export type NodePort = { id: string; label: string; type: string; limit?: number };
export type NodeKind = { id: string; title: string; description?: string; category: string; inputs: NodePort[]; outputs: NodePort[] };
export type CanvasNode = { id: string; kind: string; label: string; detail?: string; position: { x: number; y: number } };
export type CanvasEdge = { id: string; source: string; sourcePort: string; target: string; targetPort: string };
export type NodeCatalog = readonly NodeKind[];
type ConnectionIssue = "endpoint" | "port" | "duplicate" | "source-capacity" | "target-capacity";

/** A semantic adapter may add stricter rules; the kit checks endpoints, port types and capacity. */
export function validateCanvasConnection(connection: Connection, nodes: readonly CanvasNode[], edges: readonly CanvasEdge[], catalog: NodeCatalog): ConnectionIssue | undefined {
  const source = nodes.find((n) => n.id === connection.source);
  const target = nodes.find((n) => n.id === connection.target);
  if (!source || !target || source.id === target.id) return "endpoint";
  const out = catalog.find((k) => k.id === source.kind)?.outputs.find((p) => p.id === connection.sourceHandle);
  const into = catalog.find((k) => k.id === target.kind)?.inputs.find((p) => p.id === connection.targetHandle);
  if (!out || !into || out.type !== into.type) return "port";
  if (edges.some((e) => e.source === source.id && e.sourcePort === out.id && e.target === target.id && e.targetPort === into.id)) return "duplicate";
  if (out.limit !== undefined && edges.filter((e) => e.source === source.id && e.sourcePort === out.id).length >= out.limit) return "source-capacity";
  if (into.limit !== undefined && edges.filter((e) => e.target === target.id && e.targetPort === into.id).length >= into.limit) return "target-capacity";
  return undefined;
}

const nodeWidth = 160, headerHeight = 54, portHeight = 24;
export const canvasNodeHeight = (kind: NodeKind) => headerHeight + Math.max(kind.inputs.length, kind.outputs.length) * portHeight + 12;
type Data = Record<string, unknown> & { label: string; detail?: string; kind: NodeKind };
type FlowNode = Node<Data>;

function SemanticNode({ id, data, selected }: NodeProps<FlowNode>) {
  const updateNodeInternals = useUpdateNodeInternals();
  const ports = [...data.kind.inputs.map((p) => `i:${p.id}`), ...data.kind.outputs.map((p) => `o:${p.id}`)].join("|");
  useEffect(() => { updateNodeInternals(id); }, [id, ports, updateNodeInternals]);
  return <div className={cn("relative rounded-lg border-2 bg-surface text-left shadow-sm", selected ? "border-primary ring-2 ring-primary/20" : "border-border")}
    style={{ width: nodeWidth, height: canvasNodeHeight(data.kind) }}>
    <div className="px-3 pt-2">
      <div className="text-[10px] font-semibold uppercase tracking-wide text-muted">{data.kind.title}</div>
      <div className="truncate text-sm font-medium text-foreground" title={data.label}>{data.label}</div>
      {data.detail && <div className="truncate text-[11px] text-muted" title={data.detail}>{data.detail}</div>}
    </div>
    {data.kind.inputs.map((port, i) => <div key={port.id} className="absolute left-3 right-1/2 flex items-center text-[10px] text-muted"
      style={{ top: headerHeight + i * portHeight, height: portHeight }} title={`${port.label}: ${port.type}`}>
      <span className="truncate">{port.label}</span>
      <Handle type="target" id={port.id} position={Position.Left} className="!size-2 !border-0 !bg-muted"
        style={{ top: portHeight / 2, left: -16 }} />
    </div>)}
    {data.kind.outputs.map((port, i) => <div key={port.id} className="absolute left-1/2 right-3 flex items-center justify-end text-[10px] text-muted"
      style={{ top: headerHeight + i * portHeight, height: portHeight }} title={`${port.label}: ${port.type}`}>
      <span className="truncate">{port.label}</span>
      <Handle type="source" id={port.id} position={Position.Right} className="!size-2 !border-0 !bg-muted"
        style={{ top: portHeight / 2, right: -16 }} />
    </div>)}
  </div>;
}

const nodeTypes = { semantic: SemanticNode };
const missingKind = (id: string): NodeKind => ({ id, title: id, category: "unknown", inputs: [], outputs: [] });
function toFlowNode(n: CanvasNode, catalog: NodeCatalog): FlowNode {
  const kind = catalog.find((k) => k.id === n.kind) ?? missingKind(n.kind);
  return { id: n.id, type: "semantic", position: n.position, deletable: false, data: { label: n.label, detail: n.detail, kind } };
}
function toFlowEdge(e: CanvasEdge): Edge {
  return { id: e.id, source: e.source, sourceHandle: e.sourcePort, target: e.target, targetHandle: e.targetPort,
    type: "smoothstep", markerEnd: { type: MarkerType.ArrowClosed, color: "var(--muted)", width: 16, height: 16 },
    style: { stroke: "var(--muted)", strokeWidth: 1.5 } };
}

/** Controlled graph editor. React Flow owns local drag/selection; the adapter owns semantic connections. */
export function NodeCanvas({ catalog, nodes, edges, selected, onSelect, onConnect, onDisconnect, onAdd, canConnect, label, height = 320 }: {
  catalog: NodeCatalog; nodes: CanvasNode[]; edges: CanvasEdge[]; selected?: string;
  onSelect?: (id: string) => void; onConnect?: (connection: Connection) => void; onDisconnect?: (edges: CanvasEdge[]) => void;
  onAdd?: (kind: string) => void; canConnect?: (connection: Connection) => boolean; label: string; height?: number;
}) {
  const [flowNodes, setFlowNodes, onNodesChange] = useNodesState<FlowNode>(nodes.map((n) => toFlowNode(n, catalog)));
  const [flowEdges, setFlowEdges, onEdgesChange] = useEdgesState(edges.map(toFlowEdge));
  const moved = useRef<Record<string, { x: number; y: number }>>({});
  const lastIds = useRef(nodes.map((n) => n.id).join("|"));
  const [connectionIssue, setConnectionIssue] = useState<string>();
  // A parent edit changes the semantic graph. Keep dragged positions for nodes
  // that still exist, but never re-send local positions as business meaning.
  useEffect(() => {
    const ids = nodes.map((n) => n.id).join("|");
    const relayout = ids !== lastIds.current;
    lastIds.current = ids;
    setFlowNodes((previous) => {
      const old = new Map(previous.map((n) => [n.id, n]));
      const current = new Set(nodes.map((n) => n.id));
      for (const id of Object.keys(moved.current)) if (!current.has(id)) delete moved.current[id];
      const next = nodes.map((n) => {
        const next = toFlowNode(n, catalog);
        const before = old.get(n.id);
        // Semantic edits do not erase React Flow's measured dimensions. If
        // the DOM size stays the same, ResizeObserver need not fire again;
        // dropping these leaves otherwise unchanged nodes hidden forever.
        return { ...next, measured: before?.measured, position: moved.current[n.id] ?? (relayout ? next.position : before?.position ?? next.position), selected: n.id === selected };
      });
      return !relayout && next.length === previous.length && next.every((n, i) => {
        const before = previous[i];
        return before && n.id === before.id && n.position.x === before.position.x && n.position.y === before.position.y
          && n.selected === before.selected && n.data.label === before.data.label && n.data.detail === before.data.detail
          && n.data.kind.id === before.data.kind.id && n.data.kind.title === before.data.kind.title
          && JSON.stringify(n.data.kind.inputs) === JSON.stringify(before.data.kind.inputs)
          && JSON.stringify(n.data.kind.outputs) === JSON.stringify(before.data.kind.outputs);
      }) ? previous : next;
    });
  }, [catalog, nodes, selected, setFlowNodes]);
  useEffect(() => { setFlowEdges((before) => edges.length === before.length && edges.every((e, i) => {
    const old = before[i];
    return old?.id === e.id && old.source === e.source && old.sourceHandle === e.sourcePort && old.target === e.target && old.targetHandle === e.targetPort;
  }) ? before : edges.map(toFlowEdge)); }, [edges, setFlowEdges]);
  const valid = (c: Connection | Edge) => !validateCanvasConnection(c as Connection, nodes, edges, catalog) && (!canConnect || canConnect(c as Connection));
  return <CanvasFrame label={label} height={height} role="region">
    <ReactFlow nodes={flowNodes} edges={flowEdges} nodeTypes={nodeTypes} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}
      fitView fitViewOptions={fitting} colorMode="system" minZoom={0.2} maxZoom={1.6} zoomOnScroll={false} preventScrolling={false}
      nodesConnectable={!!onConnect} edgesFocusable={!!onDisconnect} deleteKeyCode={onDisconnect ? ["Backspace", "Delete"] : null}
      connectionLineType={ConnectionLineType.SmoothStep} isValidConnection={valid} onConnect={onConnect}
      onConnectStart={() => setConnectionIssue(undefined)}
      onConnectEnd={(_, state) => {
        if (state.isValid) return;
        const from = state.fromHandle, to = state.toHandle;
        if (!from || !to || from.type === to.type) {
          setConnectionIssue(t("Connect an output to a compatible input port."));
          return;
        }
        const source = from.type === "source" ? from : to;
        const target = from.type === "target" ? from : to;
        const issue = validateCanvasConnection({ source: source.nodeId, sourceHandle: source.id ?? null,
          target: target.nodeId, targetHandle: target.id ?? null }, nodes, edges, catalog);
        const reasons: Record<ConnectionIssue, string> = {
          endpoint: "Choose two different nodes.", port: "These ports have different types.",
          duplicate: "This connection already exists.", "source-capacity": "This output already has its allowed connection.",
          "target-capacity": "This input already has its allowed connections.",
        };
        setConnectionIssue(t(issue ? reasons[issue] : "This connection is not allowed."));
      }}
      onEdgesDelete={(gone) => onDisconnect?.(edges.filter((e) => gone.some((x) => x.id === e.id)))}
      onNodeClick={(_, n) => onSelect?.(n.id)} onNodeDragStop={(_, n) => { moved.current[n.id] = n.position; }}>
      <CanvasRefit signature={nodes.map((n) => n.id).join("|")} />
      <CanvasFurniture />
    </ReactFlow>
    {onAdd && <div className="absolute left-2 top-2 z-10 flex max-w-[70%] flex-wrap gap-1 rounded-md bg-surface/95 p-1 shadow-sm" aria-label={label}>
      {catalog.map((kind) => <button key={kind.id} type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-row-hover" title={kind.description} onClick={() => onAdd(kind.id)}>{kind.title}</button>)}
    </div>}
    {connectionIssue && <div role="alert" className="pointer-events-none absolute bottom-2 left-2 z-20 max-w-[70%] rounded border border-border bg-surface px-2 py-1 text-xs text-foreground shadow-sm">{connectionIssue}</div>}
  </CanvasFrame>;
}
