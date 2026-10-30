import { useRecordInventory, type Host } from "@platform/app";
import type { Api } from "@platform/kernel";
import { t } from "@platform/ui";
import { useMemo } from "react";
import { resourcePath } from "../branches/api";
import { selectedBranch } from "../branches/current";
import { kindOfType, refKey, resourceRef } from "./resources";

type ProjectRecord = Api.Application & { id: string; revision: number; archived?: boolean };
export type ProjectIndex = { byRef: Map<string, { id: string; title: string }[]>; loaded: boolean; failed?: boolean };

export function projectResourceChange(project: Pick<Api.Application, "pages" | "resources">, ref: Api.AssetRef): Partial<Api.Application> {
  if (ref.kind === "page") return project.pages?.includes(ref.name) ? {} : { pages: [...project.pages ?? [], ref.name] };
  return project.resources?.some(member => refKey(member) === refKey(ref)) ? {} : { resources: [...project.resources ?? [], ref] };
}

export async function joinProjectResource(host: Pick<Host, "client" | "decide">, application: string, ref: Api.AssetRef) {
  const branch = selectedBranch(host.client);
  let record: ProjectRecord | undefined;
  if (branch) {
    const answer = await host.client.call<Api.BranchResourceView>("GET", resourcePath(branch, "build.app", application));
    const spec = answer.ok && answer.body.op !== "delete" ? answer.body.branch as Api.Application | undefined : undefined;
    record = spec && { ...spec, id: application, revision: 0 };
  } else record = (await host.client.get<{ record?: ProjectRecord }>(`/v1/records/build.app/${encodeURIComponent(application)}`, true)).record;
  if (!record || record.archived) throw Error(t("The resource was saved, but the application could not be read. Save again to retry adding the same resource."));
  const changes = projectResourceChange(record, ref);
  if (!Object.keys(changes).length) return;
  let refusal = "";
  if (!await host.decide("build.app.edit", { type: "build.app", id: application }, changes, { expectedRevision: record.revision, quiet: true, onRefused: message => { refusal = message; } })) {
    throw Error(refusal || t("The resource was saved but has not joined the application. Save again to retry the same resource."));
  }
}

export function useProjectIndex(): ProjectIndex {
  const projects = useRecordInventory<ProjectRecord>("build.app");
  return useMemo(() => {
    const byRef = new Map<string, { id: string; title: string }[]>();
    for (const p of projects.data?.records ?? []) {
      if (p.archived) continue;
      const entry = { id: p.id, title: p.title || p.name || p.id };
      for (const ref of p.resources ?? []) (byRef.get(refKey(ref)) ?? byRef.set(refKey(ref), []).get(refKey(ref))!).push(entry);
      for (const name of p.pages ?? []) { const k = refKey({ app: "build", kind: "page", name }); (byRef.get(k) ?? byRef.set(k, []).get(k)!).push(entry); }
    }
    return { byRef, loaded: projects.isSuccess, failed: projects.isError };
  }, [projects.data, projects.isSuccess, projects.isError]);
}

export function projectGroup(index: ProjectIndex, ref: Api.AssetRef): { id: string; label: string; order?: number } {
  if (!index.loaded) return { id: "_unknown", label: index.failed ? t("Project ownership is unavailable") : t("Reading project ownership…"), order: 8 };
  const held = index.byRef.get(refKey(ref));
  if (held?.length === 1) return { id: held[0]!.id, label: held[0]!.title };
  if (held && held.length > 1) {
    const projects = [...new Map(held.map(project => [project.id, project])).values()].sort((a, b) => a.id.localeCompare(b.id));
    if (projects.length === 1) return { id: projects[0]!.id, label: projects[0]!.title };
    return { id: `shared:${JSON.stringify(projects.map(project => project.id))}`, label: t("Shared by {projects}", { projects: projects.map(project => project.title).join(", ") }), order: 4 };
  }
  return { id: "_none", label: t("Not in a project"), order: 9 };
}

export function projectGroupOfRecord(index: ProjectIndex, type: string, name: string) {
  const kind = kindOfType(type);
  return kind ? projectGroup(index, resourceRef(kind, name)) : { id: "_none", label: t("Not in a project"), order: 9 };
}
