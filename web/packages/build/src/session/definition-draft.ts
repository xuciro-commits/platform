import { sourceFieldInput } from "@platform/app";
import { apiErrorMessage } from "@platform/kernel";
import { useActionTrial, useHost } from "@platform/app";
import { t, useInputDrafts, useUnsavedChanges } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { kindOfType, resourceRef } from "../projects/resources";
import { declarationValue, replaceDeclaration } from "../editor/declaration";
import { useDraftRecord } from "../branches/draft-record";

type DefinitionDraft = { id: string; revision: number; name: string };
type Saved = { id: string; revision: number; reloaded?: boolean };

export function useDefinitionDraft<T extends DefinitionDraft>({ id, type, kind, view, empty, normalize, payload, failure, joinApplication = true, onDiscard }: {
  id: string; type: string; kind: string; view: string; empty: () => T;
  normalize?: (record: T) => T; payload: (draft: T) => unknown; failure: string;
  joinApplication?: boolean; onDiscard?: () => void;
}) {
  const host = useHost(), { open } = useApplicationWorkspace();
  const application = useApplicationScope();
  const { query, scope, branch, save: saveToBranch, publishRefusal, mainRevision } = useDraftRecord<T>(type, id);
  const [draft, setDraft] = useState(empty), [edited, setDirty] = useState(false);
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [joining, setJoining] = useState(false);
  const [createID] = useState(() => crypto.randomUUID());
  const trial = useActionTrial();
  const fields = host.source.entity(type)?.fields ?? [];
  const declaration = (value: T) => declarationValue(value, fields);
  const replace = (value: Record<string, unknown>) => {
    if (lock.current) return;
    setDraft(current => replaceDeclaration(current, value, fields));
    setDirty(true); setError("");
  };
  const ownerPayload = (value: T) => {
    const current = declaration(value), result = { ...current, ...(payload(value) as Record<string, unknown>) };
    for (const field of fields) if (!field.readOnly) {
      if (!Object.hasOwn(current, field.name)) delete result[field.name];
      else if (current[field.name] === null) result[field.name] = null;
    }
    for (const key of Object.keys(declaration(confirmed.current))) if (!(key in declaration(value))) result[key] = null;
    return result;
  };
  const structured = { expectedRevision: draft.id ? branch ? mainRevision : draft.revision : 0, fieldInput: sourceFieldInput, value: declaration(draft), onChange: replace, fields, scope: `${type}:${id}:${host.source.scope}`, disabled: busy,
    onTrial: (value: Record<string, unknown>) => trial(type + "." + (draft.id ? "edit" : "create"), { type, id: draft.id || createID }, ownerPayload(replaceDeclaration(draft, value, fields)), draft.id ? branch ? mainRevision : draft.revision : 0) };
  const inputs = useInputDrafts(), changed = edited || !!inputs?.dirty, dirty = changed || joining;
  const loaded = useRef(""), confirmed = useRef(draft), lock = useRef(false), loadedScope = useRef(scope);
  useEffect(() => {
    if (loadedScope.current === scope) return;
    loadedScope.current = scope; loaded.current = ""; confirmed.current = empty();
    setDraft(confirmed.current); setDirty(false); setJoining(false); setError("");
  }, [scope]);
  const load = (record: T) => {
    if (record.revision < confirmed.current.revision) return;
    confirmed.current = { ...empty(), ...(normalize ? normalize(record) : record) };
    setDraft(confirmed.current); loaded.current = record.id + ":" + record.revision;
  };
  const { markSaved, discardChanges, confirmDiscard } = useUnsavedChanges(dirty, () => {
    if (query.data?.record && query.data.record.revision >= confirmed.current.revision) load(query.data.record);
    else setDraft(structuredClone(confirmed.current));
    setDirty(false); setJoining(false); setError(""); onDiscard?.();
  }, busy);
  useEffect(() => {
    const record = query.data?.record;
    if (record && !dirty && !busy && loaded.current !== record.id + ":" + record.revision) load(record);
  }, [query.data, dirty, busy]);
  const patch = (change: Partial<T>) => {
    if (lock.current) return;
    setDraft(current => ({ ...current, ...change })); setDirty(true); setError("");
  };
  const perform = async (action: () => Promise<unknown>) => {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError("");
    try { await action(); } catch (problem) { setError(apiErrorMessage(problem) ?? (problem instanceof Error ? problem.message : failure)); }
    finally { lock.current = false; setBusy(false); }
  };
  const reload = async (message: string, expected?: number) => {
    try {
      const result = await query.refetch(), record = result.data?.record;
      if (result.isSuccess && record && record.revision >= confirmed.current.revision && (expected === undefined || record.revision === expected)) {
        load(record); markSaved(); setDirty(false); return true;
      }
    } catch {   }
    setError(message); return false;
  };
  const showSaved = (target: string) => {
    open({ view, params: { id: target } }, { ifCurrent: { view, params: { id } }, replace: true });
  };
  const save = async (deferNavigation = false): Promise<Saved | undefined> => {
    if (inputs?.invalid) return;
    const submitted = structuredClone(draft), target = submitted.id || createID;
    let revision = confirmed.current.revision;
    if (changed || !submitted.id) {
      if (branch) {
        await saveToBranch(target, ownerPayload(submitted));
        const fresh = (await query.refetch()).data?.record;
        if (fresh) { load(fresh); revision = fresh.revision; } else load({ ...submitted, id: target });
      } else {
        if (!await host.decide(type + "." + (submitted.id ? "edit" : "create"), { type, id: target }, ownerPayload(submitted),
          { expectedRevision: submitted.id ? revision : 0, quiet: true, onRefused: setError })) return;
        revision += 1; load({ ...submitted, id: target, revision });
      }
      inputs?.clear(); markSaved(); setDirty(false);
    }
    if (joinApplication && application && id === "new") {
      setJoining(true);
      await joinProjectResource(host, application, resourceRef(kindOfType(type)!, submitted.name));
      setJoining(false);
    }
    markSaved();
    let reloaded: boolean | undefined;
    if (!deferNavigation) {
      if (id === "new") showSaved(target);
      else reloaded = await reload(t("The resource was saved, but its current record could not be reloaded."), branch ? undefined : revision);
    }
    return { id: target, revision, reloaded };
  };
  const transition = async (name: string, accepted?: Saved) => {
    if (inputs?.invalid) return;
    if (branch) { setError(publishRefusal!); return; }
    const saved = accepted ?? (dirty || id === "new" ? await save(true) : confirmed.current);
    if (!saved?.id || !await host.decide(type + "." + name, { type, id: saved.id }, {},
      { expectedRevision: saved.revision, quiet: true, onRefused: setError })) return;
    load({ ...confirmed.current, revision: saved.revision + 1 }); markSaved();
    if (id === "new") showSaved(saved.id);
    else await reload(t("The resource was installed, but its current record could not be reloaded."), saved.revision + 1);
  };
  const review = async () => {
    if (inputs?.invalid) return;
    if (branch) { if (dirty || id === "new") await save(); return; }
    const saved = dirty || id === "new" ? await save(true) : confirmed.current;
    if (saved?.id) open({ view: "release-review", params: { kind, id: saved.id } }, { ifCurrent: { view, params: { id } }, replace: id === "new" });
  };
  return { draft, structured, createID, query, dirty, joining, busy, error, setError, inputs, patch, perform, save, transition, review, reload, discardChanges, confirmDiscard };
}
