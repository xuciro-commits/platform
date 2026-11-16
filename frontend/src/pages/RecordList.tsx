import { useMemo } from "react";
import { type Api, type Read, useRead } from "../host";
import { type ListShape, ListView } from "./ListPage";

type Row = Record<string, unknown>;
const tabular = new Set(["text", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference", "tags"]);
const columnLimit = 6;

function shown(value: unknown) {
  if (value == null) return "";
  return Array.isArray(value) ? value.join(", ") : String(value);
}

function shape(described: Api.EntityInfo, title: string, about: string, fields?: string[]): ListShape<Api.RecordPage, Row> {
  const candidates = described.fields.filter((field) => tabular.has(field.type));
  const lead = candidates.filter((field) => field.name === described.display);
  const rest = candidates.filter((field) => field.name !== described.display);
  const chosen = fields ? fields.flatMap((name) => described.fields.filter((field) => field.name === name)) : [...lead, ...rest].slice(0, columnLimit);
  return {
    title,
    about,
    rows: (answer) => answer.records as Row[],
    id: (row) => String(row.id),
    columns: chosen.map((field) => ({ key: field.name, header: field.title, text: (row) => shown(row[field.name]) })),
    groups: described.fields
      .filter((field) => field.type === "choice")
      .map((field) => ({ key: field.name, label: field.title, of: (row) => shown(row[field.name]) })),
  };
}

export function recordList({ type, title, about, fields }: { type: string; title: string; about: string; fields?: string[] }) {
  const pending: ListShape<Api.RecordPage, Row> = { title, about, rows: () => [], id: () => "", columns: [] };
  return function RecordListEntry() {
    const types = useRead<Api.EntityInfo[]>("/v1/entities");
    const described = types.status === "ready" ? types.data.find((candidate) => candidate.type === type) : undefined;
    const records = useRead<Api.RecordPage>(described ? `/v1/records/${type}?limit=200` : null);
    const definition = useMemo(() => (described ? shape(described, title, about, fields) : pending), [described]);
    const read: Read<Api.RecordPage> = types.status === "ready" ? records : types;
    return <ListView definition={definition} read={read} />;
  };
}
