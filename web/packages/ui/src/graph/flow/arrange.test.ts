// The flow family's one layout brain (ADR-0092): the canvas's tidy-up, the
// read-only step paths and the scenes that pre-compute positions all ask the
// same function, so a kind's declared size is honoured everywhere at once.
import { expect, test } from "vitest";
import { arrangeFlow } from "./arrange";
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

test("every node gets a position, branches share a layer, and declared sizes set the rhythm", () => {
  const placed = arrangeFlow(nodes, edges, "right", catalog);
  expect(Object.keys(placed).sort()).toEqual(["a", "b", "c"]);
  expect(placed.b!.x).toBe(placed.c!.x); // same layer as each other
  expect(placed.b!.x).toBeGreaterThan(placed.a!.x); // after their join's source
  expect(Math.abs(placed.b!.y - placed.c!.y)).toBeGreaterThanOrEqual(96); // side by side, not stacked
  // The declared 320-wide node widens its layer: the next one starts past it plus the gap.
  expect(placed.b!.x).toBeGreaterThanOrEqual(320 + 80);
});

test("the same drawing arranges the same way twice", () => {
  expect(arrangeFlow(nodes, edges, "right", catalog)).toEqual(arrangeFlow(nodes, edges, "right", catalog));
});

test("compact chips get the chip rhythm", () => {
  const chips = nodes.map((node) => ({ ...node, compact: true }));
  const placed = arrangeFlow(chips, edges, "right", catalog, { compact: true, gapX: 60, gapY: 28 });
  expect(placed.b!.x).toBeGreaterThanOrEqual(160 + 60);
});
