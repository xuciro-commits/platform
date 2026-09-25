// The default renderer (ADR-0019 D5): compiles the platform's visualization spec
// into Apache ECharts 6 options. Only this file knows ECharts' option format.
import type { EChartsCoreOption } from "echarts/core";
import { columnOf, markOf, type AggregateColumn, type ChartSpec, type Encoding } from "./spec";

export type Theme = { palette: string[]; foreground: string; muted: string; border: string };

const label = (e: Encoding | undefined, columns: AggregateColumn[]) =>
  e?.title ?? columns.find((c) => c.name === (e && columnOf(e)))?.title ?? (e ? columnOf(e) : "");

/** Formats a measure: money is in minor units of the row's currency. */
export function formatter(e: Encoding | undefined, columns: AggregateColumn[]): (value: unknown, row?: Record<string, unknown>) => string {
  const column = columns.find((c) => c.name === (e && columnOf(e)));
  return (value, row) => {
    const n = Number(value ?? 0);
    if (column?.money) {
      const currency = String(row?.[`${column.field}.currency`] ?? "");
      return currency ? (n / 100).toLocaleString(undefined, { style: "currency", currency }) : (n / 100).toLocaleString();
    }
    return Number.isInteger(n) ? n.toLocaleString() : n.toLocaleString(undefined, { maximumFractionDigits: 2 });
  };
}

/** The column that splits series: the colour channel, or a money measure's currency. */
function seriesColumn(spec: ChartSpec, columns: AggregateColumn[]): string | undefined {
  if (spec.encoding.color) return columnOf(spec.encoding.color);
  const money = columns.find((c) => c.kind === "measure" && c.money);
  return money && columns.some((c) => c.name === `${money.field}.currency`) ? `${money.field}.currency` : undefined;
}

const text = (v: unknown) => (v === undefined || v === null || v === "" ? "—" : String(v));

export function toOption(spec: ChartSpec, rows: Record<string, unknown>[], columns: AggregateColumn[], theme: Theme): EChartsCoreOption {
  const mark = markOf(spec);
  const base = {
    color: theme.palette,
    textStyle: { color: theme.foreground, fontFamily: "inherit", fontSize: 12 },
    tooltip: { trigger: mark.type === "arc" ? "item" : "axis", confine: true },
    animationDuration: 200,
  };
  if (mark.type === "arc") {
    const theta = spec.encoding.theta ?? spec.encoding.y, color = spec.encoding.color ?? spec.encoding.x;
    const fmt = formatter(theta, columns);
    return {
      ...base,
      legend: { type: "scroll", bottom: 0, textStyle: { color: theme.muted } },
      series: [{
        type: "pie", radius: mark.donut ? ["45%", "70%"] : "70%", top: 8, bottom: 28,
        label: { color: theme.foreground },
        tooltip: { valueFormatter: (v: unknown) => fmt(v) },
        data: rows.map((r) => ({ name: text(color && r[columnOf(color)]), value: Number(r[theta ? columnOf(theta) : "count"] ?? 0) })),
      }],
    };
  }
  const horizontal = mark.type === "bar" && mark.orient === "horizontal";
  const category = horizontal ? spec.encoding.y : spec.encoding.x;
  const measure = horizontal ? spec.encoding.x : spec.encoding.y;
  const categoryColumn = category ? columnOf(category) : undefined;
  const measureColumn = measure ? columnOf(measure) : "count";
  const split = seriesColumn(spec, columns);
  const categories = [...new Set(rows.map((r) => text(categoryColumn && r[categoryColumn])))];
  if (category?.type === "temporal" || category?.type === "ordinal") categories.sort();
  const groups = split ? [...new Set(rows.map((r) => text(r[split])))] : [""];
  const fmt = formatter(measure, columns);
  const sample = (group: string) => rows.find((r) => !split || text(r[split]) === group);
  const series = groups.map((group) => ({
    name: group || label(measure, columns),
    type: mark.type === "point" ? "scatter" : mark.type === "area" ? "line" : mark.type,
    ...(mark.type === "area" ? { areaStyle: { opacity: 0.25 } } : {}),
    ...(mark.stack ? { stack: "total" } : {}),
    ...(mark.type === "line" || mark.type === "area" ? { smooth: false, showSymbol: categories.length < 40 } : {}),
    tooltip: { valueFormatter: (v: unknown) => fmt(v, sample(group)) },
    data: categories.map((c) => {
      const row = rows.find((r) => text(categoryColumn && r[categoryColumn]) === c && (!split || text(r[split]) === group));
      return row ? Number(row[measureColumn] ?? 0) : null;
    }),
  }));
  const categoryAxis = { type: "category", data: categories, name: label(category, columns), nameLocation: "middle", nameGap: 26,
    axisLine: { lineStyle: { color: theme.border } }, axisLabel: { color: theme.muted, hideOverlap: true } };
  const valueAxis = { type: "value", name: label(measure, columns), nameTextStyle: { color: theme.muted },
    splitLine: { lineStyle: { color: theme.border } }, axisLabel: { color: theme.muted, formatter: (v: number) => fmt(v, sample(groups[0]!)) } };
  return {
    ...base,
    grid: { left: 48, right: 16, top: split ? 32 : 16, bottom: 40 },
    ...(split ? { legend: { type: "scroll", top: 0, textStyle: { color: theme.muted } } } : {}),
    xAxis: horizontal ? valueAxis : categoryAxis,
    yAxis: horizontal ? categoryAxis : valueAxis,
    series,
  };
}
