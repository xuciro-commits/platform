import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage } from "@platform/kernel";
import { t, useUnsavedChanges } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useDraftSession } from "../session/DraftSession";
import type { DraftEdit } from "../session/draft-history";
import type { WorkflowDraft } from "./workflow-model";
import { workflowInputs } from "./workflow-session";

export function useProcessDraft({ id, view, empty, failure, onDiscard, onPublished }: {
  id: string; view: "flow" | "automation"; empty: () => WorkflowDraft; failure: string;
  onDiscard?: () => void; onPublished?: () => Promise<unknown>;
}) {
  const { decide } = useHost(), { open } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: WorkflowDraft }>(`/v1/records/build.process/${encodeURIComponent(id)}`, undefined, id !== "new");
  const session = useDraftSession<WorkflowDraft>(empty());
  const { draft, dirty } = session;
  const [createID] = useState(() => crypto.randomUUID());
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const confirmed = useRef(draft), loaded = useRef(""), lock = useRef(false), allocatedName = useRef<string | undefined>(undefined);
  const load = (record: WorkflowDraft) => {
    if (record.id === confirmed.current.id && record.revision < confirmed.current.revision) return;
    confirmed.current = record; loaded.current = `${record.id}:${record.revision}`; session.load(record);
  };
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
    } catch { /* Report a read failure without resubmitting the accepted edit. */ }
    setError(message);
  };
  const invalidInputs = () => Object.values(session.inputProblems).some(Boolean);
  const save = async () => {
    if (invalidInputs()) return;
    const submitted = structuredClone(draft), target = submitted.id || createID;
    const revision = submitted.id ? confirmed.current.revision : 0;
    if (!submitted.name && view === "automation") submitted.name = allocatedName.current ??= (submitted.title.toLowerCase().replace(/[^a-z0-9]/g, "") || "automation") + createID.replace(/-/g, "").slice(0, 8);
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
  const publish = async () => {
    if (!draft.id || invalidInputs()) return;
    if (!synchronized) { setError(t("The current flow revision differs from this editing session. Reload before running or reviewing a release.")); return; }
    if (dirty && !await save()) return;
    const accepted = confirmed.current;
    if (!accepted.id || !await decide("build.process.publish", { type: "build.process", id: accepted.id }, {},
      { expectedRevision: accepted.revision, quiet: true, onRefused: setError })) return;
    await refresh(accepted, { ...accepted, revision: accepted.revision + 1 }, t("The resource was installed, but its current record could not be reloaded."));
    await onPublished?.();
  };
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
  const reviewTarget = async () => dirty || !draft.id ? await save() : draft.id;
  return { session, draft, dirty, createID, query, busy, error, setError, edit, save, publish, perform, reload, reviewTarget,
    revision: confirmed.current.revision, synchronized, confirmDiscard, discardChanges };
}
