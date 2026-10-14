// Original asset owners and editor routes. ADR-0108 organizes navigation
// by application business concepts; asset kinds remain compilation identities.
import type { Api } from "@platform/kernel";
import type { Route } from "@platform/ui";

export type ResourceKind = "object" | "action" | "link-type" | "property-type" | "query" | "page" | "flow" | "automation" | "function" | "evaluation-set" | "agent" | "compute" | "connection" | "source" | "dataset" | "pipeline" | "writeback" | "matching";

export type ResourceKindInfo = {
  kind: ResourceKind; type: string; view: string;
  label: string; plural: string; /** the AssetRef kind the host uses for this record type */ ref?: string;
};

export const resourceKinds: ResourceKindInfo[] = [
  { kind: "object", type: "build.object", view: "object-type", label: "Object type", plural: "Object types", ref: "object" },
  { kind: "link-type", type: "build.linktype", view: "link-type", label: "Link type", plural: "Link types", ref: "link-type" },
  { kind: "property-type", type: "build.propertytype", view: "property-type", label: "Shared property", plural: "Shared properties", ref: "property-type" },
  { kind: "query", type: "build.query", view: "query", label: "Query", plural: "Queries", ref: "query" },
  { kind: "page", type: "build.page", view: "module", label: "Page", plural: "Pages", ref: "page" },
  { kind: "flow", type: "build.process", view: "flow", label: "Logic flow", plural: "Logic flows", ref: "flow" },
  { kind: "function", type: "build.function", view: "function", label: "AI function", plural: "AI functions", ref: "function" },
  { kind:"evaluation-set",type:"build.testplan",view:"evaluation-set",label:"Evaluation set",plural:"Evaluation sets",ref:"evaluation-set" },
  { kind: "agent", type: "build.agent", view: "agent", label: "Agent", plural: "Agents", ref: "agent" },
  { kind: "compute", type: "build.code", view: "code", label: "Code function", plural: "Code functions", ref: "compute" },
  { kind: "connection", type: "build.connection", view: "connection", label: "Connection", plural: "Connections" },
  { kind: "source", type: "build.source", view: "data-source", label: "Data source", plural: "Data sources" },
  { kind: "dataset", type: "build.dataset", view: "dataset", label: "Dataset", plural: "Datasets" },
  { kind: "pipeline", type: "build.pipeline", view: "pipeline", label: "Pipeline", plural: "Pipelines" },
  { kind: "writeback", type: "build.writeback", view: "writeback", label: "Writeback", plural: "Writebacks" },
  { kind: "matching", type: "build.match", view: "match", label: "Matching rule", plural: "Matching rules" },
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

export type ResourceRecord = { id: string; name: string; title: string; state?: string; version?: number; archived?: boolean; object?: string; actions?: { name: string; title?: string }[]; when?: string; manual?: boolean; changed?: { at?: string; by?: string } | string };
