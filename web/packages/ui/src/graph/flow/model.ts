import type { Connection } from "@xyflow/react";
import type { ReactNode } from "react";
import type { Tone } from "../../components/StatusTag";
import type { CanvasBox, CanvasPosition } from "../core/types";
import { flowShape, notationOf, type FlowBoundary, type FlowNodeClass, type FlowNotation, type FlowShape } from "./notation";

export type FlowPort = {
  id: string; label: string; type: string; limit?: number;
  channel?: "control" | "data";
  description?: string;
};

export type FlowNodeKind = {
  id: string; title: string; description?: string;
  class?: FlowNodeClass;
  group?: string;
  inputs: FlowPort[]; outputs: FlowPort[];
  icon?: ReactNode; tone?: Tone;
  notation?: FlowNotation;
  size?: { width: number; height: number };
  collapsed?: boolean;
  addable?: boolean;
};

export type FlowNodeStatus = "idle" | "running" | "waiting" | "success" | "error" | "skipped";
export type FlowDiagnostic = { message: string; severity: "error" | "warning" | "info"; port?: string };

export type FlowNode = {
  id: string; kind: string; label: string; detail?: string; position: CanvasPosition;
  version?: string; status?: FlowNodeStatus; diagnostics?: FlowDiagnostic[]; collapsed?: boolean;
  tone?: Tone; current?: boolean; compact?: boolean;
  notation?: FlowNotation;
  loop?: boolean;
  boundary?: FlowBoundary[];
  lane?: string;
};

export type FlowLane = { id: string; title?: string; tone?: Tone };

export type FlowEdge = {
  id: string; source: string; sourcePort: string; target: string; targetPort: string;
  label?: string; channel?: "control" | "data"; tone?: Tone; dashed?: boolean;
  directed?: boolean;
};

export type FlowCatalog = readonly FlowNodeKind[];

export type FlowAddContext = {
  position: CanvasPosition;
  positions?: Record<string, CanvasPosition>;
  source?: { node: string; port: string };
  target?: { node: string; port: string };
  edge?: FlowEdge;
};

export type FlowHistory = { canUndo: boolean; canRedo: boolean; onUndo: () => void; onRedo: () => void };
export type FlowConnectionIssue = "endpoint" | "port" | "duplicate" | "source-capacity" | "target-capacity";

export const flowPortAccepts: Record<string, readonly string[]> = {
  json: ["*"],
};

export function flowPortFits(from: Pick<FlowPort, "type" | "channel">, into: Pick<FlowPort, "type" | "channel">): boolean {
  if ((from.channel ?? "control") !== (into.channel ?? "control")) return false;
  if (into.type === from.type) return true;
  return (flowPortAccepts[into.type] ?? []).some((accepted) => accepted === "*" || accepted === from.type);
}

export function validateFlowConnection(connection: Connection, nodes: readonly FlowNode[], edges: readonly FlowEdge[], catalog: FlowCatalog): FlowConnectionIssue | undefined {
  const source = nodes.find((n) => n.id === connection.source);
  const target = nodes.find((n) => n.id === connection.target);
  if (!source || !target || source.id === target.id) return "endpoint";
  const out = catalog.find((k) => k.id === source.kind)?.outputs.find((p) => p.id === connection.sourceHandle);
  const into = catalog.find((k) => k.id === target.kind)?.inputs.find((p) => p.id === connection.targetHandle);
  if (!out || !into) return "port";
  if ((out.channel ?? "control") !== (into.channel ?? "control")) return "port";
  if (!flowPortFits(out, into)) return "port";
  if (edges.some((e) => e.source === source.id && e.sourcePort === out.id && e.target === target.id && e.targetPort === into.id)) return "duplicate";
  if (out.limit !== undefined && edges.filter((e) => e.source === source.id && e.sourcePort === out.id).length >= out.limit) return "source-capacity";
  if (into.limit !== undefined && edges.filter((e) => e.target === target.id && e.targetPort === into.id).length >= into.limit) return "target-capacity";
  return undefined;
}

export function checkFlowEdges(nodes: readonly FlowNode[], edges: readonly FlowEdge[], catalog: FlowCatalog): { edge: FlowEdge; issue: FlowConnectionIssue }[] {
  return edges.flatMap((edge) => {
    const issue = validateFlowConnection({ source: edge.source, sourceHandle: edge.sourcePort, target: edge.target, targetHandle: edge.targetPort },
      nodes, edges.filter((item) => item !== edge), catalog);
    return issue ? [{ edge, issue }] : [];
  });
}

export const FLOW_NODE_DROP = "application/platform-flow-node";

export const flowNodeWidth = 216;

export function flowShapeBox(shape: FlowShape): CanvasBox {
  return shape === "gateway" ? { width: 76, height: 103 } : shape === "event" ? { width: 58, height: 85 } : { width: 160, height: 58 };
}

export const flowBlockHeight = (kind: FlowNodeKind, collapsed = kind.collapsed ?? false) => collapsed ? 74 : kind.size?.height ?? 96 + Math.max(kind.inputs.length, kind.outputs.length) * 27;
export const flowNodeHeight = (kind: FlowNodeKind) => flowBlockHeight(kind);

export function flowNodeBox(node: Pick<FlowNode, "compact" | "collapsed" | "notation" | "kind">, kind: FlowNodeKind | undefined): CanvasBox {
  if (node.compact) return flowShapeBox(flowShape(node.notation ?? notationOf(node.kind), node.kind));
  return { width: kind?.size?.width ?? flowNodeWidth, height: flowBlockHeight(kind ?? { id: node.kind, title: node.kind, inputs: [], outputs: [] }, node.collapsed) };
}

export function flowPlacement(position: CanvasPosition, height: number, boxes: { position: CanvasPosition; width: number; height: number }[], width = flowNodeWidth): CanvasPosition {
  let candidate = { ...position };
  for (let attempt = 0; attempt < boxes.length; attempt++) {
    const obstacle = boxes.find((box) => candidate.x < box.position.x + box.width + 24 && candidate.x + width + 24 > box.position.x
      && candidate.y < box.position.y + box.height + 24 && candidate.y + height + 24 > box.position.y);
    if (!obstacle) break;
    candidate = { x: obstacle.position.x + obstacle.width + 80, y: candidate.y };
  }
  return candidate;
}
