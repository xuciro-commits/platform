import { sourceFieldInput } from "@platform/app";
import { apiErrorMessage } from "@platform/kernel";
import { useActionTrial, useHost, useReadQuery } from "@platform/app";
import { t, useInputDrafts, useUnsavedChanges, type EntityRecord } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { kindOfType, resourceRef } from "../projects/resources";
import { declarationValue, replaceDeclaration } from "../editor/declaration";

type DefinitionDraft = { id: string; revision: number; name: string };
type Saved = { id: string; revision: number; reloaded?: boolean };

/** One saved declaration and revision; fields and publication rules stay in their editors. */
export function useDefinitionDraft<T extends DefinitionDraft>({ id, type, kind, view, empty, normalize, payload, failure, joinApplication = true, onDiscard }: {
  id: string; type: string; kind: string; view: string; empty: () => T;
  normalize?: (record: T) => T; payload: (draft: T) => unknown; failure: string;
  joinApplication?: boolean; onDiscard?: () => void;
}) {
  const host = useHost(), { open } = useApplicationWorkspace();
  const application = useApplicationScope();
  const query = useReadQuery<{ record?: T & EntityRecord }>("/v1/records/" + type + "/" + encodeURIComponent(id), 5000);
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
  const structured = { expectedRevision: draft.id ? draft.revision : 0, fieldInput: sourceFieldInput, value: declaration(draft), onChange: replace, fields, scope: `${type}:${id}:${host.source.scope}`, disabled: busy,
    onTrial: (value: Record<string, unknown>) => trial(type + "." + (draft.id ? "edit" : "create"), { type, id: draft.id || createID }, ownerPayload(replaceDeclaration(draft, value, fields)), draft.id ? draft.revision : 0) };
  const inputs = useInputDrafts(), changed = edited || !!inputs?.dirty, dirty = changed || joining;
  const loaded = useRef(""), confirmed = useRef(draft), lock = useRef(false);
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
    } catch { /* The accepted revision stays saved if its read fails. */ }
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
      if (!await host.decide(type + "." + (submitted.id ? "edit" : "create"), { type, id: target }, ownerPayload(submitted),
        { expectedRevision: submitted.id ? revision : 0, quiet: true, onRefused: setError })) return;
      revision += 1; load({ ...submitted, id: target, revision });
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
      else reloaded = await reload(t("The resource was saved, but its current record could not be reloaded."), revision);
    }
    return { id: target, revision, reloaded };
  };
  const transition = async (name: string, accepted?: Saved) => {
    if (inputs?.invalid) return;
    const saved = accepted ?? (dirty || id === "new" ? await save(true) : confirmed.current);
    if (!saved?.id || !await host.decide(type + "." + name, { type, id: saved.id }, {},
      { expectedRevision: saved.revision, quiet: true, onRefused: setError })) return;
    load({ ...confirmed.current, revision: saved.revision + 1 }); markSaved();
    if (id === "new") showSaved(saved.id);
    else await reload(t("The resource was installed, but its current record could not be reloaded."), saved.revision + 1);
  };
  const review = async () => {
    if (inputs?.invalid) return;
    const saved = dirty || id === "new" ? await save(true) : confirmed.current;
    if (saved?.id) open({ view: "release-review", params: { kind, id: saved.id } }, { ifCurrent: { view, params: { id } }, replace: id === "new" });
  };
  return { draft, structured, createID, query, dirty, joining, busy, error, setError, inputs, patch, perform, save, transition, review, reload, discardChanges, confirmDiscard };
}
