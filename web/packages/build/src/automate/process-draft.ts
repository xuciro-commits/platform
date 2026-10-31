import { useHost } from "@platform/app";
import { apiErrorMessage } from "@platform/kernel";
import { t, useUnsavedChanges } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { useDraftRecord } from "../branches/draft-record";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useDraftSession } from "../session/DraftSession";
import type { DraftEdit } from "../session/draft-history";
import type { WorkflowDraft } from "./workflow-model";
import { workflowInputs } from "./workflow-session";

const emptyValue = (value: unknown) => value == null || value === "" || value === false || Array.isArray(value) && value.length === 0
  || typeof value === "object" && !Array.isArray(value) && Object.keys(value as object).length === 0;

const branchSpec = (draft: WorkflowDraft) => Object.fromEntries(Object.entries(workflowInputs(draft)).map(([name, value]) =>
  [name, ["name", "title", "steps"].includes(name) || !emptyValue(value) ? value : null]));

export function useProcessDraft({ id, view, empty, failure, onDiscard }: {
  id: string; view: "flow" | "automation"; empty: () => WorkflowDraft; failure: string;
  onDiscard?: () => void;
}) {
  const { decide } = useHost(), { open } = useApplicationWorkspace();
  const { query, scope, branch, save: saveToBranch } = useDraftRecord<WorkflowDraft>("build.process", id);
  const session = useDraftSession<WorkflowDraft>(empty());
  const { draft, dirty } = session;
  const [createID] = useState(() => crypto.randomUUID());
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const confirmed = useRef(draft), loaded = useRef(""), lock = useRef(false), allocatedName = useRef<string | undefined>(undefined), loadedScope = useRef(scope);
  const load = (record: WorkflowDraft) => {
    if (record.id === confirmed.current.id && record.revision < confirmed.current.revision) return;
    confirmed.current = record; loaded.current = `${record.id}:${record.revision}`; session.load(record);
  };
  useEffect(() => {
    if (loadedScope.current === scope) return;
    loadedScope.current = scope; loaded.current = ""; confirmed.current = empty();
    session.load(confirmed.current); setError("");
  }, [scope]);
  const { markSaved, confirmDiscard, discardChanges } = useUnsavedChanges(dirty, () => {
    const record = query.data?.record;
    load(record && record.revision >= confirmed.current.revision ? record : structuredClone(confirmed.current));
    setError(""); onDiscard?.();
  }, busy);
  useEffect(() => {
    const record = query.data?.record;
    if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record);
  }, [query.data?.record, dirty, busy, session.load]);
  const edit = (change: DraftEdit<WorkflowDraft>) => {
    if (lock.current) return;
    session.edit(change); setError("");
  };
  const acknowledge = (submitted: WorkflowDraft, record: WorkflowDraft) => {
    confirmed.current = record; loaded.current = `${record.id}:${record.revision}`;
    session.saved(submitted, record); markSaved();
  };
  const refresh = async (submitted: WorkflowDraft, accepted: WorkflowDraft, message: string) => {
    acknowledge(submitted, accepted);
    try {
      const fresh = await query.refetch();
      if (fresh.isSuccess && fresh.data?.record?.revision === accepted.revision) {
        acknowledge(accepted, fresh.data.record); return;
      }
    } catch {   }
    setError(message);
  };
  const invalidInputs = () => Object.values(session.inputProblems).some(Boolean);
  const save = async () => {
    if (invalidInputs()) return;
    const submitted = structuredClone(draft), target = submitted.id || createID;
    const revision = submitted.id ? confirmed.current.revision : 0;
    if (!submitted.name && view === "automation") submitted.name = allocatedName.current ??= (submitted.title.toLowerCase().replace(/[^a-z0-9]/g, "") || "automation") + createID.replace(/-/g, "").slice(0, 8);
    if (branch) {
      await saveToBranch(target, branchSpec(submitted));
      const fresh = (await query.refetch()).data?.record;
      const accepted = { ...submitted, id: target, revision: fresh?.revision ?? revision + 1 };
      acknowledge(draft, accepted);
      if (!submitted.id) open({ view, params: { id: target } }, { ifCurrent: { view, params: { id } }, replace: true });
      return target;
    }
    if (!await decide(`build.process.${submitted.id ? "edit" : "create"}`, { type: "build.process", id: target }, workflowInputs(submitted),
      { expectedRevision: submitted.id ? revision : undefined, quiet: true, onRefused: setError })) return;
    const accepted = { ...submitted, id: target, revision: revision + 1 };
    if (!submitted.id) {
      acknowledge(draft, accepted);
      open({ view, params: { id: target } }, { ifCurrent: { view, params: { id } }, replace: true });
    } else await refresh(draft, accepted, t("The resource was saved, but its current record could not be reloaded."));
    return target;
  };
  const synchronized = !draft.id || query.data?.record?.revision === confirmed.current.revision;
  const reload = async () => {
    const fresh = await query.refetch();
    if (fresh.isSuccess && fresh.data?.record) { load(fresh.data.record); markSaved(); setError(""); onDiscard?.(); }
  };
  const perform = async (action: () => Promise<unknown>) => {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError("");
    try { await action(); }
    catch (problem) { setError(apiErrorMessage(problem) ?? (problem instanceof Error ? problem.message : failure)); }
    finally { lock.current = false; setBusy(false); }
  };
  return { session, draft, dirty, createID, branch, query, busy, error, setError, edit, save, perform, reload, revision: confirmed.current.revision, synchronized, confirmDiscard, discardChanges };
}
