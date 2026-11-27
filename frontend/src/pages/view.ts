import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";

export type ViewSort = { sortKey: string; direction: "ascending" | "descending" };
export type Density = "compact" | "balanced" | "spacious";
export type StickyEdge = "none" | "one" | "two";
export type GanttScale = "day" | "week" | "month";
export type ViewState = {
  version: 1;
  layout: "table" | "board" | "calendar" | "gantt" | "summary";
  columns: string[];
  sort: ViewSort[];
  filters: PowerSearchFilter[];
  search: string;
  group: string;
  subGroup: string;
  lanes: string;
  dates: string;
  until: string;
  after: string;
  scale: GanttScale;
  month: string;
  density: Density;
  stickyStart: StickyEdge;
  stickyEnd: StickyEdge;
  measure?: string;
  from?: string;
  to?: string;
};

const densities: Density[] = ["compact", "balanced", "spacious"];
const edges: StickyEdge[] = ["none", "one", "two"];
const scales: GanttScale[] = ["day", "week", "month"];

export function newView(columns: string[], group = "none", lanes = "", subGroup = "none"): ViewState {
  const normalizedSub = group === "none" || subGroup === group ? "none" : subGroup;
  return { version: 1, layout: "table", columns: [...columns], sort: [], filters: [], search: "", group, subGroup: normalizedSub, lanes, dates: "", until: "", after: "", scale: "day", month: "", density: "balanced", stickyStart: "none", stickyEnd: "none" };
}

export function readView(raw: string | null, fallback: ViewState): ViewState {
  if (!raw) return fallback;
  try {
    const value = JSON.parse(raw) as Partial<ViewState>;
    if (value.version !== 1) return fallback;
    const group = typeof value.group === "string" ? value.group : fallback.group;
    const rawSub = typeof value.subGroup === "string" ? value.subGroup : fallback.subGroup;
    const subGroup = group === "none" || rawSub === group ? "none" : rawSub;
    return {
      version: 1,
      layout: value.layout === "board" || value.layout === "calendar" || value.layout === "gantt" || value.layout === "summary" ? value.layout : "table",
      columns: Array.isArray(value.columns) ? value.columns.filter((column): column is string => typeof column === "string") : fallback.columns,
      sort: Array.isArray(value.sort) ? value.sort.filter((sort): sort is ViewSort => Boolean(sort && typeof sort.sortKey === "string" && (sort.direction === "ascending" || sort.direction === "descending"))) : [],
      filters: Array.isArray(value.filters) ? value.filters.filter((filter): filter is PowerSearchFilter => Boolean(filter && typeof filter.field === "string" && typeof filter.operator === "string" && filter.value)) : [],
      search: typeof value.search === "string" ? value.search : "",
      group,
      subGroup,
      lanes: typeof value.lanes === "string" ? value.lanes : fallback.lanes,
      dates: typeof value.dates === "string" ? value.dates : fallback.dates,
      until: typeof value.until === "string" ? value.until : fallback.until,
      after: typeof value.after === "string" ? value.after : fallback.after,
      scale: scales.find((scale) => scale === value.scale) ?? fallback.scale,
      month: typeof value.month === "string" && /^\d{4}-\d{2}$/.test(value.month) ? value.month : "",
      density: densities.find((density) => density === value.density) ?? fallback.density,
      stickyStart: edges.find((edge) => edge === value.stickyStart) ?? fallback.stickyStart,
      stickyEnd: edges.find((edge) => edge === value.stickyEnd) ?? fallback.stickyEnd,
      ...(typeof value.measure === "string" && value.measure ? { measure: value.measure } : fallback.measure ? { measure: fallback.measure } : {}),
      ...(typeof value.from === "string" && value.from ? { from: value.from } : fallback.from ? { from: fallback.from } : {}),
      ...(typeof value.to === "string" && value.to ? { to: value.to } : fallback.to ? { to: fallback.to } : {}),
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
      case "less_than_or_equal":
        return [[field, filter.operator === "greater_than_or_equal" ? ">=" : "<=", scalar]];
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

export function ganttSpan(month: string, scale: GanttScale) {
  const [year, number] = month.split("-").map(Number);
  const first = new Date(Date.UTC(year, number - 1, 1));
  if (scale === "month") {
    const start = first;
    const end = new Date(Date.UTC(year + 1, number - 1, 1));
    const days = Math.round((end.getTime() - start.getTime()) / 86400000);
    return { start: start.toISOString().slice(0, 10), end: end.toISOString().slice(0, 10), days };
  }
  const mondayOffset = (first.getUTCDay() + 6) % 7;
  const start = new Date(first.getTime() - mondayOffset * 86400000);
  const days = scale === "day" ? 49 : 112;
  const end = new Date(start.getTime() + days * 86400000);
  return { start: start.toISOString().slice(0, 10), end: end.toISOString().slice(0, 10), days };
}

export type NamedReport = { name: string; title?: string; group?: string; subGroup?: string; measure?: string; dates?: string; from?: string; to?: string };

export function reportView(base: ViewState, entityView: unknown, report?: string): ViewState {
  if (!report) return base;
  const reports = (entityView as { reports?: NamedReport[] } | undefined)?.reports;
  const found = reports?.find((candidate) => candidate.name === report);
  if (!found) return { ...base, layout: "summary" };
  return {
    ...base,
    layout: "summary",
    group: found.group ?? base.group,
    subGroup: found.subGroup ?? base.subGroup,
    measure: found.measure,
    dates: found.dates ?? base.dates,
    from: found.from,
    to: found.to,
  };
}

export function parseDomain(raw: string): unknown[][] {
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed) ? (parsed.filter(Array.isArray) as unknown[][]) : [];
  } catch {
    return [];
  }
}
