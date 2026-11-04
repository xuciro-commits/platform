import { ResourceControls } from "../editor/workbench";
import { PropertyList, StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { CreateObjectFromDataset } from "./dataset-object";
import { ResourceList } from "../editor/ResourceList";
import { useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import { CompartmentField, MarkingField } from "./marking";
import { Button, Checkbox, ContentTabs, DataTable, FilePicker, Histogram, Input, PageHeader, Panel, Select, SummaryStatistics, Tag, TagCounts, t, type ColumnDef, type StatisticsValue } from "@platform/ui";
import { DatasetLineage } from "./lineage";

type Field = { name: string; type: string };
type Load = { at: string; version: number; rows: number; bytes: number; drift?: string[] };
type Draft = { id: string; revision: number; name: string; title: string; keep?: number; key?: string; stream?: boolean; marking?: string; compartments?: string[]; producer?: string; schema?: Field[]; version?: number; last?: Load };
type Version = { id: string; version: number; at: string; rows: number; archived?: boolean; data?: Record<string, unknown>[] };
type VersionRow = { id: string; dataset: string; version: number; type?: string; at: string; rows: number; archived?: boolean };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "" });
const fieldClass = "grid min-w-0 gap-1 text-xs";
const transactionLabel = (type?: string): string => (({ snapshot: t("Snapshot"), append: t("Append"), update: t("Update"), delete: t("Delete") }) as Record<string, string>)[type || "snapshot"] ?? type ?? "";

export function Datasets() {
  const { source , holds} = useHost(), { open } = useApplicationWorkspace();
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Datasets")} description={t("Only a builder or integrator can edit datasets.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Datasets")} help={t("Rows as they came, in versions. A source loads a dataset instead of mapping straight to an object; a pipeline reads one and writes another or an object.")}
      actions={<Button variant="primary" onClick={() => open({ view: "dataset", params: { id: "new" } })}>{t("New dataset")}</Button>} />
    <ResourceList source={source} type="build.dataset" fields={["title", "name", "producer", "version", "marking"]} onOpen={(record) => open({ view: "dataset", params: { id: record.id } })} />
  </div>;
}

export function DatasetEditor({ id }: { id: string }) {
  const { decide, holds } = useHost(), { open } = useApplicationWorkspace();
  const application = useApplicationScope();
  const { draft, structured, query, dirty, busy, error, setError, inputs, patch, perform, save, discardChanges } = useDefinitionDraft<Draft>({
    id, type: "build.dataset", view: "dataset", empty,
    failure: t("The dataset could not be saved or loaded. Your draft is still here."),
    payload: draft => {
      return { name: draft.name, title: draft.title, keep: Number(draft.keep) || 0, marking: draft.marking ?? "", compartments: draft.compartments ?? [], key: draft.key ?? "", stream: !!draft.stream };
    },
  });
  const [loadType, setLoadType] = useState("snapshot");
  const [creating, setCreating] = useState(false);
  const [shown, setShown] = useState(0);
  const [selectedColumn, setSelectedColumn] = useState<string | undefined>(undefined);
  const [tab, setTab] = useState("overview");
  const version = shown || draft.version || 0;
  const versionQuery = useReadQuery<{ record?: Version }>(`/v1/records/build.datasetversion/${encodeURIComponent(`${draft.id}@${version}`)}`, 15000, version > 0);
  const versionsQuery = useReadQuery<{ records: VersionRow[] }>(`/v1/records/build.datasetversion?domain=${encodeURIComponent(JSON.stringify([["dataset", "=", draft.id]]))}&sort=-version&archived=true&limit=200`, 15000, !!draft.id);
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Datasets")} description={t("Only a builder or integrator can edit datasets.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Datasets")} description={query.isError ? t("The dataset could not be loaded.") : t("Loading…")} />;
  const last = draft.last, latest = draft.version ?? 0;
  if (columns.length && !selectedColumn) setSelectedColumn(columns[0]);
  const rows = versionQuery.data?.record?.data ?? [], columns = draft.schema?.map((f) => f.name) ?? Object.keys(rows[0] ?? {});
  const transactions = versionsQuery.data?.records ?? [];
  const openVersion = (v: number) => { setShown(v); setTab("preview"); };
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader crumbs={[{ label: t("Datasets"), onClick: () => open({ view: "dataset" }) }]} title={draft.title || t("New dataset")} meta={draft.name ? <span className="font-mono">{draft.name}{draft.version ? ` · v${draft.version}` : ""}</span> : undefined} help={t("Name it, then upload a CSV or point a source or a pipeline at it. Each load is a new version; the schema is inferred from the rows and drift is listed per load.")}
      actions={<>
        {query.data?.record && !dirty && <><Select aria-label={t("Transaction type")} value={loadType} onChange={event => setLoadType(event.target.value)}>{["snapshot", "append", "update", "delete"].map(type => <option key={type} value={type}>{transactionLabel(type)}</option>)}</Select><FilePicker accept=".csv,text/csv" disabled={busy} onFile={file => void perform(async () => {
          const record = query.data!.record!;
          if (await decide("build.dataset.load", { type: "build.dataset", id: record.id }, { type: loadType, csv: await file.text(), producer: `upload:${file.name}` }, { expectedRevision: record.revision, onRefused: setError })) { await query.refetch(); setShown(0); setTab("preview"); }
        })}>{t("Upload CSV")}</FilePicker></>}
        <ResourceControls type="build.dataset" record={draft} dirty={dirty} busy={busy} route={{ view: "dataset", params: { id } }} onCancel={discardChanges} onSave={() => perform(save)}
          operations={holds("build", "builder") && !!draft.schema?.length ? [{ id: "object", label: t("Create object type"), disabled: dirty || !!inputs?.invalid || !latest, run: () => setCreating(true) }] : []} />
      </>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    {creating && draft.schema && <CreateObjectFromDataset dataset={{ id: draft.id, name: draft.name, title: draft.title, schema: draft.schema }} application={application}
      onClose={() => setCreating(false)} onCreated={object => { setCreating(false); open({ view: "object-type", params: { id: object } }, { ifCurrent: { view: "dataset", params: { id } } }); }} />}
    <StructuredEditor {...structured}><fieldset disabled={busy} className="min-w-0 border-0 p-0">
      <ContentTabs label={t("Dataset sections")} value={tab} onChange={setTab} items={[
        { id: "overview", title: t("Overview"), content: <div className="grid min-w-0 gap-3">
          <Panel title={t("Dataset")} className="grid min-w-0 content-start gap-3">
            <label className={fieldClass}>{t("Dataset name")}<Input disabled={!!draft.producer} value={draft.name} placeholder="sapmaterials" onChange={(e) => patch({ name: e.target.value })} /></label>
            <label className={fieldClass}>{t("Dataset title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
            <label className={fieldClass}>{t("Versions kept")}<Input draftKey="versions-kept" type="number" min={1} max={50} value={draft.keep ?? ""} placeholder="3" onChange={(e) => patch({ keep: Number(e.target.value) })} />
              <span className="text-xs text-muted">{t("Older versions keep their row count but lose their rows.")}</span></label>
            <label className={fieldClass}>{t("Key column")}<Select value={draft.key ?? ""} onChange={event => patch({ key: event.target.value })}><option value="">{t("No key")}</option>{draft.schema?.map(field => <option key={field.name} value={field.name}>{field.name}</option>)}</Select></label>
            <Checkbox checked={!!draft.stream} onChange={stream => patch({ stream })}>{t("Streaming dataset")}</Checkbox>
            <MarkingField value={draft.marking ?? ""} onChange={(marking) => patch({ marking })} help={t("Markings follow loaded data. Remove markings in an authorized pipeline step; existing versions stay protected.")} />
            <CompartmentField value={draft.compartments} onChange={compartments => patch({ compartments })} />
            {draft.producer && <p className="text-xs text-muted">{t("Loaded by")}: <span className="font-mono">{draft.producer}</span></p>}
          </Panel>
          {draft.id && <DatasetLineage dataset={draft.id} />}
        </div> },
        { id: "schema", title: t("Schema"), content: <Panel title={t("Schema")} className="grid min-w-0 content-start gap-2">
          {!draft.schema?.length ? <p className="text-xs text-muted">{t("Inferred from the first load.")}</p> :
            <ul className="grid gap-1 text-xs">{draft.schema.map((f) => <li key={f.name} className="flex items-center gap-2"><span className="font-mono">{f.name}</span><Tag label={f.type} /></li>)}</ul>}
          {last && <div className="grid gap-3 border-t border-border pt-3"><h3 className="text-xs font-medium">{t("Latest load")}</h3><PropertyList columns={2} items={[[t("Version"), last.version], [t("Loaded at"), new Date(last.at).toLocaleString()], [t("Rows"), last.rows], [t("Size"), `${Math.round(last.bytes / 1024)} KB`]]} />
            {last.drift?.length ? <Tag label={`${t("Drift")}: ${last.drift.join(" ")}`} tone="warning" /> : null}</div>}
        </Panel> },
        { id: "preview", title: t("Preview"), content: <Panel title={version ? t("Rows · v{version}", { version }) : t("Rows")} className="grid min-w-0 content-start gap-2">
          {!latest ? <p className="text-xs text-muted">{t("No rows yet. Upload a CSV, or publish a source or pipeline that writes this dataset.")}</p> :
            versionQuery.data?.record?.archived ? <p className="text-xs text-muted">{t("This version is no longer kept.")}</p> :
            <div className="overflow-auto">
              <DataTable data={rows} getRowId={(row, index) => String(index)} searchable={false} height={Math.min(420, 40 + rows.length * 28)} columns={columns.map((name): ColumnDef<Record<string, unknown>, unknown> => ({ id: name, header: name, accessorFn: (row) => row[name], cell: (context) => <span className="font-mono">{cell(context.getValue())}</span> }))} />
              <p className="px-2 py-1 text-xs text-muted">{t("{rows} rows · full column statistics below", { rows: rows.length })}</p>
            </div>}
        </Panel> },
          {selectedColumn && <div className="grid gap-3 border-t border-border pt-3">
            <label className={fieldClass}>{t("Column")}<Select value={selectedColumn} onChange={(e) => setSelectedColumn(e.target.value)}>{columns.map((c) => <option key={c} value={c}>{c}</option>)}</Select></label>
            <div className="grid min-w-0 gap-3">
              {(() => {
                const stats = columnStats(selectedColumn, rows);
                if (stats.kind === "empty") return <p className="text-xs text-muted">{t("No values.")}</p>;
                if (stats.kind === "numeric") return <>
                  <SummaryStatistics value={stats.value} fieldTitle={selectedColumn} />
                  <Histogram value={stats.histogram} label={t("Value distribution")} />
                </>;
                return <>
                  <p className="text-xs text-muted">{t("{count} non-empty values", { count: stats.count })}</p>
                  <TagCounts terms={stats.terms.map((t) => ({ value: t.value, count: t.count }))} label={t("Value distribution")} />
                </>;
              })()}
            </div>
          </div>}
        { id: "transactions", title: t("Transactions"), badge: transactions.length || undefined, content: <Panel title={t("Transactions")} help={t("Each load is a transaction and a new version. Choose one to preview its rows.")} className="grid min-w-0 content-start gap-2">
          {versionsQuery.isError ? <p role="alert" className="text-xs text-danger">{t("Versions are unavailable to you.")}</p> :
          <DataTable data={transactions} getRowId={(row) => row.id} searchable={false} selectedId={version ? `${draft.id}@${version}` : undefined} height={Math.min(480, 80 + transactions.length * 36)}
            empty={versionsQuery.isPending ? t("Loading…") : t("No version yet. Upload a CSV, or publish a source or pipeline that writes this dataset.")}
            onRowClick={(row) => openVersion(row.version)} columns={[
              { id: "version", header: t("Version"), accessorFn: (row) => row.version, cell: (context) => <span className="font-mono">v{String(context.getValue())}</span> },
              { id: "type", header: t("Transaction type"), accessorFn: (row) => row.type, cell: (context) => <Tag label={transactionLabel(context.getValue() as string | undefined)} /> },
              { id: "at", header: t("Loaded at"), accessorFn: (row) => row.at, cell: (context) => new Date(String(context.getValue())).toLocaleString() },
              { id: "rows", header: t("Rows"), accessorFn: (row) => row.rows, cell: (context) => <span className="tabular-nums">{String(context.getValue())}</span> },
              { id: "archived", header: t("Rows kept"), accessorFn: (row) => row.archived, cell: (context) => context.getValue() ? t("No") : t("Yes") },
            ] as ColumnDef<VersionRow, unknown>[]} />}
        </Panel> },
        { id: "health", title: t("Data health"), content: <Panel title={t("Data health")} className="grid min-w-0 content-start gap-2"><DatasetHealth rows={rows} columns={columns} /></Panel> },
      ]} />
    </fieldset></StructuredEditor>
  </div>;
}

export function cell(v: unknown): string {
  if (v === null || v === undefined) return "";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

function columnStats(column: string, rows: Record<string, unknown>[]) {
  const values = rows.map((r) => r[column]).filter((v) => v !== undefined && v !== null && v !== "");
  const count = values.length;
  if (count === 0) return { kind: "empty", count: 0 };
  const allNum = values.every((v) => typeof v === "number");
  if (allNum) {
    const nums = values as number[];
    const min = Math.min(...nums), max = Math.max(...nums);
    const sum = nums.reduce((a, b) => a + b, 0);
    const value: StatisticsValue = { kind: "statistics", count: String(count), min, mean: sum / count, max, sum };
    const requestedBins = 10;
    let buckets: Api.HistogramBucket[] = [];
    const missing = rows.length - count;
    if (min === max) {
      buckets = [{ lower: String(min), upper: String(max), upperInclusive: true, count }];
    } else {
      const step = (max - min) / requestedBins;
      for (let i = 0; i < requestedBins; i++) {
        const lower = min + step * i, upper = min + step * (i + 1);
        const inclusive = i === requestedBins - 1;
        const c = nums.filter((v) => v >= lower && (inclusive ? v <= upper : v < upper)).length;
        buckets.push({ lower: String(lower), upper: String(upper), upperInclusive: inclusive, count: c });
      }
    }
    const histogram: Api.HistogramResult = { field: column, requestedBins, valid: count, missing, minimum: String(min), maximum: String(max), buckets };
    return { kind: "numeric", value, histogram };
  }
  const freq = new Map<string, number>();
  for (const v of values) freq.set(String(v), (freq.get(String(v)) ?? 0) + 1);
  const terms = [...freq.entries()].sort((a, b) => b[1] - a[1]).slice(0, 20).map(([value, count]) => ({ value, count }));
  return { kind: "text", count, terms };
}

type HealthIssue = { column: string; kind: "nulls" | "duplicates" | "outliers"; message: string; count: number };

function DatasetHealth({ rows, columns }: { rows: Record<string, unknown>[]; columns: string[] }) {
  const totalRows = rows.length;
  const issues: HealthIssue[] = [];
  let score = 100;
  if (totalRows === 0) return <p className="text-xs text-muted">{t("No data to assess.")}</p>;
  for (const column of columns) {
    const values = rows.map((r) => r[column]);
    const nulls = values.filter((v) => v === undefined || v === null || v === "").length;
    const nullRate = nulls / totalRows;
    if (nullRate > 0) {
      issues.push({ column, kind: "nulls", message: t("{pct}% empty", { pct: Math.round(nullRate * 100) }), count: nulls });
      score -= nullRate * 30;
    }
    const nonNull = values.filter((v) => v !== undefined && v !== null && v !== "");
    const freq = new Map<string, number>();
    for (const v of nonNull) freq.set(String(v), (freq.get(String(v)) ?? 0) + 1);
    const dupes = nonNull.length - freq.size;
    if (dupes > 0 && dupes / nonNull.length > 0.1) {
      issues.push({ column, kind: "duplicates", message: t("{count} duplicate values", { count: dupes }), count: dupes });
      score -= (dupes / nonNull.length) * 20;
    }
    const nums = nonNull.filter((v) => typeof v === "number");
    if (nums.length > 4) {
      const sorted = [...nums].sort((a, b) => a - b);
      const q1 = sorted[Math.floor(sorted.length * 0.25)];
      const q3 = sorted[Math.floor(sorted.length * 0.75)];
      const iqr = q3 - q1;
      const outliers = nums.filter((v) => v < q1 - 1.5 * iqr || v > q3 + 1.5 * iqr).length;
      if (outliers > 0) {
        issues.push({ column, kind: "outliers", message: t("{count} statistical outliers", { count: outliers }), count: outliers });
        score -= (outliers / nums.length) * 10;
      }
    }
  }
  const finalScore = Math.max(0, Math.min(100, Math.round(score)));
  return (
    <div className="grid gap-3">
      <div className="flex items-center gap-3">
        <div className="text-2xl font-bold">{finalScore}</div>
        <span className="text-xs text-muted">{t("out of 100")}</span>
      </div>
      {issues.length === 0 ? <p className="text-xs text-muted">{t("No data quality issues detected.")}</p> : (
        <DataTable data={issues} getRowId={(_, i) => String(i)} searchable={false} height={Math.min(420, 40 + issues.length * 36)} columns={[
          { id: "column", header: t("Column"), accessorFn: (r) => r.column },
          { id: "kind", header: t("Issue"), accessorFn: (r) => r.kind, cell: (c) => { const k = c.getValue() as "nulls" | "duplicates" | "outliers"; const tone = k === "outliers" ? "neutral" : "warning"; const label = k === "nulls" ? t("Empty values") : k === "duplicates" ? t("Duplicate values") : t("Outliers"); return <Tag tone={tone}>{label}</Tag>; } },
          { id: "message", header: t("Detail"), accessorFn: (r) => r.message },
          { id: "count", header: t("Count"), accessorFn: (r) => r.count },
        ] as ColumnDef<HealthIssue, unknown>[]} />
      )}
    </div>
  );
}

