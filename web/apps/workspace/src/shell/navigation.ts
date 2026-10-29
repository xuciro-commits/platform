import type { Route } from "@platform/ui";

const sharedOwners: Record<string, string> = {
  explorer: "core", saved: "core", observations: "data-connection", lineage: "data-connection", assistant: "agents",
};

export function sharedViewApplication(route: Route): string | undefined {
  return sharedOwners[route.view];
}

export function routeApplication(route: Route, entries: { id: string; surface?: string }[], owner?: string, chosen?: string): string | undefined {
  const context = route.params?.surface && entries.find((entry) => entry.surface === route.params!.surface);
  const id = owner ?? (context ? context.id : undefined);
  return chosen && id !== chosen ? undefined : id;
}
