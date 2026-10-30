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

export function ResourceControls({ type, record, dirty = false, busy = false, invalid = false, empty = false, onReview, onInstall, onSave, onCancel, route, extra = [] }: {
  type: string; record?: { id?: string; revision?: number; archived?: boolean; state?: string }; dirty?: boolean; busy?: boolean; invalid?: boolean; empty?: boolean;
  onReview?: () => void; onInstall?: () => void; onSave?: () => void; onCancel?: () => void; route?: Route; extra?: ContextCommand[];
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
  const archiveCommand: ContextCommand[] = record?.id && !record.archived && archive.available
    ? [{ id: "archive", label: onBranch ? t("Archive on this branch…") : t("Archive…"), danger: true, disabled: dirty || busy, separatorBefore: !onBranch, run: () => archive.take({ id: record.id!, revision: record.revision ?? 0 }, leave) }] : [];
  const commands: ContextCommand[] = onBranch ? archiveCommand : [
    ...(onReview ? [{ id: "review", label: t("Review and publish…"), icon: <Rocket />, disabled: busy || invalid || empty, run: onReview }] : []),
    ...(directInstall && onInstall ? [{ id: "install", label: t("Install directly (no release)"), disabled: dirty || busy || invalid || empty, run: onInstall }] : []),
    ...extra,
    ...(onCancel && record?.id ? [{ id: "discard", label: t("Discard unsaved changes"), disabled: !dirty || busy, separatorBefore: true, run: onCancel }] : []),
    ...archiveCommand,
  ];
  return <>
    {onCancel && (!onReview || !record?.id || onBranch) && <Button variant="ghost" disabled={busy || (!dirty && !!record?.id)} onClick={() => record?.id ? onCancel() : leave()}>{t(record?.id ? "Cancel changes" : "Cancel")}</Button>}
    {onBranch ? (onSave ?? onReview) && <Button size="sm" disabled={busy || !dirty || invalid} onClick={onSave ?? onReview}>{t("Save to branch")}</Button>
      : onSave && <Button size="sm" disabled={busy || !dirty||invalid} onClick={onSave}>{t("Save")}</Button>}
    {onBranch ? commands.length > 0 && <ActionMenu label={t("Branch options")} icon={<MoreHorizontal />} commands={commands} /> : onReview ? <div className="flex items-center">
      <Button variant="primary" size="sm" className="rounded-r-none" disabled={busy || invalid || empty} onClick={onReview} title={invalid ? t("Fix the problems first.") : empty ? t("Nothing to publish yet.") : undefined}>{t("Publish")}</Button>
      <ActionMenu label={t("Publish options")} variant="primary" icon={<ChevronDown />} commands={commands} />
    </div> : record?.id && !record.archived && archive.available && <Button variant="danger" disabled={dirty || busy} onClick={() => archive.take({ id: record.id!, revision: record.revision ?? 0 }, leave)}>{t("Archive")}</Button>}
    {archive.dialog}
  </>;
}

export function WorkbenchMessage({ children }: { children: ReactNode }) {
  return <div className="flex flex-1 items-center justify-center p-6 text-sm text-muted">{children}</div>;
}
