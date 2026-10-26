import { definitionPresentation, useDefinitions, useHost, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { useMemo } from "react";
import { kindOfType, matchesResourceKind, publishedResourceKind, refKey, resourceKinds, resourceRef, type ResourceKindInfo, type ResourceRecord } from "./resources";

export type OwnedResource = { kind: ResourceKindInfo; record?: ResourceRecord; ref: Api.AssetRef; title: string; installed: boolean; missing: boolean };
// Several editor categories share one record owner; subscribe once per type.
const inventoryTypes = [...new Set(resourceKinds.map(kind => kind.type))];

/** Inventories of every resource type, keyed by kind, resolved to the records a project names — and to
 * what the host has installed, so a published resource without a tenant draft is never called missing. */
export function useProjectResources(project?: Pick<import("@platform/kernel").Api.Application, "pages" | "resources" | "concepts">, owner = "build", editable = true, options: { enabledKinds?: readonly string[] } = {}) {
  const { role } = useHost();
  const enabledKinds = options.enabledKinds;
  const kindEnabled = (kind: ResourceKindInfo) => !enabledKinds || enabledKinds.includes(kind.ref ?? kind.kind);
  const definitionQuery = useDefinitions();
  const definitions = definitionQuery.data ?? [];
  const readDrafts = !!project && owner === "build" && editable && role("build") === "builder";
  const byType = Object.fromEntries(inventoryTypes.map(type => [type, useRecordInventory<ResourceRecord>(type, 1000, readDrafts && resourceKinds.some(kind => kind.type === type && kindEnabled(kind)))]));
  const inventories = Object.fromEntries(resourceKinds.map(kind => [kind.kind, byType[kind.type]!]));
  const records = useMemo(() => !readDrafts ? [] : resourceKinds.flatMap(kind => (inventories[kind.kind]?.data?.records ?? [])
    .filter(record => kindEnabled(kind) && !record.archived && record.name && matchesResourceKind(kind, record))
    .map(record => ({ kind, record, ref: resourceRef(kind, record.name) }))), [readDrafts, enabledKinds, ...inventoryTypes.map(type => byType[type]?.data)]);
  const owned = useMemo(() => {
    const items: OwnedResource[] = [];
    if (!project) return items;
    const installed = new Map(definitions.map(definition => [refKey(definition.ref), definition]));
    const drafts = new Map<string, typeof records[number]>();
    for (const draft of records) if (!drafts.has(refKey(draft.ref))) drafts.set(refKey(draft.ref), draft);
    const resolve = (kind: ResourceKindInfo, ref: Api.AssetRef, record: ResourceRecord | undefined, loaded: boolean): OwnedResource => {
      const definition = installed.get(refKey(ref));
      const title = record?.title || (definition ? definitionPresentation(definition).title : ref.name);
      return { kind, record, ref, title, installed: !!definition, missing: !record && !definition && loaded };
    };
    for (const name of project.pages ?? []) {
      const ref = { app: owner, kind: "page", name };
      items.push(resolve(kindOfType("build.page")!, ref, drafts.get(refKey(ref))?.record, readDrafts ? inventories.page?.isSuccess === true : definitionQuery.isSuccess));
    }
    for (const ref of project.resources ?? []) {
      const candidates = resourceKinds.filter((item) => (item.ref ?? item.kind) === ref.kind);
      const draft = drafts.get(refKey(ref)), definition = installed.get(refKey(ref));
      const kind = draft?.kind
        ?? candidates.find(kind => kind.kind === "table" && definition?.operation?.binding.kind === "native" && definition.operation.rules)
        ?? candidates[0] ?? publishedResourceKind;
      items.push(resolve(kind, ref, draft?.record, kind === publishedResourceKind || !readDrafts ? definitionQuery.isSuccess : inventories[kind.kind]?.isSuccess === true));
    }
    return items;
  }, [project, owner, readDrafts, definitions, definitionQuery.isSuccess, records, ...inventoryTypes.map(type => byType[type]?.isSuccess)]);
  /** Records of every kind that exist but are not in the project yet: what "Add existing" offers. */
  const available = useMemo(() => {
    const held = new Set(owned.map((item) => refKey(item.ref)));
    return records.filter(draft => !held.has(refKey(draft.ref)));
  }, [owned, records]);
  /** Published non-navigation resources keep the same owner when an application references them. */
  const shared = useMemo(() => {
    const held = new Set(owned.map((item) => refKey(item.ref)));
    const drafts = new Set(available.map(draft => refKey(draft.ref)));
    return definitions.filter((d) => d.ref.kind !== "page" && d.ref.kind !== "app" && !held.has(refKey(d.ref)) && !drafts.has(refKey(d.ref)))
      .map((d) => ({ ref: d.ref, title: definitionPresentation(d).title, app: d.ref.app }))
      .sort((a, b) => (a.app === "core" ? 0 : 1) - (b.app === "core" ? 0 : 1) || a.title.localeCompare(b.title));
  }, [owned, available, definitions]);
  const loading = definitionQuery.isLoading || readDrafts && Object.values(byType).some(inventory => inventory.isLoading);
  const unavailable = definitionQuery.isError || readDrafts && Object.values(byType).some(inventory => inventory.isError);
  return { inventories, owned, available, shared, loading, unavailable };
}
