// Old entry points: links written before the shell had an Applications portal,
// and saved tabs from before a view moved. They are decoded here, never
// produced. Every target names a view declared in ./views.ts and only the
// parameters that view declares; a name this file invents is a check failure.
import { HttpError } from "@platform/kernel";
import type { Route } from "@platform/ui";
import { keepParams } from "./views";
import type { Projection } from "./registry";

type Params = Record<string, string>;
const keep = (target: string, params: Params | undefined, ...names: string[]): Params => keepParams(target, names, params);

/** Routes that no longer exist, redirected to their replacement. */
const retired: Record<string, Route | ((params?: Params) => Route)> = {
  "tenant-overview": { view: "portal", params: { workspace: "admin" } },
  launcher: { view: "portal" },
  // Builder views: the Application editor became the Workshop module, pages open inside it,
  // the process editor is the object type, workflows are flows, candidate tests moved into the editors.
  applications: (params) => params?.surface === "studio" || !params?.surface ? { view: "projects", params: keep("projects", params, "surface") } : { view: "portal" },
  application: (params) => ({ view: "project", params: keep("project", params, "id", "surface") }),
  studio: { view: "projects" },
  pages: { view: "module" },
  compose: (params) => ({ view: "module", params: { ...keep("module", params, "application"), ...(params?.id ? { page: params.id } : {}) } }),
  model: (params) => ({ view: "object-type", params: keep("object-type", params, "object", "tab", "application") }),
  process: (params) => ({ view: "object-type", params: keep("object-type", params, "id", "field", "action", "access", "application") }),
  workflow: (params) => ({ view: "flow", params: keep("flow", params, "id", "application") }),
  "catalog-example": (params) => ({ view: "catalog", params: { ...keep("catalog", params, "id"), expand: "1" } }),
  governance: { view: "catalog" },
  "candidate-test": (params): Route => {
    if (params?.processId) return { view: "flow", params: { id: params.processId } };
    if (params?.functionId) return { view: "function", params: { id: params.functionId } };
    if (params?.objectId) return { view: "object-type", params: { id: params.objectId, tab: "preview" } };
    return { view: "changes" };
  },
};

export const legacyRoute = (route: Route): Route | undefined => {
  const target = Object.hasOwn(retired, route.view) ? retired[route.view] : undefined;
  return typeof target === "function" ? target(route.params) : target;
};

/** Only incoming links carry the retired surface projection. */
export const projectionOfSurface = (surface?: string): Projection | undefined =>
  surface === "studio" || surface === "developer" ? "build" : surface === "tenant" ? "admin" : surface === "work" ? "operations" : undefined;

/** Decode old links and saved tabs before any view is mounted. The two old
 * `flow` views named different record types: never guess from an ID's format.
 * Exact builder records win; a readable runtime record keeps its old identity.
 * The legacy automation list uses its explicit surface / saved application,
 * or the operator-only package scope, because it has no entity to resolve. */
export function resolveLegacyRoute(route: Route, context: {
  builder: boolean; application?: string; read: (type: string, id: string) => Promise<unknown>;
}): Route | Promise<Route> {
  const target = legacyRoute(route);
  if (target) return target;
  const params = route.params;
  if (route.view === "automation" && !params?.id && !params?.application && params?.surface !== "studio"
    && (params?.surface === "tenant" || context.application === "runs" || !context.builder)) {
    return { ...route, view: "automation-operations", params: keep("automation-operations", params, "tab") };
  }
  if (route.view !== "flow" || !params?.id || params.id === "new" || params.surface === "studio") return route;
  const id = params.id;
  return (async () => {
    const readableRecord = (value: unknown) => {
      if (!value || typeof value !== "object") return false;
      const record = (value as { record?: { id?: unknown } }).record;
      return !!record && record.id === id;
    };
    if (context.builder) {
      try { if (readableRecord(await context.read("build.process", id))) return route; }
      catch (error) { if (!HttpError.missing(error)) throw error; }
    }
    try {
      return readableRecord(await context.read("flow.instance", id)) ? { ...route, view: "flow-instance" } : route;
    } catch (error) {
      if (!HttpError.missing(error)) throw error;
      return route; // missing or unreadable records keep the original refusal
    }
  })();
}
