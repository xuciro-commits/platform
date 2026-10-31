import { useHost, useRecordArchive } from "@platform/app";
import { t, useWorkspace, type ContextCommand } from "@platform/ui";

export function useApplicationLifecycle({ project, disabled = false, before = action => action() }: { project?: { id: string; revision: number; state: string; published?: string }; disabled?: boolean; before?: (action: () => void) => void }) {
  const archive = useRecordArchive("build.app"), { decide, can } = useHost(), { open, close } = useWorkspace();
  if (!project) return { commands: [] as ContextCommand[], dialog: archive.dialog };
  const published = !!project.published || project.state === "published" || project.state === "withdrawn";
  const withdrawn = project.state === "withdrawn";
  const schema = `build.app.${withdrawn ? "restore" : "withdraw"}`;
  const leave = () => { open({ view: "projects" }); close({ view: "project", params: { id: project.id } }); };
  const commands: ContextCommand[] = [
    ...(!published && archive.available ? [{ id: "delete-draft", label: t("Delete application draft…"), danger: true, separatorBefore: true, disabled, reason: t("Reusable assets and history are kept."), run: () => before(() => archive.take(project, leave)) }] : []),
    ...(published && can(schema) ? [{ id: "lifecycle", label: t(withdrawn ? "Restore application entry" : "Withdraw application entry"), danger: !withdrawn, separatorBefore: true, disabled, reason: t("Only the application entry changes. Definitions, business records and independent tasks remain."), run: () => before(() => void decide(schema, { type: "build.app", id: project.id }, {}, { expectedRevision: project.revision })) }] : []),
  ];
  return { commands, dialog: archive.dialog };
}
