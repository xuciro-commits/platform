import { useMemo, type ReactNode } from "react";
import type { Tone } from "../components/StatusTag";
import { BlockCanvas } from "./BlockCanvas";
import { layout } from "./layout";
import type { CanvasEdge, CanvasNode, NodeCatalog } from "./model";

export { layout } from "./layout";
export type { GraphSize } from "./layout";
export type GraphNode = { id: string; label: string; detail?: string; tone?: Tone; current?: boolean };
export type GraphEdge = { from: string; to: string; label?: string; dashed?: boolean; tone?: Tone };
const catalog: NodeCatalog = [{ id: "step", title: "Step", category: "flow", inputs: [{ id: "in", label: "", type: "flow" }], outputs: [{ id: "out", label: "", type: "flow" }] }];

/** Read-only operating graph adapter; the owner supplies meaning and the shared canvas owns presentation. */
export function Graph({ nodes, edges, direction = "right", height = 280, onOpen, label, children }: {
  nodes: GraphNode[]; edges: GraphEdge[]; direction?: "right" | "down"; height?: number;
  onOpen?: (node: GraphNode) => void; label?: string; children?: ReactNode;
}) {
  const graph = useMemo(() => {
    const positions = layout(nodes, edges, direction, { width: 160, height: 58, gapX: 60, gapY: 28 });
    const ids = new Set(nodes.map((node) => node.id));
    return {
      nodes: nodes.map((node): CanvasNode => ({ ...node, kind: "step", compact: true, position: positions.get(node.id)! })),
      edges: edges.filter((edge) => ids.has(edge.from) && ids.has(edge.to)).map((edge, i): CanvasEdge => ({
        id: `${i}:${edge.from}>${edge.to}`, source: edge.from, target: edge.to, sourcePort: "out", targetPort: "in", label: edge.label, dashed: edge.dashed, tone: edge.tone,
      })),
    };
  }, [nodes, edges, direction]);
  const open = onOpen ? (id: string) => { const node = nodes.find((item) => item.id === id); if (node) onOpen(node); } : undefined;
  return <BlockCanvas catalog={catalog} nodes={graph.nodes} edges={graph.edges} mode="view" direction={direction} height={height} label={label} onSelect={open}>{children}</BlockCanvas>;
}
