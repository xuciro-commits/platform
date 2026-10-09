// The flow family's one layout brain (ADR-0092): the canvas's tidy-up, the
// read-only step paths and the scenes that pre-compute positions all ask the
// same function, so a kind's declared size is honoured everywhere at once.
import { expect, test } from "vitest";
import { arrangeFlow, arrangeFlowGraph } from "./arrange";
import type { FlowCatalog, FlowNode } from "./model";

const catalog: FlowCatalog = [
  { id: "big", title: "Big", class: "code", inputs: [], outputs: [], size: { width: 320, height: 200 } },
  { id: "plain", title: "Plain", inputs: [], outputs: [] },
];
const nodes: FlowNode[] = [
  { id: "a", kind: "big", label: "A", position: { x: 0, y: 0 } },
  { id: "b", kind: "plain", label: "B", position: { x: 0, y: 0 } },
  { id: "c", kind: "plain", label: "C", position: { x: 0, y: 0 } },
];
const edges = [{ source: "a", target: "b" }, { source: "a", target: "c" }];

test("every node gets a position, branches share a layer, and declared sizes set the rhythm", async () => {
  const placed = await arrangeFlow(nodes, edges, "right", catalog);
  expect(Object.keys(placed).sort()).toEqual(["a", "b", "c"]);
  expect(placed.b!.x).toBe(placed.c!.x); // same layer as each other
  expect(placed.b!.x).toBeGreaterThan(placed.a!.x); // after their join's source
  expect(Math.abs(placed.b!.y - placed.c!.y)).toBeGreaterThanOrEqual(96); // side by side, not stacked
  // The declared 320-wide node widens its layer: the next one starts past it plus the gap.
  expect(placed.b!.x).toBeGreaterThanOrEqual(320 + 80);
});

test("the same drawing arranges the same way twice", async () => {
  expect(await arrangeFlow(nodes, edges, "right", catalog)).toEqual(await arrangeFlow(nodes, edges, "right", catalog));
});

test("compact chips get the chip rhythm", async () => {
  const chips = nodes.map((node) => ({ ...node, compact: true }));
  const placed = await arrangeFlow(chips, edges, "right", catalog, { compact: true, gapX: 60, gapY: 28 });
  expect(placed.b!.x).toBeGreaterThanOrEqual(160 + 60);
});


test("ELK preserves typed port anchors and routes across distinct lane bands", async () => {
  const kind = { id: "work", title: "Work", inputs: [{ id: "in", label: "In", type: "flow" }], outputs: [{ id: "out", label: "Out", type: "flow" }] };
  const steps = ["a", "b", "c"].map((id) => ({ id, kind: "work", label: id, lane: id === "b" ? "floor" : "office", position: { x: 0, y: 0 } }));
  const lines = [{ id: "ab", source: "a", sourcePort: "out", target: "b", targetPort: "in" }, { id: "bc", source: "b", sourcePort: "out", target: "c", targetPort: "in" }];
  const result = await arrangeFlowGraph(steps, lines, "right", [kind], { lanes: { ids: ["office", "floor"], of: (id) => steps.find((n) => n.id === id)?.lane } });
  const officeBottom = Math.max(result.positions.a!.y, result.positions.c!.y) + 123;
  expect(result.positions.b!.y).toBeGreaterThanOrEqual(officeBottom);
  for (const edge of lines) {
    const route = result.routes[edge.id]!;
    const source = result.positions[edge.source]!, target = result.positions[edge.target]!;
    expect(route.points[0]).toEqual({ x: source.x + 216, y: source.y + 89 });
    expect(route.points.at(-1)).toEqual({ x: target.x, y: target.y + 89 });
    for (let i = 1; i < route.points.length; i++) expect(route.points[i]!.x === route.points[i - 1]!.x || route.points[i]!.y === route.points[i - 1]!.y).toBe(true);
  }
});
