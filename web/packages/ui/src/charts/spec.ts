import type {MetricPresentationOptions} from "../presentation";
import type {Api} from "@platform/kernel";

export type MeasureType = "nominal" | "ordinal" | "temporal" | "quantitative";
export type AggregateOp = "count" | "sum" | "avg" | "min" | "max";
export type TimeUnit = "day" | "week" | "month" | "year";
export type Mark = "bar" | "line" | "area" | "point" | "arc" | "kpi";

export type Encoding = {
  field?: string;
  type: MeasureType;
  aggregate?: AggregateOp;
  timeUnit?: TimeUnit;
  title?: string;
};

export type Channels = {
  x?: Encoding;
  y?: Encoding;
  color?: Encoding;
  theta?: Encoding;
  size?: Encoding;
};

export type ChartData =
  | { entity: string; domain?: unknown[]; search?: string; archived?: boolean; set?:Api.QuerySet;traversal?:Api.LinkTraversal }
  | { values: Record<string, unknown>[] };

export type ChartSpec = {
  rowIdentity?: string;
  metric?:MetricPresentationOptions;
  title?: string;
  description?: string;
  data: ChartData;
  mark: Mark | { type: Mark; orient?: "vertical" | "horizontal"; stack?: boolean; donut?: boolean; showValues?: boolean };
  encoding: Channels;
};

export type AggregateColumn = { name: string; title: string; kind: "group" | "measure"; type: MeasureType; field?: string; money?: boolean };
export type AggregateData = { window?:Api.AggregateWindowResult; histogram?:Api.HistogramResult; columns: AggregateColumn[]; rows: Record<string, unknown>[] };
export type AggregateQuery = Omit<Api.AggregateQuery,"domain"> & {domain?:unknown[]};

export const markOf = (spec: ChartSpec) => (typeof spec.mark === "string" ? { type: spec.mark } : spec.mark);

const channelNames = ["x", "y", "color", "theta", "size"] as const;

export function columnOf(e: Encoding): string {
  if (e.aggregate) return e.aggregate === "count" ? "count" : `${e.aggregate}:${e.field ?? ""}`;
  return e.timeUnit ? `${e.field ?? ""}:${e.timeUnit}` : e.field ?? "";
}

export function aggregateQuery(spec: ChartSpec): AggregateQuery | undefined {
  if (!("entity" in spec.data)) return undefined;
  const groups: string[] = [], measures: string[] = [];
  for (const name of channelNames) {
    const e = spec.encoding[name];
    if (!e) continue;
    const column = columnOf(e);
    const list = e.aggregate ? measures : groups;
    if (column && !list.includes(column)) list.push(column);
  }
  const { domain, search, archived, set, traversal } = spec.data;
  return { domain, search, archived, ...(traversal?{traversal}:{}), ...(set?{set}:{}), groups, measures: measures.length ? measures : ["count"] };
}

const bucket = (value: unknown, unit: TimeUnit): string => {
  const d = new Date(String(value));
  if (Number.isNaN(d.getTime())) return "";
  const iso = d.toISOString();
  if (unit === "day") return iso.slice(0, 10);
  if (unit === "month") return iso.slice(0, 7);
  if (unit === "year") return iso.slice(0, 4);
  const t = new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate()));
  const day = t.getUTCDay() || 7;
  t.setUTCDate(t.getUTCDate() + 4 - day); // ISO week: the Thursday's year
  const week = Math.ceil(((t.getTime() - Date.UTC(t.getUTCFullYear(), 0, 1)) / 86_400_000 + 1) / 7);
  return `${t.getUTCFullYear()}-W${String(week).padStart(2, "0")}`;
};

export function aggregateValues(spec: ChartSpec, values: Record<string, unknown>[]): Record<string, unknown>[] {
  const encodings = channelNames.map((n) => spec.encoding[n]).filter((e): e is Encoding => !!e);
  if (!encodings.some((e) => e.aggregate || e.timeUnit)) return values;
  const groups = encodings.filter((e) => !e.aggregate), measures = encodings.filter((e) => e.aggregate);
  const byKey = new Map<string, { row: Record<string, unknown>; seen: Map<string, number[]> }>();
  for (const v of values) {
    const keys = groups.map((g) => (g.timeUnit ? bucket(v[g.field ?? ""], g.timeUnit) : v[g.field ?? ""]));
    const key = JSON.stringify(keys);
    let acc = byKey.get(key);
    if (!acc) {
      acc = { row: Object.fromEntries(groups.map((g, i) => [columnOf(g), keys[i]])), seen: new Map() };
      byKey.set(key, acc);
    }
    for (const m of measures) {
      const list = acc.seen.get(columnOf(m)) ?? [];
      list.push(m.aggregate === "count" ? 1 : Number(v[m.field ?? ""] ?? 0));
      acc.seen.set(columnOf(m), list);
    }
  }
  const reduce: Record<AggregateOp, (n: number[]) => number> = {
    count: (n) => n.length, sum: (n) => n.reduce((a, b) => a + b, 0), avg: (n) => n.reduce((a, b) => a + b, 0) / n.length,
    min: (n) => Math.min(...n), max: (n) => Math.max(...n),
  };
  return [...byKey.values()].map(({ row, seen }) => ({
    ...row, ...Object.fromEntries(measures.map((m) => [columnOf(m), reduce[m.aggregate!](seen.get(columnOf(m)) ?? [])])),
  }));
}
