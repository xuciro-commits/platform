import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Input, PanelSection, StructureRow, t, useWorkspace, type Route } from "@platform/ui";
import { useState } from "react";
import type { Project } from "./project";
import { kindIcon } from "./resource-icons";
import { refKey } from "./resources";
import { useProjectResources } from "./resource-inventory";
import { assetRoute, conceptTree, studioApplicationID } from "./studio-model";

/** The resource tree is contributed by Build, inside the existing Shell. */
export function StudioNavigation({ route }: { route?: Route }) {
  const host = useHost(), { open } = useWorkspace();
  const id = studioApplicationID(route);
  const applications = useRecordInventory<Project>("build.app", 1000, !id);
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id ?? "")}`, undefined, !!id);
  const project = query.data?.record;
  const { owned, unavailable } = useProjectResources(project);
  const [filter, setFilter] = useState("");
  if (host.role("build") !== "builder") return null;
  if (!id) return <PanelSection title={t("Applications")}>
    {applications.isError && <p role="alert" className="text-xs text-danger">{t("Applications could not be loaded.")}</p>}
    {(applications.data?.records ?? []).filter(project => !project.archived).map(project => <StructureRow key={project.id} label={project.title || project.name} onClick={() => open({ view: "project", params: { id: project.id } })} />)}
  </PanelSection>;
  if (!project) return <p role={query.isError ? "alert" : "status"} className="p-2 text-xs text-muted">{query.isError ? t("The project could not be loaded.") : t("Loading…")}</p>;
  const selected = owned.find(item => item.record?.id === route?.params?.page || item.record?.id === route?.params?.id || refKey(item.ref) === route?.params?.asset);
  const visible = owned.filter(item => `${item.title} ${item.ref.name}`.toLocaleLowerCase().includes(filter.toLocaleLowerCase()));
  return <div role="region" aria-label={t("Application assets")}>
    <Input type="search" aria-label={t("Filter application assets")} placeholder={t("Filter application assets")} value={filter} onChange={event => setFilter(event.target.value)} className="h-7 text-xs" />
    {unavailable && <p role="alert" className="p-2 text-xs text-danger">{t("Some application assets are unreadable.")}</p>}
    {conceptTree(project, visible).map(group => <PanelSection key={group.id} title={group.title}>
      {group.items.map(item => <StructureRow key={refKey(item.ref)} icon={kindIcon[item.kind.kind]} label={item.title} selected={selected === item} meta={item.record?.state === "draft" ? t("draft") : undefined} onClick={() => open(assetRoute(item.ref, owned, host.definitions, project.id))} />)}
    </PanelSection>)}
    <Button variant="ghost" size="sm" onClick={() => open({ view: "project", params: { id: project.id, section: "concepts", ...(selected ? { asset: refKey(selected.ref) } : {}) } })}>{t("Organize business concepts")}</Button>
  </div>;
}
