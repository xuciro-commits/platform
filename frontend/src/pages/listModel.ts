import { pixel, proportional } from "@astryxdesign/core/Table";
import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";
import { t } from "../i18n";
import { type Cells, dotOf, idKey, targetOf, typeOf } from "./ListDetail";
import type { ListBoard, ListCalendar, ListColumn, ListGantt, ListGroup, ListShape } from "./listShape";
import type { Chip } from "./ListFilters";
import type { StickyEdge, ViewSort, ViewState } from "./view";

export type RawRow = Record<string, unknown>;
export type PreparedRow<Row> = { cells: Cells; raw: RawRow; row: Row };

const widthOf: Record<string, number> = {
  string: 160,
  person: 140,
  email: 204,
  phone: 144,
  code: 150,
  enum: 120,
  member: 132,
  number: 128,
  date: 120,
  time: 132,
  boolean: 96,
};

const operators: Record<string, string> = {
  is: "is",
  is_any_of: "is any of",
  greater_than_or_equal: "≥",
  less_than_or_equal: "≤",
  after: "after",
  before: "before",
  contains: "contains",
  is_true: "is yes",
  is_false: "is no",
};

const compare = (left: unknown, right: unknown) =>
  left instanceof Date && right instanceof Date
    ? left.getTime() - right.getTime()
    : typeof left === "number" && typeof right === "number"
      ? left - right
      : String(left ?? "").localeCompare(String(right ?? ""));

const parseDay = (value: string) => {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
  return match ? new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3])) : new Date(value);
};

const headerWidth = (text: string) =>
  [...text].reduce((sum, letter) => sum + (letter.charCodeAt(0) > 0x2e7f ? 13 : 7), 32);

export function stuck(edge: StickyEdge, keys: string[], fromEnd: boolean) {
  const count = edge === "one" ? 1 : edge === "two" ? 2 : 0;
  return count === 0 ? [] : fromEnd ? keys.slice(-count) : keys.slice(0, count);
}

export function columnWidthSpec<Row>(column: ListColumn<Row>, position: number, compact: boolean) {
  const minHeader = headerWidth(t(column.header));
  if (column.flex != null) {
    return { flex: true, spec: proportional(column.flex, { minWidth: Math.max(column.width ?? 160, minHeader) }) };
  }
  if (column.width != null) {
    return { flex: false, spec: pixel(Math.max(column.width, minHeader)) };
  }
  if (column.boolean) {
    return { flex: false, spec: pixel(Math.max(widthOf.boolean, minHeader)) };
  }
  const kind = column.kind ?? "string";
  if (position === 0) {
    return { flex: true, spec: proportional(1.6, { minWidth: Math.max(compact ? 128 : 144, minHeader) }) };
  }
  if (kind === "string") {
    return { flex: true, spec: proportional(1.4, { minWidth: Math.max(124, minHeader) }) };
  }
  if (kind === "email") {
    return { flex: true, spec: proportional(1.2, { minWidth: Math.max(144, minHeader) }) };
  }
  if (kind === "code") {
    return { flex: true, spec: proportional(1.1, { minWidth: Math.max(116, minHeader) }) };
  }
  if (kind === "person" || kind === "member") {
    return { flex: true, spec: proportional(1.0, { minWidth: Math.max(112, minHeader) }) };
  }
  if (kind === "number") {
    return { flex: true, spec: proportional(1.05, { minWidth: Math.max(132, minHeader) }) };
  }
  return { flex: true, spec: proportional(0.85, { minWidth: Math.max(Math.min(widthOf[kind] ?? 112, 116), minHeader) }) };
}

export function buildFilterChips<Row>(orderedColumns: ListColumn<Row>[]): Chip[] {
  return orderedColumns.flatMap((column): Chip[] => {
    const base = { key: column.key, label: column.header };
    if (column.chip === false) return [];
    if (column.options) return [{ ...base, kind: "many", options: column.options }];
    if (column.boolean) return [{ ...base, kind: "boolean", options: [] }];
    if (column.kind === "enum" && column.choices?.length) {
      return [{ ...base, kind: "many", options: column.choices.map((choice) => ({ value: choice, label: t(choice) })) }];
    }
    if (column.kind === "number") return [{ ...base, kind: "range", options: [] }];
    if (column.kind === "time" || column.kind === "date") return [{ ...base, kind: "date", options: [] }];
    return [];
  });
}

export function formatFilterLabel<Row>(columns: ListColumn<Row>[], filter: PowerSearchFilter): string {
  const column = columns.find((candidate) => candidate.key === filter.field);
  const raw = filter.value as { value?: unknown; unixSeconds?: number };
  const name = (value: unknown) => column?.options?.find((option) => option.value === String(value))?.label ?? t(String(value ?? ""));
  const value =
    typeof raw.unixSeconds === "number"
      ? new Date(raw.unixSeconds * 1000).toLocaleDateString()
      : Array.isArray(raw.value)
        ? raw.value.map(name).join(", ")
        : raw.value == null
          ? ""
          : typeof raw.value === "number"
            ? raw.value.toLocaleString()
            : name(raw.value);
  return `${t(column?.header ?? filter.field)} ${t(operators[filter.operator] ?? filter.operator)} ${value}`.trim();
}

export function prepareRows<Answer, Row>({
  data,
  definition,
  group,
  subGroup,
  board,
  calendar,
  gantt,
  dateField,
  currentSearch,
  view,
}: {
  data: Answer;
  definition: ListShape<Answer, Row>;
  group?: ListGroup<Row>;
  subGroup?: ListGroup<Row>;
  board?: ListBoard<Row>;
  calendar?: ListCalendar<Row>;
  gantt?: ListGantt<Row>;
  dateField: string;
  currentSearch: string;
  view: ViewState;
}): PreparedRow<Row>[] {
  return definition.rows(data).map((row) => {
    const href = definition.link?.(row) ?? "";
    const params = new URLSearchParams(currentSearch);
    if (definition.carryView) params.set("view", JSON.stringify(view));
    const cells: Cells = {
      [idKey]: definition.id(row),
      __group: group ? group.of(row) : "",
      __subGroup: subGroup ? subGroup.of(row) : "",
      __lane: board ? board.of(row) : "",
      __day: calendar ? calendar.of(row, dateField) : "",
      __start: gantt ? gantt.startOf(row) : "",
      __end: gantt ? gantt.endOf(row) : "",
      __after: gantt?.afterOf ? gantt.afterOf(row) : "",
      __ganttDot: gantt?.dotOf?.(row) ?? "",
      __href: href && definition.carryView ? href + "?" + params : href,
    };
    const raw: RawRow = { [idKey]: cells[idKey] };
    for (const column of definition.columns) {
      cells[column.key] = column.text(row);
      const dot = column.dot?.(row);
      if (dot) cells[dotOf(column.key)] = dot;
      const reference = cells[column.key] ? column.reference?.(row) : undefined;
      if (reference) [cells[typeOf(column.key)], cells[targetOf(column.key)]] = [reference.type, reference.id];
      const value = column.value ? column.value(row) : column.boolean ? cells[column.key] === t("Yes") : cells[column.key];
      raw[column.key] = column.kind === "date" ? (value ? parseDay(String(value)) : undefined) : column.kind === "time" ? (value ? new Date(String(value)) : undefined) : value;
    }
    if (board?.related) cells.__related = board.related(row) ?? "";
    if (board?.total) {
      const amount = board.total.value(row);
      if (amount) {
        cells.__amount = String(amount.amount);
        cells.__currency = amount.currency ?? "";
      }
    }
    return { cells, raw, row };
  });
}

export function sortPreparedRows<Row>(
  list: PreparedRow<Row>[],
  sort: ViewSort[],
  order?: string[],
  subOrder?: string[],
): Cells[] {
  if (!order && !subOrder && sort.length === 0) return list.map((entry) => entry.cells);
  return [...list]
    .sort((a, b) => {
      if (order && a.cells.__group !== b.cells.__group) {
        const ai = order.indexOf(a.cells.__group);
        const bi = order.indexOf(b.cells.__group);
        if (ai >= 0 && bi >= 0) return ai - bi;
        if (ai >= 0) return -1;
        if (bi >= 0) return 1;
      }
      if (subOrder && a.cells.__group === b.cells.__group && a.cells.__subGroup !== b.cells.__subGroup) {
        const ai = subOrder.indexOf(a.cells.__subGroup);
        const bi = subOrder.indexOf(b.cells.__subGroup);
        if (ai >= 0 && bi >= 0) return ai - bi;
        if (ai >= 0) return -1;
        if (bi >= 0) return 1;
      }
      for (const { sortKey, direction } of sort) {
        const difference = compare(a.raw[sortKey], b.raw[sortKey]);
        if (difference !== 0) return direction === "ascending" ? difference : -difference;
      }
      return 0;
    })
    .map((entry) => entry.cells);
}
