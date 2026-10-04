// A chart from the platform's visualization spec (ADR-0019): the data comes from
// the host's aggregates (records, within the member's scope) or inline values;
// the renderer is ECharts 6, loaded only here.
import type { ECharts } from "echarts/core";
import { useEffect, useMemo, useRef, useState } from "react";
import { toOption, formatter, type Theme } from "./echarts";
import { aggregateQuery, aggregateValues, columnOf, markOf, type AggregateColumn, type AggregateData, type AggregateQuery, type ChartSpec } from "./spec";
import { t } from "../i18n";

/** Where aggregates come from: the host's `GET /v1/aggregates/<type>`, wired by the workspace. */
export type ChartSource = { aggregate: (type: string, query: AggregateQuery) => Promise<AggregateData>; revision?: number; scope?:string };

// The kit's tokens as colours a canvas understands (they are oklch in CSS).
function resolve(variable: string): string {
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = 1;
  const ctx = canvas.getContext("2d");
  if (!ctx) return "#888";
  ctx.fillStyle = getComputedStyle(document.documentElement).getPropertyValue(variable).trim() || "#888";
  ctx.fillRect(0, 0, 1, 1);
  const [r, g, b] = ctx.getImageData(0, 0, 1, 1).data;
  return `rgb(${r}, ${g}, ${b})`;
}

function theme(): Theme {
  return {
    palette: ["--tone-info", "--tone-success", "--tone-warning", "--tone-danger", "--chart-5", "--chart-6", "--tone-neutral"].map(resolve),
    foreground: resolve("--foreground"), muted: resolve("--muted"), border: resolve("--border"),
  };
}

/** Loads a spec's rows: the host aggregates records; inline values are aggregated here the same way. */
export function useChartData(spec: ChartSpec, source?: ChartSource): { data?: AggregateData; error?: string } {
  const [state, setState] = useState<{ key?:string; data?: AggregateData; error?: string }>({});
  const key = JSON.stringify(spec.data) + JSON.stringify(spec.encoding) + JSON.stringify([source?.scope,source?.revision ?? 0]);
  const from = useRef(source); // read when the spec changes, not whenever a caller builds a new source object
  from.current = source;
  useEffect(() => {
    const source = from.current;
    const query = aggregateQuery(spec);
    if (!query) {
      const rows = aggregateValues(spec, (spec.data as { values: Record<string, unknown>[] }).values);
      const columns: AggregateColumn[] = Object.values(spec.encoding).filter(Boolean).map((e) => ({
        name: columnOf(e!), title: e!.title ?? columnOf(e!), kind: e!.aggregate ? "measure" : "group", type: e!.type,
      }));
      setState({ key, data: { columns, rows } });
      return;
    }
    if (!source || !("entity" in spec.data)) return setState({ key,error: t("No source for records") });
    let live = true;
    setState({key});
    source.aggregate(spec.data.entity, query).then((data) => live && setState({ key, data }), (e) => live && setState({ key, error: String(e) }));
    return () => { live = false; };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  return state.key===key?state:{};
}

export function Chart({ spec, source, height = 260, frame = true }: { spec: ChartSpec; source?: ChartSource; height?: number; frame?: boolean }) {
  const { data, error } = useChartData(spec, source);
  const mark = markOf(spec);
  const body = error ? <p role="alert" className="text-sm text-[var(--tone-danger)]">{error}</p>
    : !data ? <p className="text-sm text-muted">{t("Loading…")}</p>
    : mark.type === "kpi" ? <Kpi spec={spec} data={data} />
    : data.rows.length === 0 ? <p className="text-sm text-muted">{t("No data")}</p>
    : <Canvas spec={spec} data={data} height={height} />;
  if (!frame) return body;
  return (
    <section className="grid min-w-0 grid-cols-1 content-start gap-1 rounded-md border border-border bg-surface p-3">
      {spec.title && <h3 className="text-sm font-semibold">{spec.title}</h3>}
      {spec.description && <p className="text-xs text-muted">{spec.description}</p>}
      {body}
    </section>
  );
}

function Kpi({ spec, data }: { spec: ChartSpec; data: AggregateData }) {
  const e = spec.encoding.theta ?? spec.encoding.y ?? spec.encoding.x ?? { type: "quantitative" as const, aggregate: "count" as const };
  const column = columnOf(e);
  const fmt = formatter(e, data.columns),display=spec.metric,tone=display?.tone??"neutral",color=({warning:"text-[var(--tone-warning)]",danger:"text-[var(--tone-danger)]",success:"text-[var(--tone-success)]",neutral:""} as Record<string,string>)[tone]??"";
  const format=(value:unknown,row:Record<string,unknown>)=>{if(display&&value==null)return `${display.prefix??""}\u2014${display.suffix??""}`;const numeric=typeof value==="number"&&Number.isFinite(value),short=display?.formatter==="short"&&!data.columns.find(c=>c.name===column)?.money&&numeric;return `${display?.prefix??""}${short?(Math.abs(value)>=1e6?`${(value/1e6).toFixed(1)}M`:Math.abs(value)>=1e3?`${(value/1e3).toFixed(1)}K`:fmt(value,row)):fmt(value,row)}${display?.suffix??""}`;};
  // One value per currency when the measure is money.
  return (
    <div className="flex flex-wrap items-baseline gap-4">
      {(data.rows.length ? data.rows : [{}]).map((r, i) => (
        <span key={i} className={"font-semibold tabular-nums "+color+" "+(display?.variant==="tag"?"rounded-md border border-border px-2 py-1 text-sm":"text-2xl")}>{format(data.rows.length ? r[column] : display&&e.aggregate!=="count"?undefined:0, r)}</span>
      ))}
      {display?.annotation?.text&&<span className="text-xs text-muted"><span>{t("Static note")}: </span><span aria-label={t({up:"Upward annotation",down:"Downward annotation",flat:"Unchanged annotation"}[display.annotation.direction]??"Unchanged annotation")}>{({up:"↑",down:"↓",flat:"→"} as Record<string,string>)[display.annotation.direction]}</span> {display.annotation.text}</span>}
    </div>
  );
}

function Canvas({ spec, data, height }: { spec: ChartSpec; data: AggregateData; height: number }) {
  const element = useRef<HTMLDivElement>(null);
  const chart = useRef<ECharts>(null);
  const option = useMemo(() => toOption(spec, data.rows, data.columns, theme()), [spec, data]);
  const latest = useRef(option);
  latest.current = option;
  useEffect(() => {
    let resize: ResizeObserver | undefined, disposed = false;
    void import("./renderer").then(({ init }) => {
      if (disposed || !element.current) return;
      const instance = init(element.current, undefined, { renderer: "canvas" });
      chart.current = instance;
      instance.setOption(latest.current, true);
      resize = new ResizeObserver(() => instance.resize());
      resize.observe(element.current);
    });
    return () => { disposed = true; resize?.disconnect(); chart.current?.dispose(); chart.current = null; };
  }, []);
  useEffect(() => { chart.current?.setOption(option, true); }, [option]);
  return <div ref={element} role="img" aria-label={spec.title ?? "Chart"} style={{ height }} className="min-w-0 w-full" />;
}
