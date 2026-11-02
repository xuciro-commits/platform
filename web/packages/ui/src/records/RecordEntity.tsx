import { MediaAttachment } from "./MediaAttachment";
import { DateTimeInput } from "../components/DateTimeInput";
import { z } from "zod";
import { defineEntity, type Entity } from "../fields/entity";
import { checkbox, date, datetime, json, longText, markdown, multiSelect, number, singleSelect, tags, text, type FieldType } from "../fields/types";
import { Button } from "../primitives/button";
import { Input } from "../primitives/input";
import { EnterpriseReference, RecordLookup } from "./RecordLookup";
import { RecordChip } from "./RecordChip";
import { t } from "../i18n";
import type {EntityInfo, EntityRecord, FieldInfo, Lifecycle, Money, RecordSource} from "./Records";

const categoryTones = ["info", "success", "warning", "purple", "teal", "pink", "orange", "neutral"] as const;
export function choiceTone(value: string) {
  let hash = 0;
  for (let at = 0; at < value.length; at++) hash = (hash * 31 + value.charCodeAt(at)) | 0;
  return categoryTones[Math.abs(hash) % categoryTones.length]!;
}

let tenantCurrency = "EUR";
const localRowKeys = new WeakMap<object, string>();
const localRowKey = (row: object): string => {
  let key = localRowKeys.get(row);
  if (!key) { key = crypto.randomUUID(); localRowKeys.set(row, key); }
  return key;
};
export const setCurrency = (currency: string) => { if (currency) tenantCurrency = currency; };

const money = (o: { label: string; required?: boolean; readOnly?: boolean }): FieldType<Money> => ({
  type: "money", align: "right", ...o, compare: (a, b) => (a?.amount ?? 0) - (b?.amount ?? 0), operators: [],
  text: (v) => (v ? `${(v.amount / 100).toFixed(2)} ${v.currency}` : ""),
  schema: z.object({ amount: z.number().int(), currency: z.string().length(3) }),
  display: (v) => (v && v.currency ? <span className="tabular-nums">{(v.amount / 100).toLocaleString(undefined, { style: "currency", currency: v.currency })}</span>
    : <span className="text-muted">—</span>),
  editor: ({ id, value, onChange }) => (
    <span className="flex gap-1">
      <Input id={id} type="number" step={0.01} value={value ? value.amount / 100 : ""} className="min-w-28 flex-1"
        onChange={(e) => onChange(e.target.value === "" ? undefined : { amount: Math.round(Number(e.target.value) * 100), currency: value?.currency || tenantCurrency })} />
      <Input aria-label={t("Currency")} value={value?.currency || tenantCurrency} maxLength={3} className="w-16 uppercase"
        onChange={(e) => onChange({ amount: value?.amount ?? 0, currency: e.target.value.toUpperCase() })} />
    </span>
  ),
});

export type Options = Record<string, { value: string; label: string }[]>;

export function entityFrom(info: EntityInfo, options: Options = {}, source?: RecordSource, onOpen?: (type: string, id: string) => void): Entity<EntityRecord> {
  return defineEntity<EntityRecord>({ name: info.type, fields: fieldsOf(info, info.fields, options, source, onOpen), primary: info.display === "id" ? "id" : info.display });
}

function fieldsOf(info: EntityInfo, infos: FieldInfo[], options: Options, source?: RecordSource, onOpen?: (type: string, id: string) => void): Record<string, FieldType<any, EntityRecord>> {
  const fields: Record<string, FieldType<any, EntityRecord>> = {};
  for (const f of infos) {
    const common = { input:{name:f.name,type:f.type==="integer"?"integer":f.type==="decimal"?"number":f.type==="date"||f.type==="datetime"||f.type==="boolean"||f.type==="json"||f.type==="lines"?f.type:["tags","references"].includes(f.type)?"string[]":"string",required:f.required,description:f.title,constraints:f.constraints,range:f.range,group:f.group}, label: f.title, help: f.help, required: f.required, readOnly: f.readOnly, when: f.when };
    fields[f.name] = (() => {
      switch (f.type) {
        case "attachment": return { ...json(common), type: "attachment", schema: z.object({ mediaSet: z.string().min(1), item: z.string().min(1) }), display: value => source ? <MediaAttachment source={source} value={value} /> : <span>{value?.item ?? "—"}</span>, editor: ({ id, value, onChange }) => source ? <MediaAttachment id={id} source={source} value={value} onChange={onChange} /> : null };
        case "json": return json(common);
        case "longtext":
          return info.type === "knowledge.document" && f.name === "text"
            ? markdown(common)
            : longText(common);
        case "integer": return number(common);
        case "decimal": return number({ ...common, decimals: 2 });
        case "money": return money(common);
        case "date": return date(common);
        case "datetime": return {...datetime(common),editor:({value,onChange})=><DateTimeInput value={value??""} title={f.title} offset="Z" onChange={onChange}/>};
        case "boolean": return checkbox(common);
        case "choice": return info.lifecycle?.field === f.name ? lifecycleField(common, info.lifecycle)
          : singleSelect({ ...common, options: (f.choices ?? []).map((c, i) => ({ value: c, label: f.choiceTitles?.[i] ?? c, tone: choiceTone(c) })) });
        case "reference": return source && f.ref
          ? { ...text(common), type: "reference", display: (value) => value === undefined || value === null || value === "" ? <span className="text-muted">—</span>
              : f.ref === "enterprise.element" ? <EnterpriseReference source={source} value={value} onOpen={onOpen} />
              : <RecordChip source={source} type={f.ref!} id={String(value)} person={f.ref === "platform.member"} onOpen={onOpen} />,
            editor: ({ id, value, onChange }) => <RecordLookup id={id} source={source} type={f.ref!} value={value} stereotypes={[...(f.stereotype ? [f.stereotype] : []), ...(f.stereotypes ?? [])]} onChange={onChange} /> }
          : options[f.name] ? singleSelect({ ...common, options: options[f.name]! }) : { ...text(common), readOnly: true };
        case "references": return source&&f.ref ? {...multiSelect({...common,options:options[f.name]??[]}),display:(value)=>!Array.isArray(value)||!value.length?<span className="text-muted">—</span>:<span className="flex max-w-64 flex-wrap gap-1 overflow-hidden">{value.filter(item=>item!=null&&item!=="").map(item=><RecordChip key={String(item)} source={source} type={f.ref!} id={String(item)} person={f.ref==="platform.member"} onOpen={onOpen}/>)}</span>,editor:({id,value,onChange})=>{
          const values:string[]=value??[],stereotypes=[...(f.stereotype?[f.stereotype]:[]),...(f.stereotypes??[])];
          return <div id={id} role="group" aria-label={f.title} className="grid gap-2">{values.map((item,index)=><div key={index} className="flex gap-2"><span className="min-w-0 flex-1"><RecordLookup source={source} type={f.ref!} stereotypes={stereotypes} value={item} ariaLabel={f.title} onChange={next=>onChange(next?values.map((old,at)=>at===index?next:old):values.filter((_,at)=>at!==index))}/></span><Button size="sm" onClick={()=>onChange(values.filter((_,at)=>at!==index))}>{t("Remove")}</Button></div>)}<RecordLookup source={source} type={f.ref!} stereotypes={stereotypes} ariaLabel={f.title} onChange={next=>{if(next&&!values.includes(next))onChange([...values,next]);}}/></div>;
        }}:multiSelect({ ...common, options: options[f.name] ?? [] });
        case "tags": return options[f.name]?.length ? multiSelect({ ...common, options: options[f.name]! }) : tags(common);
        case "lines": return f.fields?.length
          ? lines(common, fieldsOf(info, f.fields, Object.fromEntries(Object.entries(options).flatMap(([k, v]) => k.startsWith(f.name + ".") ? [[k.slice(f.name.length + 1), v]] : [])), source, onOpen))
          : { ...json(common), type:"lines", schema:z.array(z.unknown()) };
        default: return text(common);
      }
    })();
  }
  return fields;
}

export const listed = (entity: Entity<EntityRecord>) => Object.keys(entity.fields).filter((k) => entity.fields[k]!.type !== "lines");

type Line = Record<string, unknown>;

const lines = (common: { label: string; help?: string; required?: boolean; readOnly?: boolean }, columns: Record<string, FieldType<any>>): FieldType<Line[]> => {
  const shown = Object.entries(columns);
  return {
    type: "lines", ...common, schema: z.array(z.record(z.string(), z.unknown())), compare: (a, b) => (a?.length ?? 0) - (b?.length ?? 0), operators: [],
    text: (v) => (v ? String(v.length) : ""),
    display: (v) => !v?.length ? <span className="text-muted">—</span> : (
      <table className="w-full text-sm">
        <thead><tr>{shown.map(([k, c]) => <th key={k} className={`px-1 text-xs font-medium text-muted ${c.align === "right" ? "text-right" : "text-left"}`}>{c.label}</th>)}</tr></thead>
        <tbody>{v.map((line, i) => <tr key={i} className="border-t border-border">
          {shown.map(([k, c]) => <td key={k} className={`px-1 py-0.5 ${c.align === "right" ? "text-right" : ""}`}>{c.display(line[k] as never, line)}</td>)}</tr>)}</tbody>
      </table>
    ),
    editor: ({ id, value, onChange }) => {
      const rows = value ?? [];
      const set = (i: number, k: string, v: unknown) => onChange(rows.map((r, j) => (j === i ? { ...r, [k]: v } : r)));
      return (
        <div id={id} className="grid gap-1 overflow-x-auto">
          <table className="w-full text-sm">
            <thead><tr>{shown.map(([k, c]) => <th key={k} className="px-1 text-left text-xs font-medium text-muted">{c.label}{c.required ? " *" : ""}</th>)}<th /></tr></thead>
            <tbody>{rows.map((line, i) => (
              <tr key={localRowKey(line)}>
                {shown.map(([k, c]) => <td key={k} className="min-w-28 px-1 py-0.5">{!c.readOnly&&c.editor?c.editor({ id: `${id ? `${id}:` : ""}line:${localRowKey(line)}:${k}`, value: line[k] as never, onChange: (v) => set(i, k, v) }):c.display(line[k] as never, line)}</td>)}
                <td><Button size="sm" variant="ghost" aria-label={t("Remove line")} onClick={() => onChange(rows.filter((_, j) => j !== i))}>×</Button></td>
              </tr>
            ))}</tbody>
          </table>
          <div><Button size="sm" onClick={() => onChange([...rows, {}])}>{t("Add line")}</Button></div>
        </div>
      );
    },
  };
};

const lifecycleField = (common: { label: string }, l: Lifecycle): FieldType<string> => ({
  ...singleSelect({ ...common, readOnly: true, options: l.states.map((s) => ({ value: s.name, label: s.title, tone: s.tone })) }),
});

export function StatusBar({ lifecycle, state, can, onTransition, tracker=false }: {
  tracker?:boolean; lifecycle: Lifecycle; state: string; can?: (schema: string) => boolean; onTransition?: (schema: string, title: string) => void;
}) {
  const open = lifecycle.transitions.filter((t) => t.from.includes(state) && (!can || can(t.schema)));
  return (
    <div className="flex flex-wrap items-center gap-2">
      <ol aria-label={tracker?t("Lifecycle stages"):undefined} className={tracker?"flex min-w-0 flex-wrap items-center gap-2 text-xs":"flex overflow-hidden rounded-md border border-border text-xs"}>
        {lifecycle.states.map((s) => (
          <li key={s.name} aria-current={s.name===state?"step":undefined} className={tracker?"flex min-w-0 items-center gap-1 rounded-md border border-border px-2 py-1 "+(s.name===state?"bg-primary font-medium text-primary-foreground":"text-muted"):(s.name === state ? "bg-primary px-2 py-1 font-medium text-primary-foreground" : "px-2 py-1 text-muted")}>{tracker&&<span aria-hidden className={"h-3 w-3 shrink-0 rounded-full border "+(s.name===state?"bg-primary-foreground":"border-border")}/>}<span className="break-words">{s.title}</span></li>
        ))}
      </ol>
      {onTransition && open.map((t) => <Button key={t.schema} size="sm" onClick={() => onTransition(t.schema, t.title)}>{t.title}</Button>)}
    </div>
  );
}

export const displayOf = (info: EntityInfo, r: EntityRecord) => String((info.display === "id" ? r.id : r[info.display]) ?? r.id);

export function groupable(info: EntityInfo): { value: string; label: string }[] {
  const out: { value: string; label: string }[] = [];
  for (const f of info.fields) {
    const repeats = ["choice", "reference", "boolean", "integer"].includes(f.type) || f.type === "text" && !f.search;
    if (repeats) out.push({ value: f.name, label: f.title });
    if (f.type === "date" || f.type === "datetime") for (const u of ["month", "week", "day", "year"]) out.push({ value: `${f.name}:${u}`, label: `${f.title} (${u})` });
  }
  for (const u of ["month", "week", "day"]) out.push({ value: `created:${u}`, label: `${t("Created")} (${t(u)})` });
  return out;
}

export function measurable(info: EntityInfo): { value: string; label: string }[] {
  return [{ value: "count", label: t("Count") }, ...info.fields.filter((f) => ["integer", "decimal", "money"].includes(f.type))
    .flatMap((f) => [{ value: `sum:${f.name}`, label: `${f.title} (sum)` }, { value: `avg:${f.name}`, label: `${f.title} (average)` }])];
}
