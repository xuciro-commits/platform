import { useRecordInventory } from "@platform/app";
import { useMemo } from "react";
import { useBranchChanges } from "./api";
import { useCurrentBranch } from "./current";

export type Touched = { id: string; archived?: boolean } & Record<string, unknown>;
const none: Touched[] = [];

export function useBranchTouched(branch?: string): ReadonlyMap<string, Touched[]> {
  const changes = useBranchChanges(branch);
  return useMemo(() => {
    const latest = new Map<string, Touched>(), byType = new Map<string, Touched[]>();
    for (const entry of changes.data ?? []) {
      const record: Touched = entry.op === "delete" ? { id: entry.id, archived: true } : { state: "draft", ...(entry.spec as object | undefined), id: entry.id };
      latest.set(`${entry.type}\n${entry.id}`, record);
      byType.set(entry.type, []);
    }
    for (const [key, record] of latest) byType.get(key.slice(0, key.indexOf("\n")))!.push(record);
    return byType;
  }, [changes.data]);
}

export function useBranchRecords(type: string, branch?: string): Touched[] {
  return useBranchTouched(branch).get(type) ?? none;
}

export function overlayRecords<T extends { id: string }>(main: readonly T[], touched: readonly Touched[]): T[] {
  if (!touched.length) return main as T[];
  const byId = new Map(touched.map(record => [record.id, record]));
  const merged = main.map(record => byId.has(record.id) ? { ...record, ...byId.get(record.id)! } as T : record);
  return [...merged, ...touched.filter(record => !record.archived && !main.some(item => item.id === record.id)).map(record => ({ revision: 0, ...record }) as unknown as T)];
}

export function useDraftInventory<T extends { id: string }>(type: string, limit = 1000, enabled = true) {
  const inventory = useRecordInventory<T>(type, limit, enabled);
  const { id } = useCurrentBranch(), touched = useBranchRecords(type, enabled ? id : undefined);
  const data = useMemo(() => inventory.data && touched.length ? { ...inventory.data, records: overlayRecords(inventory.data.records, touched) } : inventory.data, [inventory.data, touched]);
  return { ...inventory, data } as typeof inventory;
}
