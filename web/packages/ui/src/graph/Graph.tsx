// A read-only graph (#122, F-42): the steps of a process and how they connect,
// drawn on one canvas for every app — flows and their instances, routings,
// approval chains, agent runs. Apps give nodes and edges; the canvas lays them
// out in layers along the direction of flow, so nobody places boxes by hand.
import { Handle, MarkerType, Position, ReactFlow, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useMemo, type ReactNode } from "react";
import { cn } from "../lib/cn";
import type { Tone } from "../components/StatusTag";
import { CanvasFrame, CanvasFurniture, CanvasRefit, fitting } from "./CanvasFrame";

export type GraphNode = {
  id: string;
  label: string;
  /** A second line: a kind, who, a count. */
  detail?: string;
  /** What the process did here: done (success), here now (info), waiting (warning), failed (danger), not reached (neutral). */
  tone?: Tone;
  /** Marks where the process stands now. */
  current?: boolean;
};
export type GraphEdge = { from: string; to: string; label?: string; dashed?: boolean; tone?: Tone };

const width = 160, height = 54, gapX = 40, gapY = 24;
export type GraphSize = { width: number; height: number; gapX: number; gapY: number };

/**
 * Positions nodes in layers: each node one layer past the furthest node
 * leading to it, edges that lead back (a retry, a rework) ignored for
 * layering; within a layer, in the order given, centred on the widest layer.
 */
export function layout(nodes: GraphNode[], edges: GraphEdge[], direction: "right" | "down" = "right", size: GraphSize = { width, height, gapX, gapY }): Map<string, { x: number; y: number }> {
  const ids = new Set(nodes.map((n) => n.id));
  const out = new Map<string, string[]>(nodes.map((n) => [n.id, []]));
  for (const e of edges) if (ids.has(e.from) && ids.has(e.to)) out.get(e.from)!.push(e.to);
  // Depth-first from each node in order: an edge to a node still on the path leads back.
  const back = new Set<string>(), state = new Map<string, "open" | "done">(), order: string[] = [];
  const visit = (id: string) => {
    state.set(id, "open");
    for (const to of out.get(id)!) {
      if (state.get(to) === "open") back.add(`${id}>${to}`);
      else if (!state.has(to)) visit(to);
    }
    state.set(id, "done");
    order.push(id);
  };
  for (const n of nodes) if (!state.has(n.id)) visit(n.id);
  const layer = new Map<string, number>(nodes.map((n) => [n.id, 0]));
  for (const id of order.reverse()) { // topological order of the forward edges
    for (const to of out.get(id)!) if (!back.has(`${id}>${to}`)) layer.set(to, Math.max(layer.get(to)!, layer.get(id)! + 1));
  }
  const layers: string[][] = [];
  for (const n of nodes) (layers[layer.get(n.id)!] ??= []).push(n.id);
  const widest = Math.max(1, ...layers.map((l) => l?.length ?? 0));
  const positions = new Map<string, { x: number; y: number }>();
  layers.forEach((l, i) => l?.forEach((id, j) => {
    const along = i * ((direction === "right" ? size.width : size.height) + (direction === "right" ? size.gapX : size.gapY * 2));
    const across = (j + (widest - l.length) / 2) * (direction === "right" ? size.height + size.gapY : size.width + size.gapX / 2);
    positions.set(id, direction === "right" ? { x: along, y: across } : { x: across, y: along });
  }));
  return positions;
}

const toneColor = (tone: Tone | undefined) => tone && tone !== "neutral" ? `var(--tone-${tone})` : "var(--border)";

type Data = GraphNode & { direction: "right" | "down" };

function Box({ data }: NodeProps<Node<Data>>) {
  const [into, outOf] = data.direction === "right" ? [Position.Left, Position.Right] : [Position.Top, Position.Bottom];
  return (
    <div style={{ width, height, borderColor: toneColor(data.tone) }}
      className={cn("grid content-center gap-0.5 rounded-lg border-2 bg-surface px-2.5 text-left shadow-sm", data.current && "ring-2 ring-[var(--tone-info)] ring-offset-2 ring-offset-background")}>
      <Handle type="target" position={into} className="!size-1.5 !border-0 !bg-border" isConnectable={false} />
      <span className="truncate text-xs font-medium text-foreground" title={data.label}>{data.label}</span>
      {data.detail && <span className="truncate text-[11px] text-muted" title={data.detail}>{data.detail}</span>}
      <Handle type="source" position={outOf} className="!size-1.5 !border-0 !bg-border" isConnectable={false} />
    </div>
  );
}

const nodeTypes = { box: Box };
/** A graph of steps, laid out along its direction; read-only. The wheel scrolls the page it sits in; the controls, a pinch or a drag zoom and pan it. A node opens what it stands for through `onOpen`. */
export function Graph({ nodes, edges, direction = "right", height: tall = 280, onOpen, label, children }: {
  nodes: GraphNode[]; edges: GraphEdge[]; direction?: "right" | "down"; height?: number;
  onOpen?: (node: GraphNode) => void; label?: string; children?: ReactNode;
}) {
  const flow = useMemo(() => {
    const at = layout(nodes, edges, direction);
    const byId = new Map(nodes.map((n) => [n.id, n]));
    return {
      nodes: nodes.map((n): Node<Data> => ({ id: n.id, type: "box", position: at.get(n.id)!, data: { ...n, direction },
        width, height, draggable: false, connectable: false })),
      edges: edges.filter((e) => byId.has(e.from) && byId.has(e.to)).map((e, i): Edge => {
        const color = e.tone ? toneColor(e.tone) : "var(--muted)";
        return {
          id: `${i}:${e.from}>${e.to}`, source: e.from, target: e.to, label: e.label, type: "smoothstep",
          style: { stroke: color, strokeWidth: 1.5, strokeDasharray: e.dashed ? "5 4" : undefined },
          markerEnd: { type: MarkerType.ArrowClosed, color, width: 16, height: 16 },
          labelStyle: { fontSize: 11, fill: "var(--muted)" }, labelBgStyle: { fill: "var(--background)" },
        };
      }),
    };
  }, [nodes, edges, direction]);
  return (
    <CanvasFrame height={tall} label={label}>
      <ReactFlow nodes={flow.nodes} edges={flow.edges} nodeTypes={nodeTypes} fitView fitViewOptions={fitting}
        minZoom={0.2} maxZoom={1.6} zoomOnScroll={false} preventScrolling={false} nodesDraggable={false} nodesConnectable={false} edgesFocusable={false} colorMode="system"
        onNodeClick={onOpen && ((_, n) => onOpen(n.data as GraphNode))}>
        <CanvasRefit signature={flow.nodes.map((n) => `${n.id}:${n.position.x}:${n.position.y}`).join("|")} />
        <CanvasFurniture />
      </ReactFlow>
      {children}
    </CanvasFrame>
  );
}
