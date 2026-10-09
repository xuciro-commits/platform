// The tidy tree and the live router (ADR-0095): structural evidence — parents
// over their children, nothing overlapping, lines from side middles that never
// pass through a box — not a pixel arrangement.
import { describe, expect, it } from "vitest";
import { routeEdges, type Rect } from "../core/route";
import { relationLayout } from "./layouts";
import { relationHierarchy, tidyTree } from "./tree";

const box = { width: 180, height: 56 };
const rects = (positions: Record<string, { x: number; y: number }>): Record<string, Rect> =>
  Object.fromEntries(Object.entries(positions).map(([id, p]) => [id, { ...p, ...box }]));
const overlaps = (r: Record<string, Rect>) => {
  const list = Object.entries(r), hits: string[] = [];
  for (let i = 0; i < list.length; i++) for (let j = i + 1; j < list.length; j++) {
    const [a, p] = list[i]!, [b, q] = list[j]!;
    if (p.x < q.x + q.width && q.x < p.x + p.width && p.y < q.y + q.height && q.y < p.y + p.height) hits.push(`${a}/${b}`);
  }
  return hits;
};

// A company: two departments "part of" it (child → parent), each "responsible
// for" posts (parent → child), and a general manager the company answers for.
const ids = ["co", "ops", "hr", "gm", "ops-m", "ops-s", "ops-t", "hr-m", "hr-s", "board"];
const links = [
  { from: "ops", to: "co", parent: "target" as const }, { from: "hr", to: "co", parent: "target" as const },
  { from: "co", to: "gm", parent: "source" as const },
  { from: "ops", to: "ops-m", parent: "source" as const }, { from: "ops", to: "ops-s", parent: "source" as const }, { from: "ops", to: "ops-t", parent: "source" as const },
  { from: "hr", to: "hr-m", parent: "source" as const }, { from: "hr", to: "hr-s", parent: "source" as const },
  { from: "gm", to: "ops-m" }, // a line that is not part of the hierarchy
];

describe("the hierarchy of a drawing", () => {
  it("reads declared parents either way round and ignores undeclared lines", () => {
    const h = relationHierarchy(ids, links);
    expect(h.tree).toBe(true);
    expect(h.parent.get("ops")).toBe("co");
    expect(h.parent.get("ops-m")).toBe("ops");
    expect(h.children.get("co")).toEqual(["ops", "hr", "gm"]);
    expect(h.roots).toEqual(["co", "board"]);
    expect(h.links.has(links.length - 1)).toBe(false);
  });

  it("without declarations reads source → target, and is no tree when a node has two parents", () => {
    expect(relationHierarchy(["a", "b", "c"], [{ from: "a", to: "b" }, { from: "a", to: "c" }]).tree).toBe(true);
    expect(relationHierarchy(["a", "b", "c"], [{ from: "a", to: "c" }, { from: "b", to: "c" }]).tree).toBe(false);
  });

  it("leaves out a link that would close a cycle", () => {
    const h = relationHierarchy(["a", "b"], [{ from: "a", to: "b" }, { from: "b", to: "a" }]);
    expect(h.parent.get("b")).toBe("a");
    expect(h.parent.has("a")).toBe(false);
  });
});

describe("the tidy tree", () => {
  const h = relationHierarchy(ids, links);

  it("puts the top of the hierarchy on top, its departments below, nothing overlapping", () => {
    const p = tidyTree(h, () => box, "down");
    expect(Object.keys(p).sort()).toEqual([...ids].sort());
    expect(overlaps(rects(p))).toEqual([]);
    expect(p.ops!.y).toBeGreaterThan(p.co!.y + box.height);
    expect(p.ops!.y).toBe(p.hr!.y); // one row per level
    expect(p["ops-m"]!.y).toBeGreaterThan(p.ops!.y);
  });

  it("centres a parent over its children", () => {
    const p = tidyTree(h, () => box, "down");
    const centre = (id: string) => p[id]!.x + box.width / 2;
    expect(centre("co")).toBeCloseTo((centre("ops") + centre("hr")) / 2, 5);
  });

  it("stacks leaves on both sides of their parent's trunk instead of a wide row", () => {
    const p = tidyTree(h, () => box, "down");
    const trunk = p.ops!.x + box.width / 2;
    for (const leaf of ["ops-m", "ops-s", "ops-t"]) {
      const at = p[leaf]!;
      expect(at.x >= trunk || at.x + box.width <= trunk).toBe(true);
    }
    expect(p["ops-m"]!.y).toBe(p["ops-s"]!.y);
    expect(p["ops-t"]!.y).toBeGreaterThan(p["ops-m"]!.y);
  });

  it("turns the same tree for each direction", () => {
    const up = tidyTree(h, () => box, "up"), right = tidyTree(h, () => box, "right"), left = tidyTree(h, () => box, "left");
    expect(up.co!.y).toBeGreaterThan(up.ops!.y);
    expect(right.ops!.x).toBeGreaterThan(right.co!.x + box.width);
    expect(left.co!.x).toBeGreaterThan(left.ops!.x + box.width);
    for (const p of [up, right, left]) expect(overlaps(rects(p))).toEqual([]);
  });

  it("folds a wide family into rows either side of the trunk instead of one long strip", () => {
    const ids = ["root", ...Array.from({ length: 12 }, (_, i) => `d${i}`), ...Array.from({ length: 12 }, (_, i) => [`d${i}m`, `d${i}s`]).flat()];
    const links = Array.from({ length: 12 }, (_, i) => [{ from: "root", to: `d${i}` }, { from: `d${i}`, to: `d${i}m` }, { from: `d${i}`, to: `d${i}s` }]).flat();
    const p = tidyTree(relationHierarchy(ids, links), () => box, "down");
    const r = rects(p);
    expect(overlaps(r)).toEqual([]);
    const width = Math.max(...Object.values(r).map((b) => b.x + b.width)), height = Math.max(...Object.values(r).map((b) => b.y + b.height));
    expect(width / height).toBeLessThan(4);
    const trunk = p.root!.x + box.width / 2;
    for (let i = 0; i < 12; i++) { const d = p[`d${i}`]!; expect(d.x >= trunk || d.x + box.width <= trunk).toBe(true); }
  });

  it("is deterministic and goes through the shared layout entry", async () => {
    const nodes = ids.map((id) => ({ id }));
    const once = await relationLayout("down", nodes, links);
    expect(await relationLayout("down", nodes, links)).toEqual(once);
    expect(once).toEqual(tidyTree(h, () => box, "down"));
  });
});

describe("the live router", () => {
  const h = relationHierarchy(ids, links);
  const boxes = rects(tidyTree(h, () => box, "down"));

  it("joins a parent's bottom middle to a child's top middle, siblings on one bus", () => {
    const r = routeEdges(boxes, [{ id: "1", source: "ops", target: "co", hierarchy: { parent: "target" } }, { id: "2", source: "hr", target: "co", hierarchy: { parent: "target" } }]);
    const one = r["1"]!.points, two = r["2"]!.points;
    expect(one[0]).toEqual({ x: boxes.ops!.x + 90, y: boxes.ops!.y });
    expect(one.at(-1)).toEqual({ x: boxes.co!.x + 90, y: boxes.co!.y + 56 });
    expect(one[1]!.y).toBe(two[1]!.y);
  });

  it("reaches a stacked leaf along its parent's trunk", () => {
    const r = routeEdges(boxes, [{ id: "t", source: "ops", target: "ops-t", hierarchy: { parent: "source", trunk: true } }]);
    const pts = r.t!.points;
    expect(pts[0]).toEqual({ x: boxes.ops!.x + 90, y: boxes.ops!.y + 56 });
    expect(pts[1]!.x).toBe(boxes.ops!.x + 90);
    expect(pts.at(-1)!.y).toBe(boxes["ops-t"]!.y + 28);
  });

  it("goes around a box a manual move put in the way", () => {
    const moved: Record<string, Rect> = { a: { x: 0, y: 0, ...box }, b: { x: 0, y: 400, ...box }, wall: { x: -40, y: 180, width: 260, height: 56 } };
    const pts = routeEdges(moved, [{ id: "e", source: "a", target: "b" }]).e!.points;
    for (let i = 1; i < pts.length; i++) {
      const p = pts[i - 1]!, q = pts[i]!, w = moved.wall!;
      expect(p.x === q.x || p.y === q.y).toBe(true); // right angles only
      const hit = Math.min(p.x, q.x) < w.x + w.width && Math.max(p.x, q.x) > w.x && Math.min(p.y, q.y) < w.y + w.height && Math.max(p.y, q.y) > w.y;
      expect(hit).toBe(false);
    }
  });
});
