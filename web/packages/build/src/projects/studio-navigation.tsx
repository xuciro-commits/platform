import { useHost, useReadQuery } from "@platform/app";
import { Button, Input, PanelSection, StructureRow, t, useWorkspace, type Route } from "@platform/ui";
import { useState } from "react";
import type { Project } from "./project";
import { kindIcon } from "./resource-icons";
import { refKey } from "./resources";
import { useProjectResources } from "./resource-inventory";
import { assetRoute, conceptTree, studioApplicationID, studioAreas, studioContext, studioRoute } from "./studio-model";

/** The resource tree is contributed by Build, inside the existing Shell. */
export function StudioNavigation({ route }: { route?: Route }) {
  const host = useHost(), { open } = useWorkspace();
  const id = studioApplicationID(route);
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id ?? "")}`, undefined, !!id);
  const project = query.data?.record;
  const { owned, unavailable } = useProjectResources(project);
  const [filter, setFilter] = useState("");
  if (host.role("build") !== "builder") return null;
  if (!id) return null;
  if (!project) return <p role={query.isError ? "alert" : "status"} className="p-2 text-xs text-muted">{query.isError ? t("The project could not be loaded.") : t("Loading…")}</p>;
  const { selected, area } = studioContext(route, owned, host.definitions);
  const visible = owned.filter(item => `${item.title} ${item.ref.name} ${t(item.kind.label)}`.toLocaleLowerCase().includes(filter.toLocaleLowerCase()));
  return <div role="region" aria-label={t("Application assets")}>
    <p className="px-2 pt-1 text-xs font-medium text-muted">{t("Current application")}</p>
    <p className="break-words px-2 pb-2 text-sm font-semibold">{project.title || project.name}</p>
    <div role="navigation" aria-label={t("Application workspaces")} className="mb-2 grid gap-1">
      {studioAreas.map(item => <Button key={item.id} aria-current={area === item.id ? "page" : undefined} variant={area === item.id ? "primary" : "ghost"} size="sm" className="justify-start" onClick={() => open(studioRoute(project.id, item.id, selected ? refKey(selected.ref) : undefined))}>{t(item.title)}</Button>)}
    </div>
    <Input type="search" aria-label={t("Filter application assets")} placeholder={t("Filter application assets")} value={filter} onChange={event => setFilter(event.target.value)} className="h-7 text-xs" />
    {unavailable && <p role="alert" className="p-2 text-xs text-danger">{t("Some application assets are unreadable.")}</p>}
    {conceptTree(project, visible).filter(group => group.items.length).map(group => <PanelSection key={group.id} title={group.title}>
      {group.items.map(item => <StructureRow key={refKey(item.ref)} icon={kindIcon[item.kind.kind]} label={item.title} selected={selected === item} meta={item.missing ? t("missing") : item.record?.state === "draft" ? t("draft") : undefined} onClick={() => open(assetRoute(item.ref, owned, host.definitions, project.id))} />)}
    </PanelSection>)}
    <Button variant="ghost" size="sm" onClick={() => open({ view: "project", params: { id: project.id, section: "concepts", ...(selected ? { asset: refKey(selected.ref) } : {}) } })}>{t("Organize business concepts")}</Button>
  </div>;
}
