// What every builder editor shares on top of the Workbench chrome
// (ADR-0053 §4, D6): drafts save themselves, the title bar shows one
// status, and "Publish" is one menu with the same entries everywhere.
import { useRecordArchive } from "@platform/app";
import { ActionMenu, Button, StatusTag, defineStatuses, t, useWorkspace, type ContextCommand, type Route, type WorkbenchSaving } from "@platform/ui";
import { ChevronDown, Rocket } from "lucide-react";
import { useEffect, useRef, type ReactNode } from "react";
import { useDirectInstall } from "../releases/release-profile";
import { useApplicationScope } from "../projects/application-scope";

const draftStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" }, archived: { label: t("Archived"), tone: "neutral" } });

/** Save a dirty, valid draft after a pause; the caller's `save` owns locking and refetch. */
export function useAutoSave({ enabled = true, dirty, invalid = false, busy = false, save, delay = 900 }: {
  enabled?: boolean; dirty: boolean; invalid?: boolean; busy?: boolean; save: () => Promise<unknown>; delay?: number;
}) {
  const latest = useRef(save); latest.current = save;
  useEffect(() => {
    if (!enabled || !dirty || invalid || busy) return;
    const timer = setTimeout(() => { void latest.current(); }, delay);
    return () => clearTimeout(timer);
  }, [enabled, dirty, invalid, busy, delay]);
}

export const savingState = (dirty: boolean, saving: boolean, error?: string): WorkbenchSaving => error ? "error" : saving ? "saving" : dirty ? "dirty" : "saved";

/** The draft/published badge plus "n problems" when the draft cannot be published. */
export function DraftStatus({ state, problems = 0, archived }: { state?: string; problems?: number; archived?: boolean }) {
  return <>
    {state && <StatusTag status={archived ? "archived" : state} registry={draftStates} />}
    {problems > 0 && <span className="rounded-full bg-danger/10 px-2 py-0.5 text-[11px] text-danger" role="status">{t("{n} problems", { n: problems })}</span>}
  </>;
}

/** Publish ▾: review the release, install directly where the tenant allows it, discard the draft, archive. */
export function PublishMenu({ type, record, dirty, busy = false, invalid = false, empty = false, onReview, onInstall, onSave, onDiscard, route, extra = [] }: {
  type: string; record?: { id?: string; revision?: number; archived?: boolean; state?: string }; dirty: boolean; busy?: boolean; invalid?: boolean; empty?: boolean;
  onReview: () => void; onInstall?: () => void; onSave?: () => void; onDiscard?: () => void; route?: Route; extra?: ContextCommand[];
}) {
  const { open, close } = useWorkspace();
  const application = useApplicationScope();
  const directInstall = useDirectInstall();
  const archive = useRecordArchive(type);
  const home: Route = application ? { view: "project", params: { id: application } } : { view: "projects" };
  const tab: Route | undefined = route && application ? { view: route.view, params: { ...(route.params ?? {}), application } } : route;
  const leave = () => { open(home); if (tab) close(tab); };
  const commands: ContextCommand[] = [
    { id: "review", label: t("Review and publish…"), icon: <Rocket />, disabled: busy || invalid || empty, run: onReview },
    ...(directInstall && onInstall ? [{ id: "install", label: t("Install directly (no release)"), disabled: busy || invalid || empty, run: onInstall }] : []),
    ...extra,
    ...(onDiscard ? [{ id: "discard", label: t("Discard unsaved changes"), disabled: !dirty || busy, separatorBefore: true, run: onDiscard }] : []),
    ...(record?.id && !record.archived && archive.available ? [{ id: "archive", label: t("Archive…"), danger: true, disabled: busy, run: () => archive.take({ id: record.id!, revision: record.revision ?? 0 }, leave) }] : []),
  ];
  return <>
    {onSave && <Button size="sm" disabled={busy || !dirty} onClick={onSave}>{t("Save")}</Button>}
    <div className="flex items-center">
      <Button variant="primary" size="sm" className="rounded-r-none" disabled={busy || invalid || empty} onClick={onReview} title={invalid ? t("Fix the problems first.") : empty ? t("Nothing to publish yet.") : undefined}>{t("Publish")}</Button>
      <ActionMenu label={t("Publish options")} variant="primary" icon={<ChevronDown />} commands={commands} />
    </div>
    {archive.dialog}
  </>;
}

/** The main area of a Workbench while its record loads or is missing. */
export function WorkbenchMessage({ children }: { children: ReactNode }) {
  return <div className="flex flex-1 items-center justify-center p-6 text-sm text-muted">{children}</div>;
}

/** Back-to-project, cancel and archive for the small resource editors that keep a plain header (link types, shared properties, queries, test plans). */
export function ResourceControls({ type, record, dirty = false, busy = false, onCancel, route }: {
  type: string; record?: { id?: string; revision?: number; archived?: boolean }; dirty?: boolean; busy?: boolean;
  onCancel?: () => void; route?: Route;
}) {
  const { open, close } = useWorkspace();
  const application = useApplicationScope();
  const archive = useRecordArchive(type);
  const home: Route = application ? { view: "project", params: { id: application } } : { view: "projects" };
  const tab: Route | undefined = route && application ? { view: route.view, params: { ...(route.params ?? {}), application } } : route;
  const leave = () => { onCancel?.(); open(home); if (tab) close(tab); };
  return <>
    {application && <Button variant="ghost" disabled={busy} onClick={() => open(home)}>{t("Back to project")}</Button>}
    {onCancel && <Button variant="ghost" disabled={busy || (!dirty && !!record?.id)} onClick={() => record?.id ? onCancel() : leave()}>{t(record?.id ? "Cancel changes" : "Cancel")}</Button>}
    {record?.id && !record.archived && archive.available && <Button variant="danger" disabled={busy} onClick={() => archive.take({ id: record.id!, revision: record.revision ?? 0 }, leave)}>{t("Archive")}</Button>}
    {archive.dialog}
  </>;
}
