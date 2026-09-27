import { describe, expect, it } from "vitest";
import { validateCanvasConnection, type CanvasEdge, type CanvasNode, type NodeCatalog } from "./graph/NodeCanvas";

const catalog: NodeCatalog = [
  { id: "state", title: "State", category: "lifecycle", inputs: [{ id: "result", label: "Result", type: "result" }], outputs: [{ id: "take", label: "Take", type: "start" }] },
  { id: "action", title: "Action", category: "lifecycle", inputs: [{ id: "from", label: "From", type: "start" }], outputs: [{ id: "to", label: "To", type: "result", limit: 1 }] },
];
const nodes: CanvasNode[] = [
  { id: "found", kind: "state", label: "Found", position: { x: 0, y: 0 } },
  { id: "returned", kind: "state", label: "Returned", position: { x: 0, y: 120 } },
  { id: "handback", kind: "action", label: "Hand back", position: { x: 300, y: 0 } },
];
const edges: CanvasEdge[] = [{ id: "e1", source: "handback", sourcePort: "to", target: "returned", targetPort: "result" }];

describe("semantic canvas connection validation", () => {
  it("accepts a permitted state-to-action edge", () => {
    expect(validateCanvasConnection({ source: "found", sourceHandle: "take", target: "handback", targetHandle: "from" }, nodes, edges, catalog)).toBeUndefined();
  });
  it("refuses an unknown endpoint, incompatible port, duplicate and a second result", () => {
    expect(validateCanvasConnection({ source: "missing", sourceHandle: "take", target: "handback", targetHandle: "from" }, nodes, edges, catalog)).toBe("endpoint");
    expect(validateCanvasConnection({ source: "found", sourceHandle: "take", target: "returned", targetHandle: "result" }, nodes, edges, catalog)).toBe("port");
    expect(validateCanvasConnection({ source: "handback", sourceHandle: "to", target: "returned", targetHandle: "result" }, nodes, edges, catalog)).toBe("duplicate");
    expect(validateCanvasConnection({ source: "handback", sourceHandle: "to", target: "found", targetHandle: "result" }, nodes, edges, catalog)).toBe("source-capacity");
  });
});
