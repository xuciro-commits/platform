// Applications a tenant hands to its people. One of them is not a
// code package: it is a name, an icon and pages from the host's definition
// registry, which the workspace turns into an ordinary entry in the launcher
// and the navigation — beside the apps that came as code, with the same shell.
import type { AppUI } from "@platform/app";
import type { Api } from "@platform/kernel";
import { t, type NavSection, type Route } from "@platform/ui";
import { BarChart3, LayoutDashboard } from "lucide-react";
import { IconGlyph } from "@platform/ui";

type Definition = Api.Definition;
export { pageApplication } from "./host/applications";

/**
 * The applications this member may open, as the workspace's own app entries.
 * The host has already left out the pages they may not open and the
 * applications that would be empty; nothing here decides who sees what.
 */
export function tenantApps(definitions: Definition[], context?: { application?: string; instance?: string }): AppUI[] {
  return definitions.flatMap((definition) => {
    const application = definition.application;
    if (definition.ref.kind !== "app" || !application || definition.source === "code" || application.extends) return [];
    const pages = new Map(definitions.filter((d) => d.ref.kind === "page" && d.ref.app === definition.ref.app).map((d) => [d.ref.name, d.page]));
    const held = application.pages.filter((name) => pages.has(name));
    if (held.length === 0 && !application.resources?.length) return [];
    const icon = <IconGlyph name={application.icon} />; // the kit's vocabulary
    const id = `${definition.ref.app}:${application.name}`;
    const route = (name: string): Route => ({ view: "page", params: { app: definition.ref.app, kind: "page", name, application: id, ...(context?.application === id && context.instance ? { instance: context.instance } : {}) } });
    const item = (name: string) => ({ label: pages.get(name)?.title ?? name, route: route(name) });
    // The original application groups become the shell's expandable menus.
    // Unassigned pages retain their original order and direct entry.
    const groups = (application.groups ?? []).map((g) => ({ label: g.title, pages: g.pages.filter((name) => held.includes(name)) }))
      .filter((g) => g.pages.length > 0);
    const grouped = new Set(groups.flatMap((g) => g.pages));
    const ungrouped = held.filter((name) => !grouped.has(name));
    const menu = [...ungrouped.map(item), ...groups.map(group => ({ label: group.label, items: group.pages.map(item) }))];
    const firstPage = ungrouped[0] ?? groups[0]?.pages[0];
    const operations = { app: definition.ref.app, name: application.name, application: id };
    const overview: Route = { view: "application-overview", params: operations };
    const nav = (): NavSection[] => [
      ...(menu.length ? [{ label: application.title, items: menu }] : []),
      { label: t("Run and diagnose"), items: [
        ...(!held.length ? [{ label: t("Application overview"), icon: <LayoutDashboard />, route: overview }] : []),
        { label: t("Application runs"), icon: <BarChart3 />, route: { view: "application-runs", params: operations } },
      ] },
    ];
    return [{
      id,
      title: application.title,
      description: application.description,
      category: held.length ? "business" : "operate",
      icon,
      home: firstPage ? route(firstPage) : overview,
      views: [], // its pages are the workspace's own page view
      nav,
    }];
  });
}
