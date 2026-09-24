import type { ColumnDef } from "@tanstack/react-table";
import { Plus, X } from "lucide-react";
import { z } from "zod";
import { Button } from "../primitives/button";
import { Select } from "../primitives/input";
import type { FieldType } from "./types";

/** An entity's fields in display order, keyed by the record's property names. */
export type Entity<R> = { name: string; fields: { [K in keyof R]?: FieldType<any, R> } & Record<string, FieldType<any, R>>; primary: keyof R & string };

export function defineEntity<R>(entity: Entity<R>): Entity<R> {
  return entity;
}

export function valueOf<R>(entity: Entity<R>, key: string, row: R): unknown {
  const field = entity.fields[key];
  return field?.compute ? field.compute(row) : (row as Record<string, unknown>)[key];
}

/** Table columns from field types: display, alignment, width and sorting. */
export function columnsFor<R>(entity: Entity<R>, keys: string[] = Object.keys(entity.fields)): ColumnDef<R, any>[] {
  return keys.map((key) => {
    const field = entity.fields[key]!;
    return {
      id: key, header: field.label, accessorFn: (row: R) => valueOf(entity, key, row),
      cell: (c) => field.display(c.getValue(), c.row.original),
      sortingFn: (a, b) => {
        const x = a.getValue(key), y = b.getValue(key);
        return x === undefined ? -1 : y === undefined ? 1 : field.compare(x, y);
      },
      meta: { align: field.align, width: field.width, field },
    } satisfies ColumnDef<R, any>;
  });
}

/** Validation for a whole record: each editable field's schema, optional unless required. */
export function recordSchema<R>(entity: Entity<R>, keys: string[] = Object.keys(entity.fields)) {
  return z.object(Object.fromEntries(keys.filter((k) => !entity.fields[k]!.readOnly).map((k) => {
    const f = entity.fields[k]!;
    const schema = f.required ? f.schema.refine((v) => v !== "" && !(Array.isArray(v) && v.length === 0), "Required") : f.schema.optional();
    return [k, f.required ? schema : z.preprocess((v) => (v === "" ? undefined : v), schema)];
  })));
}

export type Filter = { field: string; operator: string; arg?: unknown };

export function applyFilters<R>(entity: Entity<R>, rows: R[], filters: Filter[]): R[] {
  const active = filters.flatMap((f) => {
    const op = entity.fields[f.field]?.operators.find((o) => o.id === f.operator);
    return op && (!op.needsArg || f.arg !== undefined) ? [{ key: f.field, op, arg: f.arg }] : [];
  });
  return active.length ? rows.filter((row) => active.every(({ key, op, arg }) => op.test(valueOf(entity, key, row), arg))) : rows;
}

/** Filters built from field types: each type offers its own operators and argument editor. */
export function FilterBar<R>({ entity, filters, onChange }: { entity: Entity<R>; filters: Filter[]; onChange: (filters: Filter[]) => void }) {
  const keys = Object.keys(entity.fields);
  const update = (i: number, next: Partial<Filter>) => onChange(filters.map((f, j) => (j === i ? { ...f, ...next } : f)));
  return (
    <div className="flex flex-wrap items-center gap-2">
      {filters.map((filter, i) => {
        const field = entity.fields[filter.field]!;
        const op = field.operators.find((o) => o.id === filter.operator);
        return (
          <div key={i} className="flex items-center gap-1 rounded-md border border-border bg-surface p-1">
            <Select aria-label="Field" value={filter.field} className="h-6 w-32"
              onChange={(e) => update(i, { field: e.target.value, operator: entity.fields[e.target.value]!.operators[0]!.id, arg: undefined })}>
              {keys.map((k) => <option key={k} value={k}>{entity.fields[k]!.label}</option>)}
            </Select>
            <Select aria-label="Operator" value={filter.operator} className="h-6 w-28" onChange={(e) => update(i, { operator: e.target.value })}>
              {field.operators.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
            </Select>
            {op?.needsArg && field.editor && <span className="min-w-28">{field.editor({ value: filter.arg, onChange: (arg) => update(i, { arg }) })}</span>}
            <button type="button" aria-label="Remove filter" className="px-1 text-muted hover:text-foreground"
              onClick={() => onChange(filters.filter((_, j) => j !== i))}><X className="size-3.5" /></button>
          </div>
        );
      })}
      <Button size="sm" variant="ghost" onClick={() => onChange([...filters, { field: keys[0]!, operator: entity.fields[keys[0]!]!.operators[0]!.id }])}>
        <Plus />Filter
      </Button>
    </div>
  );
}
