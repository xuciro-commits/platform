// Old entry points (ADR-0052 §6): routes written before the shell had an
// Applications portal still open. They are decoded here, never produced.
import type { Route } from "@platform/ui";
import type { Projection } from "./registry";

/** The views the shell owns; opening one keeps the current application. */
export const shellViews = ["flow-instance", "home", "portal", "inbox", "requests", "notifications", "outbox", "search", "assistant", "saved", "explorer", "lineage", "observations"];

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
  const target = Object.hasOwn(retired, route.view) ? retired[route.view] : undefined;
  return typeof target === "function" ? target(route.params) : target;
};

/** The projection an old link asked for, by `surface` or by the retired view it named. */
export function legacyProjection(route?: Route): Projection | undefined {
  if (!route) return undefined;
  return projectionOfSurface(route.params?.surface);
}

/** Only incoming links carry the retired surface projection. */
export const projectionOfSurface = (surface?: string): Projection | undefined =>
  surface === "studio" || surface === "developer" ? "build" : surface === "tenant" ? "admin" : surface === "work" ? "operations" : undefined;

/** Decode old links and saved tabs before any view is mounted. The two old
 * `flow` views named different record types: never guess from an ID's format.
 * Exact builder records win; a readable runtime record keeps its old identity.
 * The legacy automation list uses its explicit surface / saved application,
 * or the operator-only package scope, because it has no entity to resolve. */
export async function resolveLegacyRoute(route: Route, context: {
  builder: boolean; application?: string; read: (type: string, id: string) => Promise<unknown>;
}): Promise<Route> {
  const retired = legacyRoute(route);
  if (retired) return retired;
  const params = route.params;
  if (route.view === "automation" && !params?.id && !params?.application && params?.surface !== "studio"
    && (params?.surface === "tenant" || context.application === "runs" || !context.builder)) {
    return { ...route, view: "automation-operations", params: keep(params, "tab") };
  }
  if (route.view !== "flow" || !params?.id || params.id === "new" || params.surface === "studio") return route;
  const readableRecord = (value: unknown) => {
    if (!value || typeof value !== "object") return false;
    const record = (value as { record?: { id?: unknown } }).record;
    return !!record && record.id === params.id;
  };
  if (context.builder) {
    try { if (readableRecord(await context.read("build.process", params.id))) return route; }
    catch (error) { if (!/HTTP (401|403|404)\b/.test(String(error))) throw error; }
  }
  try {
    return readableRecord(await context.read("flow.instance", params.id)) ? { ...route, view: "flow-instance" } : route;
  } catch (error) {
    if (!/HTTP (401|403|404)\b/.test(String(error))) throw error;
    return route; // missing or unreadable records keep the original refusal
  }
}
