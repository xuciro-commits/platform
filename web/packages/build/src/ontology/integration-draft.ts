import { apiErrorMessage } from "@platform/kernel";
import { useHost, useReadQuery } from "@platform/app";
import { useInputDrafts, useUnsavedChanges, type EntityRecord } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";

type IntegrationDraft = { id: string; revision: number; name: string };

/** Integration editors share one saved record and revision. Their fields and
 * payloads stay in the original editor; submissions use the original host. */
export function useIntegrationDraft<T extends IntegrationDraft>({ id, type, kind, view, empty, normalize, payload, failure }: {
  id: string; type: string; kind: string; view: string; empty: () => T;
  normalize?: (record: T) => T; payload: (draft: T) => unknown; failure: string;
}) {
  const host = useHost(), { open, close } = useApplicationWorkspace();
  const application = useApplicationScope();
  const query = useReadQuery<{ record?: T & EntityRecord }>(`/v1/records/${type}/${encodeURIComponent(id)}`, 5000);
  const [draft, setDraft] = useState(empty), [edited, setDirty] = useState(false);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [createID] = useState(() => crypto.randomUUID());
  const inputs = useInputDrafts(), dirty = edited || !!inputs?.dirty;
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: T) => {
    setDraft({ ...empty(), ...(normalize ? normalize(record) : record) });
    baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`;
  };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    if (query.data?.record) load(query.data.record); else setDraft(empty());
    setDirty(false); setError("");
  }, busy);
  useEffect(() => {
    const record = query.data?.record;
    if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record);
  }, [query.data, dirty, busy]);
  const patch = (change: Partial<T>) => {
    if (lock.current) return;
    setDraft(current => ({ ...current, ...change })); setDirty(true); setError("");
  };
  const perform = async (action: () => Promise<unknown>) => {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError("");
    try { await action(); } catch (problem) { setError(apiErrorMessage(problem) ?? failure); }
    finally { lock.current = false; setBusy(false); }
  };
  const showSaved = (target: string) => {
    open({ view, params: { id: target } }, { ifCurrent: { view, params: { id } } });
    close({ view, params: { id } });
  };
  const save = async (deferNavigation = false) => {
    if (inputs?.invalid) return;
    const target = draft.id || createID;
    let revision = baseRevision.current;
    if (dirty || !draft.id) {
      if (!await host.decide(`${type}.${draft.id ? "edit" : "create"}`, { type, id: target }, payload(draft),
        { expectedRevision: draft.id ? revision : 0, quiet: true, onRefused: setError })) return;
      revision += 1; baseRevision.current = revision; loaded.current = `${target}:${revision}`;
      setDraft(current => ({ ...current, id: target, revision }));
      inputs?.clear(); markSaved(); setDirty(false);
    }
    if (application && id === "new") await joinProjectResource(host, application, { app: "build", kind, name: draft.name });
    markSaved();
    if (!deferNavigation) {
      if (id === "new") showSaved(target); else await query.refetch();
    }
    return { id: target, revision };
  };
  const transition = async (name: string) => {
    if (inputs?.invalid) return;
    const saved = dirty || id === "new" ? await save(true) : { id: draft.id, revision: baseRevision.current };
    if (!saved || !await host.decide(`${type}.${name}`, { type, id: saved.id }, {},
      { expectedRevision: saved.revision, quiet: true, onRefused: setError })) return;
    markSaved();
    if (id === "new") showSaved(saved.id);
    else { const result = await query.refetch(); if (result.data?.record) load(result.data.record); }
  };
  const review = async () => {
    if (inputs?.invalid) return;
    const saved = dirty || id === "new" ? await save() : { id: draft.id, revision: baseRevision.current };
    if (saved) open({ view: "release-review", params: { kind, id: saved.id } }, { ifCurrent: { view, params: { id: saved.id } } });
  };
  return { draft, createID, query, dirty, busy, error, setError, inputs, patch, perform, save, transition, review, discardChanges };
}
