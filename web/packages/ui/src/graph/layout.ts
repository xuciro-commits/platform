import type { CanvasPosition } from "./model";

export type GraphSize = { width: number; height: number; gapX: number; gapY: number };
type LayoutNode = { id: string };
type LayoutEdge = { from: string; to: string; label?: string; dashed?: boolean };

/** Layering plus barycentric sweeps keep branches and joins readable without a second layout runtime. */
export function layout(nodes: LayoutNode[], edges: LayoutEdge[], direction: "right" | "down" = "right", size: GraphSize = { width: 216, height: 112, gapX: 72, gapY: 36 }): Map<string, CanvasPosition> {
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
  const widest = Math.max(1, ...layers.map((row) => row?.length ?? 0));
  const positions = new Map<string, CanvasPosition>();
  layers.forEach((row, i) => row?.forEach((id, j) => {
    const along = i * (direction === "right" ? size.width + size.gapX : size.height + size.gapY * 2);
    const across = (j + (widest - row.length) / 2) * (direction === "right" ? size.height + size.gapY : size.width + size.gapX / 2);
    positions.set(id, direction === "right" ? { x: along, y: across } : { x: across, y: along });
  }));
  return positions;
}
