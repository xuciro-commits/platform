import type { CanvasBox, CanvasDirection, CanvasPosition } from "./types";

export type GraphSize = { width: number; height: number; gapX: number; gapY: number };
type LayoutNode = { id: string };
type LayoutEdge = { from: string; to: string; label?: string; dashed?: boolean };

/** The gaps along and across the flow. A top-to-bottom drawing keeps the roomier
 * vertical rhythm and the tighter columns its readers are used to. */
const gaps = (direction: CanvasDirection, size: GraphSize) => direction === "right"
  ? { along: size.gapX, across: size.gapY }
  : { along: size.gapY * 2, across: size.gapX / 2 };

/** Layering plus barycentric sweeps keep branches and joins readable without a
 * second layout runtime. `box` gives one node's measured size, so a drawing that
 * mixes BPMN gateways and events with full blocks still lines up (ADR-0086 D4). */
export function layeredLayout(nodes: LayoutNode[], edges: LayoutEdge[], direction: CanvasDirection = "right",
  size: GraphSize = { width: 216, height: 112, gapX: 72, gapY: 36 }, box?: (id: string) => CanvasBox): Map<string, CanvasPosition> {
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
  const breadth = connected.map((row) => row.reduce((sum, id) => sum + across(id) + gap.across, 0) - gap.across);
  const widest = Math.max(0, ...breadth);
  const positions = new Map<string, CanvasPosition>();
  let alongAt = 0;
  connected.forEach((row, i) => {
    let acrossAt = (widest - breadth[i]!) / 2;
    for (const id of row) { positions.set(id, point(alongAt, acrossAt)); acrossAt += across(id) + gap.across; }
    alongAt += thickness[i]! + gap.along;
  });
  if (isolated.size) {
    const alone = [...isolated];
    const columns = Math.max(1, Math.ceil(Math.sqrt(alone.length)));
    const column = Math.max(size.width, ...alone.map((id) => boxOf(id).width)) + gap.along;
    const row = Math.max(size.height, ...alone.map((id) => boxOf(id).height)) + gap.across;
    const offset = connected.length ? alongAt + (thickness.at(-1)! + gap.along) / 2 : 0;
    alone.forEach((id, k) => positions.set(id, point(offset + Math.floor(k / columns) * column, (k % columns) * row)));
  }
  return positions;
}
