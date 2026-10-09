import type { CanvasBox, CanvasDirection, CanvasPosition } from "./types";

export type GraphSize = { width: number; height: number; gapX: number; gapY: number };
type LayoutNode = { id: string };
type LayoutEdge = { from: string; to: string; label?: string; dashed?: boolean };

/** A BPMN lane says who or what is responsible for the steps inside it (ADR-0087
 * D1). `ids` is the declared order the bands are drawn in; `of` answers which lane
 * one node belongs to, and a node it cannot place lands in a trailing unnamed band. */
export type LaneAssignment = { ids: string[]; of: (id: string) => string | undefined };
/** The rectangle one lane's band occupies, in canvas coordinates. */
export type LaneBand = { id: string; position: CanvasPosition; box: CanvasBox };

/** The gaps along and across the flow. A top-to-bottom drawing keeps the roomier
 * vertical rhythm and the tighter columns its readers are used to. */
const gaps = (direction: CanvasDirection, size: GraphSize) => direction === "right"
  ? { along: size.gapX, across: size.gapY }
  : { along: size.gapY * 2, across: size.gapX / 2 };

/** Layering plus barycentric sweeps keep branches and joins readable without a
 * second layout runtime. `box` gives one node's measured size, so a drawing that
 * mixes BPMN gateways and events with full blocks still lines up (ADR-0086 D4). */
export function layeredLayout(nodes: LayoutNode[], edges: LayoutEdge[], direction: CanvasDirection = "right",
  size: GraphSize = { width: 216, height: 112, gapX: 72, gapY: 36 }, box?: (id: string) => CanvasBox, lanes?: LaneAssignment): Map<string, CanvasPosition> {
  const ids = new Set(nodes.map((n) => n.id));
  const out = new Map<string, string[]>(nodes.map((n) => [n.id, []]));
  const into = new Map<string, string[]>(nodes.map((n) => [n.id, []]));
  for (const e of edges) if (ids.has(e.from) && ids.has(e.to) && e.from !== e.to) {
    out.get(e.from)!.push(e.to); into.get(e.to)!.push(e.from);
  }
  const back = new Set<string>(), state = new Map<string, "open" | "done">(), order: string[] = [];
  // Explicit retry/loop edges remain visible but do not force an unbounded layer count.
  const visit = (id: string) => {
    state.set(id, "open");
    for (const to of out.get(id)!) {
      if (state.get(to) === "open") back.add(`${id}>${to}`);
      else if (!state.has(to)) visit(to);
    }
    state.set(id, "done"); order.push(id);
  };
  for (const n of nodes) if (!state.has(n.id)) visit(n.id);
  const layer = new Map<string, number>(nodes.map((n) => [n.id, 0]));
  for (const id of order.reverse()) for (const to of out.get(id)!) {
    if (!back.has(`${id}>${to}`)) layer.set(to, Math.max(layer.get(to)!, layer.get(id)! + 1));
  }
  const layers: string[][] = [];
  for (const n of nodes) (layers[layer.get(n.id)!] ??= []).push(n.id);
  const rank = new Map<string, number>();
  const rankAll = () => layers.forEach((row) => row?.forEach((id, i) => rank.set(id, i)));
  rankAll();
  for (let pass = 0; pass < 4; pass++) {
    const forward = pass % 2 === 0;
    const rows = forward ? layers : [...layers].reverse();
    for (const row of rows) {
      if (!row || row.length < 2) continue;
      const center = (id: string) => {
        const adjacent = (forward ? into : out).get(id)!.filter((other) => !back.has(forward ? `${other}>${id}` : `${id}>${other}`));
        return adjacent.length ? adjacent.reduce((sum, other) => sum + (rank.get(other) ?? 0), 0) / adjacent.length : rank.get(id)!;
      };
      row.sort((a, b) => center(a) - center(b)); rankAll();
    }
  }
  // Nodes with no connection at all are not a layer: they are packed into a
  // grid after the connected part, so thirty unrelated objects do not become
  // one thirty-high column beside a three-node chain.
  const isolated = new Set(nodes.filter((n) => !out.get(n.id)!.length && !into.get(n.id)!.length).map((n) => n.id));
  const connected = layers.map((row) => row?.filter((id) => !isolated.has(id)) ?? []).filter((row) => row.length);

  const boxOf = (id: string): CanvasBox => box?.(id) ?? { width: size.width, height: size.height };
  const horizontal = direction === "right";
  const gap = gaps(direction, size);
  /** A box's reach along the flow, and across it. */
  const along = (id: string) => horizontal ? boxOf(id).width : boxOf(id).height;
  const across = (id: string) => horizontal ? boxOf(id).height : boxOf(id).width;
  const point = (alongAt: number, acrossAt: number): CanvasPosition => horizontal ? { x: alongAt, y: acrossAt } : { x: acrossAt, y: alongAt };

  // Each layer is as thick as its widest node; each row of a layer is centred on
  // the widest row, item by item, so mixed shapes stay on one baseline.
  const thickness = connected.map((row) => Math.max(0, ...row.map(along)));
  const alone = [...isolated];
  const column = Math.max(size.width, ...alone.map((id) => boxOf(id).width)) + gap.along;
  const row = Math.max(size.height, ...alone.map((id) => boxOf(id).height)) + gap.across;
  const positions = new Map<string, CanvasPosition>();
  let alongAt = 0;

  if (!lanes) {
    const breadth = connected.map((r) => r.reduce((sum, id) => sum + across(id) + gap.across, 0) - gap.across);
    const widest = Math.max(0, ...breadth);
    connected.forEach((r, i) => {
      let acrossAt = (widest - breadth[i]!) / 2;
      for (const id of r) { positions.set(id, point(alongAt, acrossAt)); acrossAt += across(id) + gap.across; }
      alongAt += thickness[i]! + gap.along;
    });
    if (alone.length) {
      const columns = Math.max(1, Math.ceil(Math.sqrt(alone.length)));
      const offset = connected.length ? alongAt + (thickness.at(-1)! + gap.along) / 2 : 0;
      alone.forEach((id, k) => positions.set(id, point(offset + Math.floor(k / columns) * column, (k % columns) * row)));
    }
    return positions;
  }

  // With lanes, a step stays inside the band of whoever is responsible for it: the
  // band is as deep as the layer that puts the most of that lane's steps side by
  // side, so a parallel branch inside one lane does not overlap its neighbour.
  const pad = 12;
  const keyOf = (id: string) => lanes.of(id) ?? "";
  const keys = [...lanes.ids, ...[...new Set(nodes.map((n) => keyOf(n.id)))].filter((key) => !lanes.ids.includes(key))];
  const members = new Map(keys.map((key) => [key, nodes.filter((n) => keyOf(n.id) === key).map((n) => n.id)]));
  const perLayer = new Map(keys.map((key) => [key, connected.map((r) => r.filter((id) => keyOf(id) === key))]));
  const stranded = new Map(keys.map((key) => [key, alone.filter((id) => keyOf(id) === key)]));
  const columns = new Map(keys.map((key) => [key, Math.max(1, Math.ceil(Math.sqrt(stranded.get(key)!.length)))]));
  const rows = new Map(keys.map((key) => [key, Math.max(0, ...perLayer.get(key)!.map((r) => r.length),
    Math.ceil(stranded.get(key)!.length / columns.get(key)!))]));
  const deep = new Map(keys.map((key) => [key, Math.max(48, ...members.get(key)!.map(across))]));
  const band = (key: string) => rows.get(key)! * deep.get(key)! + Math.max(0, rows.get(key)! - 1) * gap.across;
  const start = new Map<string, number>();
  let bandAt = 0;
  for (const key of keys) { start.set(key, bandAt); bandAt += band(key) + pad * 2 + gap.across; }
  const placeRow = (key: string, ids: string[], at: number) => {
    if (!ids.length) return;
    const breadth = ids.reduce((sum, id) => sum + across(id) + gap.across, 0) - gap.across;
    let acrossAt = start.get(key)! + pad + (band(key) - breadth) / 2;
    for (const id of ids) { positions.set(id, point(at, acrossAt)); acrossAt += across(id) + gap.across; }
  };
  connected.forEach((_, i) => {
    for (const key of keys) placeRow(key, perLayer.get(key)![i]!, alongAt);
    alongAt += thickness[i]! + gap.along;
  });
  if (alone.length) {
    const offset = connected.length ? alongAt + (thickness.at(-1)! + gap.along) / 2 : 0;
    for (const key of keys) stranded.get(key)!.forEach((id, k) => positions.set(id,
      point(offset + Math.floor(k / columns.get(key)!) * column, start.get(key)! + pad + (k % columns.get(key)!) * row)));
  }
  return positions;
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
