import type { AppUI, Host } from "@platform/app";
import type { NavSection } from "@platform/ui";

/** Trusted UI packages can contribute to one functional app without adding
 * another portal entry or changing the backend that owns its resources. */
export type ComposedApplication = AppUI & { homeFor: (host: Host) => AppUI["home"] };
export function composeApplications(contributions: readonly AppUI[]): ComposedApplication[] {
  const groups = new Map<string, AppUI[]>();
  for (const app of contributions) groups.set(app.id, [...groups.get(app.id) ?? [], app]);
  return [...groups.values()].map<ComposedApplication>(sources => {
    const primary = sources[0]!;
    return {
      ...primary,
      serves: [...new Set(sources.flatMap(app => app.serves ?? [app.id]))],
      views: sources.flatMap(app => app.views),
      opens: Object.assign({}, ...sources.map(app => app.opens)),
      homeFor: member => (sources.find(app => !app.for || app.for(member)) ?? primary).home,
      for: member => sources.some(app => !app.for || app.for(member)),
      nav: (member, route) => {
        const sections = new Map<string, NavSection>();
        for (const app of sources) {
          if (app.for && !app.for(member)) continue;
          for (const section of app.nav(member, route)) {
            const previous = sections.get(section.label);
            sections.set(section.label, { ...section, items: [...previous?.items ?? [], ...section.items] });
          }
        }
        return [...sections.values()];
      },
    };
  });
}
