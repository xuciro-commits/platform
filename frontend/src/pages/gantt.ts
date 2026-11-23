import type { Dot } from "./Transitions";
import type { GanttScale } from "./view";

export type GanttItem = {
  id: string;
  title: string;
  start: string;
  end: string;
  after?: string;
  group?: string;
  dot?: Dot;
  href: string;
};

export type GanttUnscheduledItem = {
  id: string;
  title: string;
  group?: string;
  dot?: Dot;
  href: string;
};

export type GanttDependencySegment = { key: string; left: number; top: number; width: number; height: number };
export type GanttDependency = {
  key: string;
  fromTitle: string;
  toTitle: string;
  segments: GanttDependencySegment[];
  tipLeft: number;
  tipTop: number;
};

export type GanttBand = { key: string; label: string; width: number };
export type GanttTick = { key: string; label: string; width: number; weekend: boolean; today: boolean };
export type GanttRow =
  | { kind: "group"; key: string; label: string; count: number; collapsed: boolean }
  | { kind: "item"; key: string; item: GanttItem };

export const dayMs = 86400000;
export const rowHeight = 36;
export const nameColumnWidth = 240;

export const dayWidthOf: Record<GanttScale, number> = {
  day: 48,
  week: 18,
  month: 6,
};

export function todayIso() {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
}

export function parseIsoDay(iso: string) {
  const [y, m, d] = iso.slice(0, 10).split("-").map(Number);
  return Date.UTC(y, m - 1, d);
}

export function formatIsoDay(ms: number) {
  return new Date(ms).toISOString().slice(0, 10);
}

export function addDays(iso: string, delta: number) {
  return formatIsoDay(parseIsoDay(iso) + delta * dayMs);
}

export function diffDays(from: string, to: string) {
  return Math.round((parseIsoDay(to) - parseIsoDay(from)) / dayMs);
}

export function shiftMonth(month: string, deltaMonths: number) {
  const [year, number] = month.split("-").map(Number);
  const shifted = new Date(Date.UTC(year, number - 1 + deltaMonths, 1));
  return shifted.toISOString().slice(0, 7);
}

export function stepMonthsFor(scale: GanttScale) {
  return scale === "month" ? 3 : scale === "week" ? 2 : 1;
}

export function periodHeading(month: string, scale: GanttScale, locale?: string) {
  const [year, number] = month.split("-").map(Number);
  const start = new Date(Date.UTC(year, number - 1, 1));
  if (scale === "month") {
    const end = new Date(Date.UTC(year, number + 10, 1));
    const fmt = new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", timeZone: "UTC" });
    return `${fmt.format(start)} – ${fmt.format(end)}`;
  }
  return new Intl.DateTimeFormat(locale, { year: "numeric", month: "long", timeZone: "UTC" }).format(start);
}

export function buildTimeline(startIso: string, days: number, scale: GanttScale, nowIso: string, locale?: string) {
  const startMs = parseIsoDay(startIso);
  const dayWidth = dayWidthOf[scale];
  const bands: GanttBand[] = [];
  const ticks: GanttTick[] = [];
  const weekends: { left: number; width: number }[] = [];
  const monthFmt = new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", timeZone: "UTC" });
  const shortMonthFmt = new Intl.DateTimeFormat(locale, { month: "short", timeZone: "UTC" });
  const weekdayFmt = new Intl.DateTimeFormat(locale, { weekday: "narrow", timeZone: "UTC" });
  const dayMonthFmt = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", timeZone: "UTC" });

  let bandKey = "";
  let bandLabel = "";
  let bandDays = 0;

  for (let i = 0; i < days; i += 1) {
    const ms = startMs + i * dayMs;
    const date = new Date(ms);
    const iso = date.toISOString().slice(0, 10);
    const dow = date.getUTCDay();
    const isWeekend = dow === 0 || dow === 6;
    if (isWeekend && scale !== "month") {
      weekends.push({ left: i * dayWidth, width: dayWidth });
    }
    const nextBandKey = scale === "month" ? String(date.getUTCFullYear()) : iso.slice(0, 7);
    const nextBandLabel = scale === "month" ? String(date.getUTCFullYear()) : monthFmt.format(date);
    if (nextBandKey !== bandKey) {
      if (bandDays > 0) bands.push({ key: bandKey, label: bandLabel, width: bandDays * dayWidth });
      bandKey = nextBandKey;
      bandLabel = nextBandLabel;
      bandDays = 1;
    } else {
      bandDays += 1;
    }

    if (scale === "day") {
      ticks.push({
        key: iso,
        label: `${date.getUTCDate()} ${weekdayFmt.format(date)}`,
        width: dayWidth,
        weekend: isWeekend,
        today: iso === nowIso,
      });
    }
  }
  if (bandDays > 0) bands.push({ key: bandKey, label: bandLabel, width: bandDays * dayWidth });

  if (scale === "week") {
    for (let i = 0; i < days; i += 7) {
      const span = Math.min(7, days - i);
      const ms = startMs + i * dayMs;
      const date = new Date(ms);
      const endMs = startMs + (i + span - 1) * dayMs;
      const nowMs = parseIsoDay(nowIso);
      ticks.push({
        key: date.toISOString().slice(0, 10),
        label: dayMonthFmt.format(date),
        width: span * dayWidth,
        weekend: false,
        today: nowMs >= ms && nowMs <= endMs,
      });
    }
  } else if (scale === "month") {
    let cursor = 0;
    while (cursor < days) {
      const date = new Date(startMs + cursor * dayMs);
      const monthKey = date.toISOString().slice(0, 7);
      let span = 0;
      while (cursor + span < days && new Date(startMs + (cursor + span) * dayMs).toISOString().slice(0, 7) === monthKey) {
        span += 1;
      }
      ticks.push({
        key: monthKey,
        label: shortMonthFmt.format(date),
        width: span * dayWidth,
        weekend: false,
        today: nowIso.slice(0, 7) === monthKey,
      });
      cursor += span;
    }
  }

  const todayOffset = diffDays(startIso, nowIso);
  const todayX = todayOffset >= 0 && todayOffset < days ? todayOffset * dayWidth + Math.floor(dayWidth / 2) : null;
  return { bands, ticks, weekends, totalWidth: days * dayWidth, dayWidth, todayX };
}

export function buildGanttRows(items: GanttItem[], grouped: boolean, groupOrder: string[] | undefined, collapsed: Set<string>, ungroupedLabel: string): GanttRow[] {
  if (!grouped) {
    return items.map((item) => ({ kind: "item", key: item.id, item }));
  }
  const buckets = new Map<string, GanttItem[]>();
  for (const key of groupOrder ?? []) buckets.set(key, []);
  for (const item of items) {
    const key = item.group || ungroupedLabel;
    const list = buckets.get(key);
    if (list) list.push(item);
    else buckets.set(key, [item]);
  }
  const rows: GanttRow[] = [];
  for (const [label, list] of buckets) {
    if (list.length === 0) continue;
    const isCollapsed = collapsed.has(label);
    rows.push({ kind: "group", key: `__group:${label}`, label, count: list.length, collapsed: isCollapsed });
    if (!isCollapsed) {
      for (const item of list) rows.push({ kind: "item", key: item.id, item });
    }
  }
  return rows;
}

const categoricalHues = new Set(["blue", "purple", "cyan", "pink", "green", "orange", "red"]);

export function laneAccentColor(dot?: Dot) {
  if (!dot) return "var(--color-accent)";
  if (categoricalHues.has(dot)) return `var(--color-data-categorical-${dot})`;
  if (dot === "success") return "var(--color-status-success)";
  if (dot === "warning") return "var(--color-status-warning)";
  if (dot === "error") return "var(--color-status-error)";
  if (dot === "accent") return "var(--color-accent)";
  return "var(--color-border-emphasized)";
}

export function buildDependencyArrows(
  rows: GanttRow[], firstRow: number, lastRow: number, spanStart: string, spanDays: number, dayWidth: number,
): GanttDependency[] {
  const byId = new Map<string, { index: number; startX: number; endX: number; midY: number; title: string }>();
  rows.forEach((row, index) => {
    if (row.kind !== "item") return;
    const startOffset = diffDays(spanStart, row.item.start);
    const durationDays = Math.max(1, diffDays(row.item.start, row.item.end) + 1);
    const clippedStart = Math.max(0, startOffset);
    const clippedEnd = Math.min(spanDays, startOffset + durationDays);
    if (clippedEnd <= clippedStart) return;
    const left = clippedStart * dayWidth;
    const width = Math.max(24, (clippedEnd - clippedStart) * dayWidth - 2);
    byId.set(row.item.id, { index, startX: left, endX: left + width, midY: index * rowHeight + Math.floor(rowHeight / 2), title: row.item.title || row.item.id });
  });
  const out: GanttDependency[] = [];
  const line = 2;
  const stub = 10;
  for (const row of rows) {
    if (row.kind !== "item" || !row.item.after || row.item.after === row.item.id) continue;
    const from = byId.get(row.item.after);
    const to = byId.get(row.item.id);
    if (!from || !to) continue;
    if (Math.max(from.index, to.index) < firstRow || Math.min(from.index, to.index) >= lastRow) continue;
    const key = `${row.item.after}->${row.item.id}`;
    const segments: GanttDependencySegment[] = [];
    const addH = (id: string, x1: number, x2: number, y: number) => {
      const left = Math.min(x1, x2);
      segments.push({ key: `${key}:${id}`, left, top: y - 1, width: Math.max(line, Math.abs(x2 - x1)), height: line });
    };
    const addV = (id: string, x: number, y1: number, y2: number) => {
      const top = Math.min(y1, y2);
      segments.push({ key: `${key}:${id}`, left: x - 1, top, width: line, height: Math.max(line, Math.abs(y2 - y1)) });
    };
    const targetX = Math.max(0, to.startX - 4);
    if (targetX >= from.endX + stub) {
      const midX = from.endX + stub;
      addH("h1", from.endX, midX, from.midY);
      addV("v1", midX, from.midY, to.midY);
      addH("h2", midX, targetX, to.midY);
    } else {
      const exitX = from.endX + stub;
      const entryX = Math.max(2, to.startX - stub);
      const turnY = from.midY + (to.midY >= from.midY ? Math.floor(rowHeight / 2) : -Math.floor(rowHeight / 2));
      addH("h1", from.endX, exitX, from.midY);
      addV("v1", exitX, from.midY, turnY);
      addH("h2", exitX, entryX, turnY);
      addV("v2", entryX, turnY, to.midY);
      addH("h3", entryX, targetX, to.midY);
    }
    out.push({ key, fromTitle: from.title, toTitle: to.title, segments, tipLeft: Math.max(0, to.startX - 12), tipTop: to.midY - 7 });
  }
  return out;
}
