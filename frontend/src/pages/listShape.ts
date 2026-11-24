import type { ReactNode } from "react";
import type { CellEdit } from "./PropertyField";
import type { Dot } from "./Transitions";

export type ListColumn<Row> = {
  key: string;
  header: string;
  width?: number;
  text: (row: Row) => string;
  kind?: "string" | "enum" | "number" | "date" | "time" | "code" | "member" | "person" | "email" | "phone";
  choices?: string[];
  options?: { value: string; label: string }[];
  boolean?: boolean;
  dot?: (row: Row) => Dot | undefined;
  value?: (row: Row) => unknown;
  hiddenByDefault?: boolean;
  chip?: boolean;
  reference?: (row: Row) => { type: string; id: string } | undefined;
  edit?: CellEdit<Row>;
};

type Done = Promise<unknown>;
export type ListGroup<Row> = { key: string; label: string; of: (row: Row) => string; order?: string[] };
export type Move = (id: string, to: string) => Done;

export type ListBoard<Row> = {
  key: string;
  lanes: { key: string; label: string; dot?: Dot }[];
  of: (row: Row) => string;
  accepts?: (id: string, to: string) => boolean;
  move?: Move;
  total?: { label: string; value: (row: Row) => { amount: number; currency?: string } | undefined };
  related?: (row: Row) => string | undefined;
  create?: (lane: string) => void;
};

export type ListCalendar<Row> = {
  fields: { key: string; label: string }[];
  of: (row: Row, field: string) => string;
  accepts?: (id: string) => boolean;
  move?: (id: string, field: string, day: string) => Done;
};

export type ListGantt<Row> = {
  fields: { key: string; label: string }[];
  startField: string;
  endField: string;
  startOf: (row: Row) => string;
  endOf: (row: Row) => string;
  afterOf?: (row: Row) => string;
  dotOf?: (row: Row) => Dot | undefined;
  unscheduled: { id: string; title: string; group?: string; dot?: Dot; href: string }[];
  accepts?: (id: string) => boolean;
  move?: (id: string, startField: string, endField: string, start: string, end: string) => Done;
};

export type ListShape<Answer, Row> = {
  title: string;
  about: string;
  rows: (answer: Answer) => Row[];
  id: (row: Row) => string;
  link?: (row: Row) => string;
  columns: ListColumn<Row>[];
  groups?: ListGroup<Row>[];
  board?: ListBoard<Row>;
  calendar?: ListCalendar<Row>;
  gantt?: ListGantt<Row>;
  bulk?: (ids: string[], clear: () => void) => ReactNode;
  detail?: (id: string) => ReactNode;
  peekType?: string;
  defaultView?: unknown;
  carryView?: boolean;
  page?: { total: number; loaded: number; hasMore: boolean; loading: boolean; loadMore: () => void };
  unfiltered?: number;
  savedViews?: string;
  remote?: "all" | "search";
};
