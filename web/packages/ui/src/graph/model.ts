import type { Connection } from "@xyflow/react";
import type { ReactNode } from "react";
import type { Tone } from "../components/StatusTag";

export type CanvasPosition = { x: number; y: number };
export type CirclePresentation = {badge:string;size:number};
export type NodePort = {
  id: string; label: string; type: string; limit?: number;
  /** Control ports determine execution order; data ports bind typed values. */
  channel?: "control" | "data";
  description?: string;
};
export type NodeKind = {
  id: string; title: string; description?: string; category: string;
  inputs: NodePort[]; outputs: NodePort[];
  icon?: ReactNode; tone?: Tone;
  /** Instance projections can customize ports without appearing as palette entries. */
  addable?: boolean;
};
export type BlockStatus = "idle" | "running" | "waiting" | "success" | "error" | "skipped";
export type BlockDiagnostic = { message: string; severity: "error" | "warning" | "info"; port?: string };
export type CanvasNode = {
  id: string; kind: string; label: string; detail?: string; position: CanvasPosition;
  version?: string; status?: BlockStatus; diagnostics?: BlockDiagnostic[]; collapsed?: boolean;
  tone?: Tone; current?: boolean; compact?: boolean;
  /** A read-only graph's visual badge; it is never a semantic node identity. */
  circle?:CirclePresentation;
};
export type CanvasEdge = {
  id: string; source: string; sourcePort: string; target: string; targetPort: string;
  label?: string; channel?: "control" | "data"; tone?: Tone; dashed?: boolean;
  directed?:boolean;
};
export type NodeCatalog = readonly NodeKind[];
export type CanvasAddContext = {
  position: CanvasPosition;
  /** Auto-offset for existing blocks, applied by the owner together with insertion. */
  positions?: Record<string, CanvasPosition>;
  source?: { node: string; port: string };
  target?: { node: string; port: string };
  edge?: CanvasEdge;
};
export type CanvasHistory = { canUndo: boolean; canRedo: boolean; onUndo: () => void; onRedo: () => void };
export type ConnectionIssue = "endpoint" | "port" | "duplicate" | "source-capacity" | "target-capacity";

/** The semantic owner may add stricter rules, such as acyclicity or branch scope. */
export function validateCanvasConnection(connection: Connection, nodes: readonly CanvasNode[], edges: readonly CanvasEdge[], catalog: NodeCatalog): ConnectionIssue | undefined {
  const source = nodes.find((n) => n.id === connection.source);
  const target = nodes.find((n) => n.id === connection.target);
  if (!source || !target || source.id === target.id) return "endpoint";
  const out = catalog.find((k) => k.id === source.kind)?.outputs.find((p) => p.id === connection.sourceHandle);
  const into = catalog.find((k) => k.id === target.kind)?.inputs.find((p) => p.id === connection.targetHandle);
  if (!out || !into || out.type !== into.type && !(into.channel === "data" && into.type === "json") || (out.channel ?? "control") !== (into.channel ?? "control")) return "port";
  if (edges.some((e) => e.source === source.id && e.sourcePort === out.id && e.target === target.id && e.targetPort === into.id)) return "duplicate";
  if (out.limit !== undefined && edges.filter((e) => e.source === source.id && e.sourcePort === out.id).length >= out.limit) return "source-capacity";
  if (into.limit !== undefined && edges.filter((e) => e.target === target.id && e.targetPort === into.id).length >= into.limit) return "target-capacity";
  return undefined;
}

export const canvasNodeWidth = 216;
export const blockNodeHeight = (kind: NodeKind, compact = false, collapsed = false) => compact ? 58 : collapsed ? 74 : 96 + Math.max(kind.inputs.length, kind.outputs.length) * 27;
export const canvasNodeHeight = (kind: NodeKind) => blockNodeHeight(kind);

/** Find a free tile for a newly added block without moving the builder's existing layout. */
export function canvasPlacement(position: CanvasPosition, height: number, boxes: { position: CanvasPosition; width: number; height: number }[]): CanvasPosition {
  let candidate = { ...position };
  for (let attempt = 0; attempt < boxes.length; attempt++) {
    const obstacle = boxes.find((box) => candidate.x < box.position.x + box.width + 24 && candidate.x + canvasNodeWidth + 24 > box.position.x
      && candidate.y < box.position.y + box.height + 24 && candidate.y + height + 24 > box.position.y);
    if (!obstacle) break;
    candidate = { x: obstacle.position.x + obstacle.width + 80, y: candidate.y };
  }
  return candidate;
}
