import { useMemo, type ReactNode } from "react";
import type { Tone } from "../../components/StatusTag";
import type { LaneAssignment } from "../core/layered";
import type { CanvasDirection, CanvasPosition } from "../core/types";
import { useFlowArrangement } from "./arrange";
import { FlowCanvas } from "./FlowCanvas";
import type { FlowCatalog, FlowEdge, FlowLane, FlowNode, FlowNodeKind } from "./model";
import { flowNodeClassOf, type FlowNotation } from "./notation";

export type FlowStepNode = {
  id: string; label: string; detail?: string; tone?: Tone; current?: boolean;
  notation?: FlowNotation; kind?: string; loop?: boolean;
  class?: string;
  lane?: string;
};
export type FlowStepEdge = { id?: string; from: string; to: string; label?: string; dashed?: boolean; tone?: Tone; directed?: boolean };

const stepEntry: FlowNodeKind = { id: "step", title: "Step", class: "flow", inputs: [{ id: "in", label: "", type: "flow" }], outputs: [{ id: "out", label: "", type: "flow" }] };

export function flowStepKind(node: Pick<FlowStepNode, "id" | "kind" | "notation" | "class">): string {
  if (node.class || node.notation) return `step:${flowNodeClassOf({ class: node.class, notation: node.notation, id: node.kind ?? node.id })}`;
  return node.kind ?? stepEntry.id;
}

export function FlowSteps({ nodes, edges, lanes, direction = "right", height = 280, onOpen, label, children, positions, storeKey, selected, onActivate }: {
  nodes: FlowStepNode[]; edges: FlowStepEdge[]; lanes?: FlowLane[]; direction?: CanvasDirection; height?: number;
  selected?: string; onActivate?: (node: FlowStepNode) => void; onOpen?: (node: FlowStepNode) => void; label?: string; children?: ReactNode; positions?: Readonly<Record<string, CanvasPosition>>;
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
  const steps: FlowNode[] = nodes.map((node) => ({ ...node, kind: flowStepKind(node), compact: true, position: { x: 0, y: 0 } }));
  const laneOf = (id: string) => nodes.find((node) => node.id === id)?.lane;
  const lanesAssignment: LaneAssignment | undefined = lanes?.length ? { ids: lanes.map((lane) => lane.id), of: laneOf } : undefined;
  const arranged = useFlowArrangement(steps, edges.map((edge, i) => ({ id: edge.id ?? `${i}:${edge.from}>${edge.to}`, source: edge.from, target: edge.to, sourcePort: "out", targetPort: "in", label: edge.label })), direction, catalog,
      { lanes: lanesAssignment, compact: true });
  const graph = useMemo(() => {
    const ids = new Set(nodes.map((node) => node.id));
    return {
      nodes: steps.map((node): FlowNode => ({ ...node, position: positions?.[node.id] ?? arranged[node.id] ?? { x: 0, y: 0 } })),
      edges: edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)).map((edge, i): FlowEdge => ({
        id: edge.id ?? `${i}:${edge.from}>${edge.to}`, source: edge.from, target: edge.to, sourcePort: "out", targetPort: "in",
        label: edge.label, dashed: edge.dashed, tone: edge.tone, directed: edge.directed,
      })),
    };
  }, [nodes, edges, lanes, direction, positions, catalog, arranged]);
  const open = onOpen ? (id: string) => { const node = nodes.find((item) => item.id === id); if (node) onOpen(node); } : undefined;
  return <FlowCanvas catalog={catalog} nodes={graph.nodes} edges={graph.edges} lanes={lanes} mode="view" direction={direction} height={height} label={label} selected={selected} onSelect={open} onOpen={onActivate?(id)=>{const node=nodes.find(n=>n.id===id);if(node)onActivate(node)}:undefined} storeKey={storeKey}>{children}</FlowCanvas>;
}
