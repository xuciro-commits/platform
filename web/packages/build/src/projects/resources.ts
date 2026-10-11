// The resource kinds a project holds and the editor each opens (ADR-0055 §2).
// A project is the host's `Application` (the `build.app` record): `resources`
// are the Ontology, logic and function assets it owns; `pages`, `groups` and
// `header` are its interface. The groups below are the host's asset kinds,
// one to one — nothing here exists only in the builder.
import type { Api } from "@platform/kernel";
import type { Route } from "@platform/ui";

export type ResourceKind = "object" | "action" | "link-type" | "property-type" | "query" | "page" | "flow" | "automation" | "function" | "compute" | "connection" | "source" | "dataset" | "pipeline" | "writeback" | "matching";

export type ResourceKindInfo = {
  kind: ResourceKind; type: string; view: string; group: "ontology" | "interface" | "logic" | "functions" | "integration";
  label: string; plural: string; /** the AssetRef kind the host uses for this record type */ ref?: string;
};

export const resourceKinds: ResourceKindInfo[] = [
  { kind: "object", type: "build.object", view: "object-type", group: "ontology", label: "Object type", plural: "Object types", ref: "object" },
  { kind: "link-type", type: "build.linktype", view: "link-type", group: "ontology", label: "Link type", plural: "Link types", ref: "link-type" },
  { kind: "property-type", type: "build.propertytype", view: "property-type", group: "ontology", label: "Shared property", plural: "Shared properties", ref: "property-type" },
  { kind: "query", type: "build.query", view: "query", group: "ontology", label: "Query", plural: "Queries", ref: "query" },
  { kind: "page", type: "build.page", view: "module", group: "interface", label: "Page", plural: "Pages", ref: "page" },
  { kind: "flow", type: "build.process", view: "flow", group: "logic", label: "Logic flow", plural: "Logic flows", ref: "flow" },
  { kind: "function", type: "build.function", view: "function", group: "functions", label: "AI function", plural: "AI functions", ref: "function" },
  { kind: "compute", type: "build.code", view: "code", group: "functions", label: "Code function", plural: "Code functions", ref: "compute" },
  { kind: "connection", type: "build.connection", view: "connection", group: "integration", label: "Connection", plural: "Connections" },
  { kind: "source", type: "build.source", view: "data-source", group: "integration", label: "Data source", plural: "Data sources" },
  { kind: "dataset", type: "build.dataset", view: "dataset", group: "integration", label: "Dataset", plural: "Datasets" },
  { kind: "pipeline", type: "build.pipeline", view: "pipeline", group: "integration", label: "Pipeline", plural: "Pipelines" },
  { kind: "writeback", type: "build.writeback", view: "writeback", group: "integration", label: "Writeback", plural: "Writebacks" },
  { kind: "matching", type: "build.match", view: "match", group: "integration", label: "Matching rule", plural: "Matching rules" },
];

export const resourceGroups: { id: ResourceKindInfo["group"]; label: string }[] = [
  { id: "ontology", label: "Ontology" }, { id: "interface", label: "Interface" }, { id: "logic", label: "Logic" }, { id: "functions", label: "Functions" }, { id: "integration", label: "Integration" },
];

export const kindOfType = (type: string) => resourceKinds.find((kind) => kind.type === type);

/** Object and Flow identities include their authority prefix; the other Build names are local to their owner. */
export function resourceRef(kind: ResourceKindInfo, name: string): Api.AssetRef {
  return { app: "build", kind: kind.ref ?? kind.kind, name: kind.kind === "object" || kind.kind === "flow" ? `build.${name}` : name };
}
export const refKey = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;

/** The route that edits a resource record inside a project. */
export function resourceRoute(kind: ResourceKindInfo, id: string, project?: string): Route {
  if (kind.kind === "page") return { view: "module", params: { ...(project ? { id: project, application: project } : {}), page: id } };
  return { view: kind.view, params: { id, ...(project ? { application: project } : {}) } };
}

export type ResourceRecord = { id: string; name: string; title: string; state?: string; version?: number; archived?: boolean; object?: string; when?: string; manual?: boolean; changed?: { at?: string; by?: string } | string };
