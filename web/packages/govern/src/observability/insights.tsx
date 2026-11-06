import type { Api } from "@platform/kernel";
import { useReadQuery } from "@platform/app";
import { Button, Card, EmptyState, PageHeader, PropertyList, Select, Tag, formatStamp, t, useWorkspace } from "@platform/ui";
import { useMemo, useState } from "react";
import { ReadResult } from "../operations";

type Range = "1h" | "24h" | "7d" | "30d";

function rangeWindow(range: Range) {
  const hours = { "1h": 1, "24h": 24, "7d": 168, "30d": 720 }[range];
  const to = new Date();
  return { from: new Date(to.getTime() - hours * 3_600_000).toISOString(), to: to.toISOString() };
}

function RangePicker({ value, onChange }: { value: Range; onChange: (value: Range) => void }) {
  return <label className="grid gap-1 text-xs">{t("Time range")}
    <Select aria-label={t("Time range")} value={value} onChange={(event) => onChange(event.target.value as Range)}>
      <option value="1h">{t("Last hour")}</option>
      <option value="24h">{t("Last 24 hours")}</option>
      <option value="7d">{t("Last 7 days")}</option>
      <option value="30d">{t("Last 30 days")}</option>
    </Select>
  </label>;
}

export function MonitoringMetricsView() {
  const [range, setRange] = useState<Range>("24h");
  const times = useMemo(() => rangeWindow(range), [range]);
  const query = useReadQuery<Api.ObservationSummary>(`/v1/monitoring/metrics?${new URLSearchParams(times)}`, 5000);
  return <>
    <PageHeader title={t("Metrics")} help={t("Operational request and log-health rollups. These are diagnostic signals, not business facts; the original owners remain authoritative.")}
      actions={<RangePicker value={range} onChange={setRange} />} />
    <ReadResult query={query}>{(summary) => <>
      <div className="grid max-w-4xl gap-3 sm:grid-cols-2 lg:grid-cols-4">
        {[
          [t("Requests"), summary.requests.toLocaleString()],
          [t("Errors"), summary.errors.toLocaleString()],
          [t("Warnings"), summary.warnings.toLocaleString()],
          [t("Average latency"), `${Math.round(summary.averageMillis)} ms`],
          [t("Maximum latency"), `${summary.maxMillis} ms`],
          [t("Dropped events"), summary.dropped.toLocaleString()],
        ].map(([label, value]) => <Card key={label} className="grid gap-1 p-4">
          <span className="text-2xl font-semibold tabular-nums">{value}</span>
          <span className="text-2xs font-semibold uppercase tracking-wider text-muted">{label}</span>
        </Card>)}
      </div>
      <div className="mt-3 max-w-4xl"><PropertyList columns={3} items={[
        [t("Window"), `${formatStamp(summary.from ?? times.from)} – ${formatStamp(summary.to ?? times.to)}`],
        [t("Sampling"), summary.sample ? t("Sampled") : t("Complete for retained requests")],
        [t("Basis"), t("Retained operational events")],
      ]} /></div>
    </>}</ReadResult>
  </>;
}

type TraceRow = {
  id: string;
  entries: Api.Observation[];
  started: string;
  route: string;
  status: string;
  duration: number;
  member: string;
  error: boolean;
};

export function MonitoringTracesView() {
  const [range, setRange] = useState<Range>("24h");
  const times = useMemo(() => rangeWindow(range), [range]);
  const query = useReadQuery<Api.ObservationPage>(`/v1/monitoring/traces?${new URLSearchParams({ ...times, limit: "200" })}`, 5000);
  const { open } = useWorkspace();
  const traces = useMemo(() => {
    const groups = new Map<string, Api.Observation[]>();
    for (const entry of query.data?.entries ?? []) {
      if (!entry.traceId) continue;
      const rows = groups.get(entry.traceId) ?? [];
      rows.push(entry);
      groups.set(entry.traceId, rows);
    }
    return [...groups.entries()].map(([id, entries]): TraceRow => {
      const ordered = [...entries].sort((left, right) => left.at.localeCompare(right.at));
      const root = ordered[0]!;
      return {
        id, entries: ordered, started: root.at, route: root.route ?? "—",
        status: root.attributes?.status ?? "—", duration: Number(root.attributes?.durationMs ?? 0),
        member: root.member ?? "—",
        error: ordered.some((entry) => entry.level === "error" || Number(entry.attributes?.status ?? 0) >= 500),
      };
    }).sort((left, right) => right.started.localeCompare(left.started));
  }, [query.data]);
  return <>
    <PageHeader title={t("Traces")} help={t("Request paths grouped by W3C trace identity. A trace shows the retained request spans; use Logs for full event detail and owner runs for business execution.")}
      actions={<RangePicker value={range} onChange={setRange} />} />
    <ReadResult query={query}>{(page) => traces.length === 0
      ? <EmptyState title={t("No request traces in this window")} hint={t("Enable request logging with sampling, then send traffic or widen the time range.")} />
      : <div className="grid gap-2">
        {traces.map((trace) => <Card key={trace.id} className="grid gap-2 p-3 text-sm">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <Button variant="link" className="h-auto min-w-0 p-0 font-mono text-xs" onClick={() => open({ view: "monitoring-logs", params: { traceId: trace.id, time: range } })}>{trace.id}</Button>
            <Tag label={trace.route} tone="info" />
            <Tag label={trace.status} tone={trace.error ? "danger" : "success"} />
            <span className="text-xs text-muted">{trace.duration} ms · {t("{count} spans", { count: trace.entries.length })} · {formatStamp(trace.started)}</span>
          </div>
          <div className="flex flex-wrap gap-2 text-xs text-muted">
            {trace.member && <span>{trace.member}</span>}
            {trace.entries.map((entry) => <span key={entry.id}>{entry.spanId ?? entry.id} · {entry.message}</span>)}
          </div>
        </Card>)}
        <span className="text-xs text-muted">{t("Showing the newest retained request traces. Coverage: {coverage}", { coverage: page.coverage.retainedFrom ? formatStamp(page.coverage.retainedFrom) : t("not reported") })}</span>
      </div>}</ReadResult>
  </>;
}

export function MonitoringAlertsView() {
  const query = useReadQuery<Api.Alert[]>("/v1/monitoring/alerts", 5000);
  const { open } = useWorkspace();
  return <>
    <PageHeader title={t("Alerts")} help={t("Active alert rules are authored in Automate. Monitoring shows what is armed and links to the rule; evaluation, work and notifications stay with Automate.")} />
    <ReadResult query={query}>{(alerts) => alerts.length === 0
      ? <EmptyState title={t("No active alerts")} hint={t("Create an alert rule in Automate, then return here to see it armed.")}
        action={<Button size="sm" onClick={() => open({ view: "alert" })}>{t("Open Automate alerts")}</Button>} />
      : <div className="grid gap-2">{alerts.map((alert) => <Card key={alert.id} className="flex flex-wrap items-center gap-2 p-3 text-sm">
        <Button variant="link" className="h-auto min-w-0 p-0 text-left font-medium" onClick={() => open({ view: "alert", params: { id: alert.id } })}>{alert.title}</Button>
        <Tag label={`${alert.object}.${alert.field} ${alert.operator} ${alert.value ?? ""}`} tone="info" />
        <Tag label={alert.every ? t("Every match") : t("Once per record")} />
        <span className="text-xs text-muted">{alert.message}</span>
      </Card>)}</div>}</ReadResult>
  </>;
}
