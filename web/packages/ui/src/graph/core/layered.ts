import type { CanvasBox, CanvasDirection, CanvasPosition } from "./types";
import { layoutGraph } from "./layout/adapter";
export type GraphSize = { width: number; height: number; gapX: number; gapY: number };
export type LaneAssignment = { ids: string[]; of: (id: string) => string | undefined };
export type LaneBand = { id: string; position: CanvasPosition; box: CanvasBox };
/** Compatibility vocabulary for owners; the engine is exclusively ELK (ADR-0095). */
export async function layeredLayout(nodes: { id: string }[], edges: { from: string; to: string; label?: string }[], direction: CanvasDirection = "right",
  size: GraphSize = { width: 216, height: 112, gapX: 72, gapY: 36 }, box?: (id: string) => CanvasBox, lanes?: LaneAssignment): Promise<Map<string, CanvasPosition>> {
  const result = await layoutGraph({ nodes: nodes.map((n) => ({ id: n.id, ...(box?.(n.id) ?? size), lane: lanes?.of(n.id) })),
    edges: edges.map((e, i) => ({ id: String(i), source: e.from, target: e.to, label: e.label })), direction,
    gap: direction === "right" ? size.gapY : size.gapX / 2, layerGap: direction === "right" ? size.gapX : size.gapY * 2, lanes: lanes?.ids });
  return new Map(Object.entries(result.positions));
}

/** The rectangle each lane's band occupies, read off where its steps actually sit,
 * so an owner-supplied drawing and an arranged one both get bands that fit. A lane
 * with nothing in it still reads as a lane: a nominal band below the drawn ones. */
export function laneBands(lanes: { id: string }[], positions: ReadonlyMap<string, CanvasPosition>, direction: CanvasDirection,
  box: (id: string) => CanvasBox, lane: (id: string) => string | undefined, header = 26, pad = 12): LaneBand[] {
  const placed = [...positions].map(([id, at]) => ({ id, at, key: lane(id) }));
  if (!placed.length) return [];
  const horizontal = direction === "right";
  const along = (at: CanvasPosition) => horizontal ? at.x : at.y;
  const across = (at: CanvasPosition) => horizontal ? at.y : at.x;
  const reach = (id: string) => { const size = box(id); return { along: horizontal ? size.width : size.height, across: horizontal ? size.height : size.width }; };
  const from = Math.min(...placed.map((node) => along(node.at)));
  const to = Math.max(...placed.map((node) => along(node.at) + reach(node.id).along));
  let spare = Math.max(...placed.map((node) => across(node.at) + reach(node.id).across)) + pad;
  return lanes.map((declared) => {
    const members = placed.filter((node) => node.key === declared.id);
    const top = members.length ? Math.min(...members.map((node) => across(node.at))) - pad : spare;
    const bottom = members.length ? Math.max(...members.map((node) => across(node.at) + reach(node.id).across)) + pad : spare + 48;
    if (!members.length) spare = bottom + pad;
    const at = { along: from - header, across: top };
    const size = { along: to - from + header + pad, across: bottom - top };
    return { id: declared.id, position: horizontal ? { x: at.along, y: at.across } : { x: at.across, y: at.along },
      box: horizontal ? { width: size.along, height: size.across } : { width: size.across, height: size.along } };
  });
}
