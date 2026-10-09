// The node type system (ADR-0089): a class declares what a node is, one table
// decides what may join what, and the same rule reads a whole graph back.
import { describe, expect, it } from "vitest";
import { checkFlowEdges, flowPortFits, validateFlowConnection, type FlowCatalog, type FlowEdge, type FlowNode } from "./model";
import { flowNodeClassOf, flowNodeClasses, flowNodeGroup } from "./notation";

/** The standing scenarios the canvas reaches for first: a code node, a document
 * node, a function node and the plain default. */
const catalog: FlowCatalog = [
  { id: "code", title: "Code", class: "code", inputs: [{ id: "in", label: "In", type: "flow" }], outputs: [{ id: "out", label: "Out", type: "flow" }] },
  { id: "document", title: "Document", class: "document", inputs: [], outputs: [{ id: "doc", label: "Document", type: "object", channel: "data" }] },
  { id: "fn", title: "Function", class: "function", inputs: [{ id: "arg", label: "Argument", type: "json", channel: "data", limit: 1 }], outputs: [{ id: "out", label: "Out", type: "flow" }] },
  { id: "plain", title: "Plain", inputs: [{ id: "in", label: "In", type: "flow" }], outputs: [{ id: "out", label: "Out", type: "flow" }] },
];
const node = (id: string, kind: string): FlowNode => ({ id, kind, label: id, position: { x: 0, y: 0 } });

describe("a class declares what a node is", () => {
  it("names the standing scenarios and hands each one a glyph and a group", () => {
    for (const name of ["code", "document", "function", "task"] as const) {
      expect(flowNodeClasses[name]).toBeDefined();
      expect(flowNodeClasses[name]!.icon).toBeTruthy();
    }
    expect(flowNodeGroup({ class: "code" })).toBe("Code");
    expect(flowNodeGroup({ group: "Own group", class: "code" })).toBe("Own group");
  });

  it("falls back to the BPMN reading when the owner named no class, and to task when it named nothing at all", () => {
    expect(flowNodeClassOf({ notation: "script-task" })).toBe("code");
    expect(flowNodeClassOf({ notation: "data-object" })).toBe("document");
    expect(flowNodeClassOf({ notation: "gateway-parallel" })).toBe("control");
    expect(flowNodeClassOf({ id: "payload" })).toBe("trigger");
    expect(flowNodeClassOf({})).toBe("task");
  });
});

describe("one rule decides what may join what", () => {
  it("matches a type exactly, within one channel — an omitted channel is control", () => {
    expect(flowPortFits({ type: "flow" }, { type: "flow" })).toBe(true);
    expect(flowPortFits({ type: "rows", channel: "data" }, { type: "rows", channel: "data" })).toBe(true);
    expect(flowPortFits({ type: "rows", channel: "data" }, { type: "string", channel: "data" })).toBe(false);
    expect(flowPortFits({ type: "rows" }, { type: "rows", channel: "data" })).toBe(false);
  });

  it("lets a json input receive any data output — the carry-all", () => {
    expect(flowPortFits({ type: "object", channel: "data" }, { type: "json", channel: "data" })).toBe(true);
    expect(flowPortFits({ type: "string", channel: "data" }, { type: "json", channel: "data" })).toBe(true);
    // …but a json output is not itself a string, and control never crosses into data.
    expect(flowPortFits({ type: "json", channel: "data" }, { type: "string", channel: "data" })).toBe(false);
    expect(flowPortFits({ type: "flow" }, { type: "json", channel: "data" })).toBe(false);
  });
});

describe("the same rule reads a whole graph back", () => {
  it("reports the edges a drag would have been refused", () => {
    // A document's object output is data and a function's argument is a json
    // input that receives it. Give the second function a flow argument instead
    // and the same document no longer fits — one rule, read over the whole graph.
    const mismatched: FlowCatalog = [
      ...catalog,
      { ...catalog[2]!, id: "fn2", inputs: [{ id: "arg", label: "Argument", type: "flow", limit: 1 }] },
    ];
    const edges: FlowEdge[] = [
      { id: "ok", source: "doc", sourcePort: "doc", target: "fn", targetPort: "arg" },
      { id: "bad-type", source: "doc", sourcePort: "doc", target: "fn2", targetPort: "arg" },
    ];
    expect(checkFlowEdges([node("doc", "document"), node("fn", "fn"), node("fn2", "fn2")], edges, mismatched)).toEqual([{ edge: edges[1], issue: "port" }]);
  });

  it("names every edge over a port's capacity — no arbitrary winner", () => {
    // One output allows one connection; the graph drew two. Each edge is judged
    // as a connection into the graph it sits in, so both are named.
    const limited: FlowCatalog = [{ ...catalog[1]!, outputs: [{ id: "doc", label: "Document", type: "object", channel: "data", limit: 1 }] }, catalog[2]!];
    const edges: FlowEdge[] = [
      { id: "a", source: "doc", sourcePort: "doc", target: "fn", targetPort: "arg" },
      { id: "b", source: "doc", sourcePort: "doc", target: "fn2", targetPort: "arg" },
    ];
    const graph = [node("doc", "document"), node("fn", "fn"), node("fn2", "fn")];
    expect(checkFlowEdges(graph, edges, limited).map((issue) => issue.issue)).toEqual(["source-capacity", "source-capacity"]);
  });

  it("finds the connection issue in one place for a single connection too", () => {
    const graph = [node("doc", "document"), node("fn", "fn")];
    expect(validateFlowConnection({ source: "doc", sourceHandle: "doc", target: "fn", targetHandle: "arg" }, graph, [], catalog)).toBeUndefined();
    expect(validateFlowConnection({ source: "fn", sourceHandle: "out", target: "fn", targetHandle: "arg" }, graph, [], catalog)).toBe("endpoint");
    expect(validateFlowConnection({ source: "fn", sourceHandle: "missing", target: "doc", targetHandle: "doc" }, graph, [], catalog)).toBe("port");
  });
});
