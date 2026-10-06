// Old entry points (ADR-0052 §6): routes written before the shell had an
// Applications portal still open. They are decoded here, never produced.
import type { Route } from "@platform/ui";
import { projectionOfSurface, type Projection } from "./registry";

/** The views the shell owns; opening one keeps the current application. */
export const shellViews = ["home", "portal", "inbox", "requests", "notifications", "outbox", "search", "assistant", "saved", "explorer", "lineage"];

type Params = Record<string, string>;
const keep = (params: Params | undefined, ...names: string[]): Params => Object.fromEntries(names.flatMap((name) => params?.[name] ? [[name, params[name]!]] : []));

/** Routes that no longer exist, redirected to their replacement (ADR-0052 §6; ADR-0053 §3 for the builder's). */
const retired: Record<string, Route | ((params?: Params) => Route)> = {
  "tenant-overview": { view: "portal", params: { workspace: "admin" } },
  launcher: { view: "portal" },
  // Builder views (ADR-0053): the Application editor became the Workshop module, pages open inside it,
  // the process editor is the object type, workflows are flows, candidate tests moved into the editors.
  applications: (params) => params?.surface === "studio" || !params?.surface ? { view: "projects", params: keep(params, "surface") } : { view: "portal" },
  application: (params) => ({ view: "project", params: keep(params, "id", "surface") }),
  studio: { view: "projects" },
  pages: { view: "module" },
  compose: (params) => ({ view: "module", params: { ...keep(params, "application"), ...(params?.id ? { page: params.id } : {}) } }),
  model: (params) => ({ view: "object-type", params: keep(params, "object", "tab", "application") }),
  process: (params) => ({ view: "object-type", params: keep(params, "id", "field", "action", "access", "application") }),
  workflow: (params) => ({ view: "flow", params: keep(params, "id", "application") }),
  "catalog-example": (params) => ({ view: "catalog", params: { ...keep(params, "id"), expand: "1" } }),
  governance: { view: "catalog" },
  "candidate-test": (params): Route => {
    if (params?.processId) return { view: "flow", params: { id: params.processId } };
    if (params?.functionId) return { view: "function", params: { id: params.functionId } };
    if (params?.objectId) return { view: "object-type", params: { id: params.objectId, tab: "preview" } };
    return { view: "changes" };
  },
};

export const legacyRoute = (route: Route): Route | undefined => {
  const target = retired[route.view];
  return typeof target === "function" ? target(route.params) : target;
};

/** The projection an old link asked for, by `surface` or by the retired view it named. */
export function legacyProjection(route?: Route): Projection | undefined {
  if (!route) return undefined;
  return projectionOfSurface(route.params?.surface);
}
