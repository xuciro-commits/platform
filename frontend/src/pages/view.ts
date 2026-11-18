import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";

export type ViewSort = { sortKey: string; direction: "ascending" | "descending" };
export type ViewState = {
  version: 1;
  layout: "table" | "board";
  columns: string[];
  sort: ViewSort[];
  filters: PowerSearchFilter[];
  search: string;
  group: string;
  lanes: string;
};

export function newView(columns: string[], group = "none", lanes = ""): ViewState {
  return { version: 1, layout: "table", columns: [...columns], sort: [], filters: [], search: "", group, lanes };
}

export function readView(raw: string | null, fallback: ViewState): ViewState {
  if (!raw) return fallback;
  try {
    const value = JSON.parse(raw) as Partial<ViewState>;
    if (value.version !== 1) return fallback;
    return {
      version: 1,
      layout: value.layout === "board" ? "board" : "table",
      columns: Array.isArray(value.columns) ? value.columns.filter((column): column is string => typeof column === "string") : fallback.columns,
      sort: Array.isArray(value.sort) ? value.sort.filter((sort): sort is ViewSort => Boolean(sort && typeof sort.sortKey === "string" && (sort.direction === "ascending" || sort.direction === "descending"))) : [],
      filters: Array.isArray(value.filters) ? value.filters.filter((filter): filter is PowerSearchFilter => Boolean(filter && typeof filter.field === "string" && typeof filter.operator === "string" && filter.value)) : [],
      search: typeof value.search === "string" ? value.search : "",
      group: typeof value.group === "string" ? value.group : fallback.group,
      lanes: typeof value.lanes === "string" ? value.lanes : fallback.lanes,
    };
  } catch {
    return fallback;
  }
}

export function sameView(left: ViewState, right: ViewState) {
  return JSON.stringify(left) === JSON.stringify(right);
}
