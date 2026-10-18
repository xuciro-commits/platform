import type { Route } from "@platform/ui";

const sharedOwners: Record<string, string> = {
  explorer: "core", saved: "core", observations: "data-connection", lineage: "data-connection", assistant: "agents",
};

/** Shared renderers still have one primary application in the menu. */
export function sharedViewApplication(route: Route): string | undefined {
  return sharedOwners[route.view];
}

/** Exact view ownership beats an old surface hint; an explicit app choice
 * stays in charge until its layout has produced a route belonging to it. */
export function routeApplication(route: Route, entries: { id: string; surface?: string }[], owner?: string, chosen?: string): string | undefined {
  const context = route.params?.surface && entries.find((entry) => entry.surface === route.params!.surface);
  const id = owner ?? (context ? context.id : undefined);
  return chosen && id !== chosen ? undefined : id;
}
