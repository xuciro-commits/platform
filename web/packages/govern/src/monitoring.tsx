import type { Api } from "@platform/api";
import { useHost, useOpenRecord, useReadQuery as useRead } from "@platform/app";
import { Button, Card, DataTable, Disclosure, EmptyState, Input, Notice, PageHeader, Sheet, StatusTag, defineStatuses, t, formatStamp, useWorkspace, type ColumnDef } from "@platform/ui";
import { useState } from "react";
import { ReadResult } from "./operations";

type MonitoringView = Api.MonitoringView;

type MonitoringRun = Api.MonitoringRun;
type MonitoringRunType = MonitoringRun["type"];
type MonitoringRunStatus = MonitoringRun["status"];
type MonitoringRunPage = Api.MonitoringRunPage;
type MonitoringCoverage = Api.MonitoringCoverage;

const runStatuses = defineStatuses({
  ok: { label: "OK", tone: "success" },
  failed: { label: "Failed", tone: "danger" },
  pending: { label: "Pending", tone: "info" },
  running: { label: "Running", tone: "info" },
  warning: { label: "Warning", tone: "warning" },
  paused: { label: "Paused", tone: "neutral" },
  cancelled: { label: "Cancelled", tone: "neutral" },
  unknown: { label: "Unknown", tone: "warning" },
});

const runTypeLabels: Record<MonitoringRunType, string> = {
  sync: "Sync", build: "Build", automation: "Automation", function: "Function", delivery: "Delivery",
};

const runTypeFilters: { value: "" | MonitoringRunType; label: string }[] = [
  { value: "", label: "All" },
  { value: "sync", label: "Sync" },
  { value: "build", label: "Build" },
  { value: "automation", label: "Automation" },
  { value: "function", label: "Function" },
  { value: "delivery", label: "Delivery" },
];

function runsQuery(type: string, status: string, limit = 100) {
  const search = new URLSearchParams();
  if (type) search.set("type", type);
  if (status) search.set("status", status);
  search.set("limit", String(limit));
  const query = search.toString();
  return query ? `/v1/monitoring/runs?${query}` : "/v1/monitoring/runs";
}

export function MonitoringOverviewView() {
  const { open } = useWorkspace();
  const query = useRead<MonitoringRunPage>(runsQuery("", ""), 5000);
  return <>
    <PageHeader title={t("Overview")} help={t("What needs attention across every run: failed runs, delayed syncs and failed deliveries. Click a figure to see those runs.")} />
    <ReadResult query={query}>{(page) => {
      const blocks: { label: string; value: number; status: MonitoringRunStatus | "delayed"; type?: MonitoringRunType }[] = [
        { label: t("Failed runs"), value: page.overview.failedRuns, status: "failed" },
        { label: t("Delayed syncs"), value: page.overview.delayedSyncs, status: "delayed", type: "sync" },
        { label: t("Failed deliveries"), value: page.overview.failedDeliveries, status: "failed", type: "delivery" },
      ];
      return <>
        <div className="grid max-w-3xl gap-3 sm:grid-cols-3">
          {blocks.map((block) => <Card key={block.label} className="p-0">
            <Button variant="row" className="grid h-auto content-start gap-1 p-4 text-left"
              onClick={() => open({ view: "monitoring-runs", params: block.status === "delayed" ? { type: "sync", status: "warning" } : { ...(block.type ? { type: block.type } : {}), status: "failed" } })}>
              <span className="text-2xl font-semibold tabular-nums">{block.value}</span>
              <span className="text-2xs font-semibold uppercase tracking-wider text-muted">{block.label}</span>
            </Button>
          </Card>)}
        </div>
        <CoverageNotices coverage={page.coverage} />
      </>;
    }}</ReadResult>
  </>;
}

function CoverageNotices({ coverage }: { coverage: MonitoringCoverage[] }) {
  const limited = coverage.filter((entry) => entry.limitation);
  if (!limited.length) return null;
  const degraded = new Set(["index-unavailable", "definition-unavailable", "read-limit", "input-unavailable"]);
  const failures = limited.filter((entry) => degraded.has(entry.limitation!));
  return <div className="mt-3 grid max-w-3xl gap-2">
    {failures.map((entry) => <Notice key={entry.source} tone="warning" title={coverageSource(entry.source)} message={coverageLimitation(entry.limitation!)} />)}
    <Disclosure summary={t("Data coverage and known limits")} className="text-xs">
      <p className="mt-1 text-muted">{t("Coverage describes what each source can show. A known limit is not a failed run.")}</p>
      <dl className="mt-2 grid gap-2">
        {limited.map((entry) => <div key={entry.source} className="grid gap-0.5 border-t border-line py-2">
          <dt className="font-medium">{coverageSource(entry.source)}</dt>
          <dd className="text-muted">{coverageBasis(entry.basis)} · {coverageLimitation(entry.limitation!)}</dd>
        </div>)}
      </dl>
    </Disclosure>
  </div>;
}

function coverageSource(source: string) {
  const labels: Record<string, string> = {
    source: "Sync sources",
    pipeline: "Pipelines",
    compute: "Computed functions",
    code: "Code",
    projection: "Projections",
    function: "Function runs",
    flow: "Automations",
    agent: "Agents",
    work: "Work",
    effect: "Deliveries",
    "integration-effect": "Integration deliveries",
    "delivery-attempts": "Delivery attempts",
    "run-index": "Run index",
  };
  return labels[source] ? t(labels[source]) : source;
}

function coverageBasis(basis: MonitoringCoverage["basis"]) {
  return t({
    latest: "Current execution state",
    current: "Current state",
    retained: "Retained runs",
  }[basis]);
}

function coverageLimitation(limitation: string) {
  const labels: Record<string, string> = {
    "latest-only": "Only the latest execution is included for this source.",
    "caller-only": "Results are limited to runs visible to you.",
    "permission-filtered": "Results follow your access permissions.",
    "retained-window": "Only deliveries inside the retained window are included.",
    "metadata-only": "Only delivery metadata is included.",
    "not-included": "Individual delivery attempts are not included in this run view.",
    "index-unavailable": "The run index is temporarily unavailable; counts were rebuilt from the original owners.",
    "definition-unavailable": "A source definition is unavailable, so delayed-sync detection may be incomplete.",
    "read-limit": "A source reached the read limit, so the run list may be incomplete.",
    "input-unavailable": "Some run input details are unavailable.",
  };
  return labels[limitation] ? t(labels[limitation]) : t("An unclassified coverage limit applies to this source: {limit}", { limit: limitation });
}

export function MonitoringRunsView({ type: initialType, status: initialStatus }: { type?: string; status?: string }) {
  const [type, setType] = useState<"" | MonitoringRunType>(() => (runTypeFilters.some((f) => f.value === initialType) ? (initialType as MonitoringRunType) : ""));
  const status = initialStatus ?? "";
  const openRecord = useOpenRecord();
  const { open } = useWorkspace();
  const { decide } = useHost();
  const [saving, setSaving] = useState(false);
  const [title, setTitle] = useState("");
  const query = useRead<MonitoringRunPage>(runsQuery(type, status), 5000);
  const openResource = (ref: Api.AssetRef) => open({ view: "definition", params: { app: ref.app, kind: ref.kind, name: ref.name } }, { window: "beside" });
  const columns: ColumnDef<MonitoringRun, any>[] = [
    { accessorKey: "type", header: t("Type"), meta: { width: 110 }, cell: (c) => t(runTypeLabels[c.getValue() as MonitoringRunType] ?? c.getValue()) },
    { accessorKey: "title", header: t("Run"), cell: ({ row: { original: run } }) => run.recordRef
      ? <Button variant="link" className="h-auto p-0 text-left" onClick={() => openRecord(run.recordRef!)}>{run.title}</Button>
      : run.resourceRef
        ? <Button variant="link" className="h-auto p-0 text-left" onClick={() => openResource(run.resourceRef!)}>{run.title}</Button>
        : <span>{run.title}</span> },
    { accessorKey: "source", header: t("Source"), meta: { width: 150 }, cell: (c) => <span className="font-mono text-2xs text-muted">{c.getValue()}</span> },
    { accessorKey: "when", header: t("When"), meta: { width: 170 }, cell: (c) => formatStamp(c.getValue()) },
    { accessorKey: "status", header: t("Status"), meta: { width: 110 }, cell: ({ row: { original: run } }) =>
      <span className="flex items-center gap-1"><StatusTag status={run.status} registry={runStatuses} />{run.ownerStatus && <span className="font-mono text-2xs text-muted">{run.ownerStatus}</span>}</span> },
    { id: "retry", header: t("Actions"), meta: { width: 120 }, minSize: 120, cell: ({ row: { original: run } }) =>
      run.status === "failed" && run.retryable && run.retrySchema && run.retryTarget &&
        <Button size="sm" onClick={async () => { if (await decide(run.retrySchema!, run.retryTarget!, {}, run.retrySchemaVersion ? { schemaVersion: run.retrySchemaVersion } : undefined)) void query.refetch(); }}>
          {run.retryKind === "rerun" ? t("Run again") : t("Retry")}</Button> },
  ];
  return <>
    <PageHeader title={t("Runs")} help={t("Every run the host knows about — syncs, builds, automations, functions and deliveries — newest first. Retry a failed run, or open the resource it ran for.")}
      actions={<Button onClick={() => { setTitle(""); setSaving(true); }}>{t("Save as monitoring view")}</Button>} />
    <div className="mb-3 flex flex-wrap items-center gap-2 text-sm">
      <span className="text-2xs font-semibold uppercase tracking-wider text-muted">{t("Type")}</span>
      {runTypeFilters.map((filter) => <Button key={filter.value || "all"} size="sm" variant={type === filter.value ? "primary" : "ghost"} onClick={() => setType(filter.value)}>{t(filter.label)}</Button>)}
    </div>
    <ReadResult query={query}>{(page) => <>
      <CoverageNotices coverage={page.coverage} />
      <DataTable storageKey="monitoring:runs" data={page.runs} columns={columns} getRowId={(run) => `${run.type}:${run.id}`} height="calc(100dvh - 280px)" empty={t("No runs yet")} />
    </>}</ReadResult>
    <Sheet isolated open={saving} onOpenChange={setSaving} title={t("Save as monitoring view")}>
      <div className="grid gap-3 text-sm">
        <p className="text-muted">{t("Save the current type and status filter under a name. It appears in your Monitoring views.")}</p>
        <label className="grid gap-1 text-xs">{t("View name")}<Input aria-label={t("View name")} value={title} onChange={(e) => setTitle(e.target.value)} /></label>
        <span className="flex justify-end gap-2">
          <Button onClick={() => setSaving(false)}>{t("Cancel")}</Button>
          <Button variant="primary" disabled={!title.trim()} onClick={async () => {
            const query = new URLSearchParams();
            if (type) query.set("type", type);
            if (status) query.set("status", status);
            if (await decide("monitoring.view.save", { type: "monitoring.view", id: crypto.randomUUID() }, {
              title: title.trim(), family: "runs", query: query.toString(), time: "1h", density: "comfortable", type, status,
            })) setSaving(false);
          }}>{t("Save")}</Button>
        </span>
      </div>
    </Sheet>
  </>;
}

export function MonitoringViewsView() {
  const { open } = useWorkspace();
  const { decide } = useHost();
  const query = useRead<MonitoringView[]>("/v1/monitoring/views", 5000);
  return <>
    <PageHeader title={t("Monitoring views")} help={t("Your saved run filters. Open one to see its runs; remove the ones you no longer need. Create a view from the Runs screen.")} />
    <ReadResult query={query}>{(views) => views.length === 0
      ? <EmptyState title={t("No monitoring views yet")} hint={t("Filter Runs or Logs, then save the current query as a monitoring view.")}
          action={<Button size="sm" onClick={() => open({ view: "monitoring-runs" })}>{t("Open Runs")}</Button>} />
      : <div className="grid max-w-3xl gap-2">
          {views.map((view) => <Card key={view.id} className="flex flex-wrap items-center gap-2 p-3 text-sm">
            <Button variant="link" className="h-auto p-0 text-left font-medium" onClick={() => {
              const params = Object.fromEntries(new URLSearchParams(view.query ?? ""));
              if (!view.query && view.type) params.type = view.type;
              if (!view.query && view.status) params.status = view.status;
              open(view.family === "logs"
                ? { view: "monitoring-logs", params: { ...params, time: view.time ?? "1h", ...(view.from ? { from: view.from } : {}), ...(view.to ? { to: view.to } : {}) } }
                : { view: "monitoring-runs", params: { ...params, ...(view.type ? { type: view.type } : {}), ...(view.status ? { status: view.status } : {}) } });
            }}>{view.title}</Button>
            <span className="text-xs text-muted">{t(view.type ? runTypeLabels[view.type] : "All")} · {view.status ? <StatusTag status={view.status} registry={runStatuses} /> : t("All")}</span>
            <span className="text-xs text-muted">{view.family === "logs" ? t("Logs") : t("Runs")} · {view.time ?? "1h"}</span>
            <Button size="sm" variant="danger" className="ml-auto" onClick={async () => { if (await decide("monitoring.view.remove", { type: "monitoring.view", id: view.id }, {})) void query.refetch(); }}>{t("Remove")}</Button>
          </Card>)}
        </div>}</ReadResult>
  </>;
}
