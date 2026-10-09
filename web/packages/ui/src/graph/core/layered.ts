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

/** Crossings between two adjacent layers, counted the way Gansner et al. do it:
 * walk one layer's sockets in order and count how far each neighbour's position
 * in the other layer falls out of sequence. O(m log m) with a sorted walk. */
function crossings(layerA: string[], layerB: string[], neighbours: Map<string, string[]>): number {
  const rankB = new Map(layerB.map((id, i) => [id, i]));
  const sockets: { at: number; order: number }[] = [];
  layerA.forEach((id, i) => {
    for (const other of neighbours.get(id) ?? []) {
      const at = rankB.get(other);
      if (at !== undefined) sockets.push({ at, order: i });
    }
  });
  sockets.sort((a, b) => a.at - b.at || a.order - b.order);
  let count = 0;
  for (let i = 1; i < sockets.length; i++) for (let j = 0; j < i; j++) if (sockets[j]!.order > sockets[i]!.order) count++;
  return count;
}

/** The classic barycenter and median measures, both read over the same
 * neighbourhood. Alternating them is what keeps a sweep from oscillating. */
function measure(id: string, adjacent: string[], rank: Map<string, number>, median: boolean): number {
  const values = adjacent.map((other) => rank.get(other)).filter((value): value is number => value !== undefined).sort((a, b) => a - b);
  if (!values.length) return rank.get(id) ?? 0;
  if (!median) return values.reduce((sum, value) => sum + value, 0) / values.length;
  const middle = values.length >> 1;
  return values.length % 2 ? values[middle]! : (values[middle - 1]! + values[middle]!) / 2;
}

/** Layered layout after Sugiyama, with the parts that make it read well: long
 * edges are broken into unit segments through dummy points so they join the
 * crossing count and pull their endpoints straight (ADR-0091 D1); ordering
 * alternates barycenter and median sweeps until the crossings stop falling; and
 * coordinates are assigned by pulling each node toward its neighbours while the
 * layer's order and spacing hold. `box` gives one node's measured size, so a
 * drawing that mixes BPMN gateways and events with full blocks still lines up. */
export function layeredLayout(nodes: LayoutNode[], edges: LayoutEdge[], direction: CanvasDirection = "right",
  size: GraphSize = { width: 216, height: 112, gapX: 72, gapY: 36 }, box?: (id: string) => CanvasBox, lanes?: LaneAssignment): Map<string, CanvasPosition> {
  const ids = new Set(nodes.map((n) => n.id));
  const out = new Map<string, string[]>(nodes.map((n) => [n.id, []]));
  const into = new Map<string, string[]>(nodes.map((n) => [n.id, []]));
  const live = edges.filter((e) => ids.has(e.from) && ids.has(e.to) && e.from !== e.to);
  for (const e of live) { out.get(e.from)!.push(e.to); into.get(e.to)!.push(e.from); }
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

  // Normalize: an edge that spans more than one layer gets a chain of dummy
  // points, one per layer it passes through. They participate in ordering and
  // coordinates — which is the whole point — and vanish from the result.
  const forward = live.filter((e) => !back.has(`${e.from}>${e.to}`) && layer.get(e.to)! > layer.get(e.from)! + 1);
  forward.forEach((e, edgeIndex) => {
    const chain: string[] = [];
    for (let step = layer.get(e.from)! + 1; step < layer.get(e.to)!; step++) {
      const dummy = `~${edgeIndex}:${step}`;
      chain.push(dummy); layer.set(dummy, step);
      out.set(dummy, []); into.set(dummy, []);
    }
    const first = chain[0]!, last = chain[chain.length - 1]!;
    // Rewire the segment chain: from → dummy0 → … → dummyN → to.
    const fromList = out.get(e.from)!, intoList = into.get(e.to)!;
    fromList[fromList.indexOf(e.to)] = first;
    intoList[intoList.indexOf(e.from)] = last;
    out.get(last)!.push(e.to); into.get(e.to)!.push(last);
    for (let i = 0; i < chain.length - 1; i++) { out.get(chain[i]!)!.push(chain[i + 1]!); into.get(chain[i + 1]!)!.push(chain[i]!); }
  });

  const all = [...layer.keys()];
  const layers: string[][] = [];
  for (const id of all) (layers[layer.get(id)!] ??= []).push(id);
  const rank = new Map<string, number>();
  const rankAll = () => layers.forEach((row) => row?.forEach((id, i) => rank.set(id, i)));
  rankAll();
  // Ordering: alternate barycenter and median, keep whichever order crossed
  // less, and stop once two full rounds find nothing better. Long edges vote
  // through their dummy chains, so a corridor stays a corridor.
  let best = crossingsTotal(layers, into, back);
  for (let pass = 0; pass < 24 && best > 0; pass++) {
    const median = pass % 2 === 1;
    const forwardSweep = pass % 4 < 2;
    const rows = forwardSweep ? layers : [...layers].reverse();
    const adjacent = forwardSweep ? into : out;
    for (const row of rows) {
      if (!row || row.length < 2) continue;
      row.sort((a, b) => measure(a, adjacent.get(a)!.filter((other) => !back.has(forwardSweep ? `${other}>${a}` : `${a}>${other}`)), rank, median)
        - measure(b, adjacent.get(b)!.filter((other) => !back.has(forwardSweep ? `${other}>${b}` : `${b}>${other}`)), rank, median)
        || rank.get(a)! - rank.get(b)!);
      rankAll();
    }
    const now = crossingsTotal(layers, into, back);
    if (now < best) best = now; else if (pass >= 3 && pass % 2 === 1) break;
  }

  // Nodes with no connection at all are not a layer: they are packed into a
  // grid after the connected part, so thirty unrelated objects do not become
  // one thirty-high column beside a three-node chain.
  const isolated = new Set(nodes.filter((n) => !out.get(n.id)!.length && !into.get(n.id)!.length).map((n) => n.id));
  const connected = layers.map((row) => row?.filter((id) => !isolated.has(id)) ?? []).filter((row) => row.length);

  const boxOf = (id: string): CanvasBox => box?.(id) ?? { width: size.width, height: size.height };
  const horizontal = direction === "right";
  const gap = gaps(direction, size);
  /** A box's reach along the flow, and across it. A routing point has no extent:
   * its spacing is the layer gap alone, and it never widens a column. */
  const isDummy = (id: string) => id.startsWith("~");
  const along = (id: string) => isDummy(id) || isolated.has(id) ? 0 : horizontal ? boxOf(id).width : boxOf(id).height;
  const across = (id: string) => isDummy(id) || isolated.has(id) ? 0 : horizontal ? boxOf(id).height : boxOf(id).width;
  const point = (alongAt: number, acrossAt: number): CanvasPosition => horizontal ? { x: alongAt, y: acrossAt } : { x: acrossAt, y: alongAt };

  // Initial coordinates: each layer packed left to right, centred on the widest.
  const thickness = connected.map((row) => Math.max(0, ...row.map(along)));
  const alone = [...isolated];
  const column = Math.max(size.width, ...alone.map((id) => boxOf(id).width)) + gap.along;
  const rowHeight = Math.max(size.height, ...alone.map((id) => boxOf(id).height)) + gap.across;
  const positions = new Map<string, CanvasPosition>();
  let alongAt = 0;
  const centre = () => {
    positions.clear();
    if (lanes) return centreLanes();
    const breadth = connected.map((r) => r.reduce((sum, id) => sum + across(id) + gap.across, 0) - gap.across);
    const widest = Math.max(0, ...breadth);
    let at = 0;
    connected.forEach((r, i) => {
      let acrossAt = (widest - breadth[i]!) / 2;
      for (const id of r) { positions.set(id, point(at, acrossAt)); acrossAt += across(id) + gap.across; }
      at += thickness[i]! + gap.along;
    });
    alongAt = at;
  };

  /** With lanes, a step stays inside the band of whoever is responsible for it:
   * the band is as deep as the layer that puts the most of that lane's steps side
   * by side, so a parallel branch inside one lane does not overlap its neighbour.
   * Dummy routing points sit in the flow between the bands, unmoved by them. */
  const centreLanes = () => {
    const pad = 12;
    const keyOf = (id: string) => lanes!.of(id) ?? "";
    const keys = [...lanes!.ids, ...[...new Set(nodes.map((n) => keyOf(n.id)))].filter((key) => !lanes!.ids.includes(key))];
    const real = connected.map((r) => r.filter((id) => !isDummy(id)));
    const membersOf = new Map(keys.map((key) => [key, nodes.filter((n) => keyOf(n.id) === key).map((n) => n.id)]));
    const perLayer = new Map(keys.map((key) => [key, real.map((r) => r.filter((id) => keyOf(id) === key))]));
    const stranded = new Map(keys.map((key) => [key, alone.filter((id) => keyOf(id) === key)]));
    const columns = new Map(keys.map((key) => [key, Math.max(1, Math.ceil(Math.sqrt(stranded.get(key)!.length)))]));
    const rows = new Map(keys.map((key) => [key, Math.max(0, ...perLayer.get(key)!.map((r) => r.length),
      Math.ceil(stranded.get(key)!.length / columns.get(key)!))])); 
    const deep = new Map(keys.map((key) => [key, Math.max(48, ...membersOf.get(key)!.map(across))]));
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
    let at = 0;
    connected.forEach((r, i) => {
      for (const key of keys) placeRow(key, perLayer.get(key)![i]!, at);
      // Dummy points thread the layer at its centre so a long edge crosses the
      // bands without dragging a step out of its own.
      const bandMid = (Math.max(...keys.map((key) => start.get(key)! + band(key))) + Math.min(...keys.map((key) => start.get(key)!)) ) / 2;
      r.filter((id) => isDummy(id)).forEach((id, k) => positions.set(id, point(at, bandMid + k * gap.across)));
      at += thickness[i]! + gap.along;
    });
    alongAt = at;
    if (alone.length) {
      const offset = connected.length ? alongAt + (thickness.at(-1)! + gap.along) / 2 : 0;
      for (const key of keys) stranded.get(key)!.forEach((id, k) => positions.set(id,
        point(offset + Math.floor(k / columns.get(key)!) * column, start.get(key)! + pad + (k % columns.get(key)!) * rowHeight)));
    }
  };
  centre();

  // Coordinates: pull each node toward the middle of its neighbours, layer by
  // layer, in alternating directions — the priority step of Sugiyama's frame.
  // The layer's order and minimum spacing are never broken; a node only slides
  // as far as its neighbours allow.
  if (!lanes) {
    const acrossAt = (id: string) => horizontal ? positions.get(id)!.y : positions.get(id)!.x;
    const setAcross = (id: string, value: number) => positions.set(id, point(horizontal ? positions.get(id)!.x : value, horizontal ? value : positions.get(id)!.y));
    const desired = (id: string) => {
      // Every neighbour votes, routing points included: a chain of them is what
      // pulls a long edge straight, and what pulls its endpoints toward the
      // corridor the edge carves through the drawing.
      const values = into.get(id)!.concat(out.get(id)!).map((other) => acrossAt(other))
        .filter((value) => Number.isFinite(value)).sort((a, b) => a - b);
      if (!values.length) return acrossAt(id);
      const middle = values.length >> 1;
      return values.length % 2 ? values[middle]! : (values[middle - 1]! + values[middle]!) / 2;
    };
    for (let round = 0; round < 6; round++) {
      const rows = round % 2 === 0 ? connected : [...connected].reverse();
      for (const r of rows) {
        if (r.length < 2) continue;
        const wants = new Map(r.map((id) => [id, desired(id)]));
        // In the layer's own order, slide each node toward its wish, bounded by
        // the neighbours already placed on either side.
        const placed = new Map(r.map((id) => [id, acrossAt(id)]));
        const order = round % 2 === 0 ? r : [...r].reverse();
        for (const id of order) {
          const index = r.indexOf(id);
          const previous = index > 0 ? placed.get(r[index - 1]!) : undefined;
          const next = index + 1 < r.length ? placed.get(r[index + 1]!) : undefined;
          const room = across(id) + gap.across;
          let target = wants.get(id)!;
          if (previous !== undefined) target = Math.max(target, previous + room);
          if (next !== undefined) target = Math.min(target, next - room);
          placed.set(id, target);
        }
        for (const id of r) setAcross(id, placed.get(id)!);
      }
    }
    // Re-base each layer's column at the flow's origin so pulls never leave a
    // drifting offset between layers.
    let at = 0;
    for (const [i, r] of connected.entries()) {
      const min = Math.min(...r.map((id) => horizontal ? positions.get(id)!.x : positions.get(id)!.y));
      for (const id of r) {
        const current = positions.get(id)!;
        positions.set(id, point((horizontal ? current.x : current.y) - min + at, horizontal ? current.y : current.x));
      }
      at += thickness[i]! + gap.along;
    }
  }

  if (alone.length) {
    const columns = Math.max(1, Math.ceil(Math.sqrt(alone.length)));
    const offset = connected.length ? alongAt + (thickness.at(-1)! + gap.along) / 2 : 0;
    alone.forEach((id, k) => positions.set(id, point(offset + Math.floor(k / columns) * column, (k % columns) * rowHeight)));
  }
  // Routing points are not nodes: the result holds the real drawing only.
  for (const id of [...positions.keys()]) if (isDummy(id)) positions.delete(id);
  return positions;
}

/** Total crossings between every pair of adjacent layers. */
function crossingsTotal(layers: string[][], into: Map<string, string[]>, back: Set<string>): number {
  let total = 0;
  for (let i = 1; i < layers.length; i++) {
    const previous = layers[i - 1]!, current = layers[i]!;
    const neighbours = new Map<string, string[]>();
    for (const id of current) neighbours.set(id, (into.get(id) ?? []).filter((other) => previous.includes(other) && !back.has(`${other}>${id}`)));
    total += crossings(previous, current, neighbours);
  }
  return total;
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
