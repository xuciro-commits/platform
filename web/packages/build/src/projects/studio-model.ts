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
    : selected ? resourceArea(selected) : "assemble";
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
