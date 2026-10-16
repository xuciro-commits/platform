import {useInputDrafts} from "@platform/ui";
import { integrates } from "./marking";
import { ResourceList } from "../editor/ResourceList";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { useAuthoringModel } from "./authoring-model";
import { useEffect, useRef, useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Input, PageHeader, Panel, Select, t, useUnsavedChanges } from "@platform/ui";

// Matching rules (ADR-0074): when rows from different systems are one party,
// material or site, and whose value wins per field. Applied wherever rows land
// in the object - a source's pull, a pipeline's run - so one real thing stays
// one record.
type Key = { fields: string[]; normalize?: string };
type Preference = { field: string; producer: string };
type Draft = { id: string; revision: number; name: string; title: string; object: string; keys: Key[]; prefer: Preference[]; state: string };
type Producer = { name: string; title: string };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", object: "", keys: [], prefer: [], state: "draft" });
const NORMALIZE = ["exact", "folded", "digits"];
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function Matches() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Matching rules")} description={t("Only a builder or integrator can edit matching rules.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Matching rules")} description={t("Say when rows from different systems are the same party, material or site, and whose value wins per field. One published rule per object; it applies to every source and pipeline writing it.")}
      actions={<Button onClick={() => open({ view: "match", params: { id: "new" } })}>{t("New matching rule")}</Button>} />
    <ResourceList source={source} type="build.match" fields={["title", "object", "state"]} onOpen={(record) => open({ view: "match", params: { id: record.id } })} />
  </div>;
}

export function MatchEditor({ id }: { id: string }) {
  const host = useHost(), { decide, role } = host, { open, close } = useApplicationWorkspace();
  const application = useApplicationScope(), model = useAuthoringModel();
  const entities = role("build") === "builder" ? model.entities : host.entities;
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.match/${encodeURIComponent(id)}`, 5000);
  const pipelineQuery = useReadQuery<{ records: Producer[] }>("/v1/records/build.pipeline?limit=200");
  const sourceQuery = useReadQuery<{ records: Producer[] }>("/v1/records/build.source?limit=200");
  const pipelines = pipelineQuery.data?.records ?? [], sources = sourceQuery.data?.records ?? [];
  const [draft, setDraft] = useState<Draft>(empty), [edited, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
 const [createID]=useState(()=>crypto.randomUUID());
 const inputs=useInputDrafts(),dirty=edited||!!inputs?.dirty;
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Draft) => { setDraft({ ...empty(), ...record, keys: record.keys ?? [], prefer: record.prefer ?? [] }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); }, busy);
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch (problem) { setError(problem instanceof Error ? problem.message : t("The matching rule could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async (deferNavigation = false): Promise<{ id: string; revision: number } | undefined> => {
    if (inputs?.invalid) return;
    const target = draft.id || createID;
    const { name, title, object, keys, prefer } = draft;
    let revision = baseRevision.current;
    if (dirty || !draft.id) {
      const payload = { name, title, object, keys: keys.map((k) => ({ fields: k.fields.filter(Boolean), normalize: k.normalize ?? "" })), prefer };
      if (!await decide(`build.match.${draft.id ? "edit" : "create"}`, { type: "build.match", id: target }, payload, { expectedRevision: draft.id ? revision : 0, quiet: true, onRefused: setError })) return;
      revision += 1; baseRevision.current = revision; loaded.current = `${target}:${revision}`;
      setDraft((d) => ({ ...d, id: target, revision })); inputs?.clear(); markSaved(); setDirty(false);
    }
    if (application && id === "new") await joinProjectResource(host, application, { app: "build", kind: "matching", name });
    markSaved();
    if (!deferNavigation) {
      if (id === "new") { open({ view: "match", params: { id: target } }, { ifCurrent: { view: "match", params: { id } } }); close({ view: "match", params: { id } }); }
      else await query.refetch();
    }
    return { id: target, revision };
  };
  const transition = async (name: "publish" | "pause") => {
    const saved = dirty || id === "new" ? await save(true) : { id: draft.id, revision: baseRevision.current };
    if (!saved) return;
    if (!await decide(`build.match.${name}`, { type: "build.match", id: saved.id }, {}, { expectedRevision: saved.revision, quiet: true, onRefused: setError })) return;
    markSaved();
    if (id === "new") { open({ view: "match", params: { id: saved.id } }, { ifCurrent: { view: "match", params: { id } } }); close({ view: "match", params: { id } }); }
    else { const result = await query.refetch(); if (result.data?.record) load(result.data.record); }
  };
  const review = async () => {
    const saved = dirty || id === "new" ? await save() : { id: draft.id, revision: baseRevision.current };
    if (saved) open({ view: "release-review", params: { kind: "matching", id: saved.id } }, { ifCurrent: { view: "match", params: { id: saved.id } } });
  };
  if (!integrates(role("build"))) return <PageHeader title={t("Matching rules")} description={t("Only a builder or integrator can edit matching rules.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Matching rules")} description={query.isError ? t("The matching rule could not be loaded.") : t("Loading…")} />;
  const target = entities.find((entity) => entity.type === draft.object);
  const fields = target?.fields ?? [], writable = fields.filter((f) => !f.readOnly);
  const producers = [...pipelines, ...sources];
  const setKey = (i: number, change: Partial<Key>) => patch({ keys: draft.keys.map((k, j) => j === i ? { ...k, ...change } : k) });
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New matching rule")} description={t("Object → keys that mean the same thing → whose value wins per field. Publish applies it to every row landing in the object from then on.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "match" })}>{t("Matching rules")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !!inputs?.invalid || !dirty && id !== "new"} onClick={() => void perform(save)}>{t("Save matching rule")}</Button>
        {role("build") === "builder" && <Button disabled={busy || !!inputs?.invalid} onClick={() => void perform(review)}>{t("Review and publish…")}</Button>}
        <Button variant="primary" disabled={busy || !!inputs?.invalid} onClick={() => void perform(() => transition("publish"))}>{t("Publish")}</Button>
        {draft.state === "published" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("pause"))}>{t("Pause")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    {(pipelineQuery.isError || sourceQuery.isError) && <Panel role="alert" className="text-danger">{t("The pipelines or data sources could not be loaded.")}</Panel>}
    {role("build") === "builder" && model.drafts.isError && <Panel role="alert" className="text-danger">{t("The saved object definitions could not be loaded.")}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Object")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Rule name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="samematerial" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Rule title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Object")}<Select value={draft.object} onChange={(e) => patch({ object: e.target.value, keys: [], prefer: [] })}><option value="">{t("Choose an object type")}</option>{entities.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select></label>
      </Panel>
      <Panel title={t("Keys")} className="grid min-w-0 content-start gap-2">
        <p className="text-[11px] text-muted">{t("A row that agrees with an existing record on any key is that record: it becomes an edit instead of a second record. Folded ignores case and spacing; digits keeps only the digits without leading zeros, so 000123, 123 and M-123 agree.")}</p>
        {draft.keys.map((k, i) => <div key={i} className="grid grid-cols-[1fr_auto_auto] items-center gap-2">
          <Input aria-label={t("Fields")} value={k.fields.join(", ")} placeholder="partno" onChange={(e) => setKey(i, { fields: e.target.value.split(",").map((f) => f.trim()) })} list="match-fields" />
          <Select value={k.normalize ?? ""} onChange={(e) => setKey(i, { normalize: e.target.value })}>{NORMALIZE.map((n) => <option key={n} value={n === "exact" ? "" : n}>{t(n)}</option>)}</Select>
          <Button size="sm" variant="ghost" onClick={() => patch({ keys: draft.keys.filter((_, j) => j !== i) })}>{t("Remove")}</Button>
        </div>)}
        <datalist id="match-fields">{fields.map((f) => <option key={f.name} value={f.name} />)}</datalist>
        <div><Button size="sm" disabled={!draft.object} onClick={() => patch({ keys: [...draft.keys, { fields: [], normalize: "" }] })}>{t("Add key")}</Button></div>
        {fields.length > 0 && <span className="text-[11px] text-muted">{t("Fields")}: <span className="font-mono">{fields.map((f) => f.name).join(", ")}</span></span>}
      </Panel>
      <Panel title={t("Preferred producers")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        <p className="text-[11px] text-muted">{t("For a field, the pipeline or data source whose value wins. Others fill the field only while it is empty - SAP keeps the description, MES may add the weight.")}</p>
        {draft.prefer.map((p, i) => <div key={i} className="grid grid-cols-[1fr_1fr_auto] items-center gap-2">
          <Select value={p.field} onChange={(e) => patch({ prefer: draft.prefer.map((x, j) => j === i ? { ...x, field: e.target.value } : x) })}><option value="">{t("Choose a field")}</option>{writable.map((f) => <option key={f.name} value={f.name}>{f.name}</option>)}</Select>
          <Select value={p.producer} onChange={(e) => patch({ prefer: draft.prefer.map((x, j) => j === i ? { ...x, producer: e.target.value } : x) })}><option value="">{t("Choose a pipeline or data source")}</option>{producers.map((x) => <option key={x.name} value={x.name}>{x.title} · {x.name}</option>)}</Select>
          <Button size="sm" variant="ghost" onClick={() => patch({ prefer: draft.prefer.filter((_, j) => j !== i) })}>{t("Remove")}</Button>
        </div>)}
        <div><Button size="sm" disabled={!draft.object} onClick={() => patch({ prefer: [...draft.prefer, { field: "", producer: "" }] })}>{t("Add preference")}</Button></div>
      </Panel>
    </fieldset>
  </div>;
}
