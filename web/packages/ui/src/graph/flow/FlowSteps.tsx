import { useMemo, type ReactNode } from "react";
import type { Tone } from "../../components/StatusTag";
import type { LaneAssignment } from "../core/layered";
import type { CanvasDirection, CanvasPosition } from "../core/types";
import { arrangeFlow } from "./arrange";
import { FlowCanvas } from "./FlowCanvas";
import type { FlowCatalog, FlowEdge, FlowLane, FlowNode, FlowNodeKind } from "./model";
import { flowNodeClassOf, type FlowNotation } from "./notation";

/** A step of a process drawn for reading: what it is called, what it is doing and
 * whether the eye is on it. The owner supplies the meaning; there are no ports to
 * author here, so a reader's view never has to invent a catalog (ADR-0086 D1). */
export type FlowStepNode = {
  id: string; label: string; detail?: string; tone?: Tone; current?: boolean;
  /** Its BPMN reading; without it, the step's own kind name decides. */
  notation?: FlowNotation; kind?: string; loop?: boolean;
  /** What the step is, from the one class vocabulary (ADR-0092): declared, else
   * derived from the BPMN reading, else the plain step. */
  class?: string;
  /** The lane responsible for it, when the owner drew this process across lanes. */
  lane?: string;
};
export type FlowStepEdge = { id?: string; from: string; to: string; label?: string; dashed?: boolean; tone?: Tone; directed?: boolean };

const stepEntry: FlowNodeKind = { id: "step", title: "Step", class: "flow", inputs: [{ id: "in", label: "", type: "flow" }], outputs: [{ id: "out", label: "", type: "flow" }] };

/** The catalog entry a step draws under: its declared class or BPMN reading
 * decides the glyph; a step that declares nothing stays the plain step. */
export function flowStepKind(node: Pick<FlowStepNode, "id" | "kind" | "notation" | "class">): string {
  if (node.class || node.notation) return `step:${flowNodeClassOf({ class: node.class, notation: node.notation, id: node.kind ?? node.id })}`;
  return node.kind ?? stepEntry.id;
}

/** Read-only process adapter: the BPMN shapes the steps ask for, the class
 * glyphs the vocabulary decides, one entry per class the drawing actually uses. */
export function FlowSteps({ nodes, edges, lanes, direction = "right", height = 280, onOpen, label, children, positions, storeKey }: {
  nodes: FlowStepNode[]; edges: FlowStepEdge[]; lanes?: FlowLane[]; direction?: CanvasDirection; height?: number;
  onOpen?: (node: FlowStepNode) => void; label?: string; children?: ReactNode; positions?: Readonly<Record<string, CanvasPosition>>;
  /** The reader's own arrangement of this drawing stays on the device (ADR-0092). */
  storeKey?: string;
}) {
  const catalog: FlowCatalog = useMemo(() => {
    const entries = new Map<string, FlowNodeKind>([[stepEntry.id, stepEntry]]);
    for (const node of nodes) {
      const id = flowStepKind(node);
      if (!entries.has(id)) entries.set(id, { ...stepEntry, id, class: flowNodeClassOf({ class: node.class, notation: node.notation, id: node.kind ?? node.id }) });
    }
    return [...entries.values()];
  }, [nodes]);
  const graph = useMemo(() => {
    const steps: FlowNode[] = nodes.map((node) => ({ ...node, kind: flowStepKind(node), compact: true, position: { x: 0, y: 0 } }));
    const laneOf = (id: string) => nodes.find((node) => node.id === id)?.lane;
    const lanesAssignment: LaneAssignment | undefined = lanes?.length ? { ids: lanes.map((lane) => lane.id), of: laneOf } : undefined;
    const arranged = arrangeFlow(steps, edges.map((edge) => ({ source: edge.from, target: edge.to })), direction, catalog,
      { lanes: lanesAssignment, compact: true, gapX: 60, gapY: 28 });
    const ids = new Set(nodes.map((node) => node.id));
    return {
      nodes: steps.map((node): FlowNode => ({ ...node, position: positions?.[node.id] ?? arranged[node.id] ?? { x: 0, y: 0 } })),
      edges: edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)).map((edge, i): FlowEdge => ({
        id: edge.id ?? `${i}:${edge.from}>${edge.to}`, source: edge.from, target: edge.to, sourcePort: "out", targetPort: "in",
        label: edge.label, dashed: edge.dashed, tone: edge.tone, directed: edge.directed,
      })),
    };
  }, [nodes, edges, lanes, direction, positions, catalog]);
  const open = onOpen ? (id: string) => { const node = nodes.find((item) => item.id === id); if (node) onOpen(node); } : undefined;
  return <FlowCanvas catalog={catalog} nodes={graph.nodes} edges={graph.edges} lanes={lanes} mode="view" direction={direction} height={height} label={label} onSelect={open} storeKey={storeKey}>{children}</FlowCanvas>;
}
