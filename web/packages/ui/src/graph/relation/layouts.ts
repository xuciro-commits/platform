import { layoutGraph } from "../core/layout/adapter";
import type { LayoutResult } from "../core/layout/types";
import type { CanvasPosition } from "../core/types";
import { relationNodeSize, type RelationLayout } from "./model";
export type RelationLayoutSize = { width: number; height: number; gapX: number; gapY: number };
type N = { id: string; size?: { width: number; height: number }; badge?: { size: number } };
type E = { id?: string; from: string; to: string; tree?: boolean; label?: string };
/** Hierarchy edges point child→parent in the owner's model; reverse only inside layout. */
export function arrangeRelations(kind: RelationLayout, nodes: N[], edges: E[], size: RelationLayoutSize = relationNodeSize): Promise<LayoutResult> {
  const tree = kind === "radial" || kind === "organization";
  const hierarchyDirection = tree || kind === "tree-down" || kind === "tree-right";
  const algorithm = kind === "organization" ? "mrtree" : kind === "radial" ? "radial" : kind === "grid" ? "rectpacking" : kind === "force" ? "stress" : "layered";
  const direction = kind === "tree-down" || kind === "organization" ? "down" : kind === "layered-up" ? "up" : kind === "layered-left" ? "left" : "right";
  return layoutGraph({ nodes: nodes.map((n) => ({ id: n.id, width: n.badge?.size ?? n.size?.width ?? size.width, height: n.badge?.size ?? n.size?.height ?? size.height })),
    edges: edges.filter((e) => !tree || e.tree !== false).map((e, i) => ({ id: e.id ?? String(i), source: e.from, target: e.to, label: e.label, reverse: hierarchyDirection && e.tree !== false })),
    algorithm, direction, gap: size.gapY, layerGap: size.gapX + 40 });
}
export async function relationLayout(kind: RelationLayout, nodes: N[], edges: E[], size: RelationLayoutSize = relationNodeSize): Promise<Record<string, CanvasPosition>> {
  return (await arrangeRelations(kind, nodes, edges, size)).positions;
}

export type NeighborhoodLayoutGroup = { side: "right" | "left"; nodes: readonly string[] };

/** Two half-ellipses around one record: the original presentation of a bounded
 * neighbourhood, independent of the relationship query that filled it. */
export function neighborhoodPositions(root: string, groups: readonly NeighborhoodLayoutGroup[]): Record<string, CanvasPosition> {
  const positions = Object.create(null) as Record<string, CanvasPosition>;
  positions[root] = { x: 158, y: 68 };
  for (const group of groups) group.nodes.forEach((id, index) => {
    if (Object.hasOwn(positions, id)) return;
    const angle = (group.side === "right" ? -Math.PI / 2 : Math.PI / 2) + index / Math.max(1, group.nodes.length) * Math.PI;
    positions[id] = { x: 180 + Math.cos(angle) * 90 - 12, y: 90 + Math.sin(angle) * 55 - 12 };
  });
  return positions;
}
