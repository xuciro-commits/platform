import type {Definition as WorkspaceDefinition} from "@platform/app";
import type { Api } from "@platform/api";
import type { Route } from "@platform/ui";
import { draftResourcePresentations, releaseResourcePresentations, type ReleaseKind as GeneratedReleaseKind, type ResourceKind as GeneratedResourceKind, type ResourceRouteMode } from "../gen/resources";

export type ResourceKind = GeneratedResourceKind | "definition";
export type ReleaseKind = GeneratedReleaseKind;

export type ResourceKindInfo = {
  kind: ResourceKind; type: string; view: string;
  label: string; plural: string; ref?: string; assetKind?: string;
  app?: string; qualifiedName?: boolean; route?: ResourceRouteMode;
  projectView?: string; releaseKind?: string; release?: boolean; listed?: boolean;
};

export const resourceKinds: ResourceKindInfo[] = draftResourcePresentations.map((resource) => ({ ...resource }));
export const releaseResources: ResourceKindInfo[] = releaseResourcePresentations.map((resource) => ({ ...resource }));

type ResourceVariant = { kind?: string; variant?: string; evaluation?: unknown[]; roots?: Api.AssetRef[] };

export const matchesResourceKind = (kind: ResourceKindInfo, record: ResourceVariant) => {
  if (kind.kind === "automation") return record.kind === "automation";
  if (kind.kind === "flow") return record.kind !== "automation";
  if (kind.kind === "evaluation-set") return record.variant === "evaluation-set" || !record.variant && !!record.evaluation?.length;
  return kind.kind !== "test-plan" || record.variant === "test-plan" || !record.variant && !record.evaluation?.length && !!record.roots?.length;
};

export const kindOfType = (type: string, record?: unknown) => {
  const candidates = resourceKinds.filter(kind => kind.type === type);
  return record ? candidates.find(kind => matchesResourceKind(kind, record as ResourceVariant)) ?? candidates[0] : candidates[0];
};

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
  const app = kind.app ?? "build";
  return { app, kind: kind.ref ?? kind.kind, name: kind.qualifiedName ? `${app}.${name}` : name };
}
export const refKey = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;

export function resourceRoute(kind: ResourceKindInfo, id: string, project?: string): Route {
  if (kind.route === "page") return { view: kind.view, params: { ...(project ? { id: project, application: project } : {}), page: id } };
  if (kind.route === "release-plan" && project) return { view: kind.projectView ?? "project", params: { id: project, area: "release", plan: id } };
  return { view: kind.view, params: { id, ...(project ? { application: project } : {}) } };
}

export type ResourceRecord = { id: string; name: string; title: string; state?: string; version?: number; archived?: boolean; object?: string; kind?: string; variant?: string; actions?: { name: string; title?: string }[]; when?: string; manual?: boolean; evaluation?: unknown[]; roots?: Api.AssetRef[]; changed?: { at?: string; by?: string } | string };
