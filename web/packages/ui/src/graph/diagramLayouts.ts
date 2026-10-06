// Layouts every entity/relationship diagram needs (ADR-0068 §6): the owner
// names which edges form the hierarchy; the canvas offers these to the reader.
import { layout as layered } from "./layout";
import type { CanvasPosition } from "./model";

export type DiagramLayout = "tree-down" | "tree-right" | "radial" | "grid";
export const diagramLayouts: DiagramLayout[] = ["tree-down", "tree-right", "radial", "grid"];
type N = { id: string };
type E = { from: string; to: string; tree?: boolean };
export type DiagramSize = { width: number; height: number; gapX: number; gapY: number };
export const diagramSize: DiagramSize = { width: 180, height: 56, gapX: 40, gapY: 48 };

/** Positions for every node under the chosen layout; parents above (or left of) children. */
export function diagramLayout(kind: DiagramLayout, nodes: N[], edges: E[], size: DiagramSize = diagramSize): Record<string, CanvasPosition> {
  const tree = edges.filter((e) => e.tree !== false);
  if (kind === "grid") return grid(nodes, size);
  if (kind === "radial") return radial(nodes, tree, size);
  // The hierarchy flows parent → child; the layered layout wants edges in flow direction.
  const flow = tree.map((e) => ({ from: e.to, to: e.from }));
  return Object.fromEntries(layered(nodes, flow, kind === "tree-down" ? "down" : "right", size));
}

function grid(nodes: N[], size: DiagramSize) {
  const columns = Math.max(1, Math.ceil(Math.sqrt(nodes.length * 1.6)));
  return Object.fromEntries(nodes.map((n, i) => [n.id, { x: (i % columns) * (size.width + size.gapX), y: Math.floor(i / columns) * (size.height + size.gapY) }]));
}

/** Roots at the centre, each generation on a wider ring, siblings sharing their parent's sector. */
function radial(nodes: N[], tree: E[], size: DiagramSize) {
  const ids = new Set(nodes.map((n) => n.id));
  const children = new Map<string, string[]>(nodes.map((n) => [n.id, []]));
  const hasParent = new Set<string>();
  for (const e of tree) if (ids.has(e.from) && ids.has(e.to) && e.from !== e.to && !hasParent.has(e.from)) { children.get(e.to)!.push(e.from); hasParent.add(e.from); }
  const roots = nodes.filter((n) => !hasParent.has(n.id)).map((n) => n.id);
  const leaves = new Map<string, number>();
  const count = (id: string, seen = new Set<string>()): number => {
    if (seen.has(id)) return 0; seen.add(id);
    const kids = children.get(id)!;
    const n = kids.length ? kids.reduce((s, k) => s + count(k, seen), 0) : 1;
    leaves.set(id, n); return n;
  };
  const total = roots.reduce((s, r) => s + count(r), 0) || 1;
  const ring = Math.max(size.width, size.height) + size.gapX + size.gapY;
  const out: Record<string, CanvasPosition> = {};
  const place = (id: string, depth: number, from: number, to: number, seen = new Set<string>()) => {
    if (seen.has(id)) return; seen.add(id);
    const angle = (from + to) / 2, r = roots.length === 1 && depth === 0 ? 0 : (depth + (roots.length > 1 ? 1 : 0)) * ring;
    out[id] = { x: Math.cos(angle) * r * 1.4, y: Math.sin(angle) * r };
    let a = from;
    for (const k of children.get(id)!) { const span = (to - from) * ((leaves.get(k) ?? 1) / (leaves.get(id) || 1)); place(k, depth + 1, a, a + span, seen); a += span; }
  };
  let a = 0;
  for (const r of roots) { const span = Math.PI * 2 * ((leaves.get(r) ?? 1) / total); place(r, 0, a, a + span); a += span; }
  for (const n of nodes) out[n.id] ??= { x: 0, y: 0 };
  return out;
}
