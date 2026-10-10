import type {Api} from "@platform/kernel";
import {validCivilDate,validTimestamp,timestampNanoseconds} from "../components/date";
import type { ColumnDef } from "@tanstack/react-table";
import { Plus, X } from "lucide-react";
import { z } from "zod";
import { Button } from "../primitives/button";
import { Select } from "../primitives/input";
import { applies, type FieldType, type Operator } from "./types";
import { t } from "../i18n";

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

/** Pure projection of the host's finite input declaration (ADR-0096). */
export function inputIssues(fields:Api.Field[],values:Record<string,unknown>):Api.FieldIssue[]{
 const issues:Api.FieldIssue[]=[],valid=new Set<string>(),dependencies=new Set(fields.flatMap(f=>[f.constraints?.before,f.constraints?.after]).filter(Boolean));
 const add=(f:Api.Field,code:string,message:string,related?:string)=>{if(issues.length<64)issues.push({code,message,path:[f.name],relatedPaths:related?[[related]]:undefined});};
 for(const f of fields){const c=f.constraints??(dependencies.has(f.name)?{}:undefined),v=values[f.name];if(!c)continue;
  if(v===undefined||v===null||typeof v==="string"&&!v.trim()){if(f.required)add(f,"required",t("Enter a value."));continue;}
  const text=typeof v==="string",num=typeof v==="number";
  const local=text&&/^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d$/.test(v)&&validCivilDate(v.slice(0,10));
  const bad=f.type==="string"&&!text||f.type==="date"&&(!text||!validCivilDate(v)&&!(c.dateTime&&local))||f.type==="datetime"&&(!text||!validTimestamp(v))||(f.type==="integer"||f.type==="number")&&(!num||!Number.isFinite(v)||f.type==="integer"&&!Number.isSafeInteger(v))||f.type==="boolean"&&typeof v!=="boolean";
  if(bad){add(f,"type",t("Enter a valid value."));continue;}valid.add(f.name);
  if(f.choices?.length&&!f.choices.includes(String(v)))add(f,"choice",t("Choose a listed value."));
  if(text){if(c.minLength!==undefined&&v.length<c.minLength)add(f,"minLength",t("Use at least {count} characters.",{count:c.minLength}));if(c.maxLength!==undefined&&v.length>c.maxLength)add(f,"maxLength",t("Use at most {count} characters.",{count:c.maxLength}));}
  if(num){if(c.min!==undefined&&(v<c.min||c.exclusiveMin&&v===c.min))add(f,"min",c.exclusiveMin?t("Value must be greater than {value}.",{value:c.min}):t("Value must be at least {value}.",{value:c.min}));if(c.max!==undefined&&(v>c.max||c.exclusiveMax&&v===c.max))add(f,"max",c.exclusiveMax?t("Value must be less than {value}.",{value:c.max}):t("Value must be at most {value}.",{value:c.max}));}
 }
 for(const f of fields){const c=f.constraints;if(!c||!valid.has(f.name))continue;for(const [name,before] of [[c.before,true],[c.after,false]] as const){if(!name||!valid.has(name))continue;let value=values[f.name] as string|number|bigint,other=values[name] as string|number|bigint;if(f.type==="datetime"){value=timestampNanoseconds(String(value))!;other=timestampNanoseconds(String(other))!;}if(before&&value>other||!before&&value<other||!c.inclusive&&value===other){const label=fields.find(f=>f.name===name)?.description||name;add(f,"relation",before?(c.inclusive?t("Value must be on or before {field}.",{field:label}):t("Value must be before {field}.",{field:label})):(c.inclusive?t("Value must be on or after {field}.",{field:label}):t("Value must be after {field}.",{field:label})),name);}}}
 return issues;
}

/** Validation for a whole record: each editable field's schema, optional unless required. */
export function recordSchema<R>(entity: Entity<R>, keys: string[] = Object.keys(entity.fields)) {
  const shown = keys.filter((k) => !entity.fields[k]!.readOnly);
  const blank = (v: unknown) => v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0);
  const object = z.object(Object.fromEntries(shown.map((k) => {
    const f = entity.fields[k]!;
    // A conditional field is required only while it applies: the object-level check below decides.
    const required = f.required && !f.when;
    const checked = f.input ? f.schema.superRefine((value,ctx)=>{for(const issue of inputIssues([{...f.input!,required}],{[k]:value}))ctx.addIssue({code:"custom",message:issue.message});}) : f.schema;
    const schema = required ? checked.refine((v) => v !== "" && !(Array.isArray(v) && v.length === 0), t("Required")) : checked.optional();
    return [k, required ? schema : z.preprocess((v) => (v === "" ? undefined : v), schema)];
  })));
  return object.superRefine((values, ctx) => {
    for(const issue of inputIssues(shown.filter(k=>applies(entity.fields[k]!,values)).flatMap(k=>entity.fields[k]!.input?[entity.fields[k]!.input!]:[]),values))ctx.addIssue({code:"custom",path:issue.path,message:issue.message});
    for (const k of shown) {
      const f = entity.fields[k]!;
      if (f.when && f.required && applies(f, values as Record<string, unknown>) && blank((values as Record<string, unknown>)[k]))
        ctx.addIssue({ code: "custom", path: [k], message: t("Required") });
    }
  });
}

/** The values of a form with its inactive conditional fields left out (#138): the host refuses a value a condition does not allow. */
export function activeValues<R>(entity: Entity<R>, values: Record<string, unknown>): Record<string, unknown> {
  return Object.fromEntries(Object.entries(values).filter(([k]) => applies(entity.fields[k] ?? {}, values)));
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
export function FilterBar<R>({ entity, filters, onChange, maxFilters }: { entity: {fields:Record<string,Pick<FieldType<any,R>,"label"|"editor">&{operators:Pick<Operator<unknown>,"id"|"label"|"needsArg">[]}>}; filters: Filter[]; onChange: (filters: Filter[]) => void; maxFilters?:number }) {
  const keys = Object.keys(entity.fields).filter(key=>entity.fields[key]?.operators.length);
  const update = (i: number, next: Partial<Filter>) => onChange(filters.map((f, j) => (j === i ? { ...f, ...next } : f)));
  return (
    <div className="flex flex-wrap items-center gap-2">
      {filters.map((filter, i) => {
        const field = entity.fields[filter.field]!;
        const op = field?.operators.find((o) => o.id === filter.operator);
        return (
          <div key={i} className="flex items-center gap-1 rounded-md border border-border bg-surface p-1">
            <Select aria-label={t("Field")} value={filter.field} className="h-6 w-32"
              onChange={(e) => update(i, { field: e.target.value, operator: entity.fields[e.target.value]!.operators[0]!.id, arg: undefined })}>
              {keys.map((k) => <option key={k} value={k}>{entity.fields[k]!.label}</option>)}
            </Select>
            <Select aria-label={t("Operator")} value={filter.operator} className="h-6 w-28" onChange={(e) => update(i, { operator: e.target.value })}>
              {field?.operators.map((o) => <option key={o.id} value={o.id}>{o.label}</option>)}
            </Select>
            {op?.needsArg && field.editor && <span className="min-w-28">{field.editor({ value: filter.arg, onChange: (arg) => update(i, { arg }) })}</span>}
            <button type="button" aria-label={t("Remove filter")} className="px-1 text-muted hover:text-foreground"
              onClick={() => onChange(filters.filter((_, j) => j !== i))}><X className="size-3.5" /></button>
          </div>
        );
      })}
      <Button size="sm" variant="ghost" disabled={!keys.length||maxFilters!==undefined&&filters.length>=maxFilters} onClick={() => onChange([...filters, { field: keys[0]!, operator: entity.fields[keys[0]!]!.operators[0]!.id }])}>
        <Plus />{t("Filter")}
      </Button>
    </div>
  );
}
