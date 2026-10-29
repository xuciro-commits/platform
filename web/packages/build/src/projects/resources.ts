import type {Definition as WorkspaceDefinition} from "@platform/app";
import type { Api } from "@platform/kernel";
import type { Route } from "@platform/ui";

export type ResourceKind = "object" | "action" | "link-type" | "property-type" | "query" | "page" | "flow" | "automation" | "function" | "evaluation-set" | "test-plan" | "sample-set" | "agent" | "compute" | "table" | "alert" | "connection" | "source" | "dataset" | "pipeline" | "writeback" | "matching" | "observation" | "definition";

export type ResourceKindInfo = {
  kind: ResourceKind; type: string; view: string;
  label: string; plural: string;   ref?: string;
};

export const resourceKinds: ResourceKindInfo[] = [
 {kind:"sample-set",type:"build.sample-set",view:"sample-set",label:"Sample set",plural:"Sample sets",ref:"sample-set"},
  { kind: "object", type: "build.object", view: "object-type", label: "Object type", plural: "Object types", ref: "object" },
  { kind: "link-type", type: "build.linktype", view: "link-type", label: "Link type", plural: "Link types", ref: "link-type" },
  { kind: "property-type", type: "build.propertytype", view: "property-type", label: "Shared property", plural: "Shared properties", ref: "property-type" },
  { kind: "query", type: "build.query", view: "query", label: "Query", plural: "Queries", ref: "query" },
  { kind: "page", type: "build.page", view: "module", label: "Page", plural: "Pages", ref: "page" },
  { kind: "flow", type: "build.process", view: "flow", label: "Logic flow", plural: "Logic flows", ref: "flow" },
  { kind: "automation", type: "build.process", view: "automation", label: "Automation", plural: "Automations", ref: "flow" },
  { kind: "function", type: "build.function", view: "function", label: "AI function", plural: "AI functions", ref: "function" },
  { kind:"evaluation-set",type:"build.testplan",view:"evaluation-set",label:"Evaluation set",plural:"Evaluation sets",ref:"evaluation-set" },
  { kind: "test-plan", type: "build.testplan", view: "test-plan", label: "Business test plan", plural: "Business test plans", ref: "test-plan" },
  { kind: "agent", type: "build.agent", view: "agent", label: "Agent", plural: "Agents", ref: "agent" },
  { kind: "compute", type: "build.code", view: "code", label: "Code function", plural: "Code functions", ref: "compute" },
  { kind: "table", type: "build.table", view: "decision-table", label: "Decision table", plural: "Decision tables", ref: "compute" },
  { kind: "alert", type: "build.alertrule", view: "alert", label: "Alert rule", plural: "Alert rules", ref: "alert" },
  { kind: "connection", type: "build.connection", view: "connection", label: "Connection", plural: "Connections" },
  { kind: "observation", type: "build.observation", view: "observation-source", label: "Observation source", plural: "Observation sources" },
  { kind: "source", type: "build.source", view: "data-source", label: "Data source", plural: "Data sources" },
  { kind: "dataset", type: "build.dataset", view: "dataset", label: "Dataset", plural: "Datasets" },
  { kind: "pipeline", type: "build.pipeline", view: "pipeline", label: "Pipeline", plural: "Pipelines" },
  { kind: "writeback", type: "build.writeback", view: "writeback", label: "Writeback", plural: "Writebacks" },
  { kind: "matching", type: "build.match", view: "match", label: "Matching rule", plural: "Matching rules" },
];

export const kindOfType = (type: string) => resourceKinds.find((kind) => kind.type === type);

export function systemDefinitionsForType(definitions: readonly WorkspaceDefinition[], type: string) {
  const kind = type === "build.evaluation-set" || type === "build.testplan"
    ? resourceKinds.find(kind => kind.kind === (type === "build.testplan" ? "test-plan" : "evaluation-set")) : kindOfType(type);
  if (!kind) return [];
  return definitions.filter(definition => definition.source === "code" && definition.ref.kind === (kind.ref ?? kind.kind)
    && (type !== "build.table" || !!definition.operation?.rules)
    && (type !== "build.code" || !!definition.operation && !definition.operation.rules));
}

export const publishedResourceKind: ResourceKindInfo = { kind: "definition", type: "", view: "definition", label: "Published resource", plural: "Published resources" };

export function resourceRef(kind: ResourceKindInfo, name: string): Api.AssetRef {
  return { app: "build", kind: kind.ref ?? kind.kind, name: ["object", "flow", "automation"].includes(kind.kind) ? `build.${name}` : name };
}
export const refKey = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;

export function resourceRoute(kind: ResourceKindInfo, id: string, project?: string): Route {
  if (kind.kind === "page") return { view: "module", params: { ...(project ? { id: project, application: project } : {}), page: id } };
  if (kind.kind === "test-plan" && project) return { view: "project", params: { id: project, area: "release", plan: id } };
  return { view: kind.view, params: { id, ...(project ? { application: project } : {}) } };
}

export type ResourceRecord = { id: string; name: string; title: string; state?: string; version?: number; archived?: boolean; object?: string; kind?: string; actions?: { name: string; title?: string }[]; when?: string; manual?: boolean; evaluation?: unknown[]; roots?: Api.AssetRef[]; changed?: { at?: string; by?: string } | string };

export const matchesResourceKind = (kind: ResourceKindInfo, record: { kind?: string; evaluation?: unknown[]; roots?: Api.AssetRef[] }) => {
  if (kind.kind === "automation") return record.kind === "automation";
  if (kind.kind === "flow") return record.kind !== "automation";
  return kind.kind === "evaluation-set" ? !!record.evaluation?.length : kind.kind !== "test-plan" || !record.evaluation?.length && !!record.roots?.length;
};
