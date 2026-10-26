import { useHost, useRecordArchive } from "@platform/app";
import { Button, t, useWorkspace } from "@platform/ui";

/** Entry availability and draft archival keep their original owner actions. */
export function ApplicationLifecycleControls({ project, disabled = false, before = action => action() }: { project: { id: string; revision: number; state: string; published?: string }; disabled?: boolean; before?: (action: () => void) => void }) {
  const archive = useRecordArchive("build.app"), { decide, can } = useHost(), { open, close } = useWorkspace();
  const published = !!project.published || project.state === "published" || project.state === "withdrawn";
  const schema = `build.app.${project.state === "withdrawn" ? "restore" : "withdraw"}`;
  const leave = () => { open({ view: "projects" }); close({ view: "project", params: { id: project.id } }); };
  return <>
    {!published && archive.available && <><span className="text-xs text-muted">{t("Reusable assets and history are kept.")}</span><Button size="sm" variant="danger" disabled={disabled} onClick={() => before(() => archive.take(project, leave))}>{t("Delete application draft")}</Button></>}
    {published && can(schema) && <Button size="sm" variant={project.state === "withdrawn" ? "default" : "danger"} disabled={disabled} title={t("Only the application entry changes. Definitions, business records and independent tasks remain.")} onClick={() => before(() => void decide(schema, { type: "build.app", id: project.id }, {}, { expectedRevision: project.revision }))}>{t(project.state === "withdrawn" ? "Restore application entry" : "Withdraw application entry")}</Button>}
    {archive.dialog}
  </>;
}
