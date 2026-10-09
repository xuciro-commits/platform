// ELK owns placement. Keep evidence of structural correctness, not a particular heuristic or pixel arrangement.
import { describe, expect, it } from "vitest";
import { layeredLayout } from "./layered";
import { relationLayout } from "../relation/layouts";

const size = { width: 160, height: 56, gapX: 60, gapY: 24 };

/** Two branches that join, plus a long edge that skips a layer — the shape a
 * naive packing draws worst: the long edge cuts across the join's columns. */
const nodes = ["start", "left", "right", "merge", "tail"].map((id) => ({ id }));
const edges = [
  { from: "start", to: "left" }, { from: "start", to: "right" },
  { from: "left", to: "merge" }, { from: "right", to: "merge" },
  { from: "merge", to: "tail" },
  { from: "start", to: "tail" }, // the long edge: skips two layers
];

const overlaps = (positions: Map<string, { x: number; y: number }>, box = size) => {
  const placed = [...positions].map(([id, at]) => ({ id, at }));
  const hits: string[] = [];
  for (let i = 0; i < placed.length; i++) for (let j = i + 1; j < placed.length; j++) {
    const a = placed[i]!, b = placed[j]!;
    if (a.at.x < b.at.x + box.width && a.at.x + box.width > b.at.x && a.at.y < b.at.y + box.height && a.at.y + box.height > b.at.y) hits.push(`${a.id}/${b.id}`);
  }
  return hits;
};

describe("the layered layout", () => {
  it("places every node once, with nothing overlapping", async () => {
    const positions = await layeredLayout(nodes, edges, "right", size);
    expect([...positions.keys()].sort()).toEqual(["left", "merge", "right", "start", "tail"]);
    expect(overlaps(positions)).toEqual([]);
  });

  it("is deterministic — the same graph settles the same way", async () => {
    expect([...await layeredLayout(nodes, edges, "right", size)]).toEqual([...await layeredLayout(nodes, edges, "right", size)]);
  });

  it("runs down as well as across", async () => {
    const positions = await layeredLayout(nodes, edges, "down", size);
    const start = positions.get("start")!, tail = positions.get("tail")!;
    expect(tail.y).toBeGreaterThan(start.y);
    expect(overlaps(positions)).toEqual([]);
    expect(positions.get("left")!.y).toBe(positions.get("right")!.y);
    expect(positions.get("left")!.y).toBeGreaterThanOrEqual(start.y + size.height + size.gapY * 2);
    expect(positions.get("merge")!.y).toBeGreaterThanOrEqual(positions.get("left")!.y + size.height + size.gapY * 2);
  });

  it("orders adjacent layers to remove avoidable crossings", async () => {
    const graph = ["a", "b", "c", "d"].map((id) => ({ id }));
    const links = [{ from: "a", to: "d" }, { from: "b", to: "c" }];
    for (const direction of ["right", "down"] as const) {
      const positions = await layeredLayout(graph, links, direction, size);
      const coordinate = (id: string) => positions.get(id)![direction === "right" ? "y" : "x"];
      expect((coordinate("a") - coordinate("b")) * (coordinate("d") - coordinate("c"))).toBeGreaterThanOrEqual(0);
    }
  });

  it("keeps differently sized siblings apart while respecting flow direction", async () => {
    const graph = ["root", "large", "small", "end"].map((id) => ({ id }));
    const links = [{ from: "root", to: "large" }, { from: "root", to: "small" },
      { from: "large", to: "end" }, { from: "small", to: "end" }];
    const box = (id: string) => id === "large" ? { width: 320, height: 180 } : { width: 40, height: 40 };
    for (const direction of ["right", "down"] as const) {
      const positions = await layeredLayout(graph, links, direction, size, box);
      const large = positions.get("large")!, small = positions.get("small")!;
      const a = box("large"), b = box("small");
      expect(large.x + a.width <= small.x || small.x + b.width <= large.x || large.y + a.height <= small.y || small.y + b.height <= large.y).toBe(true);
      const axis = direction === "right" ? "x" : "y";
      expect(Math.min(large[axis], small[axis])).toBeGreaterThan(positions.get("root")![axis]);
      expect(positions.get("end")![axis]).toBeGreaterThan(Math.max(large[axis], small[axis]));
    }
  });

  it("preserves real node ids that resemble internal routing points", async () => {
    const graph = ["root", "middle", "end", "~0:1"].map((id) => ({ id }));
    const positions = await layeredLayout(graph, [{ from: "root", to: "middle" }, { from: "middle", to: "end" },
      { from: "root", to: "end" }], "down", size);
    expect([...positions.keys()].sort()).toEqual(graph.map((node) => node.id).sort());
    expect(overlaps(positions)).toEqual([]);
  });

  it("keeps a branch's siblings side by side, not stacked", async () => {
    const positions = await layeredLayout(nodes, edges, "right", size);
    const left = positions.get("left")!, right = positions.get("right")!;
    expect(left.x).toBe(right.x); // same layer
    expect(Math.abs(left.y - right.y)).toBeGreaterThanOrEqual(size.height); // side by side
  });

});

describe("the network layout", () => {
  const graph = ["a", "b", "c", "d"].map((id) => ({ id }));
  const links = [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "c", to: "d" }, { from: "d", to: "a" }, { from: "a", to: "c" }];

  it("places every node, deterministically, and connected nodes stay nearer than strangers", async () => {
    const once = await relationLayout("force", graph, links);
    expect(Object.keys(once).sort()).toEqual(["a", "b", "c", "d"]);
    expect(await relationLayout("force", graph, links)).toEqual(once);
    const distance = (x: string, y: string) => Math.hypot(once[x]!.x - once[y]!.x, once[x]!.y - once[y]!.y);
    // b–d is the only unconnected pair: every edge should be shorter than it.
    for (const edge of links) expect(distance(edge.from, edge.to)).toBeLessThan(distance("b", "d"));
  });

  it("handles a single node and an empty graph without dividing by zero", async () => {
    expect(Object.keys(await relationLayout("force", [{ id: "only" }], []))).toEqual(["only"]);
    expect(await relationLayout("force", [], [])).toEqual({});
  });
});
