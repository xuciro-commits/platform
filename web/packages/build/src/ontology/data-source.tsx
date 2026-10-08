import { integrates } from "./marking";
import { ResourceList } from "../editor/ResourceList";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useEffect, useRef, useState } from "react";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, t, useUnsavedChanges } from "@platform/ui";
import { PERIODS, periodLabel } from "../automate/workflow-model";

// Data sources (ADR-0061): an external JSON endpoint whose rows become records
// of one object. The editor is the whole wizard - endpoint, rows, mapping,
// period - and the last pull shows what happened.
type Draft = Omit<Api.Source, "mapping" | "object" | "key"> & { dataset?: string; object: string; key: string; mapping: Api.SourceField[] };
type ConnectionRow = Api.Connection;
type DatasetRow = { id: string; title: string };
const PROFILES = [["json", "JSON"], ["csv", "CSV"], ["odata", "OData entity set"], ["table", "Database table"]] as const;
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", url: "", object: "", key: "id", mapping: [], state: "draft" });
const conversions = ["", "string", "number", "boolean", "date"];
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function DataSources() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Data sources")} description={t("Only a builder or integrator can edit data sources.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Data sources")} description={t("Pull rows from a JSON or CSV endpoint, an OData entity set or a database table into one object, on a period or on request. Each row is the object's own create or edit, decided once per content.")}
      actions={<Button onClick={() => open({ view: "data-source", params: { id: "new" } })}>{t("New data source")}</Button>} />
    <ResourceList source={source} type="build.source" fields={["title", "name", "object", "dataset", "every", "state"]} onOpen={(record) => open({ view: "data-source", params: { id: record.id } })} />
  </div>;
}

export function DataSourceEditor({ id }: { id: string }) {
  const { decide, role, entities } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.source/${encodeURIComponent(id)}`, 5000);
  const connections = (useReadQuery<{ records: ConnectionRow[] }>("/v1/records/build.connection?limit=100").data?.records ?? []).filter((c) => c.state === "ready");
  const datasets = useReadQuery<{ records: DatasetRow[] }>("/v1/records/build.dataset?limit=200").data?.records ?? [];
  const [draft, setDraft] = useState<Draft>(empty), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Draft) => { setDraft({ ...empty(), ...record, mapping: record.mapping ?? [] }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); });
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch { setError(t("The data source could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async (): Promise<{ id: string; revision: number } | undefined> => {
    const target = draft.id || crypto.randomUUID(), expected = baseRevision.current;
    const { name, title, url, allowPrivate, header, path, object, key, mapping, every, connection, profile, entity, filter, since, dataset } = draft;
    const payload = { name, title, url: url ?? "", allowPrivate: !!allowPrivate, header: header ?? "", path: path ?? "", object: dataset ? "" : object, key: dataset ? "" : key, mapping: dataset ? [] : mapping, every: every ?? "", dataset: dataset ?? "",
      connection: connection ?? "", profile: profile ?? "json", entity: entity ?? "", filter: filter ?? "", since: since ?? "" };
    if (!await decide(`build.source.${draft.id ? "edit" : "create"}`, { type: "build.source", id: target }, payload, { expectedRevision: draft.id ? expected : 0, quiet: true, onRefused: setError })) return;
    baseRevision.current = expected + 1; loaded.current = `${target}:${expected + 1}`;
    setDraft((d) => ({ ...d, id: target, revision: expected + 1 })); markSaved(); setDirty(false);
    if (!draft.id) { open({ view: "data-source", params: { id: target } }); close({ view: "data-source", params: { id } }); }
    else await query.refetch();
    return { id: target, revision: expected + 1 };
  };
  const transition = async (name: "publish" | "pull" | "pause" | "reset") => {
    const saved = dirty ? await save() : { id: draft.id, revision: baseRevision.current };
    if (!saved) return;
    if (await decide(`build.source.${name}`, { type: "build.source", id: saved.id }, {}, { expectedRevision: saved.revision, quiet: true, onRefused: setError })) { const result = await query.refetch(); if (result.data?.record) load(result.data.record); }
  };
  const target = entities.find((entity) => entity.type === draft.object);
  const writable = (target?.fields ?? []).filter((field) => !field.readOnly);
  const setMapping = (mapping: Api.SourceField[]) => patch({ mapping });
  if (!integrates(role("build"))) return <PageHeader title={t("Data sources")} description={t("Only a builder or integrator can edit data sources.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Data sources")} description={query.isError ? t("The data source could not be loaded.") : t("Loading…")} />;
  const last = draft.last;
  const profile = draft.profile || "json", conn = connections.find((c) => c.id === draft.connection);
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New data source")} description={t("Endpoint → rows → mapping → period. Publish lets the host pull it; Pull now asks for one pull within seconds.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "data-source" })}>{t("Data sources")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !dirty && !!draft.id} onClick={() => void perform(save)}>{t("Save data source")}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void perform(() => transition("publish"))}>{t("Publish")}</Button>
        {draft.state === "published" && <Button disabled={busy || dirty || !!draft.requested} onClick={() => void perform(() => transition("pull"))}>{draft.requested ? t("Pull requested…") : t("Pull now")}</Button>}
        {draft.state === "published" && !!draft.since && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("reset"))}>{t("Reset cursor")}</Button>}
        {draft.state === "published" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("pause"))}>{t("Pause")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Endpoint")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Source name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="erpitems" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Source title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Connection")}<Select value={draft.connection ?? ""} onChange={(e) => { const c = connections.find((x) => x.id === e.target.value); patch({ connection: e.target.value, profile: c?.kind === "odata" ? "odata" : c?.kind === "postgres" ? "table" : profile === "odata" || profile === "table" ? "json" : profile }); }}>
          <option value="">{t("— none: a bare URL")}</option>{connections.map((c) => <option key={c.id} value={c.id}>{c.title} · {c.kind}</option>)}</Select>
          <span className="text-[11px] text-muted">{t("Ready connections only; the credential comes from the connection.")}</span></label>
        <label className={fieldClass}>{t("Profile")}<Select value={profile} onChange={(e) => patch({ profile: e.target.value as Api.Source["profile"] })}>{PROFILES.filter(([p]) => conn ? (conn.kind === "odata" ? p === "odata" : conn.kind === "postgres" ? p === "table" : p === "json" || p === "csv") : p === "json" || p === "csv").map(([p, label]) => <option key={p} value={p}>{t(label)}</option>)}</Select></label>
        {(profile === "json" || profile === "csv") && <label className={fieldClass}>{t("URL")}<Input value={draft.url ?? ""} placeholder={conn ? "items.csv" : "https://erp.example.com/api/items"} onChange={(e) => patch({ url: e.target.value })} />{conn && <span className="text-[11px] text-muted">{t("Relative to the connection's address, or absolute.")}</span>}</label>}
        {(profile === "odata" || profile === "table") && <label className={fieldClass}>{profile === "odata" ? t("Entity set") : t("Table")}<Input value={draft.entity ?? ""} placeholder={profile === "odata" ? "A_Product" : "mes.confirmations"} onChange={(e) => patch({ entity: e.target.value })} /></label>}
        {(profile === "odata" || profile === "table") && <label className={fieldClass}>{t("Filter")}<Input value={draft.filter ?? ""} placeholder={profile === "odata" ? "Plant eq '1000'" : "plant = '1000' and active = true"} onChange={(e) => patch({ filter: e.target.value })} /></label>}
        {(profile === "odata" || profile === "table") && <label className={fieldClass}>{t("Incremental column")}<Input value={draft.since ?? ""} placeholder={profile === "odata" ? "LastChangeDateTime" : "changed_at"} onChange={(e) => patch({ since: e.target.value })} />
          <span className="text-[11px] text-muted">{t("A timestamp or sequence; each pull reads rows past the cursor.")}{draft.cursor ? ` ${t("Cursor")}: ${draft.cursor}` : ""}</span></label>}
        {!conn && <label className={fieldClass}>{t("Request header")}<Input value={draft.header ?? ""} placeholder="Authorization: Bearer …" onChange={(e) => patch({ header: e.target.value })} /></label>}
        {!conn && <Checkbox checked={!!draft.allowPrivate} onChange={(allowPrivate) => patch({ allowPrivate })}>{t("Allow http and private addresses (on-premise systems)")}</Checkbox>}
        {profile === "json" && <label className={fieldClass}>{t("Rows at")}<Input value={draft.path ?? ""} placeholder="data.items" onChange={(e) => patch({ path: e.target.value })} /><span className="text-[11px] text-muted">{t("Dotted path to the array inside the answer; empty when the answer is the array.")}</span></label>}
        <label className={fieldClass}>{t("Pull every")}<Select value={draft.every ?? ""} onChange={(e) => patch({ every: e.target.value })}><option value="">{t("Only when asked")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>
      </Panel>
      <Panel title={t("Target and mapping")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Rows go to")}<Select value={draft.dataset ? "dataset" : "object"} onChange={(e) => e.target.value === "dataset" ? patch({ dataset: datasets[0]?.id ?? "", object: "", mapping: [] }) : patch({ dataset: "" })}>
          <option value="object">{t("An object, mapped here")}</option><option value="dataset">{t("A dataset, as they came")}</option></Select></label>
        {draft.dataset ? <label className={fieldClass}>{t("Target dataset")}<Select value={draft.dataset} onChange={(e) => patch({ dataset: e.target.value })}>{datasets.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select>
          <span className="text-[11px] text-muted">{t("Each pull becomes a new version; a pipeline maps the rows to an object later.")}</span></label> : <>
        <label className={fieldClass}>{t("Target object")}<Select value={draft.object} onChange={(e) => patch({ object: e.target.value, mapping: [] })}><option value="">{t("Choose an object type")}</option>{entities.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select></label>
        <label className={fieldClass}>{t("Row id field")}<Input value={draft.key} placeholder="id" onChange={(e) => patch({ key: e.target.value })} /><span className="text-[11px] text-muted">{t("The row field whose value becomes the record id; the same id edits the existing record.")}</span></label>
        <div className="grid gap-2">
          <div className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2 text-[11px] font-medium text-muted"><span>{t("Row field")}</span><span>{t("Object field")}</span><span>{t("Convert")}</span><span /></div>
          {draft.mapping.map((m, i) => <div key={i} className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2">
            <Input value={m.from} placeholder="sku" onChange={(e) => setMapping(draft.mapping.map((x, j) => j === i ? { ...x, from: e.target.value } : x))} />
            <Select value={m.to} onChange={(e) => setMapping(draft.mapping.map((x, j) => j === i ? { ...x, to: e.target.value } : x))}><option value="">{t("Choose a field")}</option>{writable.map((field) => <option key={field.name} value={field.name}>{field.title}</option>)}</Select>
            <Select value={m.convert ?? ""} onChange={(e) => setMapping(draft.mapping.map((x, j) => j === i ? { ...x, convert: e.target.value } : x))}>{conversions.map((c) => <option key={c} value={c}>{c ? t(c) : t("as is")}</option>)}</Select>
            <Button size="sm" variant="ghost" onClick={() => setMapping(draft.mapping.filter((_, j) => j !== i))}>{t("Remove")}</Button>
          </div>)}
          <div><Button size="sm" disabled={!draft.object} onClick={() => setMapping([...draft.mapping, { from: "", to: writable.find((f) => !draft.mapping.some((m) => m.to === f.name))?.name ?? "", convert: "" }])}>{t("Add mapping")}</Button></div>
        </div></>}
      </Panel>
      <Panel title={t("Last pull")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        {!last ? <p className="text-xs text-muted">{t("Not pulled yet.")}</p> : <>
          <p className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.error ? t("Failed") : last.failed ? t("Partly applied") : t("Applied")} tone={last.error ? "danger" : last.failed ? "warning" : "success"} />
            <span>{new Date(last.at).toLocaleString()}</span><span>{t("{rows} rows · {applied} applied · {failed} failed", { rows: last.rows, applied: last.applied, failed: last.failed })}</span></p>
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
          {last.failures?.length ? <ul className="grid gap-1 text-xs">{last.failures.map((f, i) => <li key={i} className="font-mono break-all">{f.id || "—"} · {f.outcome}</li>)}</ul> : null}
        </>}
      </Panel>
    </fieldset>
  </div>;
}
