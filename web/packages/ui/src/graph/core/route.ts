// Lines between boxes that stay where the boxes are (ADR-0095). Every line is
// drawn from the drawing as it stands — a layout, a reader's drag, a saved view
// all route the same way — so no line is ever left over from an arrangement the
// boxes have since left. A line joins the middles of the two sides that face
// each other with right angles; siblings share one bus a fixed distance from
// their row; a leaf stacked on its parent's trunk is reached from the trunk; and
// a line that would cross a box searches its way around the boxes instead.
import type { CanvasPosition } from "./types";

export type Rect = { x: number; y: number; width: number; height: number };
export type Side = "top" | "right" | "bottom" | "left";
export type Route = { points: CanvasPosition[]; sourceSide: Side; targetSide: Side };
/** A line of the drawing's hierarchy names its parent end. `trunk`: the child is a
 * stacked leaf, reached along the parent's trunk. `axis`: the way the parent's
 * children lie from it, so they share one bus even when one sits far to the side. */
export type RouteRequest = { id: string; source: string; target: string; hierarchy?: { parent: "source" | "target"; trunk?: boolean; axis?: "vertical" | "horizontal" } };

/** How far a bus runs from the row it serves. */
export const BUS = 20;
const MARGIN = 8, STUB = 14, BEND = 28, PAD = 96, BUDGET = 120;

const cx = (r: Rect) => r.x + r.width / 2, cy = (r: Rect) => r.y + r.height / 2;
const mid = (r: Rect, s: Side): CanvasPosition => s === "top" ? { x: cx(r), y: r.y } : s === "bottom" ? { x: cx(r), y: r.y + r.height } : s === "left" ? { x: r.x, y: cy(r) } : { x: r.x + r.width, y: cy(r) };
const normal: Record<Side, CanvasPosition> = { top: { x: 0, y: -1 }, bottom: { x: 0, y: 1 }, left: { x: -1, y: 0 }, right: { x: 1, y: 0 } };
const sides: Side[] = ["top", "right", "bottom", "left"];

function sideOf(from: CanvasPosition, to: CanvasPosition, end: "start" | "end"): Side {
  const dx = to.x - from.x, dy = to.y - from.y;
  // The side a line leaves through points along its first segment; it enters against its last.
  const s: Side = Math.abs(dx) > Math.abs(dy) ? (dx > 0 ? "right" : "left") : (dy > 0 ? "bottom" : "top");
  return end === "start" ? s : ({ top: "bottom", bottom: "top", left: "right", right: "left" } as const)[s];
}

function simplify(points: CanvasPosition[]): CanvasPosition[] {
  const out: CanvasPosition[] = [];
  for (const p of points) {
    const last = out.at(-1);
    if (last && Math.abs(last.x - p.x) < 0.5 && Math.abs(last.y - p.y) < 0.5) continue;
    const prev = out.at(-2);
    if (prev && last && ((Math.abs(prev.x - last.x) < 0.5 && Math.abs(last.x - p.x) < 0.5) || (Math.abs(prev.y - last.y) < 0.5 && Math.abs(last.y - p.y) < 0.5))) out[out.length - 1] = p;
    else out.push(p);
  }
  return out;
}

/** The facing-sides connector: out of the parent, along the bus that runs a fixed
 * distance from the child's row, into the child. */
function direct(a: Rect, b: Rect, axis?: "vertical" | "horizontal"): CanvasPosition[] {
  const hgap = Math.max(b.x - a.x - a.width, a.x - b.x - b.width), vgap = Math.max(b.y - a.y - a.height, a.y - b.y - b.height);
  const vertical = vgap > 0 && (axis === "vertical" || (axis !== "horizontal" || hgap <= 0) && vgap >= hgap);
  if (vertical) {
    const down = b.y > a.y, p = mid(a, down ? "bottom" : "top"), q = mid(b, down ? "top" : "bottom");
    const y = vgap >= 2 * BUS ? q.y - (down ? BUS : -BUS) : (p.y + q.y) / 2;
    return simplify([p, { x: p.x, y }, { x: q.x, y }, q]);
  }
  if (hgap > 0) {
    const right = b.x > a.x, p = mid(a, right ? "right" : "left"), q = mid(b, right ? "left" : "right");
    const x = hgap >= 2 * BUS ? q.x - (right ? BUS : -BUS) : (p.x + q.x) / 2;
    return simplify([p, { x, y: p.y }, { x, y: q.y }, q]);
  }
  // Overlapping boxes: no side faces the other; join the nearest middles.
  const s = Math.abs(cx(b) - cx(a)) > Math.abs(cy(b) - cy(a)) ? (cx(b) > cx(a) ? "right" : "left") : (cy(b) > cy(a) ? "bottom" : "top");
  return [mid(a, s), mid(b, ({ top: "bottom", bottom: "top", left: "right", right: "left" } as const)[s])];
}

/** Down the parent's trunk to the leaf's middle, then across into its facing side. */
function trunk(parent: Rect, leaf: Rect): CanvasPosition[] | undefined {
  const below = leaf.y >= parent.y + parent.height, above = leaf.y + leaf.height <= parent.y, x = cx(parent);
  if (!(below || above) || leaf.x >= parent.x + parent.width || leaf.x + leaf.width <= parent.x || (leaf.x < x && leaf.x + leaf.width > x)) return undefined;
  const p = mid(parent, below ? "bottom" : "top"), y = cy(leaf);
  return [p, { x, y }, { x: leaf.x >= x ? leaf.x : leaf.x + leaf.width, y }];
}

const inside = (p: CanvasPosition, r: Rect) => p.x > r.x + 0.5 && p.x < r.x + r.width - 0.5 && p.y > r.y + 0.5 && p.y < r.y + r.height - 0.5;
function crosses(points: CanvasPosition[], obstacles: Rect[]): boolean {
  for (let i = 1; i < points.length; i++) {
    const a = points[i - 1]!, b = points[i]!;
    const seg = { x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), width: Math.abs(a.x - b.x), height: Math.abs(a.y - b.y) };
    for (const o of obstacles) if (seg.x < o.x + o.width && seg.x + seg.width > o.x && seg.y < o.y + o.height && seg.y + seg.height > o.y) return true;
  }
  return false;
}

const grow = (r: Rect, m: number): Rect => ({ x: r.x - m, y: r.y - m, width: r.width + 2 * m, height: r.height + 2 * m });
const meets = (a: Rect, b: Rect) => a.x < b.x + b.width && b.x < a.x + a.width && a.y < b.y + b.height && b.y < a.y + a.height;

/** Shortest right-angled path from a side of `a` to a side of `b` that enters no
 * box, paying for every bend: A* over the lines the boxes' edges draw. */
function search(a: Rect, b: Rect, all: Rect[], region: Rect): CanvasPosition[] | undefined {
  const obstacles = [...all.filter((o) => meets(o, region)), a, b].map((o) => grow(o, MARGIN));
  const ports = (r: Rect) => sides.map((s) => { const p = mid(r, s), n = normal[s]; return { s, p, stub: { x: p.x + n.x * STUB, y: p.y + n.y * STUB } }; })
    .filter((port) => !obstacles.some((o) => inside(port.stub, o)));
  const from = ports(a), to = ports(b);
  if (!from.length || !to.length) return undefined;
  const lines = (pick: (r: Rect) => [number, number], stubs: number[], lo: number, hi: number) => {
    const raw = [...new Set([lo, hi, ...stubs, ...obstacles.flatMap(pick)])].filter((v) => v >= lo && v <= hi).sort((p, q) => p - q);
    const out: number[] = [];
    raw.forEach((v, i) => { if (i && v - raw[i - 1]! > 2) out.push((v + raw[i - 1]!) / 2); out.push(v); });
    return out;
  };
  const xs = lines((o) => [o.x, o.x + o.width], [...from, ...to].map((p) => p.stub.x), region.x, region.x + region.width);
  const ys = lines((o) => [o.y, o.y + o.height], [...from, ...to].map((p) => p.stub.y), region.y, region.y + region.height);
  const nx = xs.length, ny = ys.length, cells = nx * ny;
  const lower = (list: number[], v: number) => { let l = 0, h = list.length; while (l < h) { const m = (l + h) >> 1; if (list[m]! < v) l = m + 1; else h = m; } return l; };
  const blocked = new Uint8Array(cells), hBlocked = new Uint8Array(cells), vBlocked = new Uint8Array(cells);
  for (const o of obstacles) {
    const i0 = lower(xs, o.x), i1 = lower(xs, o.x + o.width), j0 = lower(ys, o.y), j1 = lower(ys, o.y + o.height);
    for (let j = j0; j <= j1 && j < ny; j++) for (let i = i0; i <= i1 && i < nx; i++) {
      const x = xs[i]!, y = ys[j]!, k = j * nx + i;
      const xin = x > o.x && x < o.x + o.width, yin = y > o.y && y < o.y + o.height;
      if (xin && yin) blocked[k] = 1;
      if (yin && i + 1 < nx && x >= o.x && xs[i + 1]! <= o.x + o.width) hBlocked[k] = 1;
      if (xin && j + 1 < ny && y >= o.y && ys[j + 1]! <= o.y + o.height) vBlocked[k] = 1;
    }
  }
  const at = (p: CanvasPosition) => { const i = lower(xs, p.x), j = lower(ys, p.y); return xs[i] === p.x && ys[j] === p.y ? j * nx + i : -1; };
  const dirOf: Record<Side, number> = { right: 0, bottom: 1, left: 2, top: 3 };
  const goals = new Map<number, number>();
  for (const port of to) { const k = at(port.stub); if (k >= 0 && !blocked[k]) goals.set(k, (dirOf[port.s] + 2) % 4); }
  const g = new Float64Array(cells * 4).fill(Infinity), prev = new Int32Array(cells * 4).fill(-1);
  const heap: [number, number][] = [];
  const push = (f: number, s: number) => { heap.push([f, s]); let i = heap.length - 1; while (i) { const p = (i - 1) >> 1; if (heap[p]![0] <= heap[i]![0]) break; [heap[p], heap[i]] = [heap[i]!, heap[p]!]; i = p; } };
  const pop = () => { const top = heap[0]!, last = heap.pop()!; if (heap.length) { heap[0] = last; let i = 0; for (;;) { const l = 2 * i + 1, r = l + 1; let m = i; if (l < heap.length && heap[l]![0] < heap[m]![0]) m = l; if (r < heap.length && heap[r]![0] < heap[m]![0]) m = r; if (m === i) break; [heap[m], heap[i]] = [heap[i]!, heap[m]!]; i = m; } } return top; };
  const hx = (k: number) => { const x = xs[k % nx]!, y = ys[Math.floor(k / nx)]!; let best = Infinity; for (const p of to) best = Math.min(best, Math.abs(p.stub.x - x) + Math.abs(p.stub.y - y)); return best; };
  for (const port of from) { const k = at(port.stub); if (k < 0 || blocked[k]) continue; const s = k * 4 + dirOf[port.s]; g[s] = STUB; push(STUB + hx(k), s); }
  let best = Infinity, end = -1;
  const step = [[1, 0], [0, 1], [-1, 0], [0, -1]] as const;
  while (heap.length) {
    const [f, s] = pop();
    if (f >= best) break;
    if (f > g[s]! + hx(s >> 2) + 1e-6) continue;
    const k = s >> 2, d = s & 3, i = k % nx, j = Math.floor(k / nx);
    const goal = goals.get(k);
    if (goal !== undefined) { const total = g[s]! + (goal === d ? 0 : BEND) + STUB; if (total < best) { best = total; end = s; } }
    for (let nd = 0; nd < 4; nd++) {
      if (nd === (d + 2) % 4) continue;
      const ni = i + step[nd]![0], nj = j + step[nd]![1];
      if (ni < 0 || nj < 0 || ni >= nx || nj >= ny) continue;
      const nk = nj * nx + ni;
      if (blocked[nk]) continue;
      if (nd === 0 && hBlocked[k]) continue; if (nd === 2 && hBlocked[nk]) continue;
      if (nd === 1 && vBlocked[k]) continue; if (nd === 3 && vBlocked[nk]) continue;
      const cost = g[s]! + Math.abs(xs[ni]! - xs[i]!) + Math.abs(ys[nj]! - ys[j]!) + (nd === d ? 0 : BEND);
      const ns = nk * 4 + nd;
      if (cost < g[ns]!) { g[ns] = cost; prev[ns] = s; push(cost + hx(nk), ns); }
    }
  }
  if (end < 0) return undefined;
  const path: CanvasPosition[] = [];
  for (let s = end; s >= 0; s = prev[s]!) { const k = s >> 2; path.push({ x: xs[k % nx]!, y: ys[Math.floor(k / nx)]! }); }
  path.reverse();
  const first = from.find((p) => p.stub.x === path[0]!.x && p.stub.y === path[0]!.y)!, last = to.find((p) => p.stub.x === path.at(-1)!.x && p.stub.y === path.at(-1)!.y)!;
  return simplify([first.p, ...path, last.p]);
}

const cache = new Map<string, CanvasPosition[] | null>();
const r1 = (r: Rect) => `${Math.round(r.x)},${Math.round(r.y)},${Math.round(r.width)},${Math.round(r.height)}`;

/** One route per line, from the boxes as they are now. */
export function routeEdges(boxes: Readonly<Record<string, Rect>>, edges: readonly RouteRequest[]): Record<string, Route> {
  const all = Object.entries(boxes);
  const out: Record<string, Route> = {};
  let budget = BUDGET;
  for (const e of edges) {
    const a = boxes[e.source], b = boxes[e.target];
    if (!a || !b || e.source === e.target) continue;
    const others = all.filter(([id]) => id !== e.source && id !== e.target).map(([, r]) => r);
    // Route parent → child, then hand the line back in the owner's own direction.
    const flip = e.hierarchy?.parent === "target";
    const [p, c] = flip ? [b, a] : [a, b];
    let points: CanvasPosition[] | undefined;
    if (e.hierarchy?.trunk) { const t = trunk(p, c); if (t && !crosses(t, others.map((o) => grow(o, 2)))) points = t; }
    points ??= direct(p, c, e.hierarchy?.axis);
    if (flip) points = [...points].reverse();
    if (crosses(points, others.map((o) => grow(o, 2))) && budget > 0) {
      const near = grow({ x: Math.min(a.x, b.x), y: Math.min(a.y, b.y), width: Math.max(a.x + a.width, b.x + b.width) - Math.min(a.x, b.x), height: Math.max(a.y + a.height, b.y + b.height) - Math.min(a.y, b.y) }, PAD);
      const blockers = others.filter((o) => meets(o, near));
      const key = [r1(a), r1(b), ...blockers.map(r1)].join("|");
      let found = cache.get(key);
      if (found === undefined) {
        budget--;
        found = search(a, b, blockers, near) ?? (() => {
          const xs = others.flatMap((o) => [o.x, o.x + o.width]).concat(a.x, a.x + a.width, b.x, b.x + b.width), ys = others.flatMap((o) => [o.y, o.y + o.height]).concat(a.y, a.y + a.height, b.y, b.y + b.height);
          const world = grow({ x: Math.min(...xs), y: Math.min(...ys), width: Math.max(...xs) - Math.min(...xs), height: Math.max(...ys) - Math.min(...ys) }, PAD);
          return search(a, b, others, world);
        })() ?? null;
        if (cache.size > 600) cache.delete(cache.keys().next().value!);
        cache.set(key, found);
      }
      if (found) points = found;
    }
    out[e.id] = { points, sourceSide: sideOf(points[0]!, points[1] ?? points[0]!, "start"), targetSide: sideOf(points.at(-2) ?? points.at(-1)!, points.at(-1)!, "end") };
  }
  return out;
}

/** The SVG path of a route, with its bends rounded, and the place its name sits:
 * the middle of its own last stretch, which no sibling shares. */
export function routePath(points: readonly CanvasPosition[], radius = 8): { path: string; label: CanvasPosition } {
  let path = `M${points[0]!.x},${points[0]!.y}`;
  for (let i = 1; i < points.length; i++) {
    const p = points[i]!, prev = points[i - 1]!, next = points[i + 1];
    if (!next) { path += ` L${p.x},${p.y}`; break; }
    const r = Math.min(radius, Math.hypot(p.x - prev.x, p.y - prev.y) / 2, Math.hypot(next.x - p.x, next.y - p.y) / 2);
    const ux = Math.sign(p.x - prev.x), uy = Math.sign(p.y - prev.y), vx = Math.sign(next.x - p.x), vy = Math.sign(next.y - p.y);
    path += ` L${p.x - ux * r},${p.y - uy * r} Q${p.x},${p.y} ${p.x + vx * r},${p.y + vy * r}`;
  }
  const segments = points.slice(1).map((p, i) => ({ a: points[i]!, b: p, length: Math.hypot(p.x - points[i]!.x, p.y - points[i]!.y) }));
  const last = segments.at(-1);
  const chosen = last && last.length >= 16 ? last : segments.reduce((x, y) => (y.length > x.length ? y : x), segments[0] ?? { a: points[0]!, b: points[0]!, length: 0 });
  return { path, label: { x: (chosen.a.x + chosen.b.x) / 2, y: (chosen.a.y + chosen.b.y) / 2 } };
}
