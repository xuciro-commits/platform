import { layoutGraph } from "../core/layout/adapter";
import type { LayoutAlgorithm, LayoutDirection } from "../core/layout/types";
import type { CanvasBox, CanvasPosition } from "../core/types";
import { relationNodeSize, type RelationLayout } from "./model";
import { relationHierarchy, tidyTree, type HierarchyLink } from "./tree";

type N = { id: string; size?: CanvasBox; badge?: { size: number } };

const boxOf = (n: N): CanvasBox => n.badge ? { width: n.badge.size, height: n.badge.size } : n.size ?? relationNodeSize;

/** Positions for a relation drawing. A tree is drawn by the tidy tree in the
 * chosen direction; a graph that is not one is layered by ELK, which also owns
 * the radial, distance and packing arrangements. Lines are never taken from a
 * layout: the canvas routes them from wherever the boxes end up. */
export async function relationLayout(kind: RelationLayout, nodes: readonly N[], edges: readonly HierarchyLink[]): Promise<Record<string, CanvasPosition>> {
  if (!nodes.length) return {};
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const hierarchy = relationHierarchy(nodes.map((n) => n.id), edges);
  if (kind === "down" || kind === "up" || kind === "right" || kind === "left") {
    if (hierarchy.tree) return tidyTree(hierarchy, (id) => boxOf(byId.get(id)!), kind);
  }
  const algorithm: LayoutAlgorithm = kind === "radial" ? "radial" : kind === "grid" ? "rectpacking" : kind === "force" ? "stress" : "layered";
  // ELK reads its edges parent → child; the hierarchy says which way that is.
  const links = kind === "radial" ? [...hierarchy.parent].map(([c, p]) => ({ from: p, to: c }))
    : edges.map((e) => (e.parent === "target" ? { from: e.to, to: e.from } : { from: e.from, to: e.to }));
  const vertical = kind === "down" || kind === "up";
  const result = await layoutGraph({ nodes: nodes.map((n) => ({ id: n.id, ...boxOf(n) })), edges: links.map((e, i) => ({ id: String(i), source: e.from, target: e.to })),
    algorithm, direction: (algorithm === "layered" ? kind : "down") as LayoutDirection, gap: vertical ? 32 : 24, layerGap: vertical ? 72 : 96 });
  return result.positions;
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
