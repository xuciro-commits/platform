// Old entry points (ADR-0052 §6): routes written before the shell had an
// Applications portal still open. They are decoded here, never produced.
import type { Route } from "@platform/ui";
import { projectionOfSurface, type Projection } from "./registry";

/** The views the shell owns; opening one keeps the current application. */
export const shellViews = ["home", "portal", "inbox", "requests", "notifications", "outbox", "search", "assistant", "saved", "explorer", "lineage"];

/** Routes that no longer exist, redirected to their replacement. */
const retired: Record<string, Route> = {
  "tenant-overview": { view: "portal", params: { workspace: "admin" } },
  launcher: { view: "portal" },
};

export const legacyRoute = (route: Route): Route | undefined => retired[route.view];

/** The projection an old link asked for, by `surface` or by the retired view it named. */
export function legacyProjection(route?: Route): Projection | undefined {
  if (!route) return undefined;
  return projectionOfSurface(route.params?.surface) ?? (["catalog", "catalog-example"].includes(route.view) && route.params?.mode === "builder" ? "build" : undefined);
}
