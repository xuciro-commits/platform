// One layout brain for the flow family (ADR-0092): the canvas's own "tidy up",
// the read-only step paths and the scenes that pre-compute positions all ask
// the same function, so a kind's declared size or a lane's rule can never be
// honoured in one place and forgotten in another.
import { layeredLayout, type LaneAssignment } from "../core/layered";
import type { CanvasBox, CanvasDirection, CanvasPosition } from "../core/types";
import { flowNodeBox, flowNodeHeight, flowNodeWidth, type FlowCatalog, type FlowNode } from "./model";

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
export function arrangeFlow(nodes: FlowNode[], edges: { source: string; target: string }[], direction: CanvasDirection,
  catalog: FlowCatalog, options: ArrangeOptions = {}): Record<string, CanvasPosition> {
  const compact = options.compact ?? (nodes.length > 0 && nodes.every((node) => node.compact));
  const boxOf = options.box ?? ((id: string): CanvasBox => {
    const node = nodes.find((item) => item.id === id);
    return node ? flowNodeBox(node, catalog.find((kind) => kind.id === node.kind)) : { width: flowNodeWidth, height: 96 };
  });
  return Object.fromEntries(layeredLayout(nodes, edges.map((edge) => ({ from: edge.source, to: edge.target })), direction,
    { width: compact ? 160 : flowNodeWidth, height: compact ? 58 : Math.max(58, ...catalog.map((kind) => flowNodeHeight(kind))), gapX: options.gapX ?? 80, gapY: options.gapY ?? 36 },
    boxOf, options.lanes));
}
