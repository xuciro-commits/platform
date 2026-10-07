// Alert rules (ADR-0077): when a record of an object comes to match a
// condition, a role is told - once per record or every time. The operations
// face's "situation": nobody has to keep a page open to see it.
import { useApplicationWorkspace } from "../projects/application-scope";
import { installedObjects, type WorkflowObject } from "../automate/workflow-model";
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, RecordList, Select, Textarea, t, useUnsavedChanges } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { operators } from "../ontology/object-model";

type Draft = { id: string; revision: number; name: string; title: string; object: string; field: string; operator: string; value?: string; message: string; role?: string; every?: boolean; active: boolean };
type Source = WorkflowObject & { fields?: { name: string; title: string; type: string }[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", object: "", field: "", operator: "=", message: "", active: true });
const fieldClass = "grid gap-1 text-xs";

export function Alerts() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (role("build") !== "builder") return <PageHeader title={t("Alert rules")} description={t("Only a builder can edit alert rules.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Alert rules")} description={t("Tell a role when a record comes to match a condition: a late order, a stock below its minimum, a receipt without a supplier.")}
      actions={<Button onClick={() => open({ view: "alert", params: { id: "new" } })}>{t("New alert rule")}</Button>} />
    <RecordList source={source} type="build.alertrule" fields={["title", "object", "field", "operator", "value", "role", "active"]} onOpen={(record) => open({ view: "alert", params: { id: record.id } })} />
  </div>;
}

export function AlertEditor({ id }: { id: string }) {
  const { decide, role } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.alertrule/${encodeURIComponent(id)}`);
  const inventory = useRecordInventory<Source>("build.object");
  const objects = installedObjects(inventory.data?.records ?? []);
  const [draft, setDraft] = useState<Draft>(empty), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const loaded = useRef("");
  const load = (record: Draft) => { setDraft({ ...empty(), ...record }); loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); });
  useEffect(() => { const r = query.data?.record; if (r && !dirty && !busy && loaded.current !== `${r.id}:${r.revision}`) load(r); }, [query.data, dirty, busy]);
  const change = (patch: Partial<Draft>) => { setDraft((d) => ({ ...d, ...patch })); setDirty(true); setError(""); };
  const object = objects.find((o) => `build.${o.name}` === draft.object);
  const fields = object?.fields ?? [];
  const roles = [...new Set(["builder", ...objects.flatMap((o) => (o.access ?? []).map((a) => a.role))])];
  const noValue = draft.operator === "empty" || draft.operator === "not empty";
  const save = async () => {
    setBusy(true); setError("");
    try {
      const target = draft.id || crypto.randomUUID();
      const { name, title, object, field, operator, value, message, role, every, active } = draft;
      if (!await decide(`build.alertrule.${draft.id ? "edit" : "create"}`, { type: "build.alertrule", id: target }, { name, title, object, field, operator, value: noValue ? "" : value ?? "", message, role: role ?? "", every: !!every, active },
        { expectedRevision: draft.id ? draft.revision : 0, quiet: true, onRefused: setError })) return;
      markSaved(); setDirty(false);
      if (!draft.id) { open({ view: "alert", params: { id: target } }); close({ view: "alert", params: { id } }); }
      else { const r = await query.refetch(); if (r.data?.record) load(r.data.record); }
    } catch { setError(t("The alert rule could not be saved. Your draft is still here.")); } finally { setBusy(false); }
  };
  if (role("build") !== "builder") return <PageHeader title={t("Alert rules")} description={t("Only a builder can edit alert rules.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Alert rules")} description={query.isError ? t("The alert rule could not be loaded.") : t("Loading…")} />;
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={draft.title || t("New alert rule")} description={t("Checked after every change to a record of the object. People holding the role get a notification that opens the record.")}
      actions={<div className="flex flex-wrap gap-2">
        <Button variant="ghost" onClick={() => open({ view: "alert" })}>{t("Alert rules")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button variant="primary" disabled={busy || (!dirty && !!draft.id)} onClick={() => void save()}>{t("Save alert rule")}</Button>
      </div>} />
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 gap-3 lg:grid-cols-2">
      <Panel title={t("Rule")} className="grid content-start gap-3">
        <label className={fieldClass}>{t("Rule name")}<Input value={draft.name} placeholder="lateorder" onChange={(e) => change({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Rule title")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Object")}<Select value={draft.object} onChange={(e) => change({ object: e.target.value, field: "" })}>
          <option value="">{t("Choose an object")}</option>{objects.map((o) => <option key={o.name} value={`build.${o.name}`}>{o.title}</option>)}</Select></label>
        <div className="grid grid-cols-3 gap-2">
          <label className={fieldClass}>{t("Field")}<Select value={draft.field} onChange={(e) => change({ field: e.target.value })}>
            <option value="">{t("Choose a field")}</option>{fields.map((f) => <option key={f.name} value={f.name}>{f.title || f.name}</option>)}{object?.states && <option value="state">{t("State")}</option>}</Select></label>
          <label className={fieldClass}>{t("Operator")}<Select value={draft.operator} onChange={(e) => change({ operator: e.target.value })}>{operators.map((op) => <option key={op} value={op}>{t(op)}</option>)}</Select></label>
          <label className={fieldClass}>{t("Value")}<Input disabled={noValue} value={draft.value ?? ""} onChange={(e) => change({ value: e.target.value })} /></label>
        </div>
      </Panel>
      <Panel title={t("Who is told")} className="grid content-start gap-3">
        <label className={fieldClass}>{t("Message")}<Textarea rows={3} value={draft.message} placeholder={t("What the people told should know or do.")} onChange={(e) => change({ message: e.target.value })} /></label>
        <label className={fieldClass}>{t("Tell")}<Select value={draft.role ?? ""} onChange={(e) => change({ role: e.target.value || undefined })}>
          <option value="">{t("The builder")}</option>{roles.filter((r) => r !== "builder").map((r) => <option key={r} value={r}>{r}</option>)}</Select></label>
        <Checkbox checked={!!draft.every} onChange={(every) => change({ every })}>{t("Every time it matches, not once per record")}</Checkbox>
        <Checkbox checked={draft.active} onChange={(active) => change({ active })}>{t("Active")}</Checkbox>
      </Panel>
    </fieldset>
  </div>;
}
