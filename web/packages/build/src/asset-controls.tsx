import { useRecordArchive } from "@platform/app";
import { Button, t, useWorkspace, type Route } from "@platform/ui";
import { useApplicationScope } from "./application-scope";

/** Editing is local; archiving always uses the original host action. */
export function AssetControls({ type, record, dirty = false, busy = false, onCancel, route }: {
  type: string; record?: { id?: string; revision?: number; archived?: boolean }; dirty?: boolean; busy?: boolean;
  onCancel?: () => void; route?: Route;
}) {
  const { open, close } = useWorkspace();
  const application = useApplicationScope();
  const archive = useRecordArchive(type);
  // A resource opened from an application returns to it (ADR-0047 §6.2 M2).
  const home: Route = application ? { view: "application", params: { id: application } } : { view: "studio" };
  // The editor's own tab keeps the application in its route, so closing uses the same key.
  const tab: Route | undefined = route && application ? { view: route.view, params: { ...(route.params ?? {}), application } } : route;
  const leave = () => { onCancel?.(); open(home); if (tab) close(tab); };
  return <>
    {application && <Button disabled={busy} onClick={() => open(home)}>{t("Back to application")}</Button>}
    {onCancel && <Button disabled={busy || (!dirty && !!record?.id)} onClick={() => record?.id ? onCancel() : leave()}>{t(record?.id ? "Cancel changes" : "Cancel")}</Button>}
    {record?.id && !record.archived && archive.available && <Button variant="danger" disabled={busy} onClick={() => archive.take({ id: record.id!, revision: record.revision ?? 0 }, leave)}>{t("Archive")}</Button>}
    {archive.dialog}
  </>;
}
