import type { SerializedDockview } from "dockview-react";
import { routeKey, type Route } from "./route";

type Node = SerializedDockview["grid"]["root"];
type Group = Exclude<Node["data"], Node[]>;

/** Rewrite route identities and their group references together. Used for
 * incoming aliases and for app-only persistence; never mutate Dockview's live
 * layout or keep a second editor/draft store. */
export function rewriteLayout(layout: SerializedDockview, map: (route: Route, panel: string) => Route | undefined, keyOf: (route: Route) => string = routeKey): SerializedDockview {
  const panels: SerializedDockview["panels"] = Object.create(null);
  const keys = new Map<string, string>();
  for (const [id, panel] of Object.entries(layout.panels)) {
    const original = panel.params?.route as Route | undefined;
    const route = original && map(original, id);
    if (!route) continue;
    const key = keyOf(route);
    keys.set(id, key);
    panels[key] ??= { ...panel, id: key, params: { ...panel.params, route } };
  }
  const placed = new Set<string>(), groups = new Set<string>();
  const group = (data: Group): Group | undefined => {
    const views = data.views.flatMap((id) => {
      const key = keys.get(id);
      if (!key || placed.has(key)) return [];
      placed.add(key); return [key];
    });
    if (!views.length) return undefined;
    groups.add(data.id);
    const active = data.activeView && keys.get(data.activeView);
    return { ...data, views, activeView: active && views.includes(active) ? active : views[0] };
  };
  const node = (data: Node): Node | undefined => {
    if (data.type === "leaf") {
      const mapped = group(data.data as Group);
      return mapped ? { ...data, data: mapped } : undefined;
    }
    const children = (data.data as Node[]).flatMap((child) => { const mapped = node(child); return mapped ? [mapped] : []; });
    return children.length ? { ...data, data: children } : undefined;
  };
  let root = node(layout.grid.root) ?? { type: "branch" as const, data: [] };
  // Floating is retired (ADR-0018 D1). Keep its old tabs readable, but
  // restore them as ordinary grid groups rather than resurrecting that UI.
  for (const entry of layout.floatingGroups ?? []) {
    const data = entry.data && group(entry.data);
    const restored = entry.grid ? node(entry.grid.root) : data ? { type: "leaf" as const, data } : undefined;
    if (restored) root = root.type === "branch"
      ? { ...root, data: [...root.data as Node[], restored] }
      : { type: "branch", data: [root, restored] };
  }
  const popoutGroups = layout.popoutGroups?.flatMap((entry) => {
    const data = entry.data && group(entry.data), grid = entry.grid && node(entry.grid.root);
    return data || grid ? [{ ...entry, data, grid: grid && entry.grid ? { ...entry.grid, root: grid } : undefined }] : [];
  });
  for (const key of Object.keys(panels)) if (!placed.has(key)) delete panels[key];
  return { ...layout, panels, grid: { ...layout.grid, root }, floatingGroups: undefined, popoutGroups,
    activeGroup: layout.activeGroup && groups.has(layout.activeGroup) ? layout.activeGroup : undefined };
}

/** Resolve only saved/incoming routes, not the canonical routes apps produce.
 * A failed compatibility read leaves the old tab and its refusal intact. */
export async function resolveLayout(layout: SerializedDockview, resolve: (route: Route) => Route | Promise<Route>, exists: (view: string) => boolean, keyOf: (route: Route) => string = routeKey): Promise<SerializedDockview> {
  const routes = new Map<string, Route>();
  await Promise.all(Object.entries(layout.panels).map(async ([id, panel]) => {
    const original = panel.params?.route as Route | undefined;
    if (!original) return;
    let route = original;
    try { route = await resolve(original); } catch { /* preserve an unavailable original route */ }
    if (exists(route.view)) routes.set(id, route);
  }));
  return rewriteLayout(layout, (_route, id) => routes.get(id), keyOf);
}
