import { expect, test } from "vitest";
import { layout } from "./graph/Graph";

const n = (...ids: string[]) => ids.map((id) => ({ id, label: id }));

test("a node lies one layer past the furthest node leading to it", () => {
  const at = layout(n("start", "check", "fast", "approve", "end"), [
    { from: "start", to: "check" }, { from: "check", to: "fast" }, { from: "check", to: "approve" },
    { from: "fast", to: "end" }, { from: "approve", to: "end" }, { from: "start", to: "end" },
  ]);
  const x = (id: string) => at.get(id)!.x;
  expect(x("start")).toBe(0);
  expect(x("check")).toBeGreaterThan(x("start"));
  expect(x("fast")).toBe(x("approve")); // branches side by side
  expect(at.get("fast")!.y).not.toBe(at.get("approve")!.y);
  expect(x("end")).toBeGreaterThan(x("fast")); // past the longest path, not the short edge
});

test("an edge leading back does not move what it leads to", () => {
  const at = layout(n("op10", "op20", "op30"), [{ from: "op10", to: "op20" }, { from: "op20", to: "op30" }, { from: "op30", to: "op10", dashed: true }]);
  expect(at.get("op10")!.x).toBe(0);
  expect(at.get("op30")!.x).toBeGreaterThan(at.get("op20")!.x);
});

test("down lays layers from top to bottom", () => {
  const at = layout(n("a", "b"), [{ from: "a", to: "b" }], "down");
  expect(at.get("a")!.x).toBe(at.get("b")!.x);
  expect(at.get("b")!.y).toBeGreaterThan(at.get("a")!.y);
});
