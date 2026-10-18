import type { Api } from "@platform/kernel";
import type { Route } from "@platform/ui";
import { refKey, resourceRoute } from "./resources";
import type { OwnedResource } from "./resource-inventory";
import { installedStudioRef } from "./studio-applications";

export const studioAreas = [
  { id: "model", title: "Model", description: "Objects, fields, relationships, versions and constraints", kinds: ["object", "link-type", "property-type", "query", "interface"] },
  { id: "behavior", title: "Behavior", description: "States, actions, transactions, rules and approvals", kinds: ["object", "action", "compute", "alert"] },
  { id: "interface", title: "Interface", description: "Pages, templates, application header and navigation", kinds: ["page"] },
  { id: "assemble", title: "Assemble", description: "Data connections, flows, AI functions and agents", kinds: ["flow", "function", "agent", "compute", "connection", "source", "dataset", "pipeline", "writeback", "matching", "observation", "protocol"] },
  { id: "verify", title: "Verify", description: "Samples, roles, candidates, migrations and delivery", kinds: ["sample-set", "test-plan", "evaluation-set", "migration"] },
] as const;
export type StudioArea = typeof studioAreas[number]["id"];
export const studioArea = (value?: string): StudioArea => studioAreas.find(area => area.id === value)?.id ?? "model";
export const studioAreaKinds = (area: StudioArea): readonly string[] => studioAreas.find(item => item.id === area)?.kinds ?? [];
export function studioAreaForRoute(route?: Route): StudioArea {
  if (route?.view === "project") return studioArea(route.params?.area);
  if (["module", "studio-templates"].includes(route?.view ?? "")) return "interface";
  if (["action-type", "decision-table", "alert"].includes(route?.view ?? "")) return "behavior";
  if (["object-type", "link-type", "property-type", "query"].includes(route?.view ?? "")) return "model";
  if (["sample-set", "test-plan", "evaluation-set", "changes", "release-review", "release-history"].includes(route?.view ?? "")) return "verify";
  if (["page-preview"].includes(route?.view ?? "") || route?.params?.kind === "page") return "interface";
  if (["definition", "observations"].includes(route?.view ?? "")) {
    if (["object", "link-type", "property-type", "query"].includes(route?.params?.kind ?? "")) return "model";
    if (["action", "compute", "alert"].includes(route?.params?.kind ?? "")) return "behavior";
    return "assemble";
  }
  if (["connection", "data-source", "dataset", "pipeline", "writeback", "match", "observation-source", "observations", "agent", "function", "code", "flow", "automation"].includes(route?.view ?? "")) return "assemble";
  return "model";
}
export const studioApplicationID = (route?: Route) => route?.view === "project" || route?.view === "module" && route.params?.id ? route.params?.id : route?.params?.application;
export const studioRoute = (id: string, area: StudioArea, asset?: string): Route => ({ view: "project", params: { id, area, ...(asset ? { asset } : {}) } });

/** The tree and the editor's return bar describe the same original asset. */
export function studioContext(route: Route | undefined, resources: readonly OwnedResource[], definitions: readonly Api.Definition[]) {
  const params = route?.params;
  const definitionKind = route?.view === "observations" ? "observation" : params?.kind;
  const definition = ["definition", "page-preview", "observations"].includes(route?.view ?? "")
    ? definitions.find(item => item.ref.app === params?.app && item.ref.kind === definitionKind && item.ref.name === params?.name) : undefined;
  const recordID = route?.view === "project" ? params?.plan : route?.view === "module" ? params?.page : params?.id;
  const selected = resources.find(item => {
    if (params?.asset && refKey(item.ref) === params.asset || params?.object && item.ref.name === params.object) return true;
    if (definition && (refKey(item.ref) === refKey(definition.ref) || item.ref.kind === "object" && item.ref.name === definition.action?.target)) return true;
    if (!recordID || item.record?.id !== recordID) return false;
    return route?.view === item.kind.view
      || route?.view === "project" && item.kind.kind === "test-plan"
      || item.kind.type === "build.process" && ["flow", "automation"].includes(route?.view ?? "")
      || route?.view === "action-type" && item.ref.kind === "object"
      || route?.view === "release-review" && (params?.kind === item.kind.kind || params?.kind === item.ref.kind);
  });
  const ref = definition?.ref ?? (route?.view === "action-type" && selected && params?.action
    ? { app: selected.ref.app, kind: "action", name: `${selected.ref.name}.${params.action}` } : selected?.ref);
  const area: StudioArea = route?.view === "project" ? studioArea(params?.area)
    : ["changes", "release-review", "release-history", "test-plan", "sample-set", "evaluation-set"].includes(route?.view ?? "") ? "verify"
    : ["action-type", "decision-table", "alert"].includes(route?.view ?? "") || definition?.ref.kind === "action" ? "behavior"
    : selected ? resourceArea(selected) : studioAreaForRoute(route);
  return { selected, ref, area };
}

function resourceArea(item: OwnedResource): StudioArea {
  return studioAreas.find(area => (area.kinds as readonly string[]).includes(item.ref.kind))?.id ?? "assemble";
}

export function resourceInArea(item: OwnedResource, area: StudioArea): boolean {
  const matches = studioAreas.filter(candidate => (candidate.kinds as readonly string[]).includes(item.ref.kind));
  return matches.length ? matches.some(candidate => candidate.id === area) : area === "assemble";
}

/** Default groups use the asset's business namespace, never its platform kind. */
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
const semanticGroups: Record<StudioArea, { id: string; title: string; kinds: readonly string[] }[]> = {
  model: [
    { id: "business-model", title: "Business model", kinds: ["object"] },
    { id: "relationships", title: "Relationships and shared properties", kinds: ["link-type", "property-type"] },
    { id: "queries", title: "Queries", kinds: ["query"] },
  ],
  behavior: [
    { id: "actions", title: "Objects and actions", kinds: ["object", "action"] },
    { id: "rules", title: "Rules and decisions", kinds: ["compute", "alert"] },
  ],
  interface: [{ id: "pages", title: "Pages", kinds: ["page"] }],
  assemble: [
    { id: "data", title: "Connections and data", kinds: ["connection", "source", "dataset", "pipeline", "writeback", "matching"] },
    { id: "automation", title: "Flows and automation", kinds: ["flow", "automation", "compute"] },
    { id: "intelligence", title: "Agents and observations", kinds: ["agent", "function", "observation"] },
  ],
  verify: [
    { id: "tests", title: "Tests and evaluation", kinds: ["sample-set", "test-plan", "evaluation-set"] },
    { id: "delivery", title: "Release and migration", kinds: ["definition", "migration"] },
  ],
};

/** A short business-facing tree for the active workspace. */
export function studioResourceGroups(application: Pick<Api.Application, "title" | "name" | "concepts">, area: StudioArea, resources: readonly OwnedResource[]): StudioResourceGroup[] {
  const groups = semanticGroups[area].map(group => ({ ...group, items: [] as OwnedResource[] }));
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
    const group = groups.find(candidate => candidate.kinds.includes(item.kind.kind) || candidate.kinds.includes(item.kind.ref ?? ""));
    (group?.items ?? groups[0]?.items ?? []).push(item);
  }
  return [...custom, ...groups].filter(group => group.items.length).map(group => ({ id: group.id, title: group.title, items: [...group.items].sort((a, b) => a.title.localeCompare(b.title) || refKey(a.ref).localeCompare(refKey(b.ref))) }));
}

/** Actions retain their original identity beside the object they affect. */
export function studioActionGroups(resources: readonly OwnedResource[], definitions: readonly Api.Definition[]) {
  const objects = resources.filter(item => item.ref.kind === "object");
  const actions = resources.filter(item => item.ref.kind === "action");
  const definitionsByRef = new Map(definitions.map(item => [refKey(item.ref), item]));
  const assigned = new Set<string>();
  const groups = objects.map(object => ({ object, actions: actions.filter(action => {
    if (action.ref.app !== object.ref.app || definitionsByRef.get(refKey(action.ref))?.action?.target !== object.ref.name) return false;
    assigned.add(refKey(action.ref));
    return true;
  }) }));
  return { groups, other: resources.filter(item => item.ref.kind !== "object" && !assigned.has(refKey(item.ref))) };
}

/** Only application metadata changes. The original references and definitions stay intact. */
export function moveConceptAsset(concepts: readonly Api.AppConcept[], ref: Api.AssetRef, id: string) {
  return concepts.map(group => ({ ...group, assets: [
    ...group.assets.filter(asset => refKey(asset) !== refKey(ref)), ...(group.id === id ? [ref] : []),
  ] }));
}

export function assetRoute(ref: Api.AssetRef, resources: readonly OwnedResource[], definitions: readonly Api.Definition[], application: string): Route {
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
