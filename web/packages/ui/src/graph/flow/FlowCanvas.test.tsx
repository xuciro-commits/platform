import { render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

// jsdom has no ResizeObserver, which React Flow measures the pane with.
globalThis.ResizeObserver ??= class { observe() {} unobserve() {} disconnect() {} } as never;
// jsdom has no DOMMatrixReadOnly, which React Flow measures an SVG node with.
globalThis.DOMMatrixReadOnly ??= class {
  a: number; b: number; c: number; d: number; e: number; f: number;
  m11: number; m12: number; m21: number; m22: number; m41: number; m42: number;
  constructor(init?: string | number[]) {
    const v = Array.isArray(init) ? init : [1, 0, 0, 1, 0, 0];
    this.m11 = this.a = v[0] ?? 1; this.m12 = this.b = v[1] ?? 0;
    this.m21 = this.c = v[2] ?? 0; this.m22 = this.d = v[3] ?? 1;
    this.m41 = this.e = v[4] ?? 0; this.m42 = this.f = v[5] ?? 0;
  }
} as never;
import { FlowCanvas } from "./FlowCanvas";
import type { FlowCatalog } from "./model";

const catalog = (): FlowCatalog => [{
  id: "step", title: "Step", class: "flow",
  inputs: [{ id: "in", label: "In", type: "rows", limit: 1, channel: "data" }],
  outputs: [{ id: "out", label: "Out", type: "rows", limit: 1, channel: "data" }],
}];
const nodes = [
  { id: "a", kind: "step", label: "Alpha", position: { x: 0, y: 0 } },
  { id: "b", kind: "step", label: "Beta", position: { x: 260, y: 0 } },
];
const edges = [{ id: "a>b", source: "a", sourcePort: "out", target: "b", targetPort: "in" }];

// An owner that builds its catalog while rendering — a title read through t(), a
// description from the record it just loaded — must not send the canvas round a
// render loop: the catalog is one of the effect's dependencies.
test("a fresh catalog array every render does not loop", () => {
  const errors = vi.spyOn(console, "error").mockImplementation(() => {});
  render(<FlowCanvas catalog={catalog()} nodes={nodes} edges={edges} height={240} mode="view" label="Steps" />);
  expect(screen.getAllByLabelText("Steps").length).toBeGreaterThan(0);
  const loops = errors.mock.calls.flat().filter((m) => String(m).includes("Maximum update depth"));
  errors.mockRestore();
  expect(loops).toEqual([]);
});
