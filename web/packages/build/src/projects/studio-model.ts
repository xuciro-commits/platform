import type { Api } from "@platform/kernel";
import type { Route } from "@platform/ui";
import { refKey, resourceRoute } from "./resources";
import type { OwnedResource } from "./resource-inventory";

export const studioAreas = [
  { id: "model", title: "Model", description: "Objects, fields, relationships, versions and constraints", kinds: ["object", "link-type", "property-type", "query"] },
  { id: "behavior", title: "Behavior", description: "States, actions, transactions, rules and approvals", kinds: ["object", "action", "compute"] },
  { id: "interface", title: "Interface", description: "Pages, templates, application header and navigation", kinds: ["page"] },
  { id: "assemble", title: "Assemble", description: "Data connections, flows, AI functions and agents", kinds: ["flow", "function", "agent", "compute", "connection", "source", "dataset", "pipeline", "writeback", "matching"] },
  { id: "verify", title: "Verify", description: "Samples, roles, candidates, migrations and delivery", kinds: ["evaluation-set"] },
] as const;
export type StudioArea = typeof studioAreas[number]["id"];
export const studioArea = (value?: string): StudioArea => studioAreas.find(area => area.id === value)?.id ?? "model";
export const studioApplicationID = (route?: Route) => route?.view === "project" || route?.view === "module" && route.params?.id ? route.params?.id : route?.params?.application;
export const studioRoute = (id: string, area: StudioArea, asset?: string): Route => ({ view: "project", params: { id, area, ...(asset ? { asset } : {}) } });

export function resourceArea(item: OwnedResource): StudioArea {
  return studioAreas.find(area => (area.kinds as readonly string[]).includes(item.ref.kind))?.id ?? "assemble";
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
  if (ref.kind === "object") return { view: "object-type", params: { object: ref.name, application } };
  return { view: "definition", params: { ...ref, surface: "studio", application } };
}

/** Relations come solely from the owner's declared dependency graph. */
export function assetDependencies(ref: Api.AssetRef, definitions: readonly Api.Definition[]) {
  const key = refKey(ref), definition = definitions.find(item => refKey(item.ref) === key);
  return {
    requires: definition?.requires ?? [],
    usedBy: definitions.filter(item => item.requires?.some(required => refKey(required) === key)).map(item => item.ref),
  };
}
