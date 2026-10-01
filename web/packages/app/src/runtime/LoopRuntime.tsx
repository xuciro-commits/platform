import { useEffect, useRef, useState, type ReactNode } from "react";
import { pageUIManifest, type Api } from "@platform/kernel";
import { Panel, VirtualStack, t, type EntityRecord, type RecordSource } from "@platform/ui";
import { PageSessionStore, type RecordReference, type PageSessionSnapshot } from "./Session";
import { evaluateVariables, type VariableResult } from "./variables";

export const loopItemKey = (owner: string, reference: RecordReference) => JSON.stringify([owner, reference.object, reference.id]);
export type LoopContext = { owner: string; key: string; reference: RecordReference; record: EntityRecord; source: RecordSource;
  values: Record<string, VariableResult>; set: (id: string, value: string | boolean) => void };

export function LoopRuntime({ owner, loop, label, result, session, snapshot, variables, resources, children }: {
  owner: string; loop: Api.PageLoop; label: string; result?: VariableResult; session: PageSessionStore; snapshot: PageSessionSnapshot;
  variables: Record<string, Api.PageVariable>; resources: Record<string, VariableResult>; children: (context: LoopContext) => ReactNode;
}) {
  const currentWindow = result && (result.status === "value" || result.status === "empty") && typeof result.value === "object" && result.value?.kind === "object-set" ? result.value.window : undefined;
  const prior = useRef<{ window: NonNullable<typeof currentWindow>; scope?: string; signature: string } | undefined>(undefined);
  const source = session.readSource(), queryKey = variables[loop.collection]?.source?.section ?? "";
  if (currentWindow) prior.current = { window: currentWindow, scope: source.scope, signature: JSON.stringify([currentWindow.object, currentWindow.query]) };
  const retaining = !!prior.current && !currentWindow && result?.status !== "error" && prior.current?.scope === source.scope && prior.current?.signature === session.querySignature(queryKey);
  const window = currentWindow ?? (retaining ? prior.current?.window : undefined);
  const records = window?.records.slice(0, loop.limit) ?? [], key = JSON.stringify(records), signature = window ? JSON.stringify([window.object, window.query]) : undefined;
  useEffect(() => { if (signature !== undefined) session.reconcileLoop(owner, signature, JSON.parse(key).map((reference: RecordReference) => loopItemKey(owner, reference))); else if (!retaining) session.clearLoop(owner); }, [session, owner, signature, key, retaining]);
  useEffect(() => () => session.clearLoop(owner), [session, owner]);
  if (result?.status === "error") return <Panel role="alert">{t("Loop source could not be read.")}</Panel>;
  if (result?.status === "pending" && !retaining) return <Panel role="status">{t("Loading loop records…")}</Panel>;
  if (!window || !records.length) return <Panel role="status">{t("No records in this query window.")}</Panel>;
  if (new Set(records.map((record) => loopItemKey(owner, record))).size !== records.length || records.some((record) => record.object !== window.object || !record.id)) return <Panel role="alert">{t("Loop record identities are invalid.")}</Panel>;
  return <div className="grid min-w-0 gap-2">
    {retaining && <p role="status" className="text-xs text-muted">{t("Refreshing loop records…")}</p>}
    <p className="text-xs text-muted">{t("Showing {shown} of {window} records in this window; {total} match overall.", { shown: records.length, window: window.records.length, total: window.total })}</p>
    <VirtualStack items={records} label={label} itemKey={(reference) => loopItemKey(owner, reference)} renderItem={(reference) => <LoopItem key={loopItemKey(owner, reference)} reference={reference} owner={owner} loop={loop} signature={signature!} session={session} snapshot={snapshot} variables={variables} resources={resources}>{children}</LoopItem>} />
  </div>;
}

function LoopItem({ reference, owner, loop, signature, session, snapshot, variables, resources, children }: {
  reference: RecordReference; owner: string; loop: Api.PageLoop; signature: string; session: PageSessionStore; snapshot: PageSessionSnapshot;
  variables: Record<string, Api.PageVariable>; resources: Record<string, VariableResult>; children: (context: LoopContext) => ReactNode;
}) {
  const source = session.readSource(), [read, setRead] = useState<{ record?: EntityRecord; error?: boolean }>({});
  useEffect(() => { let current = true; source.get(reference.object, reference.id).then((view) => { if (current) setRead({ record: view.record }); }, () => { if (current) setRead({ error: true }); }); return () => { current = false; }; }, [source, source.scope, source.revision, reference.object, reference.id]);
  if (read.error) return <Panel role="alert" className="min-h-48">{t("This loop record is unavailable.")}</Panel>;
  if (!read.record) return <Panel role="status" className="min-h-48">{t("Loading loop record…")}</Panel>;
  const key = loopItemKey(owner, reference);
  const values = evaluateVariables(variables, { ...snapshot.scalars, ...session.itemValues(owner, signature, key) }, pageUIManifest.runtime,
    { ...resources, [loop.itemVariable]: { status: "value", value: { kind: "record", reference } } }, owner);
  const set = (id: string, value: string | boolean) => {
    const variable = variables[id];
    if (variable?.scope !== "loop-item" || variable.owner !== owner || variable.mode !== "state" || typeof value !== variable.type || typeof value === "string" && new TextEncoder().encode(value).length > pageUIManifest.runtime.maxStringBytes) return;
    session.setItemScalar(owner, key, id, value);
  };
  return <Panel aria-label={reference.id} className="@container grid min-w-0 gap-3">{children({ owner, key, reference, record: read.record, source, values, set })}</Panel>;
}
