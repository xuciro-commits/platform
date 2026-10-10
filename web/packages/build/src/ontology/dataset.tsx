import {useInputDrafts} from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useEffect, useRef, useState } from "react";
import { RecordActions, useHost, useReadQuery } from "@platform/app";
import { MarkingField, integrates } from "./marking";
import { Button, DataTable, Input, PageHeader, Panel, Select, Tag, t, useUnsavedChanges, type ColumnDef, type EntityRecord } from "@platform/ui";
import { DatasetLineage } from "./lineage";

// Datasets (ADR-0071): rows as they came from a source or a pipeline, kept as
// versions with an inferred schema. A builder names one; sources and pipelines
// load it; this page shows the schema, the versions kept and the rows of one.
type Field = { name: string; type: string };
type Load = { at: string; version: number; rows: number; bytes: number; drift?: string[] };
type Draft = { id: string; revision: number; name: string; title: string; keep?: number; marking?: string; producer?: string; schema?: Field[]; version?: number; last?: Load };
type Version = { id: string; version: number; at: string; rows: number; archived?: boolean; data?: Record<string, unknown>[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "" });
const fieldClass = "grid min-w-0 gap-1 text-xs";
const PREVIEW = 50;

export function Datasets() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Datasets")} description={t("Only a builder or integrator can edit datasets.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Datasets")} description={t("Rows as they came, in versions. A source loads a dataset instead of mapping straight to an object; a pipeline reads one and writes another or an object.")}
      actions={<Button onClick={() => open({ view: "dataset", params: { id: "new" } })}>{t("New dataset")}</Button>} />
    <ResourceList source={source} type="build.dataset" fields={["title", "name", "producer", "version", "marking"]} onOpen={(record) => open({ view: "dataset", params: { id: record.id } })} />
  </div>;
}

export function DatasetEditor({ id }: { id: string }) {
  const { decide, role } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: Draft & EntityRecord }>(`/v1/records/build.dataset/${encodeURIComponent(id)}`, 5000);
  const [draft, setDraft] = useState<Draft>(empty), [edited, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
 const [createID]=useState(()=>crypto.randomUUID()),[objectID]=useState(()=>crypto.randomUUID());
 const inputs=useInputDrafts(),dirty=edited||!!inputs?.dirty;
  const [shown, setShown] = useState(0);
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Draft) => { setDraft({ ...empty(), ...record }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); });
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const version = shown || draft.version || 0;
  const versionQuery = useReadQuery<{ record?: Version }>(`/v1/records/build.datasetversion/${encodeURIComponent(`${draft.id}@${version}`)}`, 15000, version > 0);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch { setError(t("The dataset could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async () => {
    const target = draft.id || createID, expected = baseRevision.current;
    const payload = { name: draft.name, title: draft.title, keep: Number(draft.keep) || 0, marking: draft.marking ?? "" };
    if (!await decide(`build.dataset.${draft.id ? "edit" : "create"}`, { type: "build.dataset", id: target }, payload, { expectedRevision: draft.id ? expected : 0, quiet: true, onRefused: setError })) return;
    baseRevision.current = expected + 1; loaded.current = `${target}:${expected + 1}`;
    setDraft((d) => ({ ...d, id: target, revision: expected + 1 })); markSaved(); setDirty(false);
    if (!draft.id) { open({ view: "dataset", params: { id: target } }); close({ view: "dataset", params: { id } }); }
    else await query.refetch();
  };
  // Builder Assist without a model (ADR-0077): the dataset's inferred schema
  // becomes an object draft - one property per column, typed - that the
  // builder then names, shapes and publishes as any other.
  const draftObject = async () => {
    const name = (draft.name || "imported").replace(/[^a-z0-9]/g, "").slice(0, 24) || "imported";
    const typeOf: Record<string, string> = { string: "text", number: "decimal", boolean: "boolean", date: "date", json: "longtext" };
    const seen = new Set<string>();
    const fields = (draft.schema ?? []).map((f) => {
      let n = f.name.toLowerCase().replace(/[^a-z0-9]/g, "") || "field";
      while (seen.has(n)) n += "2";
      seen.add(n);
      return { name: n, title: f.name, type: typeOf[f.type] ?? "text" };
    });
    const target = objectID;
    const payload = { name, title: draft.title || name, description: t("Drafted from the dataset {name}", { name: draft.name }), fields,
      states: [{ name: "active", title: t("Active"), tone: "success" }], actions: [] };
    if (await decide("build.object.create", { type: "build.object", id: target }, payload, { expectedRevision: 0, quiet: true, onRefused: setError })) open({ view: "object-type", params: { id: target } });
  };
  if (!integrates(role("build"))) return <PageHeader title={t("Datasets")} description={t("Only a builder or integrator can edit datasets.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Datasets")} description={query.isError ? t("The dataset could not be loaded.") : t("Loading…")} />;
  const last = draft.last, latest = draft.version ?? 0, keep = draft.keep || 3;
  const versions = Array.from({ length: Math.min(latest, keep) }, (_, i) => latest - i);
  const rows = versionQuery.data?.record?.data ?? [], columns = draft.schema?.map((f) => f.name) ?? Object.keys(rows[0] ?? {});
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New dataset")} description={t("Name it, then point a source or a pipeline at it. Each load is a new version; the schema is inferred from the rows and drift is listed per load.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "dataset" })}>{t("Datasets")}</Button>
        {role("build") === "builder" && !!draft.schema?.length && <Button disabled={busy || dirty || !!error} onClick={() => void perform(draftObject)}>{t("Draft an object from this schema")}</Button>}
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button variant="primary" disabled={busy || !dirty && !!draft.id} onClick={() => void perform(save)}>{t("Save dataset")}</Button>
        {query.data?.record && !dirty && !busy && <RecordActions type="build.dataset" record={query.data.record} allowed={["build.dataset.load"]} />}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Dataset")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Dataset name")}<Input disabled={!!draft.producer} value={draft.name} placeholder="sapmaterials" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Dataset title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Versions kept")}<Input draftKey="versions-kept" type="number" min={1} max={50} value={draft.keep ?? ""} placeholder="3" onChange={(e) => patch({ keep: Number(e.target.value) })} />
          <span className="text-[11px] text-muted">{t("Older versions keep their row count but lose their rows.")}</span></label>
        <MarkingField value={draft.marking ?? ""} onChange={(marking) => patch({ marking })} help={t("Raised by what loads it - the connection, the pipeline's input; lowered only here. Confidential and restricted data only reaches object fields that name their readers; restricted never leaves as CSV.")} />
        {draft.producer && <p className="text-xs text-muted">{t("Loaded by")}: <span className="font-mono">{draft.producer}</span></p>}
      </Panel>
      <Panel title={t("Schema")} className="grid min-w-0 content-start gap-2">
        {!draft.schema?.length ? <p className="text-xs text-muted">{t("Inferred from the first load.")}</p> :
          <ul className="grid gap-1 text-xs">{draft.schema.map((f) => <li key={f.name} className="flex items-center gap-2"><span className="font-mono">{f.name}</span><Tag label={f.type} /></li>)}</ul>}
        {last && <p className="flex flex-wrap items-center gap-2 text-xs"><span>{t("Latest load")}: v{last.version} · {new Date(last.at).toLocaleString()} · {t("{rows} rows", { rows: last.rows })} · {Math.round(last.bytes / 1024)} KB</span>
          {last.drift?.length ? <Tag label={`${t("Drift")}: ${last.drift.join(" ")}`} tone="warning" /> : null}</p>}
      </Panel>
      {draft.id && <div className="lg:col-span-2"><DatasetLineage dataset={draft.id} /></div>}
      <Panel title={t("Rows")} className="grid min-w-0 content-start gap-2 lg:col-span-2"
        actions={versions.length > 1 ? <Select value={String(version)} onChange={(e) => setShown(Number(e.target.value))}>{versions.map((v) => <option key={v} value={v}>v{v}</option>)}</Select> : undefined}>
        {!latest ? <p className="text-xs text-muted">{t("No rows yet. Publish a source that feeds this dataset, or a pipeline that writes it.")}</p> :
          versionQuery.data?.record?.archived ? <p className="text-xs text-muted">{t("This version is no longer kept.")}</p> :
          <div className="overflow-auto">
            <DataTable data={rows.slice(0, PREVIEW)} getRowId={(row) => String(rows.indexOf(row))} searchable={false} height={Math.min(420, 40 + rows.length * 28)} columns={columns.map((name): ColumnDef<Record<string, unknown>, unknown> => ({ id: name, header: name, accessorFn: (row) => row[name], cell: (context) => <span className="font-mono">{cell(context.getValue())}</span> }))} />
            {rows.length > PREVIEW && <p className="px-2 py-1 text-[11px] text-muted">{t("First {n} of {rows} rows", { n: PREVIEW, rows: rows.length })}</p>}
          </div>}
      </Panel>
    </fieldset>
  </div>;
}

export function cell(v: unknown): string {
  if (v === null || v === undefined) return "";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}
