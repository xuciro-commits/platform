import { createContext, useCallback, useContext, useMemo, useRef, type ReactNode } from "react";
import { t } from "../i18n";
import { navigationLinks, type NavSection } from "./Navigation";
import { routeKey, type Route } from "./route";
import type { Applications, OpenOptions, RecentEntry, ShellCommand } from "./WorkspaceContext";

export const searchScopes = [">", "@", "#", ":"] as const;
export const searchKindOrder = ["command", "current", "application", "resource", "object", "member", "knowledge", "recent"] as const;

export type SearchScope = typeof searchScopes[number];
export type SearchKind = typeof searchKindOrder[number];
export type SearchWindow = NonNullable<OpenOptions["window"]>;
export type SearchSource = (text: string, scope?: SearchScope) => Promise<SearchResult[]>;

type SearchBase = {
  id: string;
  label: string;
  detail?: string;
  icon?: ReactNode;
  disabled?: string;
};

export type SearchResult =
  | SearchBase & { kind: "command"; run: () => void }
  | SearchBase & { kind: Exclude<SearchKind, "command">; route: Route };

export type SearchGroup = {
  kind: SearchKind;
  label: string;
  items: SearchResult[];
  truncated: boolean;
};

export type SearchAnswer = {
  groups: SearchGroup[];
  errors: string[];
};

export type SearchApi = {
  search: (text: string) => Promise<SearchAnswer>;
  open: (result: SearchResult, window?: SearchWindow) => void;
  ready: (text: string) => boolean;
};

type SearchOptions = {
  commands: ShellCommand[];
  currentApp?: string;
  nav: NavSection[];
  applications?: Pick<Applications, "apps" | "categories">;
  recent: RecentEntry[];
  backend?: SearchSource;
  knowledge?: SearchSource;
  open: (route: Route, options?: OpenOptions) => void;
};

const limits: Record<SearchKind, number> = {
  command: 12,
  current: 12,
  application: 12,
  resource: 12,
  object: 12,
  member: 12,
  knowledge: 8,
  recent: 8,
};

export function searchTerm(text: string): { scope?: SearchScope; term: string } {
  const trimmed = text.trimStart();
  const scope = searchScopes.find(value => trimmed.startsWith(value));
  return { scope, term: (scope ? trimmed.slice(1) : trimmed).trim() };
}

export function searchReady(text: string): boolean {
  const { scope, term } = searchTerm(text);
  if (!term) return false;
  return !!scope || term.length >= 2;
}

function matches(term: string, ...values: (string | undefined)[]): boolean {
  const needle = term.toLocaleLowerCase();
  return values.some(value => value?.toLocaleLowerCase().includes(needle));
}

function labelOf(kind: SearchKind, currentApp?: string): string {
  switch (kind) {
    case "command": return t("Commands");
    case "current": return currentApp || t("Current app");
    case "application": return t("Applications");
    case "resource": return t("Resources");
    case "object": return t("Objects");
    case "member": return t("Members");
    case "knowledge": return t("Knowledge");
    case "recent": return t("Recent");
  }
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function useSearchController(options: SearchOptions): SearchApi {
  const latest = useRef(options);
  latest.current = options;

  const search = useCallback(async (text: string): Promise<SearchAnswer> => {
    const current = latest.current;
    const { scope, term } = searchTerm(text);
    if (text.trim() === "" && !scope) {
      const recent = current.recent.map(entry => ({
        kind: "recent" as const,
        id: `recent:${routeKey(entry.route)}`,
        label: entry.title,
        detail: entry.app ? current.applications?.apps.find(app => app.id === entry.app)?.title : undefined,
        route: entry.route,
      }));
      return { groups: groupResults(recent, current.currentApp), errors: [] };
    }
    if (scope === ">" && !term) {
      const commands = current.commands.map(command => commandResult(command));
      return { groups: groupResults(commands, current.currentApp), errors: [] };
    }
    if (!searchReady(text)) return { groups: [], errors: [] };

    const local: SearchResult[] = [];
    if (!scope || scope === ">") {
      local.push(...current.commands.filter(command => matches(term, command.label, command.group, command.id)).map(commandResult));
    }
    if (!scope && current.currentApp) {
      local.push(...current.nav.flatMap(section => navigationLinks(section.items)
        .filter(item => matches(term, item.label, section.label))
        .map(item => ({
          kind: "current" as const,
          id: `current:${routeKey(item.route)}:${section.label}`,
          label: item.label,
          detail: section.label,
          icon: item.icon,
          route: item.route,
        }))));
    }
    if (!scope && current.applications) {
      local.push(...current.applications.apps
        .filter(app => matches(term, app.title, app.description, app.id, current.applications!.categories.find(category => category.id === app.category)?.label))
        .map(app => ({
          kind: "application" as const,
          id: `application:${app.id}`,
          label: app.title,
          detail: current.applications!.categories.find(category => category.id === app.category)?.label,
          icon: app.icon,
          route: app.home,
        })));
    }

    const requests: Promise<SearchResult[]>[] = [];
    if (scope !== ">" && current.backend) requests.push(current.backend(term, scope));
    if (!scope && current.knowledge) requests.push(current.knowledge(term));
    const remote = await Promise.allSettled(requests);
    const errors: string[] = [];
    const found: SearchResult[] = [...local];
    for (const result of remote) {
      if (result.status === "fulfilled") {
        found.push(...result.value.filter(value =>
          scope !== "@" || value.kind === "member"
        ).filter(value =>
          scope !== "#" || value.kind === "resource"
        ).filter(value =>
          scope !== ":" || value.kind === "object"
        ));
      }
      else errors.push(errorMessage(result.reason));
    }
    return { groups: groupResults(found, current.currentApp), errors };
  }, []);

  const open = useCallback((result: SearchResult, window?: SearchWindow) => {
    if (result.disabled) return;
    if (result.kind === "command") { result.run(); return; }
    latest.current.open(result.route, window ? { window } : undefined);
  }, []);

  return useMemo(() => ({ search, open, ready: searchReady }), [search, open]);
}

function commandResult(command: ShellCommand): SearchResult {
  return {
    kind: "command",
    id: `command:${command.id}`,
    label: command.label,
    detail: command.shortcut ?? command.group,
    run: command.run,
  };
}

function groupResults(results: SearchResult[], currentApp?: string): SearchGroup[] {
  return searchKindOrder.flatMap(kind => {
    const all = results.filter(result => result.kind === kind);
    if (!all.length) return [];
    const items = all.slice(0, limits[kind]);
    return [{ kind, label: labelOf(kind, currentApp), items, truncated: all.length > items.length }];
  });
}

export const SearchContext = createContext<SearchApi | null>(null);

export function useSearch(): SearchApi {
  const search = useContext(SearchContext);
  if (!search) throw new Error("useSearch outside the workspace search provider");
  return search;
}
