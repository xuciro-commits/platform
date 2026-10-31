import type { ApplicationUIInfo, AppUI, Definition, Host } from "@platform/app";
import type { NavSection, Route } from "@platform/ui";

export type ComposedApplication = AppUI & { homeFor: (host: Host) => Route };
export function composeApplications(contributions: readonly AppUI[]): ComposedApplication[] {
  const groups = new Map<string, AppUI[]>();
  for (const app of contributions) groups.set(app.id, [...groups.get(app.id) ?? [], app]);
  return [...groups.values()].map<ComposedApplication>(sources => {
    const primary = sources[0]!;
    return {
      ...primary,
      navigation: sources.find(app => app.navigation)?.navigation,
      referenceContext: sources.find(app => app.referenceContext)?.referenceContext,
      serves: [...new Set(sources.flatMap(app => app.serves ?? [app.id]))],
      views: sources.flatMap(app => app.views),
      opens: Object.assign({}, ...sources.map(app => app.opens)),
      homeFor: member => {
        const source = sources.find(app => !app.for || app.for(member)) ?? primary;
        return typeof source.home === "function" ? source.home(member) : source.home;
      },
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
        return [...sections.values()].filter(section => section.items.length > 0);
      },
    };
  });
}

type ApplicationDefinition = Definition & { application: NonNullable<Definition["application"]> };
type ApplicationEntry = Pick<AppUI, "id" | "serves">;
const applicationID = (definition: ApplicationDefinition) => `${definition.ref.app}:${definition.application.name}`;
const refKey = (ref: Definition["ref"]) => `${ref.app}/${ref.kind}/${ref.name}`;
const applicationDefinitions = (definitions: readonly Definition[]) => definitions.filter((definition): definition is ApplicationDefinition => definition.ref.kind === "app" && !!definition.application);

export function applicationEntry(definition: ApplicationDefinition, entries: readonly ApplicationEntry[]): string | undefined {
  const target = definition.application.extends;
  if (!target && definition.source !== "code") return applicationID(definition);
  const owner = target?.app ?? definition.ref.app;
  const candidates = entries.filter(entry => (entry.serves ?? [entry.id]).includes(owner));
  if (definition.application.entry) return candidates.find(entry => entry.id === definition.application.entry)?.id;
  return candidates.find(entry => entry.id === owner)?.id ?? (candidates.length === 1 ? candidates[0]!.id : undefined);
}

function publishedExtensions(definitions: readonly Definition[], entries: readonly ApplicationEntry[]) {
  const applications = applicationDefinitions(definitions);
  return applications.filter(definition => definition.source !== "code" && definition.application.extends
    && applications.some(native => native.source === "code" && refKey(native.ref) === refKey(definition.application.extends!)))
    .map(definition => ({ definition, entry: applicationEntry(definition, entries) }))
    .filter((item): item is { definition: ApplicationDefinition; entry: string } => !!item.entry)
    .sort((a, b) => refKey(a.definition.ref).localeCompare(refKey(b.definition.ref)));
}

function pageRoute(definition: ApplicationDefinition, page: string, params?: Route["params"]): Route {
  return { view: "page", params: { ...params, app: definition.ref.app, kind: "page", name: page, application: applicationID(definition) } };
}

function hasPage(definitions: readonly Definition[], definition: ApplicationDefinition, name: string) {
  return definition.application.pages.includes(name) && definitions.some(page => page.ref.app === definition.ref.app && page.ref.kind === "page" && page.ref.name === name && !!page.page);
}

function pageMembership(route: Route, definitions: readonly Definition[], entries: readonly ApplicationEntry[], current?: string) {
  const params = route.params;
  if (route.view === "application-runs" || route.view === "application-overview") {
    return applicationDefinitions(definitions).find(definition => definition.ref.app === params?.app && definition.application.name === params?.name
      && (!params?.application || params.application === applicationID(definition)));
  }
  if (route.view !== "page" || params?.kind !== "page") return undefined;
  const holders = applicationDefinitions(definitions).filter(definition => definition.ref.app === params.app && definition.application.pages.includes(params.name ?? ""));
  const explicit = holders.find(definition => params.application === applicationID(definition));
  if (explicit) return explicit;
  const selected = holders.filter(definition => applicationEntry(definition, entries) === (params.application ?? current));
  return selected.length === 1 ? selected[0] : holders.length === 1 ? holders[0] : undefined;
}

export function pageApplication(route: Route, definitions: readonly Definition[], current?: string, entries: readonly ApplicationEntry[] = []): string | undefined {
  const definition = pageMembership(route, definitions, entries, current);
  if (definition) return applicationEntry(definition, entries);
  if (route.view === "page" && route.params?.kind === "page" && !applicationDefinitions(definitions).some(definition => definition.ref.app === route.params?.app && definition.application.pages.includes(route.params?.name ?? ""))) return route.params.app;
  return undefined;
}

export function resolveApplicationRoute(route: Route, definitions: readonly Definition[], entries: readonly ApplicationUIInfo[], current?: string): Route {
  for (const { definition, entry } of publishedExtensions(definitions, entries)) {
    if (!replacementEntryView(entries, entry, route.view)) continue;
    const replacement = definition.application.replacements?.find(replacement => replacement.view === route.view && hasPage(definitions, definition, replacement.page));
    if (replacement) return pageRoute(definition, replacement.page, route.params);
  }
  const definition = pageMembership(route, definitions, entries, current);
  return route.view === "page" && definition && !route.params?.application ? pageRoute(definition, route.params?.name ?? "", route.params) : route;
}

function replacementEntryView(entries: readonly ApplicationUIInfo[], entry: string, view: string) {
  const application = entries.find(app => app.id === entry);
  if (!application || entries.some(app => app.id !== entry && app.views.some(declared => declared.id === view))) return false;
  const visible = (items: NavSection["items"]): boolean => items.some(item => "route" in item ? item.route.view === view : visible(item.items));
  return application.home.view === view || Object.values(application.opens).includes(view) || application.sections.some(section => visible(section.items));
}

export function extendApplications(entries: readonly AppUI[], definitions: readonly Definition[], metadata: readonly ApplicationUIInfo[], context?: { application?: string; instance?: string }): AppUI[] {
  const extensions = publishedExtensions(definitions, entries);
  const replaceItems = (items: NavSection["items"]): NavSection["items"] => items.map(item => "route" in item
    ? { ...item, route: resolveApplicationRoute(item.route, definitions, metadata) }
    : { ...item, items: replaceItems(item.items) });
  return entries.map(entry => {
    const attached = extensions.filter(extension => extension.entry === entry.id);
    if (!attached.length) return entry;
    return { ...entry, home: host => resolveApplicationRoute(typeof entry.home === "function" ? entry.home(host) : entry.home, definitions, metadata), nav: (host, route) => [
      ...entry.nav(host, route).map(section => ({ ...section, items: replaceItems(section.items) })),
      ...attached.flatMap(({ definition }) => {
        const application = definition.application;
        const replaced = new Set(application.replacements?.filter(replacement => replacementEntryView(metadata, entry.id, replacement.view)).map(replacement => replacement.page));
        const pages = application.pages.filter(name => !replaced.has(name) && hasPage(definitions, definition, name));
        if (!pages.length) return [];
        const groups = (application.groups ?? []).map(group => ({ ...group, pages: group.pages.filter(name => pages.includes(name)) })).filter(group => group.pages.length);
        const grouped = new Set(groups.flatMap(group => group.pages));
        const item = (name: string) => ({ label: definitions.find(page => page.ref.app === definition.ref.app && page.ref.kind === "page" && page.ref.name === name)?.page?.title ?? name,
          route: pageRoute(definition, name, context?.application === applicationID(definition) && context.instance ? { instance: context.instance } : undefined) });
        return [{ label: application.title, items: [...pages.filter(name => !grouped.has(name)).map(item), ...groups.map(group => ({ label: group.title, items: group.pages.map(item) }))] }];
      }),
    ] };
  });
}

export function applicationUIInfo(contributions: readonly AppUI[], host: Host): ApplicationUIInfo[] {
  return composeApplications(contributions.filter(app => !app.for || app.for(host))).map(app => ({
    id: app.id, title: app.title, serves: app.serves ?? [app.id], home: app.homeFor(host), sections: app.nav(host),
    views: app.views.map(view => ({ id: view.id, title: view.title({}) })), opens: { ...app.opens },
  }));
}
