// One draft session over a `build.object` record, shared by the object type
// workbench and the action type workbench (ADR-0053 §5–§6): both edit the same
// record through its own action, autosave it and publish it; the host checks
// everything again at publication.
import { useHost, useReadQuery } from "@platform/app";
import { notify, t, useUnsavedChanges } from "@platform/ui";
import { useEffect, useRef, useState } from "react";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useDraftSession } from "../session/DraftSession";
import { useAutoSave } from "../editor/workbench";
import { actionIssues, type ObjectRecord, type Process } from "./object-model";

const hydrate = (record: ObjectRecord): Process => ({ states: record.states ?? [], actions: record.actions ?? [], access: record.access ?? [], fields: record.fields ?? [], scope: record.scope, implements: record.implements ?? [], extends: record.extends ?? "", numbering: record.numbering });

export function useObjectDraft(id: string, onReset?: () => void) {
  const { decide, entities, definitions } = useHost();
  const { open } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: ObjectRecord }>(`/v1/records/build.object/${encodeURIComponent(id)}`);
  const object = query.data?.record;
  const session = useDraftSession<Process>({ states: [], actions: [], access: [], fields: [], implements: [], extends: "" });
  const { draft: process, dirty } = session;
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const [busy, setBusy] = useState(false);
  const [refused, setRefused] = useState<string>(); // why the host refused, kept on screen
  const reset = useRef(onReset); reset.current = onReset;
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    if (object) { session.load(hydrate(object)); baseRevision.current = object.revision; loaded.current = `${object.id}:${object.revision}`; }
    setRefused(undefined); reset.current?.();
  });
  useEffect(() => {
    if (!object || dirty || busy || loaded.current === `${object.id}:${object.revision}`) return;
    session.load(hydrate(object)); baseRevision.current = object.revision; loaded.current = `${object.id}:${object.revision}`;
  }, [object, dirty, busy, session.load]);
  const saveRef = useRef<() => Promise<unknown>>(async () => false);
  useAutoSave({ dirty, busy, save: () => saveRef.current() });

  const parent = object ? `build.${object.name}` : "";
  const targets = entities.filter((entity) => entity.type !== parent && entity.fields.some((field) => field.type === "reference" && field.ref === parent)
    && definitions.some((definition) => definition.source === "tenant" && definition.ref.app === "build" && definition.ref.kind === "object" && definition.ref.name === entity.type));
  const issuesOf = (name: string) => { const a = process.actions.find((x) => x.name === name); return a ? actionIssues(a, process, parent, targets, entities) : []; };
  const issues = process.actions.flatMap((a) => actionIssues(a, process, parent, targets, entities).map((message) => `${a.title || a.name}: ${message}`));
  const change = (next: Process) => { if (!lock.current) session.edit(next); };
  const save = async () => {
    setRefused(undefined);
    const expectedRevision = baseRevision.current;
    const ok = await decide("build.object.edit", { type: "build.object", id }, process, { expectedRevision, onRefused: setRefused });
    if (ok) {
      const result = await query.refetch();
      const confirmed = result.isSuccess && result.data?.record?.revision === expectedRevision + 1 ? result.data.record : undefined;
      baseRevision.current = expectedRevision + 1; loaded.current = `${id}:${baseRevision.current}`;
      session.saved(process, confirmed ? hydrate(confirmed) : undefined); markSaved();
    }
    return ok;
  };
  const perform = async (action: () => Promise<unknown>) => {
    if (lock.current) return; lock.current = true; setBusy(true); setRefused(undefined);
    try { await action(); } catch { setRefused(t("The object request could not be completed. Your draft is still here.")); }
    finally { lock.current = false; setBusy(false); }
  };
  saveRef.current = () => perform(save);
  const publish = async () => {
    setRefused(undefined);
    if (issues.length) { setRefused(issues.join(" ")); return; }
    if (dirty && !(await save())) return; // what is published is what was saved
    if (await decide("build.object.publish", { type: "build.object", id }, {}, { onRefused: setRefused })) notify.success(t("The object is installed with its states and actions."));
  };
  const review = async () => {
    if (issues.length || (dirty && !await save())) return;
    open({ view: "release-review", params: { kind: "object", id } });
  };
  const roles = process.access.map((a) => a.role).filter((r) => process.access.find((x) => x.role === r)?.read !== "none");
  const approverRoles = ["builder", ...(process.access.length ? process.access.map((a) => a.role) : [])];
  return { object, failed: query.isError, process, dirty, busy, refused, setRefused, change, save, publish, review, perform, issues, issuesOf, parent, targets, entities, roles, approverRoles, session, discardChanges, open };
}
