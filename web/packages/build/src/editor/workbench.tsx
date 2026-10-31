import {useInputDrafts} from "@platform/ui";
import { useRecordArchive } from "@platform/app";
import { ActionMenu, Button, StatusTag, defineStatuses, t, useWorkspace, type ContextCommand, type Route, type WorkbenchSaving } from "@platform/ui";
import { ChevronDown, MoreHorizontal, Rocket } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { useDirectInstall } from "../releases/release-profile";
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

export function ResourceControls({ type, record, dirty = false, busy = false, invalid = false, empty = false, onReview, onInstall, onPublish, onSave, onCancel, onReload, route, operations = [], extra = [], publishBlocked }: {
  type: string; record?: { id?: string; revision?: number; archived?: boolean; state?: string }; dirty?: boolean; busy?: boolean; invalid?: boolean; empty?: boolean;
  onReview?: () => void; onInstall?: () => void; onPublish?: () => void; onSave?: () => void; onCancel?: () => void; onReload?: () => void; route?: Route;
  operations?: EditorOperation[]; extra?: ContextCommand[]; publishBlocked?: string;
}) {
  const inputs=useInputDrafts();invalid ||=!!inputs?.invalid;busy ||=!!inputs?.busy;
  const { open, close } = useWorkspace();
  const application = useApplicationScope();
  const directInstall = useDirectInstall();
  const archive = useRecordArchive(type);
  const onBranch = !!useCurrentBranch().id;
  const home: Route = application ? { view: "project", params: { id: application } } : { view: "projects" };
  const tab: Route | undefined = route && application ? { view: route.view, params: { ...(route.params ?? {}), application } } : route;
  const leave = () => { onCancel?.(); open(home); if (tab) close(tab); };
  const saved = !!record?.id;
  const blocked = busy || invalid || empty || !!publishBlocked;
  const blockedReason = invalid ? t("Fix the problems first.") : publishBlocked ?? (empty ? t("Nothing to publish yet.") : undefined);
  const inline = operations.slice(0, 2), more = operations.slice(2);
  const publishMenu: ContextCommand[] = [
    ...(onReview && onPublish ? [{ id: "publish-now", label: t("Publish now, without a release"), disabled: blocked, run: onPublish }] : []),
    ...(directInstall && onInstall ? [{ id: "install", label: t("Install directly (no release)"), disabled: dirty || blocked, run: onInstall }] : []),
  ];
  const overflow: ContextCommand[] = [
    ...more.map(operation => ({ ...operation, disabled: busy || operation.disabled })),
    ...extra,
    ...(onReload && saved ? [{ id: "reload", label: t("Reload saved version"), disabled: busy, separatorBefore: more.length + extra.length > 0, run: onReload }] : []),
    ...(saved && !record?.archived && archive.available ? [{ id: "archive", label: onBranch ? t("Archive on this branch…") : t("Archive…"), danger: true, disabled: dirty || busy, separatorBefore: more.length + extra.length > 0 || !!onReload, run: () => archive.take({ id: record!.id!, revision: record!.revision ?? 0 }, leave) }] : []),
  ];
  const publish = onReview ?? onPublish;
  const saveAction = onBranch ? onSave ?? onReview : onSave;
  return <>
    {inline.map(operation => <Button key={operation.id} disabled={busy || operation.disabled} loading={operation.busy} title={operation.disabled ? operation.reason : undefined} onClick={operation.run}>{operation.icon}{operation.label}</Button>)}
    {onCancel && (saved ? dirty && <Button variant="ghost" disabled={busy} onClick={onCancel}>{t("Discard changes")}</Button> : <Button variant="ghost" disabled={busy} onClick={leave}>{t("Cancel")}</Button>)}
    {saveAction && <Button variant={onBranch ? "primary" : "default"} disabled={busy || invalid || saved && !dirty} onClick={saveAction}>{onBranch ? t("Save to branch") : t("Save")}</Button>}
    {!onBranch && publish && <div className="flex items-center">
      <Button variant="primary" className={publishMenu.length ? "rounded-r-none" : undefined} disabled={blocked} onClick={publish} title={blockedReason}>{onReview ? <><Rocket />{t("Publish")}</> : t("Publish")}</Button>
      {publishMenu.length > 0 && <ActionMenu label={t("Publish options")} variant="primary" className="rounded-l-none border-l-primary-foreground/30" icon={<ChevronDown />} commands={publishMenu} />}
    </div>}
    {overflow.length > 0 && <ActionMenu label={t("More actions")} icon={<MoreHorizontal />} commands={overflow} />}
    {archive.dialog}
  </>;
}

export function WorkbenchMessage({ children }: { children: ReactNode }) {
  return <div className="flex flex-1 items-center justify-center p-6 text-sm text-muted">{children}</div>;
}
