import { ResourceControls } from "../editor/workbench";
import { PropertyList, StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { CreateObjectFromDataset } from "./dataset-object";
import { ResourceList } from "../editor/ResourceList";
import { useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import { MarkingField } from "./marking";
import { Button, DataTable, FilePicker, Input, PageHeader, Panel, Select, Tag, t, type ColumnDef } from "@platform/ui";
import { DatasetLineage } from "./lineage";

type Field = { name: string; type: string };
type Load = { at: string; version: number; rows: number; bytes: number; drift?: string[] };
type Draft = { id: string; revision: number; name: string; title: string; keep?: number; marking?: string; producer?: string; schema?: Field[]; version?: number; last?: Load };
type Version = { id: string; version: number; at: string; rows: number; archived?: boolean; data?: Record<string, unknown>[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "" });
const fieldClass = "grid min-w-0 gap-1 text-xs";
const PREVIEW = 50;

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
      return { name: draft.name, title: draft.title, keep: Number(draft.keep) || 0, marking: draft.marking ?? "" };
    },
  });
  const [creating, setCreating] = useState(false);
  const [shown, setShown] = useState(0);
  const version = shown || draft.version || 0;
  const versionQuery = useReadQuery<{ record?: Version }>(`/v1/records/build.datasetversion/${encodeURIComponent(`${draft.id}@${version}`)}`, 15000, version > 0);
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Datasets")} description={t("Only a builder or integrator can edit datasets.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Datasets")} description={query.isError ? t("The dataset could not be loaded.") : t("Loading…")} />;
  const last = draft.last, latest = draft.version ?? 0, keep = draft.keep || 3;
  const versions = Array.from({ length: Math.min(latest, keep) }, (_, i) => latest - i);
  const rows = versionQuery.data?.record?.data ?? [], columns = draft.schema?.map((f) => f.name) ?? Object.keys(rows[0] ?? {});
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader crumbs={[{ label: t("Datasets"), onClick: () => open({ view: "dataset" }) }]} title={draft.title || t("New dataset")} meta={draft.name ? <span className="font-mono">{draft.name}{draft.version ? ` · v${draft.version}` : ""}</span> : undefined} help={t("Name it, then upload a CSV or point a source or a pipeline at it. Each load is a new version; the schema is inferred from the rows and drift is listed per load.")}
      actions={<>
        {query.data?.record && !dirty && <FilePicker accept=".csv,text/csv" disabled={busy} onFile={file => void perform(async () => {
          const record = query.data!.record!;
          if (await decide("build.dataset.load", { type: "build.dataset", id: record.id }, { csv: await file.text(), producer: `upload:${file.name}` }, { expectedRevision: record.revision, onRefused: setError })) await query.refetch();
        })}>{t("Upload CSV")}</FilePicker>}
        <ResourceControls type="build.dataset" record={draft} dirty={dirty} busy={busy} route={{ view: "dataset", params: { id } }} onCancel={discardChanges} onSave={() => perform(save)}
          operations={holds("build", "builder") && !!draft.schema?.length ? [{ id: "object", label: t("Create object type"), disabled: dirty || !!inputs?.invalid || !latest, run: () => setCreating(true) }] : []} />
      </>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    {creating && draft.schema && <CreateObjectFromDataset dataset={{ id: draft.id, name: draft.name, title: draft.title, schema: draft.schema }} application={application}
      onClose={() => setCreating(false)} onCreated={object => { setCreating(false); open({ view: "object-type", params: { id: object } }, { ifCurrent: { view: "dataset", params: { id } } }); }} />}
    <StructuredEditor {...structured}><fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Dataset")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Dataset name")}<Input disabled={!!draft.producer} value={draft.name} placeholder="sapmaterials" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Dataset title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Versions kept")}<Input draftKey="versions-kept" type="number" min={1} max={50} value={draft.keep ?? ""} placeholder="3" onChange={(e) => patch({ keep: Number(e.target.value) })} />
          <span className="text-xs text-muted">{t("Older versions keep their row count but lose their rows.")}</span></label>
        <MarkingField value={draft.marking ?? ""} onChange={(marking) => patch({ marking })} help={t("Raised by what loads it - the connection, the pipeline's input; lowered only here. Confidential and restricted data only reaches object fields that name their readers; restricted never leaves as CSV.")} />
        {draft.producer && <p className="text-xs text-muted">{t("Loaded by")}: <span className="font-mono">{draft.producer}</span></p>}
      </Panel>
      <Panel title={t("Schema")} className="grid min-w-0 content-start gap-2">
        {!draft.schema?.length ? <p className="text-xs text-muted">{t("Inferred from the first load.")}</p> :
          <ul className="grid gap-1 text-xs">{draft.schema.map((f) => <li key={f.name} className="flex items-center gap-2"><span className="font-mono">{f.name}</span><Tag label={f.type} /></li>)}</ul>}
        {last && <div className="grid gap-3 border-t border-border pt-3"><h3 className="text-xs font-medium">{t("Latest load")}</h3><PropertyList columns={2} items={[[t("Version"), last.version], [t("Loaded at"), new Date(last.at).toLocaleString()], [t("Rows"), last.rows], [t("Size"), `${Math.round(last.bytes / 1024)} KB`]]} />
          {last.drift?.length ? <Tag label={`${t("Drift")}: ${last.drift.join(" ")}`} tone="warning" /> : null}</div>}
      </Panel>
      {draft.id && <div className="lg:col-span-2"><DatasetLineage dataset={draft.id} /></div>}
      <Panel title={t("Rows")} className="grid min-w-0 content-start gap-2 lg:col-span-2"
        actions={versions.length > 1 ? <Select value={String(version)} onChange={(e) => setShown(Number(e.target.value))}>{versions.map((v) => <option key={v} value={v}>v{v}</option>)}</Select> : undefined}>
        {!latest ? <p className="text-xs text-muted">{t("No rows yet. Upload a CSV, or publish a source or pipeline that writes this dataset.")}</p> :
          versionQuery.data?.record?.archived ? <p className="text-xs text-muted">{t("This version is no longer kept.")}</p> :
          <div className="overflow-auto">
            <DataTable data={rows.slice(0, PREVIEW)} getRowId={(row) => String(rows.indexOf(row))} searchable={false} height={Math.min(420, 40 + rows.length * 28)} columns={columns.map((name): ColumnDef<Record<string, unknown>, unknown> => ({ id: name, header: name, accessorFn: (row) => row[name], cell: (context) => <span className="font-mono">{cell(context.getValue())}</span> }))} />
            {rows.length > PREVIEW && <p className="px-2 py-1 text-xs text-muted">{t("First {n} of {rows} rows", { n: PREVIEW, rows: rows.length })}</p>}
          </div>}
      </Panel>
    </fieldset></StructuredEditor>
  </div>;
}

export function cell(v: unknown): string {
  if (v === null || v === undefined) return "";
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}
