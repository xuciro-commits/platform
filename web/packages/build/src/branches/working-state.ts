import { useHost, useRecordInventory } from "@platform/app";
import { useMemo } from "react";
import { matchesResourceKind, resourceKinds, type ResourceRecord } from "../projects/resources";
import type { JointChoice, ReleaseKind } from "../releases/selection";

const pendingStates = new Set(["draft", "compiled", "failed"]);
const releasable = resourceKinds.filter(kind => kind.kind !== "action" && kind.kind !== "automation");
const types = [...new Set(releasable.map(kind => kind.type))];

export type WorkingDraft = JointChoice & { title: string; label: string };

export function useWorkingDrafts(enabled: boolean) {
  const { holds } = useHost();
  const read = enabled && holds("build", "builder");
  const stored = Object.fromEntries(types.map(type => [type, useRecordInventory<ResourceRecord>(type, 1000, read)]));
  const drafts = useMemo(() => !read ? [] : releasable.flatMap(kind => (stored[kind.type]?.data?.records ?? [])
    .filter(record => !record.archived && !!record.name && pendingStates.has(String(record.state ?? "")) && matchesResourceKind(kind, record))
    .map(record => ({ kind: kind.kind as ReleaseKind, id: record.id, title: record.title || record.name, label: kind.label }))), [read, ...types.map(type => stored[type]?.data)]);
  const loading = read && types.some(type => stored[type]?.isLoading);
  return { drafts: drafts as WorkingDraft[], loading };
}
