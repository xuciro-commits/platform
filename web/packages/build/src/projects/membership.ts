// Which project holds a resource (ADR-0083 D6): inventories group by it, so a
// builder sees object types, actions, queries and functions under the
// applications they belong to rather than as one flat list. A resource no
// project lists is shown under "Not in a project" — never guessed into one.
import { useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { t } from "@platform/ui";
import { useMemo } from "react";
import { kindOfType, refKey, resourceRef } from "./resources";

type ProjectRecord = Api.Application & { id: string; archived?: boolean };
export type ProjectIndex = { byRef: Map<string, { id: string; title: string }[]>; loaded: boolean };

export function useProjectIndex(): ProjectIndex {
  const projects = useRecordInventory<ProjectRecord>("build.app");
  return useMemo(() => {
    const byRef = new Map<string, { id: string; title: string }[]>();
    for (const p of projects.data?.records ?? []) {
      if (p.archived) continue;
      const entry = { id: p.id, title: p.title || p.name || p.id };
      for (const ref of p.resources ?? []) (byRef.get(refKey(ref)) ?? byRef.set(refKey(ref), []).get(refKey(ref))!).push(entry);
      for (const name of p.pages ?? []) { const k = refKey({ app: "build", kind: "page", name }); (byRef.get(k) ?? byRef.set(k, []).get(k)!).push(entry); }
    }
    return { byRef, loaded: !!projects.data };
  }, [projects.data]);
}

/** The group a resource falls in when grouped by project. */
export function projectGroup(index: ProjectIndex, ref: Api.AssetRef): { id: string; label: string; order?: number } {
  const held = index.byRef.get(refKey(ref));
  if (held?.length) return { id: held[0]!.id, label: held[0]!.title };
  return { id: "_none", label: t("Not in a project"), order: 9 };
}

/** The group of a Build record (`build.query` …) by the project that lists it. */
export function projectGroupOfRecord(index: ProjectIndex, type: string, name: string) {
  const kind = kindOfType(type);
  return kind ? projectGroup(index, resourceRef(kind, name)) : { id: "_none", label: t("Not in a project"), order: 9 };
}
