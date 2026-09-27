// Applications a tenant hands to its people (ADR-0036). One of them is not a
// code package: it is a name, an icon and pages from the host's definition
// registry, which the workspace turns into an ordinary entry in the launcher
// and the navigation — beside the apps that came as code, with the same shell.
import type { AppUI } from "@platform/app";
import type { Api } from "@platform/kernel";
import type { NavSection } from "@platform/ui";
import { BarChart3, Boxes, CalendarDays, ClipboardList, Map as MapPin, Sparkles, Users, Wrench } from "lucide-react";
import type { ReactNode } from "react";

type Definition = Api.Definition;

/** The icons a tenant may choose (ADR-0036 D3), drawn by the kit's set. */
const icons: Record<string, ReactNode> = {
  boxes: <Boxes />, clipboard: <ClipboardList />, people: <Users />, calendar: <CalendarDays />,
  wrench: <Wrench />, map: <MapPin />, chart: <BarChart3 />, sparkles: <Sparkles />,
};

/** A page of the builder app, by its name in the registry. */
const route = (name: string) => ({ view: "page", params: { app: "build", kind: "page", name } });

/**
 * The applications this member may open, as the workspace's own app entries.
 * The host has already left out the pages they may not open and the
 * applications that would be empty; nothing here decides who sees what.
 */
export function tenantApps(definitions: Definition[]): AppUI[] {
  const pages = new Map(definitions.filter((d) => d.ref.kind === "page").map((d) => [d.ref.name, d.page]));
  return definitions.flatMap((definition) => {
    const application = definition.application;
    if (definition.ref.kind !== "app" || !application) return [];
    const held = application.pages.filter((name) => pages.has(name));
    if (held.length === 0) return [];
    const icon = icons[application.icon ?? ""] ?? <Boxes />;
    const item = (name: string) => ({ label: pages.get(name)?.title ?? name, icon, route: route(name) });
    // Its pages under their headings (17b): those in no group first, under the
    // application's own name, then each group in the order the builder chose.
    const groups = (application.groups ?? []).map((g) => ({ label: g.title, pages: g.pages.filter((name) => held.includes(name)) }))
      .filter((g) => g.pages.length > 0);
    const grouped = new Set(groups.flatMap((g) => g.pages));
    const sections = [{ label: application.title, pages: held.filter((name) => !grouped.has(name)) }, ...groups].filter((g) => g.pages.length > 0);
    const nav = (): NavSection[] => sections.map((g) => ({ label: g.label, items: g.pages.map(item) }));
    return [{
      id: `${definition.ref.app}:${application.name}`,
      title: application.title,
      icon,
      home: route(sections[0]!.pages[0]!), // the first page people see in its navigation
      views: [], // its pages are the workspace's own page view (ADR-0032)
      nav,
    }];
  });
}
