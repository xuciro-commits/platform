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
    const nav = (): NavSection[] => [{
      label: application.title,
      items: held.map((name) => ({ label: pages.get(name)?.title ?? name, icon: icons[application.icon ?? ""] ?? <Boxes />, route: route(name) })),
    }];
    return [{
      id: `${definition.ref.app}:${application.name}`,
      title: application.title,
      icon: icons[application.icon ?? ""] ?? <Boxes />,
      home: route(held[0]!),
      views: [], // its pages are the workspace's own page view (ADR-0032)
      nav,
    }];
  });
}
