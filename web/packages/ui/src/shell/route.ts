/** A route names one view and, through params, one entity: `{ view: "workOrder", params: { id: "WO-7" } }`. */
export type Route = { view: string; params?: Record<string, string> };

/** Stable identity of a route: the same entity always maps to the same tab. */
export function routeKey(route: Route): string {
  const params = Object.entries(route.params ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return params.length ? `${route.view}?${new URLSearchParams(params)}` : route.view;
}

export function routeToHash(route: Route): string {
  return `#/${routeKey(route)}`;
}

export function routeFromHash(hash: string): Route | undefined {
  const text = hash.replace(/^#\/?/, "");
  if (!text) return undefined;
  const [view, query] = text.split("?", 2) as [string, string | undefined];
  const params = Object.fromEntries(new URLSearchParams(query ?? ""));
  return Object.keys(params).length ? { view, params } : { view };
}
