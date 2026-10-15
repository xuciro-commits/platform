import { useHost, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { useMemo } from "react";
import type { Project } from "./project";
import { kindOfType, refKey, resourceKinds, resourceRef, type ResourceKindInfo, type ResourceRecord } from "./resources";

export type OwnedResource = { kind: ResourceKindInfo; record?: ResourceRecord; ref: Api.AssetRef; title: string; installed: boolean; missing: boolean };

/** Inventories of every resource type, keyed by kind, resolved to the records a project names — and to
 * what the host has installed, so a published resource without a tenant draft is never called missing. */
export function useProjectResources(project?: Project) {
  const { definitions } = useHost();
  const inventories = Object.fromEntries(resourceKinds.map((kind) => [kind.kind, useRecordInventory<ResourceRecord>(kind.type, 1000, !!project)])) as Record<string, ReturnType<typeof useRecordInventory<ResourceRecord>>>;
  const owned = useMemo(() => {
    const items: OwnedResource[] = [];
    if (!project) return items;
    const installedKeys = new Set(definitions.map((d) => refKey(d.ref)));
    const resolve = (kind: ResourceKindInfo, ref: Api.AssetRef, record: ResourceRecord | undefined, loaded: boolean): OwnedResource => {
      const installed = installedKeys.has(refKey(ref));
      const definition = installed ? definitions.find((d) => refKey(d.ref) === refKey(ref)) : undefined;
      const title = record?.title ?? definition?.page?.title ?? definition?.entity?.title ?? definition?.query?.title ?? definition?.function?.title ?? definition?.observation?.title ?? ref.name;
      return { kind, record, ref, title, installed, missing: !record && !installed && loaded };
    };
    const pages = inventories.page?.data?.records ?? [];
    for (const name of project.pages ?? []) {
      items.push(resolve(kindOfType("build.page")!, { app: "build", kind: "page", name }, pages.find((page) => page.name === name && !page.archived), inventories.page?.isSuccess === true));
    }
    for (const ref of project.resources ?? []) {
      const kind = resourceKinds.find((item) => (item.ref ?? item.kind) === ref.kind);
      if (!kind) continue;
      const record = (inventories[kind.kind]?.data?.records ?? []).find((item) => !item.archived && refKey(resourceRef(kind, item.name)) === refKey(ref));
      items.push(resolve(kind, ref, record, inventories[kind.kind]?.isSuccess === true));
    }
    return items;
  }, [project, definitions, ...resourceKinds.map((kind) => inventories[kind.kind]?.data)]);
  /** Records of every kind that exist but are not in the project yet: what "Add existing" offers. */
  const available = useMemo(() => {
    const held = new Set(owned.map((item) => refKey(item.ref)));
    return resourceKinds.flatMap((kind) => (inventories[kind.kind]?.data?.records ?? []).filter((record) => !record.archived && record.name && !held.has(refKey(kind.kind === "page" ? { app: "build", kind: "page", name: record.name } : resourceRef(kind, record.name)))).map((record) => ({ kind, record })));
  }, [owned, ...resourceKinds.map((kind) => inventories[kind.kind]?.data)]);
  /** Object types other apps installed (the shared master data of `core`, HR's employees…): a project
   * references them instead of defining its own (ADR-0058 A1). */
  const shared = useMemo(() => {
    const held = new Set(owned.map((item) => refKey(item.ref)));
    return definitions.filter((d) => d.ref.kind === "object" && d.ref.app !== "build" && d.entity && !held.has(refKey(d.ref)))
      .map((d) => ({ ref: d.ref, title: d.entity!.title || d.ref.name, app: d.ref.app }))
      .sort((a, b) => (a.app === "core" ? 0 : 1) - (b.app === "core" ? 0 : 1) || a.title.localeCompare(b.title));
  }, [owned, definitions]);
  const loading = Object.values(inventories).some((inventory) => inventory.isLoading);
  const unavailable = Object.values(inventories).some(inventory => inventory.isError);
  return { inventories, owned, available, shared, loading, unavailable };
}
