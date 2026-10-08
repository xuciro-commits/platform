import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useEffect, useRef, useState } from "react";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { MarkingField, integrates } from "./marking";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, t, useUnsavedChanges } from "@platform/ui";

// Connections (ADR-0070): the external systems a tenant reads through - a kind,
// an address and the name of a secret the host holds. Check reaches the system
// once from the host; sources then read through a ready connection.
type Draft = Api.Connection;
const empty = (): Draft => ({ id: "", revision: 0, created: { at: "" } as Api.Stamp, changed: { at: "" } as Api.Stamp, name: "", title: "", kind: "http", address: "", state: "draft" });
const KINDS = [["http", "HTTP / REST / files"], ["odata", "OData (SAP Gateway)"], ["postgres", "PostgreSQL (read-only)"]] as const;
const placeholders: Record<string, string> = { http: "https://erp.example.com/api/", odata: "https://sap.example.com/sap/opu/odata/sap/API_PRODUCT_SRV/", postgres: "postgres://reader@mes-db.example.com:5432/mes?sslmode=require" };
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function Connections() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (!integrates(role("build"))) return <PageHeader title={t("Connections")} description={t("Only a builder or integrator can edit connections.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Connections")} description={t("The systems this tenant reads from. A connection names its credential in the host's secret store; the credential itself never enters a definition or a release.")}
      actions={<Button onClick={() => open({ view: "connection", params: { id: "new" } })}>{t("New connection")}</Button>} />
    <ResourceList source={source} type="build.connection" fields={["title", "name", "kind", "address", "state"]} onOpen={(record) => open({ view: "connection", params: { id: record.id } })} />
  </div>;
}

export function ConnectionEditor({ id }: { id: string }) {
  const { decide, role } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.connection/${encodeURIComponent(id)}`, 5000);
  const [draft, setDraft] = useState<Draft>(empty), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Draft) => { setDraft({ ...empty(), ...record }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); });
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch { setError(t("The connection could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async (): Promise<{ id: string; revision: number } | undefined> => {
    const target = draft.id || crypto.randomUUID(), expected = baseRevision.current;
    const { name, title, kind, address, secret, allowPrivate, marking } = draft;
    const payload = { name, title, kind, address, secret: secret ?? "", allowPrivate: !!allowPrivate, marking: marking ?? "" };
    if (!await decide(`build.connection.${draft.id ? "edit" : "create"}`, { type: "build.connection", id: target }, payload, { expectedRevision: draft.id ? expected : 0, quiet: true, onRefused: setError })) return;
    baseRevision.current = expected + 1; loaded.current = `${target}:${expected + 1}`;
    setDraft((d) => ({ ...d, id: target, revision: expected + 1 })); markSaved(); setDirty(false);
    if (!draft.id) { open({ view: "connection", params: { id: target } }); close({ view: "connection", params: { id } }); }
    else await query.refetch();
    return { id: target, revision: expected + 1 };
  };
  const transition = async (name: "check" | "retire") => {
    const saved = dirty ? await save() : { id: draft.id, revision: baseRevision.current };
    if (!saved) return;
    if (await decide(`build.connection.${name}`, { type: "build.connection", id: saved.id }, {}, { expectedRevision: saved.revision, quiet: true, onRefused: setError })) { const result = await query.refetch(); if (result.data?.record) load(result.data.record); }
  };
  if (!integrates(role("build"))) return <PageHeader title={t("Connections")} description={t("Only a builder or integrator can edit connections.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Connections")} description={query.isError ? t("The connection could not be loaded.") : t("Loading…")} />;
  const last = draft.last;
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New connection")} description={t("Kind → address → secret name. Check reaches the system from the host within seconds; a ready connection can carry data sources.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "connection" })}>{t("Connections")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !dirty && !!draft.id} onClick={() => void perform(save)}>{t("Save connection")}</Button>
        <Button variant="primary" disabled={busy || !!draft.requested} onClick={() => void perform(() => transition("check"))}>{draft.requested ? t("Checking…") : t("Check")}</Button>
        {draft.state === "ready" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("retire"))}>{t("Retire")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("System")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Connection name")}<Input disabled={draft.state === "ready"} value={draft.name} placeholder="sap" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Connection title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Kind")}<Select value={draft.kind} onChange={(e) => patch({ kind: e.target.value as Api.Connection["kind"] })}>{KINDS.map(([k, label]) => <option key={k} value={k}>{t(label)}</option>)}</Select></label>
        <label className={fieldClass}>{t("Address")}<Input value={draft.address} placeholder={placeholders[draft.kind]} onChange={(e) => patch({ address: e.target.value })} />
          <span className="text-[11px] text-muted">{draft.kind === "postgres" ? t("A postgres:// URL without the password; the secret is the password.") : t("The service root; sources give a path or an entity set beneath it.")}</span></label>
        <label className={fieldClass}>{t("Secret name")}<Input value={draft.secret ?? ""} placeholder="sap-reader" onChange={(e) => patch({ secret: e.target.value })} />
          <span className="text-[11px] text-muted">{t("Looked up by the host (PLATFORM_SECRETS_DIR or PLATFORM_SECRET_<NAME>). For http and OData its content is the Authorization header's value, such as Basic … or Bearer ….")}</span></label>
        <Checkbox checked={!!draft.allowPrivate} onChange={(allowPrivate) => patch({ allowPrivate })}>{t("Allow http and private addresses (on-premise systems)")}</Checkbox>
        <MarkingField value={draft.marking ?? ""} onChange={(marking) => patch({ marking: marking as Api.Connection["marking"] })} help={t("Everything read through it is at least this; datasets loaded from it are raised to it.")} />
      </Panel>
      <Panel title={t("Last check")} className="grid min-w-0 content-start gap-2">
        {!last ? <p className="text-xs text-muted">{t("Not checked yet.")}</p> : <>
          <p className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.ok ? t("Reachable") : t("Failed")} tone={last.ok ? "success" : "danger"} /><span>{new Date(last.at).toLocaleString()}</span>{last.detail && <span className="font-mono">{last.detail}</span>}</p>
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
        </>}
      </Panel>
    </fieldset>
  </div>;
}
