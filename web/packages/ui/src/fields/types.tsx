// Field types (Airtable/Directus-style): one object decides how a value is shown,
// edited, validated, sorted, filtered and searched. Domains declare entities with
// them in typed code (entity.tsx); tables, forms, filters and cards follow.
import type {Api} from "@platform/kernel";
import { Check, Copy, ExternalLink, Paperclip, Star, X } from "lucide-react";
import { useContext, type ReactNode } from "react";
import { z } from "zod";
import { cn } from "../lib/cn";
import { Input, Select } from "../primitives/input";
import { Toggles } from "../primitives/controls";
import { Tag, type Tone } from "../components/StatusTag";
import { Markdown, MarkdownEditor } from "../components/Markdown";
import { WorkspaceContext } from "../shell/Workspace";
import type { Route } from "../shell/route";
import { t } from "../i18n";

export type EditorProps<V> = { id?: string; value: V | undefined; onChange: (value: V | undefined) => void; invalid?: boolean; autoFocus?: boolean; describedBy?: string };
export type Operator<V> = { id: string; label: string; needsArg: boolean; test: (value: V | undefined, arg: V | undefined) => boolean };

export type FieldType<V = any, R = any> = {
 input?: Api.Field;
  type: string;
  label: string;
  /** What the field holds, from its declaration (ADR-0023 D1): shown under the label of forms. */
  help?: string;
  required?: boolean;
  readOnly?: boolean;
  /** The field applies only while another field holds one of these values (#138): forms hide it otherwise and send nothing for it. */
  when?: FieldCondition;
  align?: "left" | "right";
  width?: number;
  /** Computed fields (formula, created time) read their value from the row. */
  compute?: (row: R) => V | undefined;
  display: (value: V | undefined, row: R) => ReactNode;
  editor?: (props: EditorProps<V>) => ReactNode;
  /** Validates a present value; `required` decides whether it may be absent. */
  schema: z.ZodType<V>;
  compare: (a: V, b: V) => number;
  operators: Operator<V>[];
  text: (value: V | undefined) => string;
};

export type FieldCondition = { field: string; in: string[] };
/** Whether a conditional field applies to a record's current values ("true"/"false" for booleans). */
export const applies = (f: { when?: FieldCondition }, values: Record<string, unknown>) =>
  !f.when || f.when.in.includes(values[f.when.field] === true ? "true" : values[f.when.field] === false ? "false" : String(values[f.when.field] ?? ""));
type Common = { label: string; help?: string; required?: boolean; readOnly?: boolean; when?: FieldCondition; width?: number };
const empty = (v: unknown) => v === undefined || v === null || v === "" || (Array.isArray(v) && v.length === 0);
const muted = <span className="text-muted">—</span>;
const byString = (a: unknown, b: unknown) => String(a).localeCompare(String(b));
const byNumber = (a: number, b: number) => a - b;
const isEmpty: Operator<any> = { id: "empty", label: t("is empty"), needsArg: false, test: (v) => empty(v) };
const notEmpty: Operator<any> = { id: "notEmpty", label: t("is not empty"), needsArg: false, test: (v) => !empty(v) };
const equals: Operator<any> = { id: "is", label: t("is"), needsArg: true, test: (v, a) => v === a };
const numberOps: Operator<number>[] = [equals,
  { id: "gt", label: ">", needsArg: true, test: (v, a) => v !== undefined && a !== undefined && v > a },
  { id: "lt", label: "<", needsArg: true, test: (v, a) => v !== undefined && a !== undefined && v < a }, isEmpty, notEmpty];
const textOps: Operator<string>[] = [
  { id: "contains", label: t("contains"), needsArg: true, test: (v, a) => !a || (v ?? "").toLowerCase().includes(a.toLowerCase()) },
  equals, isEmpty, notEmpty];
const timeOps: Operator<string>[] = [equals,
  { id: "before", label: t("is before"), needsArg: true, test: (v, a) => !!v && !!a && v < a },
  { id: "after", label: t("is after"), needsArg: true, test: (v, a) => !!v && !!a && v > a }, isEmpty, notEmpty];

function input<V>(type: string, parse: (raw: string) => V | undefined, show: (v: V) => string = String, extra: object = {}) {
  return ({ id, value, onChange, invalid, autoFocus, describedBy }: EditorProps<V>) => (
    <Input id={id} type={type} aria-invalid={invalid} aria-describedby={describedBy} autoFocus={autoFocus} {...extra}
      value={value === undefined || value === null ? "" : show(value)}
      onChange={(e) => onChange(e.target.value === "" ? undefined : parse(e.target.value))} />
  );
}

export const text = (o: Common & { placeholder?: string; maxLength?: number }): FieldType<string> => ({
  type: "text", ...o, display: (v) => (empty(v) ? muted : v), schema: z.string().max(o.maxLength ?? 10_000),
  editor: input("text", (s) => s, String, { placeholder: o.placeholder }), compare: byString, operators: textOps, text: (v) => v ?? "",
});

/** Structured values remain read-only here; their owner supplies any schema-aware editor. */
export const json = (o: Common): FieldType<unknown> => {
  const format = (value: unknown) => value === undefined ? "" : JSON.stringify(value) ?? "";
  return {
    type: "json", ...o, readOnly: true, width: o.width ?? 320,
    display: (value) => value === undefined ? muted : <code className="line-clamp-3 whitespace-pre-wrap break-all text-xs" title={format(value)}>{format(value)}</code>,
    schema: z.json(), text: format, compare: (a, b) => format(a).localeCompare(format(b)), operators: [],
  };
};


export const longText = (o: Common & { rows?: number; placeholder?: string }): FieldType<string> => ({
  ...text(o), type: "longText",
  display: (v) => (empty(v) ? muted : <span className="line-clamp-2 whitespace-pre-wrap">{v}</span>),
  editor: ({ id, value, onChange, invalid, autoFocus, describedBy }) => (
    <textarea id={id} aria-invalid={invalid} aria-describedby={describedBy} autoFocus={autoFocus} rows={o.rows ?? 4} value={value ?? ""}
      placeholder={o.placeholder}
      onChange={(e) => onChange(e.target.value || undefined)}
      className="w-full min-h-[88px] rounded-md border border-border bg-surface px-2.5 py-1.5 text-sm outline-none focus-visible:border-ring" />
  ),
});

export const markdown = (o: Common & { rows?: number; placeholder?: string }): FieldType<string> => ({
  ...text(o), type: "markdown",
  display: (v) => (empty(v) ? muted : <Markdown content={v!} />),
  editor: ({ id, value, onChange, invalid, autoFocus }) => (
    <MarkdownEditor id={id} value={value} onChange={onChange} invalid={invalid} autoFocus={autoFocus} placeholder={o.placeholder} rows={o.rows} />
  ),
});

export const number = (o: Common & { decimals?: number; unit?: string; min?: number; max?: number }): FieldType<number> => {
  const format = (v: number) => v.toLocaleString(undefined, { minimumFractionDigits: o.decimals ?? 0, maximumFractionDigits: o.decimals ?? 0 });
  return {
    type: "number", align: "right", ...o, compare: byNumber, operators: numberOps, text: (v) => (v === undefined ? "" : String(v)),
    schema: z.number().min(o.min ?? -Infinity).max(o.max ?? Infinity),
    display: (v) => (v === undefined || v === null ? muted : <span className="tabular-nums">{format(v)}{o.unit ? ` ${o.unit}` : ""}</span>),
    editor: input("number", (s) => Number(s), String, { optional:!o.required,step: o.decimals ? 1 / 10 ** o.decimals : 1 }),
  };
};

export const currency = (o: Common & { currency: string }): FieldType<number> => ({
  ...number({ ...o, decimals: 2 }), type: "currency",
  display: (v) => (v === undefined || v === null ? muted
    : <span className="tabular-nums">{v.toLocaleString(undefined, { style: "currency", currency: o.currency })}</span>),
});

export const percent = (o: Common): FieldType<number> => ({
  ...number({ ...o, min: 0, max: 1 }), type: "percent",
  display: (v) => (v === undefined || v === null ? muted : (
    <span className="flex items-center justify-end gap-2 tabular-nums">
      <span className="h-1.5 w-12 overflow-hidden rounded-full bg-border"><span className="block h-full bg-primary" style={{ width: `${v * 100}%` }} /></span>
      {Math.round(v * 100)}%
    </span>)),
  editor: input("number", (s) => Number(s) / 100, (v) => String(Math.round(v * 100)), { min: 0, max: 100 }),
});

export const checkbox = (o: Common): FieldType<boolean> => ({
  type: "checkbox", align: "left", width: 90, ...o, schema: z.boolean(), compare: (a, b) => Number(a) - Number(b), text: (v) => v === undefined ? "" : v ? "yes" : "no",
  operators: [{ id: "checked", label: t("is checked"), needsArg: false, test: (v) => v === true }, { id: "unchecked", label: t("is not checked"), needsArg: false, test: (v) => v === false }, isEmpty, notEmpty],
  display: (v) => v === undefined || v === null ? muted : v ? <Check className="size-3.5 text-[var(--tone-success)]" aria-label="yes" /> : <span className="text-muted">{t("No")}</span>,
  editor: ({ id, value, onChange }) => <span className="inline-flex items-center gap-2">
    <input id={id} type="checkbox" checked={!!value} onChange={(e) => onChange(e.target.checked)} className="size-4 accent-[var(--primary)]" />
    {!o.required && value !== undefined && <button type="button" className="rounded p-0.5 text-muted hover:bg-row-hover" aria-label={t("Clear value")} title={t("Clear value")} onClick={() => onChange(undefined)}><X className="size-3.5" /></button>}
  </span>,
});

export const date = (o: Common): FieldType<string> => ({
  type: "date", width: 110, ...o, schema: z.iso.date(), compare: byString, operators: timeOps, text: (v) => v ?? "",
  display: (v) => (v ? <span className="tabular-nums">{v}</span> : muted), editor: input("date", (s) => s),
});

/** Stored as `YYYY-MM-DDTHH:MM` local time, the value of a datetime-local input. */
export const datetime = (o: Common): FieldType<string> => ({
  ...date(o), type: "datetime", width: 150, schema: z.string().regex(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/, "Date and time"),
  display: (v) => (v ? <span className="tabular-nums">{v.slice(0, 16).replace("T", " ")}</span> : muted),
  editor: input("datetime-local", (s) => s, (v) => v.slice(0, 16)),
});

/** Minutes, shown as `1h 30m`. */
export const duration = (o: Common): FieldType<number> => ({
  ...number({ ...o, min: 0 }), type: "duration",
  display: (v) => (v === undefined || v === null ? muted : <span className="tabular-nums">{Math.floor(v / 60) ? `${Math.floor(v / 60)}h ` : ""}{v % 60}m</span>),
});

export type Option = { value: string; label: string; tone?: Tone };

export const singleSelect = (o: Common & { options: Option[] }): FieldType<string> => {
  const find = (v?: string) => o.options.find((x) => x.value === v);
  return {
    type: "singleSelect", width: 130, ...o, schema: z.enum(o.options.map((x) => x.value) as [string, ...string[]]),
    compare: (a, b) => o.options.findIndex((x) => x.value === a) - o.options.findIndex((x) => x.value === b),
    operators: [equals, { id: "isNot", label: t("is not"), needsArg: true, test: (v, a) => v !== a }, isEmpty, notEmpty],
    text: (v) => find(v)?.label ?? v ?? "",
    display: (v) => (v ? <Tag label={find(v)?.label ?? v} tone={find(v)?.tone} /> : muted),
    editor: ({ id, value, onChange, invalid, autoFocus, describedBy }) => (
      <Select id={id} aria-invalid={invalid} aria-describedby={describedBy} autoFocus={autoFocus} value={value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
        <option value="">—</option>{o.options.map((x) => <option key={x.value} value={x.value}>{x.label}</option>)}
      </Select>
    ),
  };
};

export const multiSelect = (o: Common & { options: Option[] }): FieldType<string[]> => {
  const find = (v: string) => o.options.find((x) => x.value === v);
  return {
    type: "multiSelect", width: 180, ...o, schema: z.array(z.enum(o.options.map((x) => x.value) as [string, ...string[]])),
    compare: (a, b) => a.length - b.length, text: (v) => (v ?? []).map((x) => find(x)?.label ?? x).join(" "),
    operators: [{ id: "has", label: "has", needsArg: true, test: (v, a) => !!a?.every((x) => v?.includes(x)) }, isEmpty, notEmpty],
    display: (v) => (empty(v) ? muted : <span className="flex gap-1 overflow-hidden">{v!.map((x) => <Tag key={x} label={find(x)?.label ?? x} tone={find(x)?.tone} />)}</span>),
    editor: ({ value = [], onChange }) => <Toggles options={o.options} value={value} onChange={onChange} />,
  };
};

/** Words a person types, each kept as its own value: labels, field names, tags
 *  with no list to choose from. Enter or a comma adds one; each chip removes. */
export const tags = (o: Common & { placeholder?: string }): FieldType<string[]> => ({
  type: "tags", width: 200, ...o, schema: z.array(z.string().min(1)),
  compare: (a, b) => a.length - b.length, text: (v) => (v ?? []).join(" "),
  operators: [{ id: "has", label: "has", needsArg: true, test: (v, a) => !!a?.every((x) => v?.includes(x)) }, isEmpty, notEmpty],
  display: (v) => (empty(v) ? muted : <span className="flex gap-1 overflow-hidden">{v!.map((x) => <Tag key={x} label={x} />)}</span>),
  editor: ({ id, value = [], onChange, invalid, autoFocus }) => {
    const add = (word: string) => {
      const next = word.trim();
      if (next && !value.includes(next)) onChange([...value, next]);
    };
    return (
      <span className="flex flex-wrap items-center gap-1">
        {value.map((x) => (
          <button key={x} type="button" onClick={() => onChange(value.filter((y) => y !== x))} aria-label={`${t("Remove")} ${x}`}
            className="flex items-center gap-1 rounded-sm border border-border px-1.5 py-0.5 text-xs hover:bg-row-hover">
            {x}<X className="size-3" aria-hidden />
          </button>
        ))}
        <Input id={id} aria-invalid={invalid} autoFocus={autoFocus} placeholder={o.placeholder ?? t("Add…")} className="w-32"
          onKeyDown={(e) => {
            const field = e.currentTarget;
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              add(field.value);
              field.value = "";
            } else if (e.key === "Backspace" && field.value === "" && value.length > 0) {
              onChange(value.slice(0, -1));
            }
          }}
          onBlur={(e) => { add(e.currentTarget.value); e.currentTarget.value = ""; }} />
      </span>
    );
  },
});

export const email = (o: Common): FieldType<string> => ({
  ...text(o), type: "email", schema: z.email("Email address"),
  display: (v) => (v ? <a href={`mailto:${v}`} className="text-primary hover:underline" onClick={(e) => e.stopPropagation()}>{v}</a> : muted),
  editor: input("email", (s) => s),
});

export const url = (o: Common): FieldType<string> => ({
  ...text(o), type: "url", schema: z.url("Web address"),
  display: (v) => (v ? <a href={v} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}
    className="inline-flex items-center gap-1 text-primary hover:underline">{v.replace(/^https?:\/\//, "")}<ExternalLink className="size-3" /></a> : muted),
  editor: input("url", (s) => s),
});

export const phone = (o: Common): FieldType<string> => ({
  ...text(o), type: "phone", schema: z.string().regex(/^\+?[\d\s()-]{5,}$/, "Phone number"),
  display: (v) => (v ? <a href={`tel:${v.replace(/\s/g, "")}`} className="tabular-nums hover:underline" onClick={(e) => e.stopPropagation()}>{v}</a> : muted),
  editor: input("tel", (s) => s),
});

/** Codes read by scanners (EAN, Code 128, QR payloads); scanners type into the editor like a keyboard. */
export const barcode = (o: Common & { pattern?: RegExp }): FieldType<string> => ({
  ...text(o), type: "barcode", width: 160, schema: o.pattern ? z.string().regex(o.pattern, "Barcode format") : z.string(),
  display: (v) => (v ? (
    <span className="inline-flex items-center gap-1 font-mono text-xs">{v}
      <button type="button" aria-label={t("Copy")} className="text-muted hover:text-foreground"
        onClick={(e) => { e.stopPropagation(); void navigator.clipboard?.writeText(v); }}><Copy className="size-3" /></button>
    </span>) : muted),
  editor: input("text", (s) => s.trim(), String, { className: "font-mono", inputMode: "numeric", autoComplete: "off" }),
});

export const rating = (o: Common & { max?: number }): FieldType<number> => {
  const max = o.max ?? 5;
  const stars = (v: number, onChange?: (v: number) => void) => (
    <span className="inline-flex">{Array.from({ length: max }, (_, i) => (
      <button key={i} type="button" disabled={!onChange} aria-label={`${i + 1} of ${max}`} onClick={() => onChange?.(i + 1 === v ? 0 : i + 1)}>
        <Star className={cn("size-3.5", i < v ? "fill-[var(--tone-warning)] text-[var(--tone-warning)]" : "text-border")} />
      </button>))}</span>
  );
  return { ...number({ ...o, min: 0, max }), type: "rating", align: "left", width: 110,
    display: (v) => stars(v ?? 0), editor: ({ value, onChange }) => stars(value ?? 0, onChange) };
};

export type Attachment = { name: string; url: string; size?: number };

/** Files attached to a record; storing them is a capability of the app (object storage). */
export const attachment = (o: Common & { accept?: string; upload?: (file: File) => Promise<Attachment> }): FieldType<Attachment[]> => ({
  type: "attachment", width: 180, ...o, schema: z.array(z.object({ name: z.string(), url: z.string(), size: z.number().optional() })),
  compare: (a, b) => a.length - b.length, operators: [isEmpty, notEmpty], text: (v) => (v ?? []).map((a) => a.name).join(" "),
  display: (v) => (empty(v) ? muted : (
    <span className="flex gap-1 overflow-hidden">{v!.map((a) => (
      <a key={a.url} href={a.url} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}
        className="inline-flex items-center gap-1 rounded-sm border border-border px-1 text-xs hover:bg-row-hover"><Paperclip className="size-3" />{a.name}</a>))}
    </span>)),
  editor: ({ id, value = [], onChange }) => (
    <span className="grid gap-1">
      {value.map((a) => (
        <span key={a.url} className="flex items-center gap-1 text-xs"><Paperclip className="size-3" />{a.name}
          <button type="button" aria-label={`Remove ${a.name}`} onClick={() => onChange(value.filter((x) => x !== a))}><X className="size-3" /></button>
        </span>))}
      <input id={id} type="file" multiple accept={o.accept} className="text-xs"
        onChange={async (e) => {
          const files = Array.from(e.target.files ?? []);
          const added = await Promise.all(files.map((f) => o.upload?.(f) ?? Promise.resolve({ name: f.name, url: URL.createObjectURL(f), size: f.size })));
          onChange([...value, ...added]);
        }} />
    </span>
  ),
});

/** A reference to another entity; shown as a link that opens its route in the workspace. */
export const link = (o: Common & { to: (id: string) => Route; title?: (id: string) => string }): FieldType<string> => ({
  ...text(o), type: "link",
  display: (v) => (v ? <LinkChip id={v} route={o.to(v)} title={o.title?.(v) ?? v} /> : muted),
});

function LinkChip({ id, route, title }: { id: string; route: Route; title: string }) {
  const workspace = useContext(WorkspaceContext);
  return (
    <button type="button" data-entity={id} disabled={!workspace} onClick={(e) => { e.stopPropagation(); workspace?.open(route); }}
      className="rounded-sm bg-row-selected px-1.5 text-xs font-medium text-primary hover:underline disabled:no-underline">{title}</button>
  );
}

/** Read-only value computed from the row, shown with another field type's display. */
export const formula = <V, R>(o: { label: string; compute: (row: R) => V | undefined; as: FieldType<V> }): FieldType<V, R> => ({
  ...o.as, type: "formula", label: o.label, readOnly: true, required: false, compute: o.compute, editor: undefined,
});

/** When the record was created or last changed (K4 recorded time). */
export const timestamp = <R,>(o: { label: string; of: (row: R) => string | undefined }): FieldType<string, R> =>
  formula({ label: o.label, compute: o.of, as: datetime({ label: o.label }) });
