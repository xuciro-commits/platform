// Layout quality (ADR-0091): the layered layout breaks long edges into routing
// points so they join the crossing count and pull straight; the force layout
// arranges a graph that is not a hierarchy. Both must be deterministic — the
// same drawing always settles the same way.
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
  it("places every node once, with nothing overlapping", () => {
    const positions = layeredLayout(nodes, edges, "right", size);
    expect([...positions.keys()].sort()).toEqual(["left", "merge", "right", "start", "tail"]);
    expect(overlaps(positions)).toEqual([]);
  });

  it("is deterministic — the same graph settles the same way", () => {
    expect([...layeredLayout(nodes, edges, "right", size)]).toEqual([...layeredLayout(nodes, edges, "right", size)]);
  });

  it("runs down as well as across", () => {
    const positions = layeredLayout(nodes, edges, "down", size);
    const start = positions.get("start")!, tail = positions.get("tail")!;
    expect(tail.y).toBeGreaterThan(start.y);
    expect(overlaps(positions)).toEqual([]);
  });

  it("keeps a branch's siblings side by side, not stacked", () => {
    const positions = layeredLayout(nodes, edges, "right", size);
    const left = positions.get("left")!, right = positions.get("right")!;
    expect(left.x).toBe(right.x); // same layer
    expect(Math.abs(left.y - right.y)).toBeGreaterThanOrEqual(size.height); // side by side
  });

  it("straightens a long edge: its ends share a line where the layers allow", () => {
    const positions = layeredLayout(nodes, edges, "right", size);
    // The long edge start→tail crosses two layers through routing points; the
    // tail's row should end up near the start's row, not flung to a corner.
    const start = positions.get("start")!, tail = positions.get("tail")!;
    expect(Math.abs(start.y - tail.y)).toBeLessThanOrEqual(size.height * 2);
  });
});

describe("the force layout", () => {
  const graph = ["a", "b", "c", "d"].map((id) => ({ id }));
  const links = [{ from: "a", to: "b" }, { from: "b", to: "c" }, { from: "c", to: "d" }, { from: "d", to: "a" }, { from: "a", to: "c" }];

  it("places every node, deterministically, and connected nodes stay nearer than strangers", () => {
    const once = relationLayout("force", graph, links);
    expect(Object.keys(once).sort()).toEqual(["a", "b", "c", "d"]);
    expect(relationLayout("force", graph, links)).toEqual(once);
    const distance = (x: string, y: string) => Math.hypot(once[x]!.x - once[y]!.x, once[x]!.y - once[y]!.y);
    // b–d is the only unconnected pair: every edge should be shorter than it.
    for (const edge of links) expect(distance(edge.from, edge.to)).toBeLessThan(distance("b", "d"));
  });

  it("handles a single node and an empty graph without dividing by zero", () => {
    expect(Object.keys(relationLayout("force", [{ id: "only" }], []))).toEqual(["only"]);
    expect(relationLayout("force", [], [])).toEqual({});
  });
});
