import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";

export type ViewSort = { sortKey: string; direction: "ascending" | "descending" };
export type Density = "compact" | "balanced" | "spacious";
export type StickyEdge = "none" | "one" | "two";
export type ViewState = {
  version: 1;
  layout: "table" | "board" | "calendar";
  columns: string[];
  sort: ViewSort[];
  filters: PowerSearchFilter[];
  search: string;
  group: string;
  lanes: string;
  dates: string;
  month: string;
  density: Density;
  stickyStart: StickyEdge;
  stickyEnd: StickyEdge;
};

const densities: Density[] = ["compact", "balanced", "spacious"];
const edges: StickyEdge[] = ["none", "one", "two"];

export function newView(columns: string[], group = "none", lanes = ""): ViewState {
  return { version: 1, layout: "table", columns: [...columns], sort: [], filters: [], search: "", group, lanes, dates: "", month: "", density: "balanced", stickyStart: "none", stickyEnd: "none" };
}

export function readView(raw: string | null, fallback: ViewState): ViewState {
  if (!raw) return fallback;
  try {
    const value = JSON.parse(raw) as Partial<ViewState>;
    if (value.version !== 1) return fallback;
    return {
      version: 1,
      layout: value.layout === "board" || value.layout === "calendar" ? value.layout : "table",
      columns: Array.isArray(value.columns) ? value.columns.filter((column): column is string => typeof column === "string") : fallback.columns,
      sort: Array.isArray(value.sort) ? value.sort.filter((sort): sort is ViewSort => Boolean(sort && typeof sort.sortKey === "string" && (sort.direction === "ascending" || sort.direction === "descending"))) : [],
      filters: Array.isArray(value.filters) ? value.filters.filter((filter): filter is PowerSearchFilter => Boolean(filter && typeof filter.field === "string" && typeof filter.operator === "string" && filter.value)) : [],
      search: typeof value.search === "string" ? value.search : "",
      group: typeof value.group === "string" ? value.group : fallback.group,
      lanes: typeof value.lanes === "string" ? value.lanes : fallback.lanes,
      dates: typeof value.dates === "string" ? value.dates : fallback.dates,
      month: typeof value.month === "string" && /^\d{4}-\d{2}$/.test(value.month) ? value.month : "",
      density: densities.find((density) => density === value.density) ?? fallback.density,
      stickyStart: edges.find((edge) => edge === value.stickyStart) ?? fallback.stickyStart,
      stickyEnd: edges.find((edge) => edge === value.stickyEnd) ?? fallback.stickyEnd,
    };
  } catch {
    return fallback;
  }
}

export function sameView(left: ViewState, right: ViewState) {
  return JSON.stringify(left) === JSON.stringify(right);
}

export function hasFilters(view: ViewState, domain?: string | null) {
  return Boolean(view.filters.length || view.search.trim() || (domain && domain !== "[]"));
}

export function domainOf(filters: PowerSearchFilter[], choiceValue: (field: string, value: string) => string = (_field, value) => value): unknown[][] {
  return filters.flatMap((filter): unknown[][] => {
    const field = filter.field;
    const value = filter.value;
    const scalar =
      value.type === "empty"
        ? true
        : value.type === "enum"
          ? choiceValue(field, value.value)
          : value.type === "string"
            ? value.value
          : value.type === "integer" || value.type === "float"
            ? value.value
            : value.type === "enum_list"
              ? value.value.map((item) => choiceValue(field, item))
              : value.type === "string_list"
              ? value.value
              : value.type === "time"
                ? value.value
                : value.type === "date_absolute"
                  ? new Date(value.unixSeconds * 1000).toISOString()
                  : undefined;
    if (scalar === undefined) return [];
    switch (filter.operator) {
      case "contains":
        return [[field, "like", scalar]];
      case "not_contains":
        return [[field, "not-like", scalar]];
      case "starts_with":
        return [[field, "starts-with", scalar]];
      case "not_starts_with":
        return [[field, "not-starts-with", scalar]];
      case "ends_with":
        return [[field, "ends-with", scalar]];
      case "not_ends_with":
        return [[field, "not-ends-with", scalar]];
      case "is":
      case "is_true":
      case "equals":
        return [[field, "=", scalar]];
      case "is_not":
      case "is_false":
      case "not_equals":
        return [[field, "!=", scalar]];
      case "greater_than":
      case "after":
        return [[field, ">", scalar]];
      case "less_than":
      case "before":
        return [[field, "<", scalar]];
      case "greater_than_or_equal":
        return [[field, ">=", scalar]];
      case "less_than_or_equal":
        return [[field, "<=", scalar]];
      case "is_any_of":
        return [[field, "in", scalar]];
      case "is_none_of":
        return [[field, "not in", scalar]];
      default:
        return [];
    }
  });
}

export function shownMonth(view: ViewState) {
  return view.month || new Date().toISOString().slice(0, 7);
}

export function monthSpan(month: string) {
  const [year, number] = month.split("-").map(Number);
  const first = new Date(Date.UTC(year, number - 1, 1));
  const start = new Date(first.getTime() - ((first.getUTCDay() + 6) % 7) * 86400000);
  const end = new Date(start.getTime() + 42 * 86400000);
  return { year, month: number, start: start.toISOString().slice(0, 10), end: end.toISOString().slice(0, 10) };
}
