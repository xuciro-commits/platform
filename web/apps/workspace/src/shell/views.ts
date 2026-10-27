// Every view the workspace can open, and the parameters each one reads.
//
// This is the single declaration: a view that is not listed cannot be mounted,
// a parameter that is not listed cannot arrive from a link, and the retired
// aliases below can only redirect to a view declared here. Ownership says who
// renders it — the shell owns these views itself, an application package
// contributes the rest.
export type ViewDeclaration = { id: string; owner: "shell" | "build"; params: readonly string[] };

export const viewDeclarations: readonly ViewDeclaration[] = [
  // The shell's own views: home, the portal, records and the shared readers.
  { id: "home", owner: "shell", params: [] },
  { id: "portal", owner: "shell", params: ["surface", "workspace"] },
  { id: "saved", owner: "shell", params: ["id"] },
  { id: "dashboard", owner: "shell", params: ["app", "id"] },
  { id: "explorer", owner: "shell", params: ["type"] },
  { id: "observations", owner: "shell", params: ["app", "name", "record"] },
  { id: "lineage", owner: "shell", params: ["ref"] },
  { id: "records", owner: "shell", params: [] },
  { id: "definitions", owner: "shell", params: [] },
  { id: "definition", owner: "shell", params: ["app", "application", "kind", "name"] },
  { id: "page", owner: "shell", params: ["app", "kind", "name"] },
  { id: "page-preview", owner: "shell", params: ["app", "kind", "name"] },
  { id: "record", owner: "shell", params: ["id", "inspect", "type"] },
  { id: "application-overview", owner: "shell", params: ["app", "name"] },
  { id: "application-runs", owner: "shell", params: ["app", "name"] },
  { id: "run", owner: "shell", params: ["application", "id", "project"] },
  { id: "flow-instance", owner: "shell", params: ["application", "id", "project"] },
  { id: "operation-run", owner: "shell", params: ["application", "id", "project", "subject"] },
  { id: "assistant", owner: "shell", params: ["about"] },
  { id: "search", owner: "shell", params: [] },
  // Views the build application contributes; the shell still routes and
  // redirects into them, so their parameters are declared in the same place.
  { id: "projects", owner: "build", params: ["surface"] },
  { id: "project", owner: "build", params: ["id", "surface"] },
  { id: "module", owner: "build", params: ["application", "page"] },
  { id: "object-type", owner: "build", params: ["access", "action", "application", "field", "id", "object", "tab"] },
  { id: "flow", owner: "build", params: ["application", "id"] },
  { id: "catalog", owner: "build", params: ["expand", "id"] },
  { id: "changes", owner: "build", params: [] },
  { id: "function", owner: "build", params: ["id"] },
  { id: "automation-operations", owner: "build", params: ["tab"] },
];

const declared = new Map(viewDeclarations.map((view) => [view.id, view]));

export const declaredView = (id: string): ViewDeclaration | undefined => declared.get(id);

/** The parameters a view reads; an unknown view reads none. */
export const viewParams = (id: string): readonly string[] => declared.get(id)?.params ?? [];

/** Views the shell renders itself: a route to one of these keeps the current application. */
export const shellViews: readonly string[] = viewDeclarations.filter((view) => view.owner === "shell").map((view) => view.id);

/** The parameters of `names` that `target` declares. A link cannot invent one;
 * `scripts/architecture.py` rejects a name this view has not declared. */
export const keepParams = (target: string, names: readonly string[], params?: Record<string, string>): Record<string, string> => {
  const allowed = viewParams(target);
  return Object.fromEntries(names.filter((name) => allowed.includes(name)).flatMap((name) => params?.[name] ? [[name, params[name]!]] : []));
};
