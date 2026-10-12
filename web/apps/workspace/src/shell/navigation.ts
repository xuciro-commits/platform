import type { Route } from "@platform/ui";

/** Exact view ownership beats an old surface hint; an explicit app choice
 * stays in charge until its layout has produced a route belonging to it. */
export function routeApplication(route: Route, entries: { id: string; surface?: string }[], owner?: string, chosen?: string): string | undefined {
  const context = route.params?.surface && entries.find((entry) => entry.surface === route.params!.surface);
  const id = owner ?? (context ? context.id : undefined);
  return chosen && id !== chosen ? undefined : id;
}
