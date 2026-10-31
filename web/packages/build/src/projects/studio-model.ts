import type {Definition as WorkspaceDefinition} from "@platform/app";
import type { Api } from "@platform/kernel";
import type { Route } from "@platform/ui";
import { refKey, resourceKinds, resourceRoute } from "./resources";
import type { OwnedResource } from "./resource-inventory";
import { installedStudioRef } from "./studio-applications";

type StudioLink = { title: string; view: string; params?: Record<string, string> };
type StudioGroup = { id: string; title: string; kinds: readonly string[]; links?: readonly StudioLink[]; contextKinds?: readonly string[] };

const domains = [
  { id: "model", title: "Model", description: "Objects, fields, relationships and reusable queries", groups: [
    { id: "business-model", title: "Business model", kinds: ["object"], links: [{ title: "Discover", view: "discover" }] },
    { id: "relationships", title: "Relationships and shared properties", kinds: ["link-type", "property-type", "value-type", "interface"] },
    { id: "queries", title: "Queries", kinds: ["query"] },
  ] },
  { id: "logic", title: "Behavior", description: "States, actions, flows, computation and intelligence", groups: [
    { id: "actions", title: "Object behavior", kinds: ["action"], contextKinds: ["object"], links: [{ title: "Object behavior", view: "action-type" }] },
    { id: "automation", title: "Flows and automation", kinds: ["flow"] },
    { id: "computation", title: "Functions and decisions", kinds: ["compute", "function", "agent", "alert"] },
  ] },
  { id: "interface", title: "Interface", description: "Pages, templates, application header and navigation", groups: [
    { id: "pages", title: "Pages", kinds: ["page"], links: [{ title: "Templates", view: "studio-templates" }] },
  ] },
  { id: "integration", title: "Connections", description: "Connections, sources, observations and data delivery", groups: [
    { id: "data", title: "Connections and data", kinds: ["connection", "source", "dataset", "pipeline", "matching", "writeback"] },
    { id: "observations", title: "Observations and protocols", kinds: ["observation", "protocol"], links: [{ title: "Integration health", view: "integration-health" }] },
  ] },
  { id: "release", title: "Release", description: "Samples, evaluation, candidates and delivery", groups: [
    { id: "tests", title: "Tests and evaluation", kinds: ["sample-set", "test-plan", "evaluation-set"] },
    { id: "delivery", title: "Release", kinds: [], links: [{ title: "Branches and proposals", view: "changes" }, { title: "Release review", view: "release-review" }, { title: "Release history", view: "release-history" }] },
  ] },
] as const;
export type StudioArea = typeof domains[number]["id"];
export const studioAreas = domains.map(area => ({ ...area, groups: area.groups as readonly StudioGroup[], kinds: [...new Set((area.groups as readonly StudioGroup[]).flatMap(group => [...group.kinds]))] as readonly string[], contextKinds: [...new Set((area.groups as readonly StudioGroup[]).flatMap(group => [...group.contextKinds ?? []]))] as readonly string[] }));
const areaForKind = (kind?: string): StudioArea => kind === "action" ? "logic" : studioAreas.find(area => area.kinds.includes(kind ?? ""))?.id ?? "integration";

export function studioArea(value?: string, kind?: string): StudioArea {
  if (value === "behavior") return "logic";
  if (value === "connections") return "integration";
  if (value === "verify") return "release";
  if (value === "assemble") return kind ? areaForKind(kind) : "integration";
  return studioAreas.find(area => area.id === value)?.id ?? "model";
}
export function studioAreaForRoute(route?: Route): StudioArea {
  const params = route?.params;
  if (route?.view === "project") return studioArea(params?.area, params?.asset?.split("/")[1]);
  if (route?.view === "object-type" && (params?.action || ["actions", "lifecycle"].includes(params?.tab ?? ""))) return "logic";
  if (route?.view === "object-type" && ["constraints", "versions", "protocols", "sets", "permissions"].includes(params?.tab ?? "")) return "model";
  for (const area of studioAreas) {
    if (area.groups.some(group => group.links?.some(link => link.view === route?.view))) return area.id;
    if (resourceKinds.some(kind => kind.view === route?.view && area.kinds.includes(kind.ref ?? kind.kind))) return area.id;
  }
  if (route?.view === "runs") return "logic";
  if (route?.view === "page-preview" || params?.kind === "page") return "interface";
  if (["definition", "observations"].includes(route?.view ?? "")) return areaForKind(route?.view === "observations" ? "observation" : params?.kind);
  return "model";
}
export const studioApplicationID = (route?: Route) => route?.view === "project" || route?.view === "module" && route.params?.id ? route.params?.id : route?.params?.application;
export const studioRoute = (id: string, area: StudioArea, asset?: string): Route => ({ view: "project", params: { id, area, ...(asset ? { asset } : {}) } });


function resourceArea(item: OwnedResource): StudioArea {
  return areaForKind(item.ref.kind);
}

export function resourceInArea(item: OwnedResource, area: StudioArea): boolean {
  return resourceArea(item) === area || (studioAreas.find(domain => domain.id === area)?.contextKinds ?? []).includes(item.ref.kind);
}

export function conceptTree(application: Pick<Api.Application, "title" | "name" | "concepts">, resources: readonly OwnedResource[]) {
  const groups: { id: string; title: string; items: OwnedResource[] }[] = (application.concepts ?? []).map(group => ({ id: `concept:${group.id}`, title: group.title, items: [] }));
  for (const item of resources) {
    const explicit = application.concepts?.find(group => group.assets.some(ref => refKey(ref) === refKey(item.ref)));
    const local = item.ref.name.startsWith(`${item.ref.app}.`) ? item.ref.name.slice(item.ref.app.length + 1) : item.ref.name;
    const prefix = local.match(/^(.+?)[._-]/)?.[1];
    const id = explicit ? `concept:${explicit.id}` : `namespace:${item.ref.app}:${prefix ?? ""}`;
    let group = groups.find(group => group.id === id);
    if (!group) { group = { id, title: prefix ?? (item.ref.app === "build" ? application.title || application.name : item.ref.app), items: [] }; groups.push(group); }
    group.items.push(item);
  }
  return groups;
}

export type StudioResourceGroup = { id: string; title: string; items: OwnedResource[] };
export function studioResourceGroups(application: Pick<Api.Application, "title" | "name" | "concepts">, area: StudioArea, resources: readonly OwnedResource[]): StudioResourceGroup[] {
  const groups = studioAreas.find(domain => domain.id === area)!.groups.map(group => ({ ...group, items: [] as OwnedResource[] }));
  const custom = (application.concepts ?? []).map(group => ({ id: `concept:${group.id}`, title: group.title, items: [] as OwnedResource[] }));
  const assigned = new Set<string>();
  for (const concept of application.concepts ?? []) {
    const group = custom.find(item => item.id === `concept:${concept.id}`)!;
    for (const ref of concept.assets) {
      const item = resources.find(resource => refKey(resource.ref) === refKey(ref));
      if (item) { group.items.push(item); assigned.add(refKey(item.ref)); }
    }
  }
  for (const item of resources) {
    if (assigned.has(refKey(item.ref))) continue;
    const group = groups.find(candidate => candidate.kinds.includes(item.kind.kind) || candidate.kinds.includes(item.kind.ref ?? "") || candidate.contextKinds?.includes(item.ref.kind));
    (group?.items ?? groups[0]?.items ?? []).push(item);
  }
  return [...custom, ...groups].filter(group => group.items.length).map(group => ({ id: group.id, title: group.title, items: [...group.items].sort((a, b) => a.title.localeCompare(b.title) || refKey(a.ref).localeCompare(refKey(b.ref))) }));
}


export function moveConceptAsset(concepts: readonly Api.AppConcept[], ref: Api.AssetRef, id: string) {
  return concepts.map(group => ({ ...group, assets: [
    ...group.assets.filter(asset => refKey(asset) !== refKey(ref)), ...(group.id === id ? [ref] : []),
  ] }));
}

export function assetRoute(ref: Api.AssetRef, resources: readonly OwnedResource[], definitions: readonly WorkspaceDefinition[], application: string): Route {
  const resource = resources.find(item => refKey(item.ref) === refKey(ref));
  if (resource?.record) return resourceRoute(resource.kind, resource.record.id, application);
  if (ref.kind === "action") {
    const action = definitions.find(definition => refKey(definition.ref) === refKey(ref));
    const object = resources.find(item => item.ref.kind === "object" && item.ref.name === action?.action?.target);
    const name = object && ref.name.slice(object.ref.name.length + 1);
    if (object?.record?.actions?.some(action => action.name === name)) return { view: "action-type", params: { id: object.record.id, action: name!, application } };
  }
  if (ref.kind === "observation") return { view: "observations", params: { app: ref.app, name: ref.name, application } };
  if (installedStudioRef(application)) return { view: "definition", params: { ...ref, surface: "studio", application } };
  if (ref.kind === "object") return { view: "object-type", params: { object: ref.name, application } };
  return { view: "definition", params: { ...ref, surface: "studio", application } };
}
