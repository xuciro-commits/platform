import type { ElkNode, ElkExtendedEdge } from "elkjs/lib/elk-api";
import { runElk, registeredLayoutAlgorithms } from "./elk";
import type { LayoutRequest, LayoutResult, LayoutRoute } from "./types";
const cache = new Map<string, Promise<LayoutResult>>();
let algorithms: Promise<Set<string>> | undefined;
const portId = (node: string, port: string) => JSON.stringify([node, port]);

/** ELK owns all automatic placements and routed sections. Node ids, edge direction and ports remain the owner's facts. */
export function layoutGraph(input: LayoutRequest): Promise<LayoutResult> {
  const key = JSON.stringify(input);
  const previous = cache.get(key); if (previous) return previous;
  const result = calculate(input).catch((error) => { cache.delete(key); throw error; });
  cache.set(key, result); if (cache.size > 32) cache.delete(cache.keys().next().value!);
  return result;
}
async function calculate(input: LayoutRequest): Promise<LayoutResult> {
  const positions: LayoutResult["positions"] = {}, routes: LayoutResult["routes"] = {};
  const boxes = Object.fromEntries(input.nodes.map((node) => [node.id, { width: node.width, height: node.height }]));
  if (!input.nodes.length) return { positions, routes, boxes };
  const algorithm = input.algorithm ?? "layered";
  algorithms ??= registeredLayoutAlgorithms().then((items) => new Set(items.map((item) => item.id!))).catch((error) => { algorithms = undefined; throw error; });
  if (!(await algorithms).has(`org.eclipse.elk.${algorithm}`)) throw new Error(`ELK algorithm not registered: ${algorithm}`);
  const direction = input.direction ?? "right";
  const options = { "elk.algorithm": algorithm, "elk.direction": direction.toUpperCase(), "elk.randomSeed": "1",
    "elk.spacing.nodeNode": String(input.gap ?? 36), "elk.spacing.componentComponent": "72",
    "elk.layered.spacing.nodeNodeBetweenLayers": String(input.layerGap ?? 80), "elk.edgeRouting": "ORTHOGONAL",
    "elk.padding": "[top=24,left=24,bottom=24,right=24]", "elk.nodeSize.constraints": "[]",
     "elk.layered.crossingMinimization.semiInteractive": "false" };
  const nodes: ElkNode[] = input.nodes.map((node) => ({ id: node.id, width: node.width, height: node.height,
    layoutOptions: { "elk.portConstraints": "FIXED_POS" },
    ports: algorithm === "layered" ? node.ports?.map((port) => ({ id: portId(node.id, port.id), x: port.x, y: port.y, width: 0, height: 0,
      layoutOptions: { "elk.port.side": port.side } })) : undefined }));
  const ids = new Set(nodes.map((node) => node.id));
  const live = input.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target));
  const edges: ElkExtendedEdge[] = live.map((edge) => {
    const source = algorithm === "layered" && edge.sourcePort ? portId(edge.source, edge.sourcePort) : edge.source;
    const target = algorithm === "layered" && edge.targetPort ? portId(edge.target, edge.targetPort) : edge.target;
    return { id: edge.id, sources: [edge.reverse ? target : source], targets: [edge.reverse ? source : target],
      labels: algorithm === "layered" && edge.label ? [{ text: edge.label, width: Math.min(240, [...edge.label].length * 8), height: 18 }] : undefined };
  });
  // Lanes are compound groups, not ELK partitions (which run along the flow axis).
  // INCLUDE_CHILDREN keeps cross-lane edges in the same routing calculation.
  const laneKeys = input.lanes?.length ? [...input.lanes, ...[...new Set(input.nodes.map((node) => node.lane ?? ""))].filter((id) => !input.lanes!.includes(id))] : [];
  const children = laneKeys.length ? laneKeys.map((lane, i): ElkNode => ({ id: `\u0000lane:${i}`, children: nodes.filter((node) => (input.nodes.find((n) => n.id === node.id)?.lane ?? "") === lane),
    layoutOptions: { ...options, "elk.padding": "[top=36,left=36,bottom=36,right=36]" } })) : nodes;
  const graph = await runElk({ id: "\u0000root", layoutOptions: { ...options, ...(laneKeys.length ? { "elk.hierarchyHandling": "INCLUDE_CHILDREN", "elk.layered.cycleBreaking.strategy": "MODEL_ORDER", "elk.direction": direction === "right" || direction === "left" ? "DOWN" : "RIGHT" } : {}) }, children,
    edges: algorithm === "rectpacking" ? [] : edges });
  const visit = (parent: ElkNode, dx = 0, dy = 0) => {
    for (const node of parent.children ?? []) {
      const x = dx + (node.x ?? 0), y = dy + (node.y ?? 0);
      if (ids.has(node.id)) positions[node.id] = { x, y };
      visit(node, x, y);
    }
    for (const edge of parent.edges ?? []) {
      const original = live.find((item) => item.id === edge.id);
      const section = edge.sections?.[0]; if (!section || !original) continue;
      let points = [section.startPoint, ...(section.bendPoints ?? []), section.endPoint].map((at) => ({ x: at.x + dx, y: at.y + dy }));
      if (original.reverse) points = points.reverse();
      const label = edge.labels?.[0];
      const route: LayoutRoute = { points, label: label?.x !== undefined && label.y !== undefined ? { x: label.x + dx + (label.width ?? 0) / 2, y: label.y + dy + (label.height ?? 0) / 2 } : undefined };
      routes[edge.id] = route;
    }
  };
  visit(graph);
  for (const edge of live) {
    const route = routes[edge.id]; if (!route) continue;
    const side = (id: string, at: { x: number; y: number }): NonNullable<LayoutRoute["sourceSide"]> => {
      const p = positions[id]!, box = boxes[id]!;
      return ([ ["WEST", Math.abs(at.x - p.x)], ["EAST", Math.abs(at.x - p.x - box.width)],
        ["NORTH", Math.abs(at.y - p.y)], ["SOUTH", Math.abs(at.y - p.y - box.height)] ] as const).reduce((a, b) => a[1] <= b[1] ? a : b)[0];
    };
    route.sourceSide = side(edge.source, route.points[0]!); route.targetSide = side(edge.target, route.points.at(-1)!);
  }
  // Algorithms without explicit port support still return sections; the renderer uses their actual endpoints.
  for (const node of input.nodes) if (!positions[node.id] || !Number.isFinite(positions[node.id]!.x + positions[node.id]!.y)) throw new Error(`ELK did not place ${node.id}`);
  return { positions, routes, boxes };
}
