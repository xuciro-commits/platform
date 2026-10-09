import { useMemo, type ReactNode } from "react";
import type { Tone } from "../../components/StatusTag";
import { layeredLayout, type LaneAssignment } from "../core/layered";
import type { CanvasDirection, CanvasPosition } from "../core/types";
import { FlowCanvas } from "./FlowCanvas";
import { flowNodeBox, type FlowCatalog, type FlowEdge, type FlowLane, type FlowNode } from "./model";
import type { FlowNotation } from "./notation";

/** A step of a process drawn for reading: what it is called, what it is doing and
 * whether the eye is on it. The owner supplies the meaning; there are no ports to
 * author here, so a reader's view never has to invent a catalog (ADR-0086 D1). */
export type FlowStepNode = {
  id: string; label: string; detail?: string; tone?: Tone; current?: boolean;
  /** Its BPMN reading; without it, the step's own kind name decides. */
  notation?: FlowNotation; kind?: string; loop?: boolean;
  /** The lane responsible for it, when the owner drew this process across lanes. */
  lane?: string;
};
export type FlowStepEdge = { id?: string; from: string; to: string; label?: string; dashed?: boolean; tone?: Tone; directed?: boolean };

const stepKind = { id: "step", title: "Step", class: "flow" as const, inputs: [{ id: "in", label: "", type: "flow" }], outputs: [{ id: "out", label: "", type: "flow" }] };
const catalog: FlowCatalog = [stepKind];

/** Read-only process adapter: one step kind, the BPMN shapes the steps ask for. */
export function FlowSteps({ nodes, edges, lanes, direction = "right", height = 280, onOpen, label, children, positions }: {
  nodes: FlowStepNode[]; edges: FlowStepEdge[]; lanes?: FlowLane[]; direction?: CanvasDirection; height?: number;
  onOpen?: (node: FlowStepNode) => void; label?: string; children?: ReactNode; positions?: Readonly<Record<string, CanvasPosition>>;
}) {
  const graph = useMemo(() => {
    const steps: FlowNode[] = nodes.map((node) => ({ ...node, kind: node.kind ?? "step", compact: true, position: { x: 0, y: 0 } }));
    const laneOf = (id: string) => nodes.find((node) => node.id === id)?.lane;
    const assignment: LaneAssignment | undefined = lanes?.length ? { ids: lanes.map((lane) => lane.id), of: laneOf } : undefined;
    const arranged = layeredLayout(steps, edges, direction, { width: 160, height: 58, gapX: 60, gapY: 28 },
      (id) => flowNodeBox(steps.find((node) => node.id === id) ?? { kind: "step", compact: true }, stepKind), assignment);
    const ids = new Set(nodes.map((node) => node.id));
    return {
      nodes: steps.map((node): FlowNode => ({ ...node, position: positions?.[node.id] ?? arranged.get(node.id)! })),
      edges: edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)).map((edge, i): FlowEdge => ({
        id: edge.id ?? `${i}:${edge.from}>${edge.to}`, source: edge.from, target: edge.to, sourcePort: "out", targetPort: "in",
        label: edge.label, dashed: edge.dashed, tone: edge.tone, directed: edge.directed,
      })),
    };
  }, [nodes, edges, lanes, direction, positions]);
  const open = onOpen ? (id: string) => { const node = nodes.find((item) => item.id === id); if (node) onOpen(node); } : undefined;
  return <FlowCanvas catalog={catalog} nodes={graph.nodes} edges={graph.edges} lanes={lanes} mode="view" direction={direction} height={height} label={label} onSelect={open}>{children}</FlowCanvas>;
}
