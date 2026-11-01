import type { SortingState, VisibilityState } from "@tanstack/react-table";

export type TablePreferences = {
  sizing: Record<string, number>;
  sorting: SortingState;
  visibility: VisibilityState;
};

export function readTablePreferences(key?: string): TablePreferences {
  const empty = { sizing: {}, sorting: [], visibility: {} };
  if (!key) return empty;
  try {
    const saved = JSON.parse(localStorage.getItem(`table:${key}`) ?? "null");
    if (!saved || typeof saved !== "object") return empty;
    return {
      sizing: Object.fromEntries(Object.entries(saved.sizing ?? {}).filter((entry): entry is [string, number] => typeof entry[1] === "number" && Number.isFinite(entry[1]) && entry[1] >= 48)),
      sorting: Array.isArray(saved.sorting) ? saved.sorting.filter((sort: unknown): sort is SortingState[number] => !!sort && typeof sort === "object" && "id" in sort && typeof sort.id === "string" && "desc" in sort && typeof sort.desc === "boolean") : [],
      visibility: Object.fromEntries(Object.entries(saved.visibility ?? {}).filter((entry): entry is [string, boolean] => typeof entry[1] === "boolean")),
    };
  } catch { return empty; }
}
