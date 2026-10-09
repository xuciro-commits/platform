import type { Connection } from "@xyflow/react";
import type { ReactNode } from "react";
import type { Tone } from "../../components/StatusTag";
import type { CanvasBox, CanvasPosition } from "../core/types";
import { flowShape, notationOf, type FlowBoundary, type FlowNotation, type FlowShape } from "./notation";

/** A flow canvas draws one process: ordered activities joined through typed ports.
 * The owner says what a step means and whether connecting two of them is allowed;
 * the canvas says how it looks and how a person drives it (ADR-0086 D1). */

export type FlowPort = {
  id: string; label: string; type: string; limit?: number;
  /** Control ports determine execution order; data ports bind typed values. */
  channel?: "control" | "data";
  description?: string;
};

export type FlowNodeKind = {
  id: string; title: string; description?: string; category: string;
  inputs: FlowPort[]; outputs: FlowPort[];
  icon?: ReactNode; tone?: Tone;
  /** BPMN reading of this kind of step; without it the kind's own name decides. */
  notation?: FlowNotation;
  /** Instance projections can customize ports without appearing as palette entries. */
  addable?: boolean;
};

export type FlowNodeStatus = "idle" | "running" | "waiting" | "success" | "error" | "skipped";
export type FlowDiagnostic = { message: string; severity: "error" | "warning" | "info"; port?: string };

export type FlowNode = {
  id: string; kind: string; label: string; detail?: string; position: CanvasPosition;
  version?: string; status?: FlowNodeStatus; diagnostics?: FlowDiagnostic[]; collapsed?: boolean;
  tone?: Tone; current?: boolean; compact?: boolean;
  /** BPMN reading of this step; without it its kind decides. */
  notation?: FlowNotation;
  /** A repeating step carries BPMN's loop marker. */
  loop?: boolean;
  /** Events attached to the activity: the timeout it may hit, the error it may
   * escape through. They are markers on the border, not steps of their own. */
  boundary?: FlowBoundary[];
  /** The lane responsible for this step: who or what does it (ADR-0087 D1). */
  lane?: string;
};

/** A BPMN lane: one responsibility drawn as a band across the process, with the
 * steps it owns inside it. The owner declares the lanes and says which step sits
 * in which; the canvas draws the bands and keeps a step in the lane it is dropped
 * into. A lane is scenery — it is never selected, dragged or connected. */
export type FlowLane = { id: string; title?: string; tone?: Tone };

export type FlowEdge = {
  id: string; source: string; sourcePort: string; target: string; targetPort: string;
  label?: string; channel?: "control" | "data"; tone?: Tone; dashed?: boolean;
  directed?: boolean;
};

export type FlowCatalog = readonly FlowNodeKind[];

export type FlowAddContext = {
  position: CanvasPosition;
  /** Auto-offset for existing blocks, applied by the owner together with insertion. */
  positions?: Record<string, CanvasPosition>;
  source?: { node: string; port: string };
  target?: { node: string; port: string };
  edge?: FlowEdge;
};

export type FlowHistory = { canUndo: boolean; canRedo: boolean; onUndo: () => void; onRedo: () => void };
export type FlowConnectionIssue = "endpoint" | "port" | "duplicate" | "source-capacity" | "target-capacity";

/** The semantic owner may add stricter rules, such as acyclicity or branch scope. */
export function validateFlowConnection(connection: Connection, nodes: readonly FlowNode[], edges: readonly FlowEdge[], catalog: FlowCatalog): FlowConnectionIssue | undefined {
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

/** The dataTransfer type an owner's own library sets to drop a block onto the canvas. */
export const FLOW_NODE_DROP = "application/platform-flow-node";

export const flowNodeWidth = 216;

/** The box one compact BPMN shape occupies: a gateway is a diamond, an event a
 * circle, an activity a block. */
export function flowShapeBox(shape: FlowShape): CanvasBox {
  return shape === "gateway" ? { width: 76, height: 103 } : shape === "event" ? { width: 58, height: 85 } : { width: 160, height: 58 };
}

/** A full block is as tall as its ports need. */
export const flowBlockHeight = (kind: FlowNodeKind, collapsed = false) => collapsed ? 74 : 96 + Math.max(kind.inputs.length, kind.outputs.length) * 27;
export const flowNodeHeight = (kind: FlowNodeKind) => flowBlockHeight(kind);

/** One node's box, whichever way the drawing shows it — the layout and the node
 * view measure the same thing, so a drawing that mixes shapes still lines up. */
export function flowNodeBox(node: Pick<FlowNode, "compact" | "collapsed" | "notation" | "kind">, kind: FlowNodeKind | undefined): CanvasBox {
  if (node.compact) return flowShapeBox(flowShape(node.notation ?? notationOf(node.kind), node.kind));
  return { width: flowNodeWidth, height: flowBlockHeight(kind ?? { id: node.kind, title: node.kind, category: "", inputs: [], outputs: [] }, node.collapsed ?? false) };
}

/** Find a free tile for a newly added block without moving the builder's existing layout. */
export function flowPlacement(position: CanvasPosition, height: number, boxes: { position: CanvasPosition; width: number; height: number }[]): CanvasPosition {
  let candidate = { ...position };
  for (let attempt = 0; attempt < boxes.length; attempt++) {
    const obstacle = boxes.find((box) => candidate.x < box.position.x + box.width + 24 && candidate.x + flowNodeWidth + 24 > box.position.x
      && candidate.y < box.position.y + box.height + 24 && candidate.y + height + 24 > box.position.y);
    if (!obstacle) break;
    candidate = { x: obstacle.position.x + obstacle.width + 80, y: candidate.y };
  }
  return candidate;
}
