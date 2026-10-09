// The hierarchy a relation drawing is arranged by, and the tidy tree that
// arranges it. An organisation chart, a site breakdown or a lineage is a tree
// far more often than a general graph, and a tree drawn as one — parents
// centred over their children, siblings packed against each other's contours,
// leaves stacked along a trunk — reads at a glance where a layered graph engine
// spreads it into a wall of columns (ADR-0095).
import type { CanvasBox, CanvasPosition } from "../core/types";

export type TreeDirection = "down" | "up" | "right" | "left";
export type HierarchyLink = { id?: string; from: string; to: string; parent?: "source" | "target" };

export type Hierarchy = {
  parent: Map<string, string>;
  children: Map<string, string[]>;
  roots: string[];
  /** The links the tree is made of, by index in the list given. */
  links: Set<number>;
  /** The drawing is a tree: the owner declared a hierarchy, or every node has one parent at most. */
  tree: boolean;
};

/** When the owner declares a hierarchy, only its links count and each says
 * which end is the parent; otherwise every link reads source → target. A node
 * keeps its first parent, and a link that would close a cycle is left out. */
export function relationHierarchy(ids: readonly string[], links: readonly HierarchyLink[]): Hierarchy {
  const order = new Map(ids.map((id, i) => [id, i]));
  const declared = links.some((l) => l.parent);
  const parent = new Map<string, string>(), used = new Set<number>();
  let tree = true;
  const above = (node: string, ancestor: string) => {
    for (let at: string | undefined = node; at !== undefined; at = parent.get(at)) if (at === ancestor) return true;
    return false;
  };
  links.forEach((link, i) => {
    if (declared && !link.parent) return;
    const [p, c] = link.parent === "target" ? [link.to, link.from] : [link.from, link.to];
    if (p === c || !order.has(p) || !order.has(c)) return;
    if (parent.has(c) || above(p, c)) { if (parent.get(c) !== p) tree = false; return; }
    parent.set(c, p); used.add(i);
  });
  const children = new Map<string, string[]>();
  for (const [c, p] of parent) children.set(p, [...(children.get(p) ?? []), c]);
  for (const list of children.values()) list.sort((a, b) => order.get(a)! - order.get(b)!);
  return { parent, children, roots: ids.filter((id) => !parent.has(id)), links: used, tree: declared || tree };
}

/** A leaf that shares its parent with others hangs off the parent's trunk
 * rather than taking a column of its own — only in a vertical drawing, where
 * columns are what make a tree wide. */
export function stackedLeaf(h: Hierarchy, id: string): boolean {
  const p = h.parent.get(id);
  return p !== undefined && !h.children.has(id) && (h.children.get(p)?.length ?? 0) > 1;
}

export type TreeSpacing = { sibling: number; layer: number; trunk: number; stackTop: number; stack: number; forest: number };
export const verticalSpacing: TreeSpacing = { sibling: 24, layer: 56, trunk: 40, stackTop: 20, stack: 12, forest: 64 };
export const horizontalSpacing: TreeSpacing = { sibling: 16, layer: 72, trunk: 24, stackTop: 0, stack: 0, forest: 40 };

// Canonical frame: breadth along x, depth along y, root at the top. Each
// direction is this frame turned; the tree itself is computed once.
type Box = { id: string; x: number; y: number; w: number; h: number };

/** How far `next` must move right to clear every box of `placed` it shares a band of depth with. */
function clearance(placed: readonly Box[], next: readonly Box[], gap: number): number {
  let shift = -Infinity;
  for (const a of placed) for (const b of next)
    if (a.y < b.y + b.h && b.y < a.y + a.h) shift = Math.max(shift, a.x + a.w + gap - b.x);
  return shift;
}

export function tidyTree(h: Hierarchy, box: (id: string) => CanvasBox, direction: TreeDirection,
  spacing: TreeSpacing = direction === "down" || direction === "up" ? verticalSpacing : horizontalSpacing): Record<string, CanvasPosition> {
  const vertical = direction === "down" || direction === "up";
  const size = (id: string) => { const b = box(id); return vertical ? { w: b.width, h: b.height } : { w: b.height, h: b.width }; };
  const kids = (id: string) => h.children.get(id) ?? [];

  // Every subtree is drawn with its root centred on x = 0.
  const place = (id: string, y: number): Box[] => {
    const { w, h: height } = size(id);
    const out: Box[] = [{ id, x: -w / 2, y, w, h: height }];
    const children = kids(id);
    if (!children.length) return out;
    const leaves = vertical ? children.filter((c) => stackedLeaf(h, c)) : [];
    let bottom = y + height;
    for (let row = 0; row * 2 < leaves.length; row++) {
      const pair = leaves.slice(row * 2, row * 2 + 2).map((c) => ({ c, ...size(c) }));
      const top = bottom + (row ? spacing.stack : spacing.stackTop);
      pair.forEach((leaf, side) => out.push({ id: leaf.c, x: side ? spacing.trunk / 2 : -spacing.trunk / 2 - leaf.w, y: top, w: leaf.w, h: leaf.h }));
      bottom = top + Math.max(...pair.map((leaf) => leaf.h));
    }
    const rest = children.filter((c) => !leaves.includes(c));
    if (!rest.length) return out;
    const subs = rest.map((c) => place(c, 0));
    const extent = (boxes: readonly Box[]) => ({ w: Math.max(...boxes.map((b) => b.x + b.w)) - Math.min(...boxes.map((b) => b.x)), h: Math.max(...boxes.map((b) => b.y + b.h)) - Math.min(...boxes.map((b) => b.y)) });
    const shifted = (sub: readonly Box[], dx: number, dy: number) => sub.map((b) => ({ ...b, x: b.x + dx, y: b.y + dy }));
    // One row, the parent centred over it: the classic chart.
    const single = (): Box[] => {
      const row: Box[] = [], centres: number[] = [];
      for (const raw of subs) {
        const sub = shifted(raw, 0, bottom + spacing.layer);
        const shift = row.length ? clearance(row, sub, spacing.sibling) : 0;
        const dx = Number.isFinite(shift) ? shift : (centres.at(-1) ?? 0) + spacing.sibling;
        row.push(...shifted(sub, dx, 0));
        centres.push(dx);
      }
      const mid = (centres[0]! + centres.at(-1)!) / 2;
      return row.map((b) => ({ ...b, x: b.x - mid }));
    };
    // Rows of k either side of the parent's trunk, which runs down between them,
    // so a wide family folds into a block instead of a strip.
    const wrapped = (k: number): Box[] => {
      const all: Box[] = [];
      let top = bottom + spacing.layer;
      for (let at = 0; at < subs.length; at += k) {
        const chunk = subs.slice(at, at + k), split = Math.ceil(chunk.length / 2), row: Box[] = [];
        for (const raw of chunk.slice(split)) {
          const sub = shifted(raw, 0, top);
          const shift = row.length ? clearance(row, sub, spacing.sibling) : -Infinity;
          row.push(...shifted(sub, Math.max(spacing.trunk / 2 - Math.min(...sub.map((b) => b.x)), shift), 0));
        }
        const left: Box[] = [];
        for (const raw of chunk.slice(0, split).reverse()) {
          const sub = shifted(raw, 0, top);
          let dx = -spacing.trunk / 2 - Math.max(...sub.map((b) => b.x + b.w));
          for (const a of left) for (const b of sub) if (a.y < b.y + b.h && b.y < a.y + a.h) dx = Math.min(dx, a.x - spacing.sibling - b.x - b.w);
          left.push(...shifted(sub, dx, 0));
        }
        row.push(...left);
        all.push(...row);
        top = Math.max(...row.map((b) => b.y + b.h)) + spacing.layer;
      }
      return all;
    };
    // The arrangement whose block comes nearest the screen's proportions wins.
    const target = vertical ? 1.6 : 1 / 1.6;
    const score = (boxes: readonly Box[]) => { const e = extent([...out, ...boxes]); const r = e.w / Math.max(1, e.h); return Math.max(r / target, target / r); };
    let best = single(), bestScore = score(best);
    for (let k = 2; subs.length >= 4 && k < subs.length; k++) {
      const candidate = wrapped(k), s = score(candidate);
      if (s < bestScore - 0.05) { best = candidate; bestScore = s; }
    }
    out.push(...best);
    return out;
  };

  const forest: Box[] = [];
  const singles = h.roots.filter((r) => !kids(r).length);
  for (const root of h.roots.filter((r) => kids(r).length)) {
    const sub = place(root, 0);
    const shift = forest.length ? Math.max(...forest.map((b) => b.x + b.w)) + spacing.forest - Math.min(...sub.map((b) => b.x)) : 0;
    for (const b of sub) forest.push({ ...b, x: b.x + shift });
  }
  // Things that belong to nothing in this drawing gather in a block of their own beside the trees.
  if (singles.length) {
    const cols = Math.max(1, Math.ceil(Math.sqrt(singles.length)));
    const cellW = Math.max(...singles.map((s) => size(s).w)) + spacing.sibling, cellH = Math.max(...singles.map((s) => size(s).h)) + Math.max(spacing.sibling, spacing.stack + 12);
    const left = forest.length ? Math.max(...forest.map((b) => b.x + b.w)) + spacing.forest : 0;
    singles.forEach((s, i) => { const { w, h: height } = size(s); forest.push({ id: s, x: left + (i % cols) * cellW, y: Math.floor(i / cols) * cellH, w, h: height }); });
  }

  const minX = Math.min(...forest.map((b) => b.x)), maxY = Math.max(...forest.map((b) => b.y + b.h));
  const positions: Record<string, CanvasPosition> = {};
  for (const b of forest) {
    const x = b.x - minX, y = b.y;
    positions[b.id] = direction === "down" ? { x, y } : direction === "up" ? { x, y: maxY - y - b.h }
      : direction === "right" ? { x: y, y: x } : { x: maxY - y - b.h, y: x };
  }
  return positions;
}
