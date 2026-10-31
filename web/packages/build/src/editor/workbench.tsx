import {useInputDrafts} from "@platform/ui";
import { useRecordArchive } from "@platform/app";
import { ActionMenu, Button, StatusTag, cn, defineStatuses, t, useWorkspace, type ContextCommand, type Route, type WorkbenchSaving } from "@platform/ui";
import { Check, MoreHorizontal } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useApplicationScope } from "../projects/application-scope";
import { useCurrentBranch } from "../branches/current";

const draftStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" }, archived: { label: t("Archived"), tone: "neutral" } });

export function useAutoSave({ enabled = true, dirty, invalid = false, busy = false, save, delay = 900, generation }: {
  enabled?: boolean; dirty: boolean; invalid?: boolean; busy?: boolean; save: () => Promise<unknown>; delay?: number; generation?:unknown;
}) {
  const inputs=useInputDrafts(),attempt=useRef<unknown>(undefined),latest = useRef(save); latest.current = save;
  invalid ||= !!inputs?.invalid;busy ||=!!inputs?.busy;
  useEffect(() => {
    if (!enabled || !dirty || invalid || busy || attempt.current===generation&&generation!==undefined) return;
    const timer = setTimeout(() => { attempt.current=generation;void latest.current().catch(()=>undefined); }, delay);
    return () => clearTimeout(timer);
  }, [enabled, dirty, invalid, busy, delay,generation]);
}

export const savingState = (dirty: boolean, saving: boolean, error?: string): WorkbenchSaving => error ? "error" : saving ? "saving" : dirty ? "dirty" : "saved";

export function DraftStatus({ state, problems = 0, archived }: { state?: string; problems?: number; archived?: boolean }) {
  return <>
    {state && <StatusTag status={archived ? "archived" : state} registry={draftStates} />}
    {problems > 0 && <span className="rounded-full bg-danger/10 px-2 py-0.5 text-xs text-danger" role="status">{t("{n} problems", { n: problems })}</span>}
  </>;
}

export type EditorOperation = ContextCommand & { busy?: boolean };

export function ResourceControls({ type, record, dirty = false, busy = false, invalid = false, empty = false, autosave = true, showStatus = true, onSave, onCancel, onReload, route, operations = [], extra = [] }: {
  type: string; record?: { id?: string; revision?: number; archived?: boolean; state?: string }; dirty?: boolean; busy?: boolean; invalid?: boolean; empty?: boolean;
  autosave?: boolean; showStatus?: boolean; onSave?: () => void; onCancel?: () => void; onReload?: () => void; route?: Route;
  operations?: EditorOperation[]; extra?: ContextCommand[];
}) {
  const inputs=useInputDrafts();invalid ||=!!inputs?.invalid;busy ||=!!inputs?.busy;
  const { open, close } = useWorkspace();
  const application = useApplicationScope();
  const archive = useRecordArchive(type);
  const onBranch = !!useCurrentBranch().id;
  const home: Route = route ? { view: route.view, ...(application ? { params: { application } } : {}) } : application ? { view: "project", params: { id: application } } : { view: "projects" };
  const tab: Route | undefined = route && application ? { view: route.view, params: { ...(route.params ?? {}), application } } : route;
  const leave = () => { onCancel?.(); open(home); if (tab) close(tab); };
  const saved = !!record?.id;
  const [stalled, setStalled] = useState(false);
  const attempt = useRef<number | undefined>(undefined), wasBusy = useRef(busy);
  useEffect(() => { if (!dirty) { setStalled(false); attempt.current = undefined; } }, [dirty]);
  useEffect(() => { setStalled(false); }, [record?.revision]);
  useEffect(() => {
    if (wasBusy.current && !busy && dirty && attempt.current !== undefined && attempt.current === record?.revision) setStalled(true);
    wasBusy.current = busy;
  }, [busy, dirty, record?.revision]);
  useAutoSave({ enabled: autosave && saved && !!onSave && !record?.archived && !stalled, dirty, invalid, busy, save: async () => { attempt.current = record?.revision; onSave?.(); } });
  const inline = operations.slice(0, 2), more = operations.slice(2);
  const overflow: ContextCommand[] = [
    ...more.map(operation => ({ ...operation, disabled: busy || operation.disabled })),
    ...extra,
    ...(saved && dirty && onCancel ? [{ id: "discard", label: t("Discard unsaved changes"), disabled: busy, separatorBefore: more.length + extra.length > 0, run: onCancel }] : []),
    ...(onReload && saved ? [{ id: "reload", label: t("Reload saved version"), disabled: busy, separatorBefore: more.length + extra.length > 0 && !(dirty && onCancel), run: onReload }] : []),
    ...(saved && !record?.archived && archive.available ? [{ id: "archive", label: onBranch ? t("Archive on this branch…") : t("Archive…"), danger: true, disabled: dirty || busy, separatorBefore: true, run: () => archive.take({ id: record!.id!, revision: record!.revision ?? 0 }, leave) }] : []),
  ];
  const manual = !autosave || stalled || invalid;
  const status = busy ? t("Saving…") : dirty ? (stalled ? t("Not saved") : invalid ? t("Fix the problems to save") : t("Unsaved changes")) : onBranch ? t("Saved to branch") : t("Saved as draft");
  return <>
    {inline.map(operation => <Button key={operation.id} disabled={busy || operation.disabled} loading={operation.busy} title={operation.disabled ? operation.reason : undefined} onClick={operation.run}>{operation.icon}{operation.label}</Button>)}
    {saved ? <>
      {showStatus && onSave && <span role="status" className={cn("flex items-center gap-1 px-1 text-xs [&_svg]:size-3.5", stalled ? "text-danger" : "text-muted")}>{!dirty && !busy && <Check />}{status}</span>}
      {onSave && dirty && manual && <Button disabled={busy || invalid} title={invalid ? t("Fix the problems first.") : undefined} onClick={onSave}>{t("Save")}</Button>}
    </> : <>
      {onCancel && <Button variant="ghost" disabled={busy} onClick={leave}>{t("Cancel")}</Button>}
      {onSave && <Button variant="primary" disabled={busy || invalid || empty} title={invalid ? t("Fix the problems first.") : undefined} onClick={onSave}>{t("Create")}</Button>}
    </>}
    {overflow.length > 0 && <ActionMenu label={t("More actions")} icon={<MoreHorizontal />} commands={overflow} />}
    {archive.dialog}
  </>;
}

export function WorkbenchMessage({ children }: { children: ReactNode }) {
  return <div className="flex flex-1 items-center justify-center p-6 text-sm text-muted">{children}</div>;
}
