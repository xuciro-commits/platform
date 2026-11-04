import { categoryOf, type AppUI, type Host, type PlatformAppCategory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { t, type PlatformApplication } from "@platform/ui";

export const categories: { id: PlatformAppCategory; label: () => string }[] = [
  { id: "build", label: () => t("Build") },
  { id: "analyze", label: () => t("Analyze") },
  { id: "govern", label: () => t("Govern") },
  { id: "business", label: () => t("Business applications") },
];

export type Projection = "operations" | "build" | "admin";
export const projections: { id: Projection; categories: PlatformAppCategory[] }[] = [
  { id: "operations", categories: ["business", "analyze"] },
  { id: "build", categories: ["build", "analyze"] },
  { id: "admin", categories: ["govern"] },
];

export const portalEntry = (app: AppUI, host: Host): PlatformApplication =>
  ({ id: app.id, rid: app.rid, owningProject: app.owningProject, owningProjectTitle: app.owningProjectTitle, managedBy: app.managedBy,
    title: app.title, icon: app.icon, description: app.description,
    category: categoryOf(app) === "developer" ? "build" : categoryOf(app),
    home: typeof app.home === "function" ? app.home(host) : app.home });

export type ApplicationGroup = { id: string; label: string };
export function applicationGroup(app: PlatformApplication, resources: readonly Api.ResourceEntry[]): ApplicationGroup | undefined {
  if (app.category !== "business") return undefined;
  if (app.managedBy) return { id: `product:${app.managedBy}`, label: app.owningProjectTitle || app.managedBy };
  if (app.owningProject) return { id: `project:${app.owningProject}`, label: app.owningProjectTitle || app.owningProject };
  const [owner, name] = app.id.split(":");
  const resource = resources.find(item => item.id === app.rid)
    ?? resources.find(item => item.ref.kind === "app" && item.ref.app === owner && item.ref.name === name);
  if (!resource) return undefined;
  const project = resources.find(item => item.ref.kind === "app" && item.project === resource.project);
  const label = resource.managedBy ? project?.title || resource.managedBy : project?.title || resource.path[0] || resource.project;
  return resource.managedBy ? { id: `product:${resource.managedBy}`, label } : { id: `project:${resource.project}`, label };
}
