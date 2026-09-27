// Editable presentation of registered semantic assets. The owner supplies the
// catalog and applies edits to its own typed definition; this canvas executes
// no business logic and stores no second copy of that definition (ADR-0040 D4).
import { Background, Controls, Handle, MarkerType, Position, ReactFlow, useReactFlow, useStore, type Connection, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useEffect, useMemo, useState } from "react";
import { cn } from "../lib/cn";

export type NodePort = { id: string; label: string; type: string; limit?: number };
export type NodeKind = { id: string; title: string; description?: string; category: string; inputs: NodePort[]; outputs: NodePort[] };
export type CanvasNode = { id: string; kind: string; label: string; detail?: string; position: { x: number; y: number } };
export type CanvasEdge = { id: string; source: string; sourcePort: string; target: string; targetPort: string };
export type NodeCatalog = readonly NodeKind[];

/** A semantic adapter may add stricter rules; the kit always checks endpoint, port type and capacity. */
export function validateCanvasConnection(connection: Connection, nodes: readonly CanvasNode[], edges: readonly CanvasEdge[], catalog: NodeCatalog): string | undefined {
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

type Data = Record<string, unknown> & { label: string; detail?: string; kind: NodeKind };

function SemanticNode({ data, selected }: NodeProps<Node<Data>>) {
  const height = 76 + Math.max(data.kind.inputs.length, data.kind.outputs.length) * 25;
  return <div className={cn("relative w-48 rounded-lg border bg-surface px-3 py-2 text-left shadow-sm", selected ? "border-primary ring-2 ring-primary/20" : "border-border")} style={{ minHeight: height }}>
    <div className="text-[10px] font-semibold uppercase tracking-wide text-muted">{data.kind.title}</div>
    <div className="truncate text-sm font-medium text-foreground" title={data.label}>{data.label}</div>
    {data.detail && <div className="truncate text-[11px] text-muted" title={data.detail}>{data.detail}</div>}
    {data.kind.inputs.map((port, i) => <div key={port.id} className="relative mt-1 text-[10px] text-muted">
      <Handle type="target" id={port.id} position={Position.Left} style={{ top: 73 + i * 25, left: -17 }} title={`${port.label}: ${port.type}`} />{port.label}
    </div>)}
    {data.kind.outputs.map((port, i) => <div key={port.id} className="absolute right-3 text-right text-[10px] text-muted" style={{ top: 68 + i * 25 }}>
      {port.label}<Handle type="source" id={port.id} position={Position.Right} style={{ top: 7, right: -17 }} title={`${port.label}: ${port.type}`} />
    </div>)}
  </div>;
}

const nodeTypes = { semantic: SemanticNode };
const fitting = { padding: 0.16, maxZoom: 1 };

// Initial fit uses the catalog's declared node dimensions, so an editor that
// updates a label does not briefly zoom to one measured node. It responds to
// docking and narrow screens, while dragging a node remains local view state.
function Refit({ nodes }: { nodes: CanvasNode[] }) {
  const { setViewport } = useReactFlow();
  const width = useStore((s) => s.width), height = useStore((s) => s.height);
  const geometry = nodes.map((n) => `${n.id}:${n.position.x}:${n.position.y}`).join("|");
  useEffect(() => {
    if (!nodes.length || width <= 0 || height <= 0) return;
    const left = Math.min(...nodes.map((n) => n.position.x));
    const top = Math.min(...nodes.map((n) => n.position.y));
    const right = Math.max(...nodes.map((n) => n.position.x + 192));
    const bottom = Math.max(...nodes.map((n) => n.position.y + 126));
    const zoom = Math.max(0.3, Math.min(1, (width - 48) / (right - left), (height - 48) / (bottom - top)));
    void setViewport({ x: (width - (right - left) * zoom) / 2 - left * zoom,
      y: (height - (bottom - top) * zoom) / 2 - top * zoom, zoom });
  // geometry is the stable identity/layout signal; labels and selection must not reset a person's zoom.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [setViewport, width, height, geometry]);
  return null;
}

/** Controlled graph editor. Node positions are view state; connections are sent to the semantic owner. */
export function NodeCanvas({ catalog, nodes, edges, selected, onSelect, onConnect, onDisconnect, onAdd, canConnect, label, height = 340 }: {
  catalog: NodeCatalog; nodes: CanvasNode[]; edges: CanvasEdge[]; selected?: string;
  onSelect?: (id: string) => void; onConnect?: (connection: Connection) => void; onDisconnect?: (edges: CanvasEdge[]) => void;
  onAdd?: (kind: string) => void; canConnect?: (connection: Connection) => boolean; label: string; height?: number;
}) {
  const [positions, setPositions] = useState<Record<string, { x: number; y: number }>>({});
  const flow = useMemo(() => ({
    nodes: nodes.map((n): Node<Data> => {
      const kind = catalog.find((k) => k.id === n.kind)!;
      return { id: n.id, type: "semantic", position: positions[n.id] ?? n.position,
        data: { label: n.label, detail: n.detail, kind }, selected: n.id === selected,
        width: 192, height: 76 + Math.max(kind.inputs.length, kind.outputs.length) * 25 };
    }),
    edges: edges.map((e): Edge => ({ id: e.id, source: e.source, sourceHandle: e.sourcePort, target: e.target, targetHandle: e.targetPort,
      type: "smoothstep", markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 }, style: { strokeWidth: 1.5 } })),
  }), [catalog, edges, nodes, positions, selected]);
  const valid = (c: Connection | Edge) => !validateCanvasConnection(c as Connection, nodes, edges, catalog) && (!canConnect || canConnect(c as Connection));
  return <div className="relative overflow-hidden rounded-md border border-border bg-background" style={{ height }} role="region" aria-label={label}>
    <ReactFlow nodes={flow.nodes} edges={flow.edges} nodeTypes={nodeTypes} fitView fitViewOptions={fitting}
      colorMode="system" minZoom={0.3} maxZoom={1.5} zoomOnScroll={false} preventScrolling={false}
      nodesConnectable={!!onConnect} edgesFocusable={!!onDisconnect} deleteKeyCode={onDisconnect ? ["Backspace", "Delete"] : null}
      isValidConnection={valid} onConnect={onConnect} onEdgesDelete={(gone) => onDisconnect?.(edges.filter((e) => gone.some((x) => x.id === e.id)))}
      onNodeClick={(_, n) => onSelect?.(n.id)} onNodeDragStop={(_, n) => setPositions((at) => ({ ...at, [n.id]: n.position }))}>
      <Refit nodes={nodes} />
      <Background gap={16} size={1} color="var(--border)" />
      <Controls showInteractive={false} position="bottom-right" />
    </ReactFlow>
    {onAdd && <div className="absolute left-2 top-2 z-10 flex max-w-[70%] flex-wrap gap-1 rounded-md bg-surface/95 p-1 shadow-sm" aria-label={label}>
      {catalog.map((kind) => <button key={kind.id} type="button" className="rounded border border-border px-2 py-1 text-xs hover:bg-row-hover" title={kind.description} onClick={() => onAdd(kind.id)}>{kind.title}</button>)}
    </div>}
  </div>;
}
