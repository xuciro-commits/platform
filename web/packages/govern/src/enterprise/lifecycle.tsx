import { useHost, useOpenRecord, useReadQuery } from "@platform/app";
import { Button, Dialog, Form, Input, Notice, Select, t, useUnsavedChanges, useWorkspace } from "@platform/ui";
import { useState } from "react";
import { ELEMENT, PLACEMENT, RELATIONSHIP, allowedRelationships, live, sourceRelationship, type Element, type Metamodel, type Model } from "./model";

export type EnterpriseUsage = { canDelete: boolean; canClose: boolean; hasRestrictedUsage: boolean; items: { kind: string; title: string; ref?: string; field?: string; current: boolean }[] };
export function Usage({ element, until }: { element: string; until?: string }) {
  const query = useReadQuery<EnterpriseUsage>(`/v1/enterprise-usage?element=${encodeURIComponent(element)}${until ? `&until=${encodeURIComponent(until)}` : ""}`);
  const openRecord = useOpenRecord();
  return <div className="grid gap-2 text-xs">
    <h3 className="font-semibold">{t("Business usage and impact")}</h3>
    {query.isLoading && <p role="status">{t("Loading…")}</p>}
    {query.isError && <div><Notice tone="warning" message={t("Usage could not be loaded. Try again before changing this element.")} /><Button onClick={() => void query.refetch()}>{t("Try again")}</Button></div>}
    {query.data && <UsageContent usage={query.data} openRecord={ref => openRecord(ref.replace(/^record:/, ""))} />}
  </div>;
}
function UsageContent({ usage, openRecord }: { usage: EnterpriseUsage; openRecord?: (ref: string) => void }) {
  const { open } = useWorkspace();
  const label = (item: EnterpriseUsage["items"][number]) => item.title || t(({ relationship: "Relationship", view: "View", property: "Property", grant: "Role grant", "tenant-link": "Tenant link", tenant: "Tenant", record: "Record" } as Record<string, string>)[item.kind] ?? item.kind);
  const target = (item: EnterpriseUsage["items"][number]) => {
    if (!item.ref) return;
    if (item.kind === "record" && item.ref.startsWith("record:")) openRecord?.(item.ref);
    else if ((item.kind === "grant" || item.kind === "member") && item.ref.startsWith("member:")) open({ view: "member", params: { id: item.ref.slice(7) } });
    else if (item.kind === "grant" && item.ref.startsWith("team:")) open({ view: "permissions" });
    else if (item.kind === "property") open({ view: "enterprise", params: { element: item.ref, section: "organization" } });
    else if (item.kind === "tenant") open({ view: "organisation" });
  };
  return <>
    {usage.hasRestrictedUsage && <p role="note" className="text-muted">{t("Additional restricted usage exists. Its details are not available with your access.")}</p>}
    <ul className="grid gap-1">{usage.items.map((item, index) => <li key={`${item.kind}:${item.ref ?? index}:${item.field ?? ""}`} className="flex flex-wrap items-center gap-2">
      {openRecord && item.ref && ["record", "grant", "member", "property", "tenant"].includes(item.kind) ? <Button variant="ghost" size="sm" onClick={() => target(item)}>{label(item)}</Button> : <span>{label(item)}</span>}
      <span className="text-muted">{item.field} · {t(item.current ? "Current usage" : "Historical usage")}</span>
    </li>)}</ul>
    {!usage.items.length && !usage.hasRestrictedUsage && <p className="text-muted">{t("No usage is recorded for this element.")}</p>}
  </>;
}
export type LifecycleOperation = "close" | "reopen" | "reinstate" | "delete";
const operationTitle = { close: "Close element", reopen: "Reopen element", reinstate: "Reinstate element", delete: "Delete unused element" };
export function LifecycleDialog({ operation, element, day, onClose }: { operation: LifecycleOperation; element: Element; day: string; onClose: () => void }) {
  const host = useHost();
  const [date, setDate] = useState(day), [reason, setReason] = useState(""), [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useUnsavedChanges(!!reason || date !== day, () => { setReason(""); setDate(day); }, busy);
  const needsDate = operation === "close" || operation === "reopen";
  const usage = useReadQuery<EnterpriseUsage>(`/v1/enterprise-usage?element=${encodeURIComponent(element.id)}&until=${encodeURIComponent(date)}`, undefined, operation === "close" || operation === "delete");
  const allowed = operation === "close" ? usage.data?.canClose : operation === "delete" ? usage.data?.canDelete : true;
  return <Dialog open onOpenChange={open => !open && !busy && onClose()} title={`${t(operationTitle[operation])}: ${element.name}`}>
    <Form className="grid gap-3" onSubmit={async () => {
      setError("");
      if (!allowed || !host.can(`enterprise.element.${operation}`)) return;
      const payload = { reason, ...(operation === "close" ? { until: date } : operation === "reopen" ? { from: date } : {}) };
      setBusy(true);
      try { if (await host.decide(`enterprise.element.${operation}`, { type: ELEMENT, id: element.id }, payload, { onRefused: setError })) onClose(); }
      finally { setBusy(false); }
    }}>
      <p className="text-sm text-muted">{t(operation === "close" ? "End the current validity period. Existing history stays readable." : operation === "reopen" ? "Start a new validity period. The inactive interval remains in history." : operation === "reinstate" ? "Undo an accidental closure. This corrects the last validity period." : "Delete only an unused local element. Referenced elements must be closed instead.")}</p>
      {needsDate && <label className="grid gap-1 text-xs">{t("Effective date")}<Input type="date" value={date} onChange={event => setDate(event.target.value)} required /></label>}
      <label className="grid gap-1 text-xs">{t("Reason")}<Input value={reason} onChange={event => setReason(event.target.value)} required /></label>
      {(operation === "close" || operation === "delete") && <div key={date}>{usage.isLoading ? <p role="status">{t("Loading…")}</p> : usage.isError ? <div><p role="alert">{t("Usage could not be loaded. Try again before changing this element.")}</p><Button onClick={() => void usage.refetch()}>{t("Try again")}</Button></div> : usage.data && <UsageContent usage={usage.data} />}{usage.data && !allowed && <p role="alert" className="text-danger">{t(operation === "delete" ? "This element is in use and cannot be deleted." : "Current usage prevents this element from closing on the selected date.")}</p>}</div>}
      {error && <p role="alert" className="text-danger text-xs">{error}</p>}
      <div className="flex justify-end gap-2"><Button variant="ghost" disabled={busy} onClick={onClose}>{t("Cancel")}</Button><Button type="submit" variant={operation === "delete" || operation === "close" ? "danger" : "primary"} disabled={busy || !allowed || !reason.trim() || needsDate && !date}>{t(operationTitle[operation])}</Button></div>
    </Form>
  </Dialog>;
}

export function MoveElementDialog({ element, model, meta, day, kind, onClose }: { element: Element; model: Model; meta: Metamodel; day: string; kind: string; onClose: () => void }) {
  const host = useHost();
  const placement = model.relationships.find(relation => relation.stereotype === PLACEMENT && relation.source === element.id && relation.kind === kind && live(relation, day));
  const action = placement ? "enterprise.relationship.change" : "enterprise.relationship.add";
  const [target, setTarget] = useState(placement?.target ?? ""), [date, setDate] = useState(day), [error, setError] = useState("");
  const [replacement] = useState(() => crypto.randomUUID());
  const [busy, setBusy] = useState(false);
  return <Dialog open onOpenChange={open => !open && !busy && onClose()} title={`${t("Move to")}: ${element.name}`}><Form className="grid gap-3" onSubmit={async () => {
    setError("");
    const payload = { stereotype: PLACEMENT, kind, source: element.id, target, relation: placement?.relation || "part of", from: date, ...(placement ? { replacement, share: placement.share } : {}) };
    setBusy(true);
    try { if (await host.decide(action, { type: RELATIONSHIP, id: placement?.id ?? replacement }, payload, { onRefused: setError })) onClose(); }
    finally { setBusy(false); }
  }}>
    <p className="text-xs text-muted">{t("Change the model relationship. Positions on a drawing do not change the parent.")}</p>
    <label className="grid gap-1 text-xs">{t("New parent")}<Select value={target} onChange={event => setTarget(event.target.value)} required><option value="">{t("Choose an organisation")}</option>{model.elements.filter(candidate => candidate.id !== element.id && live(candidate, date) && allowedRelationships(meta, element.stereotype, candidate.stereotype).includes(PLACEMENT)).map(candidate => <option key={candidate.id} value={candidate.id}>{candidate.name}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Effective date")}<Input type="date" value={date} onChange={event => setDate(event.target.value)} required /></label>
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
    <div className="flex justify-end gap-2"><Button variant="ghost" disabled={busy} onClick={onClose}>{t("Cancel")}</Button><Button type="submit" disabled={busy || !host.can(action) || !!placement && sourceRelationship(placement) || !target || target === placement?.target || !date}>{t("Move")}</Button></div>
  </Form></Dialog>;
}
