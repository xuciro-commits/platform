// One layout brain for the flow family (ADR-0092): the canvas's own "tidy up",
// the read-only step paths and the scenes that pre-compute positions all ask
// the same function, so a kind's declared size or a lane's rule can never be
// honoured in one place and forgotten in another.
import { type LaneAssignment } from "../core/layered";
import { useCanvasLayout } from "../core/layout/use-layout";
import { layoutGraph } from "../core/layout/adapter";
import type { LayoutResult, LayoutPort } from "../core/layout/types";
import type { CanvasBox, CanvasDirection, CanvasPosition } from "../core/types";
import { flowNodeBox, type FlowCatalog, type FlowNode } from "./model";

export type ArrangeOptions = {
  /** The lanes the drawing spans; steps are arranged inside their own band. */
  lanes?: LaneAssignment;
  /** One node's measured box, when the caller holds state the kit cannot (collapse, size overrides). */
  box?: (id: string) => CanvasBox;
  /** Compact BPMN chips rather than port-carrying blocks. */
  compact?: boolean;
  gapX?: number;
  gapY?: number;
};

/** Positions for every node of a flow drawing, in the chosen direction. */
export async function arrangeFlow(nodes: FlowNode[], edges: { id?: string; source: string; target: string; sourcePort?: string; targetPort?: string; label?: string }[], direction: CanvasDirection,
  catalog: FlowCatalog, options: ArrangeOptions = {}): Promise<Record<string, CanvasPosition>> {
  return (await arrangeFlowGraph(nodes, edges, direction, catalog, options)).positions;
}

export async function arrangeFlowGraph(nodes: FlowNode[], edges: { id?: string; source: string; target: string; sourcePort?: string; targetPort?: string; label?: string }[], direction: CanvasDirection,
  catalog: FlowCatalog, options: ArrangeOptions = {}): Promise<LayoutResult> {
  return layoutGraph({ nodes: nodes.map((original) => {
    const node = { ...original, compact: original.compact ?? options.compact };
    const kind = catalog.find((k) => k.id === node.kind);
    const box = options.box?.(node.id) ?? flowNodeBox(node, kind);
    const ports: LayoutPort[] = [];
    for (const [input, list] of [[true, kind?.inputs ?? []], [false, kind?.outputs ?? []]] as const) list.forEach((port, i) => {
      const vertical = direction === "down";
      const across = node.compact || (node.collapsed ?? kind?.collapsed) ? (i + 1) * box.height / (list.length + 1) : 89 + i * 27;
      ports.push({ id: port.id, side: vertical ? input ? "NORTH" : "SOUTH" : input ? "WEST" : "EAST",
        x: vertical ? (i + 1) * box.width / (list.length + 1) : input ? 0 : box.width,
        y: vertical ? input ? 0 : box.height : across });
    });
    return { id: node.id, ...box, ports, lane: options.lanes?.of(node.id) };
  }), edges: edges.map((edge, i) => ({ ...edge, id: edge.id ?? String(i) })), direction, lanes: options.lanes?.ids,
    gap: options.gapY ?? 36, layerGap: options.gapX ?? 80 });
}

/** Owner adapters may need initial positions before mounting a canvas. */
export function useFlowArrangement(nodes: FlowNode[], edges: { source: string; target: string; sourcePort?: string; targetPort?: string }[], direction: CanvasDirection,
  catalog: FlowCatalog, options: ArrangeOptions = {}) {
  const result = useCanvasLayout(() => arrangeFlow(nodes, edges, direction, catalog, options), JSON.stringify([
    nodes.map((n) => [n.id, n.kind, n.compact, n.collapsed, n.notation, options.lanes?.of(n.id), options.box?.(n.id)]), edges, direction,
    catalog.map((k) => [k.id, k.inputs, k.outputs, k.size, k.collapsed]), options.gapX, options.gapY, options.lanes?.ids,
  ]));
  return result.data ?? {};
}
