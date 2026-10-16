import {useInputDrafts} from "@platform/ui";
import { integrates } from "./marking";
import { ResourceList } from "../editor/ResourceList";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { useAuthoringModel } from "./authoring-model";
import { useEffect, useRef, useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Input, PageHeader, Panel, Select, Tag, t, useUnsavedChanges } from "@platform/ui";

// Writebacks retain the connection and request mapping. Explicit business
// confirmations and device dispatches decide when to send and whether to retry.
type Field = { from: string; to: string; convert?: string };
type Answer = { at: string; effect: string; target: string; result: string; detail?: string; answer?: string };
type Draft = { id: string; revision: number; name: string; title: string; connection: string; object: string; on: string; method?: string; path?: string; mapping: Field[]; result: Field[]; state: string; sent?: number; rejected?: number; failed?: number; answers?: Answer[] };
type ConnectionRow = { id: string; title: string; kind: string; state: string };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", connection: "", object: "", on: "create", method: "POST", path: "", mapping: [], result: [], state: "draft" });
const conversions = ["", "string", "number", "boolean", "date"];
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function Writebacks() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Writebacks")} description={t("Only a builder or integrator can edit writebacks.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Writebacks")} description={t("Declare the connection and request mapping for external writes. Accepted actions, business confirmations and device dispatches keep their own sending and retry rules.")}
      actions={<Button onClick={() => open({ view: "writeback", params: { id: "new" } })}>{t("New writeback")}</Button>} />
    <ResourceList source={source} type="build.writeback" fields={["title", "object", "on", "connection", "sent", "failed", "state"]} onOpen={(record) => open({ view: "writeback", params: { id: record.id } })} />
  </div>;
}

export function WritebackEditor({ id }: { id: string }) {
  const host = useHost(), { decide, role } = host, { open, close } = useApplicationWorkspace();
  const application = useApplicationScope(), model = useAuthoringModel();
  const entities = role("build") === "builder" ? model.entities : host.entities;
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.writeback/${encodeURIComponent(id)}`, 5000);
  const connectionQuery = useReadQuery<{ records: ConnectionRow[] }>("/v1/records/build.connection?limit=100");
  const connections = (connectionQuery.data?.records ?? []).filter((c) => c.state !== "retired" && c.kind !== "postgres");
  const effects = useReadQuery<Api.IntegrationEffect[]>("/v1/integration-effects", 5000).data ?? [];
  const [draft, setDraft] = useState<Draft>(empty), [edited, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
 const [createID]=useState(()=>crypto.randomUUID());
 const inputs=useInputDrafts(),dirty=edited||!!inputs?.dirty;
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Draft) => { setDraft({ ...empty(), ...record, mapping: record.mapping ?? [], result: record.result ?? [] }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); }, busy);
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch (problem) { setError(problem instanceof Error ? problem.message : t("The writeback could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async (deferNavigation = false): Promise<{ id: string; revision: number } | undefined> => {
    if (inputs?.invalid) return;
    const target = draft.id || createID;
    const { name, title, connection, object, on, method, path, mapping, result } = draft;
    let revision = baseRevision.current;
    if (dirty || !draft.id) {
      const payload = { name, title, connection, object, on, method: method ?? "POST", path: path ?? "", mapping, result };
      if (!await decide(`build.writeback.${draft.id ? "edit" : "create"}`, { type: "build.writeback", id: target }, payload, { expectedRevision: draft.id ? revision : 0, quiet: true, onRefused: setError })) return;
      revision += 1; baseRevision.current = revision; loaded.current = `${target}:${revision}`;
      setDraft((d) => ({ ...d, id: target, revision })); inputs?.clear(); markSaved(); setDirty(false);
    }
    if (application && id === "new") await joinProjectResource(host, application, { app: "build", kind: "writeback", name });
    markSaved();
    if (!deferNavigation) {
      if (id === "new") { open({ view: "writeback", params: { id: target } }, { ifCurrent: { view: "writeback", params: { id } } }); close({ view: "writeback", params: { id } }); }
      else await query.refetch();
    }
    return { id: target, revision };
  };
  const transition = async (name: "publish" | "pause") => {
    const saved = dirty || id === "new" ? await save(true) : { id: draft.id, revision: baseRevision.current };
    if (!saved) return;
    if (!await decide(`build.writeback.${name}`, { type: "build.writeback", id: saved.id }, {}, { expectedRevision: saved.revision, quiet: true, onRefused: setError })) return;
    markSaved();
    if (id === "new") { open({ view: "writeback", params: { id: saved.id } }, { ifCurrent: { view: "writeback", params: { id } } }); close({ view: "writeback", params: { id } }); }
    else { const result = await query.refetch(); if (result.data?.record) load(result.data.record); }
  };
  const review = async () => {
    const saved = dirty || id === "new" ? await save() : { id: draft.id, revision: baseRevision.current };
    if (saved) open({ view: "release-review", params: { kind: "writeback", id: saved.id } }, { ifCurrent: { view: "writeback", params: { id: saved.id } } });
  };
  if (!integrates(role("build"))) return <PageHeader title={t("Writebacks")} description={t("Only a builder or integrator can edit writebacks.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Writebacks")} description={query.isError ? t("The writeback could not be loaded.") : t("Loading…")} />;
  const target = entities.find((entity) => entity.type === draft.object);
  const fields = target?.fields ?? [], writable = fields.filter((f) => !f.readOnly);
  const connection = connections.find((item) => item.id === draft.connection);
  const verbs = ["confirmation", "dispatch", "transaction", "create", "edit", ...(target?.lifecycle?.transitions ?? []).map((a) => a.name)];
  const queue = effects.filter((x) => x.event === `writeback/${draft.name}` && (x.state === "pending" || x.state === "retrying" || x.state === "held"));
  const rows = (key: "mapping" | "result", list: Field[], fromOptions: string[] | undefined, toOptions: string[] | undefined, fromLabel: string, toLabel: string) => <div className="grid gap-2">
    <div className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2 text-[11px] font-medium text-muted"><span>{fromLabel}</span><span>{toLabel}</span><span>{t("Convert")}</span><span /></div>
    {list.map((m, i) => <div key={i} className="grid grid-cols-[1fr_1fr_auto_auto] items-center gap-2">
      {fromOptions ? <Select aria-label={fromLabel} value={m.from} onChange={(e) => patch({ [key]: list.map((x, j) => j === i ? { ...x, from: e.target.value } : x) })}><option value="">{t("Choose a field")}</option>{fromOptions.map((f) => <option key={f} value={f}>{f}</option>)}</Select> : <Input aria-label={fromLabel} value={m.from} placeholder="d.MaterialDocument" onChange={(e) => patch({ [key]: list.map((x, j) => j === i ? { ...x, from: e.target.value } : x) })} />}
      {toOptions ? <Select aria-label={toLabel} value={m.to} onChange={(e) => patch({ [key]: list.map((x, j) => j === i ? { ...x, to: e.target.value } : x) })}><option value="">{t("Choose a field")}</option>{toOptions.map((f) => <option key={f} value={f}>{f}</option>)}</Select> : <Input aria-label={toLabel} value={m.to} placeholder="Material" onChange={(e) => patch({ [key]: list.map((x, j) => j === i ? { ...x, to: e.target.value } : x) })} />}
      <Select aria-label={t("Convert")} value={m.convert ?? ""} onChange={(e) => patch({ [key]: list.map((x, j) => j === i ? { ...x, convert: e.target.value } : x) })}>{conversions.map((c) => <option key={c} value={c}>{c ? t(c) : t("as is")}</option>)}</Select>
      <Button size="sm" variant="ghost" onClick={() => patch({ [key]: list.filter((_, j) => j !== i) })}>{t("Remove")}</Button>
    </div>)}
    <div><Button size="sm" disabled={!draft.object} onClick={() => patch({ [key]: [...list, { from: "", to: "", convert: "" }] })}>{key === "mapping" ? t("Add body field") : t("Add answer field")}</Button></div>
  </div>;
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New writeback")} description={t("Choose the original object, trigger, connection and request fields. Explicit confirmations and dispatches send only when their declared business operation runs.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "writeback" })}>{t("Writebacks")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !!inputs?.invalid || !dirty && id !== "new"} onClick={() => void perform(save)}>{t("Save writeback")}</Button>
        {role("build") === "builder" && <Button disabled={busy || !!inputs?.invalid} onClick={() => void perform(review)}>{t("Review and publish…")}</Button>}
        <Button variant="primary" disabled={busy || !!inputs?.invalid} onClick={() => void perform(() => transition("publish"))}>{t("Publish")}</Button>
        {draft.state === "published" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("pause"))}>{t("Pause")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    {connectionQuery.isError && <Panel role="alert" className="text-danger">{t("The connections could not be loaded.")}</Panel>}
    {role("build") === "builder" && model.drafts.isError && <Panel role="alert" className="text-danger">{t("The saved object definitions could not be loaded.")}</Panel>}
    {connection && connection.state !== "ready" && <Panel role="status" className="text-xs text-warning">{t("This saved connection is not ready. Check the connection before publishing or running this integration.")}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Trigger")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Writeback name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="grtosap" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Writeback title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Object")}<Select value={draft.object} onChange={(e) => patch({ object: e.target.value, on: "create", mapping: [], result: [] })}><option value="">{t("Choose an object type")}</option>{entities.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select></label>
        <label className={fieldClass}>{t("After action")}<Select value={draft.on} onChange={(e) => patch({ on: e.target.value })}>{verbs.map((v) => <option key={v} value={v}>{v==="confirmation"?t("Only explicit business confirmations"):v==="dispatch"?t("Only declared device dispatches"):v==="transaction"?t("Only explicit transaction effects"):v}</option>)}</Select>
          <span className="text-[11px] text-muted">{t("The decision's payload is what gets sent; create and edit carry the fields, a declared action carries its own payload.")}</span></label>
      </Panel>
      <Panel title={t("Request")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Connection")}<Select value={draft.connection} onChange={(e) => patch({ connection: e.target.value })}><option value="">{t("Choose a connection")}</option>{connections.map((c) => <option key={c.id} value={c.id}>{c.title} · {c.kind}</option>)}</Select>
          <span className="text-[11px] text-muted">{t("Saved http or OData connections can be configured here; publishing still requires a ready connection. Credentials stay with the connection.")}</span></label>
        <div className="grid grid-cols-[auto_1fr] gap-2">
          <label className={fieldClass}>{t("Method")}<Select value={draft.method ?? "POST"} onChange={(e) => patch({ method: e.target.value })}>{["POST", "PUT", "PATCH"].map((m) => <option key={m} value={m}>{m}</option>)}</Select></label>
          <label className={fieldClass}>{t("Path")}<Input value={draft.path ?? ""} placeholder="A_MaterialDocumentHeader" onChange={(e) => patch({ path: e.target.value })} /><span className="text-[11px] text-muted">{t("Relative to the connection's address; {id} and {field} are filled from the payload.")}</span></label>
        </div>
      </Panel>
      <Panel title={t("Body")} className="grid min-w-0 content-start gap-2">
        <p className="text-[11px] text-muted">{t("Payload field → key in the request body. Empty sends the whole payload with its id.")}</p>
        {rows("mapping", draft.mapping, ["id", ...fields.map((f) => f.name)], undefined, t("Payload field"), t("Body key"))}
      </Panel>
      <Panel title={t("Answer")} className="grid min-w-0 content-start gap-2">
        <p className="text-[11px] text-muted">{t("Key in the answer (dotted; OData's d is unwrapped) → field written onto the record once delivered.")}</p>
        {rows("result", draft.result, undefined, writable.map((f) => f.name), t("Answer key"), t("Record field"))}
      </Panel>
      <Panel title={t("Deliveries")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        <p className="flex flex-wrap items-center gap-2 text-xs">
          <Tag label={t("{n} delivered", { n: draft.sent ?? 0 })} tone="success" /><Tag label={t("{n} rejected", { n: draft.rejected ?? 0 })} tone={draft.rejected ? "danger" : undefined} /><Tag label={t("{n} failed", { n: draft.failed ?? 0 })} tone={draft.failed ? "danger" : undefined} />
          {queue.length > 0 && <Tag label={t("{n} queued", { n: queue.length })} tone="warning" />}{queue[0]?.error && <span className="text-warning">{t(queue[0].error)}</span>}
        </p>
        {queue.length > 0 && <p className="text-[11px] text-muted">{t("Queued decisions go in order once the system answers; Settings → Integrations shows every attempt.")}</p>}
        {draft.answers?.length ? <ul className="grid gap-1 text-xs">{draft.answers.map((a, i) => <li key={i} className="flex flex-wrap items-center gap-2 font-mono break-all">
          <Tag label={a.result} tone={a.result === "delivered" ? "success" : "danger"} /><span>{new Date(a.at).toLocaleString()}</span><span>{a.target}</span>{a.detail && <span className="text-danger">{a.detail}</span>}{a.answer && <span className="text-muted">{a.answer.slice(0, 160)}</span>}
        </li>)}</ul> : <p className="text-xs text-muted">{t("Nothing sent yet.")}</p>}
      </Panel>
    </fieldset>
  </div>;
}
