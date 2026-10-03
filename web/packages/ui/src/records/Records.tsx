// The application model's pages (ADR-0016): an entity type described by the
// host (`GET /v1/entities`) becomes a kit entity, a list page with server-side
// search, sort and paging, a record page (fields, related records, history) and
// generated forms. Components take a RecordSource, so the kit knows no client.
import { ChevronLeft, ChevronRight, History as HistoryIcon } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { z } from "zod";
import {RecordCards} from "./RecordCards";
import {RecordComments} from "./RecordComments";
import {RecordUploader} from "./RecordUploader";
import {EditableRecordGrid,type RecordEditPort,type RecordSelectionPort} from "./EditableRecordGrid";
import {presentRecordColumns,type RecordColumnPresentation} from "./ColumnPresentation";
import { DataTable } from "../components/DataTable";
import {ContentTabs} from "../layout/ContentTabs";
import {pageUIManifest} from "@platform/kernel";
import { PropertyList } from "../components/EntityCard";
import { Tag } from "../components/StatusTag";
import { columnsFor, defineEntity, type Entity } from "../fields/entity";
import { checkbox, date, datetime, longText, markdown, multiSelect, number, singleSelect, tags, text, type FieldType } from "../fields/types";
import { Button } from "../primitives/button";
import { Input, Select } from "../primitives/input";
import { RecordLookup } from "./RecordLookup";
import { Chart } from "../charts/Chart";
import { Graph, type GraphEdge, type GraphNode } from "../graph/Graph";
import { Pivot } from "../charts/Pivot";
import type { AggregateData, AggregateQuery, ChartSpec, Mark } from "../charts/spec";
import { t } from "../i18n";
import { humanizeKernelError } from "../lib/errors";
import type { Api } from "@platform/kernel";

// What the host describes is generated from its Go types (ADR-0023 D7); the kit
// only refines what it holds in general: any entity's record.
export type FieldInfo = Api.FieldInfo;
export type State = Api.State;
export type Lifecycle = Api.LifecycleInfo;
export type EntityInfo = Api.EntityInfo;
export type Stamp = Api.Stamp;
export type EntityRecord = { id: string; revision: number; created: Stamp; changed: Stamp; archived?: boolean } & Record<string, unknown>;
export type RecordQuery = Omit<Api.Query,"domain"> & {domain?:unknown[]};
export type RecordPageData = Omit<Api.RecordPage, "records"> & { records: EntityRecord[] };
export type RecordChange = Api.RecordChange;
/** A file attached to a record (ADR-0028). */
export type AttachedFile = EntityRecord & { name: string; size: number; contentType: string; by: string };

/** A comment on a record (ADR-0028 D6). */
export type RecordComment = EntityRecord & { text: string; by: string; mentions?: string[] };

export type RecordView = Omit<Api.RecordView, "record" | "related" | "linked" | "activity" | "processes" | "approvals" | "tasks" | "files" | "comments"> & {
  /** Open tasks about the record the member may take (a flow's question, a correction to make). */
  tasks: Api.InboxTask[];
  /** The approval requests to move the record, newest first (F-38). */
  approvals: Api.ApprovalRequest[];
  files: AttachedFile[];
  comments: RecordComment[];
  record: EntityRecord; related: RelatedRecords[];
  /** Records of any app linked to it (the relations app), by type. */
  linked: RelatedRecords[];
  /** What apps told about it through protocols, oldest first. */
  activity: Api.Note[];
  /** The flow instances about the record (ADR-0026 D4). */
  processes: (EntityRecord & { title: string; state: string; tokens?: { step: string }[] })[];
};
type RelatedRecords = Omit<Api.Related, "records"> & { records: EntityRecord[] };
export type Money = { amount: number; currency: string };

/** Where records come from: the host's reads, wired by the app; with aggregates, lists can group, pivot and chart (ADR-0019). */
export type RecordSource = {
  /** Stable member/definition scope; changing it discards cached view data. */
  scope?: string;
  entity: (type: string) => EntityInfo | undefined;
  list: (type: string, query: RecordQuery) => Promise<RecordPageData>;
  get: (type: string, id: string) => Promise<RecordView>;
  aggregate?: (type: string, query: AggregateQuery) => Promise<AggregateData>;
  /** Moves each time the host's data changed (F-32): lists, pages, charts and pivots read again. */
  revision?: number;
};

// The tenant's currency (ADR-0024): the default of amounts people enter; the workspace sets it from /v1/me.
let tenantCurrency = "EUR";
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

/** A kit entity from the host's description; `options` gives the choices of reference fields, a line's by "<lines>.<column>". */
export function entityFrom(info: EntityInfo, options: Options = {}, source?: RecordSource): Entity<EntityRecord> {
  return defineEntity<EntityRecord>({ name: info.type, fields: fieldsOf(info, info.fields, options, source), primary: info.display === "id" ? "id" : info.display });
}

function fieldsOf(info: EntityInfo, infos: FieldInfo[], options: Options, source?: RecordSource): Record<string, FieldType<any, EntityRecord>> {
  const fields: Record<string, FieldType<any, EntityRecord>> = {};
  for (const f of infos) {
    const common = { label: f.title, help: f.help, required: f.required, readOnly: f.readOnly };
    fields[f.name] = (() => {
      switch (f.type) {
        case "longtext":
          return info.type === "knowledge.document" && f.name === "text"
            ? markdown(common)
            : longText(common);
        case "integer": return number(common);
        case "decimal": return number({ ...common, decimals: 2 });
        case "money": return money(common);
        case "date": return date(common);
        case "datetime": return datetime(common);
        case "boolean": return checkbox(common);
        case "choice": return info.lifecycle?.field === f.name ? lifecycleField(common, info.lifecycle)
          : singleSelect({ ...common, options: (f.choices ?? []).map((c, i) => ({ value: c, label: f.choiceTitles?.[i] ?? c })) });
        case "reference": return source && f.ref
          ? { ...text(common), type: "reference", editor: ({ id, value, onChange }) => <RecordLookup id={id} source={source} type={f.ref!} value={value} onChange={onChange} /> }
          : options[f.name] ? singleSelect({ ...common, options: options[f.name]! }) : { ...text(common), readOnly: true };
        case "references": return multiSelect({ ...common, readOnly: true, options: options[f.name] ?? [] });
        // Tags with a list to choose from are that list; tags without one are
        // words a person types (labels, field names).
        case "tags": return options[f.name]?.length ? multiSelect({ ...common, options: options[f.name]! }) : tags(common);
        case "lines": return f.fields?.length
          ? lines(common, fieldsOf(info, f.fields.filter(field=>!field.aside), Object.fromEntries(Object.entries(options).flatMap(([k, v]) => k.startsWith(f.name + ".") ? [[k.slice(f.name.length + 1), v]] : [])), source))
          : { ...text(common), readOnly: true, display: (v: unknown) => <span className="text-muted">{Array.isArray(v) ? `${v.length} lines` : "—"}</span> } as FieldType<any>;
        default: return text(common);
      }
    })();
  }
  return fields;
}

// Lists show a record per row: its lines are for its page.
const listed = (entity: Entity<EntityRecord>) => Object.keys(entity.fields).filter((k) => entity.fields[k]!.type !== "lines");

type Line = Record<string, unknown>;

// Child lines (Odoo's one2many, ADR-0024): a table on record pages, and rows
// to add, edit and remove in forms; the app's rules check them.
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
              <tr key={i}>
                {shown.map(([k, c]) => <td key={k} className="min-w-28 px-1 py-0.5">{c.editor?.({ value: line[k] as never, onChange: (v) => set(i, k, v) }) ?? c.display(line[k] as never, line)}</td>)}
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

// A status field shows its state with the lifecycle's tones.
const lifecycleField = (common: { label: string }, l: Lifecycle): FieldType<string> => ({
  ...singleSelect({ ...common, readOnly: true, options: l.states.map((s) => ({ value: s.name, label: s.title, tone: s.tone })) }),
});

/** The lifecycle's states, the current one marked, and the transitions the caller may take from it. */
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

const displayOf = (info: EntityInfo, r: EntityRecord) => String((info.display === "id" ? r.id : r[info.display]) ?? r.id);

/** A list of one entity type: server-side search, sort and paging; a row opens the record. */
/** The fields a list can group by (ADR-0019): values that repeat, and dates by bucket. */
export function groupable(info: EntityInfo): { value: string; label: string }[] {
  const out: { value: string; label: string }[] = [];
  for (const f of info.fields) {
    const repeats = ["choice", "reference", "boolean", "integer"].includes(f.type) || f.type === "text" && !f.search; // searched text is mostly names
    if (repeats) out.push({ value: f.name, label: f.title });
    if (f.type === "date" || f.type === "datetime") for (const u of ["month", "week", "day", "year"]) out.push({ value: `${f.name}:${u}`, label: `${f.title} (${u})` });
  }
  for (const u of ["month", "week", "day"]) out.push({ value: `created:${u}`, label: `${t("Created")} (${t(u)})` });
  return out;
}

/** The measures a list can show: the count, and sums and averages of numbers and money. */
export function measurable(info: EntityInfo): { value: string; label: string }[] {
  return [{ value: "count", label: t("Count") }, ...info.fields.filter((f) => ["integer", "decimal", "money"].includes(f.type))
    .flatMap((f) => [{ value: `sum:${f.name}`, label: `${f.title} (sum)` }, { value: `avg:${f.name}`, label: `${f.title} (average)` }])];
}

type ListView = "list" | "pivot" | "chart";
/** What a list shows: kept by a member as a saved view (ADR-0019 D4). */
export type ListState = {
  view?: ListView; search?: string; sort?: string; archived?: boolean; drilled?: unknown[];
  group?: string; columns?: string; measure?: string; mark?: Mark;
};

export function RecordList({ source, type, onOpen, toolbar, height = "calc(100dvh - 230px)", pageSize = 100, domain: fixed, initial = {}, onSave, fields, window,inlineEdit,selectionSet,columnPresentation,showSearch=true,cards,selectedId,onNavigate }: {
  onNavigate?:(record:EntityRecord)=>void;cards?:{layout:"grid"|"list";labelField:string};selectedId?:string;columnPresentation?:RecordColumnPresentation[];showSearch?:boolean;
  selectionSet?:RecordSelectionPort;
  inlineEdit?:RecordEditPort;
  source: RecordSource; type: string; onOpen?: (r: EntityRecord) => void; toolbar?: ReactNode; height?: number | string; pageSize?: number;
  /** Presentation subset. The source's permission-filtered entity is still authoritative. */
  fields?: string[];
  /** A caller-owned authorized window. Controls emit view changes and never
   * issue another list/aggregate read or override the owning plan's domain. */
  window?: { query:RecordQuery; page?:RecordPageData; error?:string; searchLocked?:boolean; sortLocked?:boolean; inputSearch?:string; maxOffset:number; onChange:(change:{search?:string;sort?:string[];offset?:number})=>void };
  /** Always applied, like an app's own view of the type. */
  domain?: unknown[];
  /** Where the list starts, such as a saved view; `onSave` offers to save where it is. */
  initial?: ListState; onSave?: (state: ListState) => void;
}) {
  const cardIdentity=JSON.stringify([source.scope,type,cards]),[cardChoice,setCardChoice]=useState<{identity:string;layout:"grid"|"list"}>();
  const cardLayout=cardChoice?.identity===cardIdentity?cardChoice.layout:cards?.layout??"grid";
  const info = source.entity(type);
  const [localSearch, setSearch] = useState(initial.search ?? "");
  const [localSort, setSort] = useState(initial.sort ?? "-changed");
  const [localOffset, setOffset] = useState(0);
  const [archived, setArchived] = useState(initial.archived ?? false);
  const [loadedPage, setLoadedPage] = useState<{ scope?: string; page: RecordPageData }>();
  const page = window ? window.page : loadedPage?.scope === source.scope ? loadedPage?.page : undefined;
  const search = window ? window.inputSearch ?? window.query.search ?? "" : localSearch, sort = window ? window.query.sort?.[0] ?? "id" : localSort, offset = window ? window.query.offset ?? 0 : localOffset;
  if (window) pageSize = window.query.limit ?? pageSize;
  const windowKey = JSON.stringify(window?.query);
  const [localError, setError] = useState<string>();
  const error = window ? window.error : localError;
  const [localView, setView] = useState<ListView>(initial.view ?? "list");
  const view = window ? "list" : localView;
  const [drilled, setDrilled] = useState<unknown[] | undefined>(initial.drilled);
  const groups = useMemo(() => (info ? groupable(info) : []), [info]);
  const measures = useMemo(() => (info ? measurable(info) : []), [info]);
  const [group, setGroup] = useState(initial.group ?? "");
  const [columns, setColumns] = useState(initial.columns ?? "");
  const [measure, setMeasure] = useState(initial.measure ?? "count");
  const [mark, setMark] = useState<Mark>(initial.mark ?? "bar");
  const rows = group || (info?.lifecycle?.field ?? groups[0]?.value ?? "");
  const fixedKey = JSON.stringify(fixed ?? []);
  useEffect(() => { setOffset(0); }, [fixedKey, type]);
  const domain = useMemo(() => [...JSON.parse(fixedKey), ...(drilled ?? [])], [fixedKey, drilled]);
  const entity = useMemo(() => (info ? entityFrom(info,{},source) : undefined), [info,source]);
  useEffect(() => {
    if (window || !info || view !== "list") return;
    let current = true;
    const scope = source.scope;
    setLoadedPage(undefined); setError(undefined);
    const field = sort.replace(/^-/, "");
    const known = ["id", "created", "changed"].includes(field) || info.fields.some((f) => f.name === field);
    const handle = setTimeout(() => {
      source.list(type, { domain, search, sort: known ? [sort] : ["id"], offset, limit: pageSize, archived })
        .then((p) => { if (current && source.scope === scope) { setLoadedPage({ scope, page: p }); setError(undefined); } }, (e) => { if (current && source.scope === scope) setError(String(e)); });
    }, 150);
    return () => { current = false; clearTimeout(handle); };
  }, [source, source.scope, source.revision, type, info, search, sort, offset, archived, pageSize, domain, view, windowKey]);
  if (!info || !entity) return <p className="text-sm text-muted">{t("Unknown entity type")} {type}.</p>;
  const columnsOf = presentRecordColumns([{ id: "id", header: "ID", accessorKey: "id", meta: { width: 130 }, cell: (c: any) => <span className="font-mono text-xs">{c.getValue()}</span> },
    ...columnsFor(entity,[...new Set(fields??listed(entity))].filter(name=>listed(entity).includes(name))).map((c) => ({ ...c, enableSorting: false }))],entity,info,columnPresentation);
  const total = page?.total ?? 0;
  const aggregate = window ? undefined : source.aggregate;
  const query = { domain, search, archived };
  const measureEncoding = measure === "count" ? { type: "quantitative" as const, aggregate: "count" as const }
    : { type: "quantitative" as const, aggregate: measure.split(":")[0] as "sum" | "avg", field: measure.split(":")[1] };
  const groupEncoding = { field: rows.split(":")[0], type: rows.includes(":") ? "temporal" as const : "nominal" as const, ...(rows.includes(":") ? { timeUnit: rows.split(":")[1] as "month" } : {}) };
  const spec: ChartSpec = {
    data: { entity: type, domain, search, archived }, mark,
    encoding: mark === "arc" ? { theta: measureEncoding, color: groupEncoding } : { x: groupEncoding, y: measureEncoding },
  };
  return (
    <div className="grid min-w-0 grid-cols-1 gap-2">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        {showSearch&&<Input aria-label={t("Search")} placeholder={t("Search {things}", { things: info.plural.toLowerCase() })} value={search} disabled={window?.searchLocked} className="w-56"
          onChange={(e) => { if(window)window.onChange({search:e.target.value,offset:0});else {setSearch(e.target.value);setOffset(0);} }} />}
        {view === "list" ? (
          <Select aria-label={t("Sort")} value={sort} disabled={window?.sortLocked} className="w-48" onChange={(e) => { if(window)window.onChange({sort:[e.target.value],offset:0});else {setSort(e.target.value);setOffset(0);} }}>
            {[["-changed", t("Recently changed")], ["id", t("ID")], ...info.fields.filter((f) => f.type !== "references" && f.type !== "tags").flatMap((f) =>
              [[f.name, `${f.title} ↑`], [`-${f.name}`, `${f.title} ↓`]])].map(([v, l]) => <option key={v} value={v}>{l}</option>)}
          </Select>
        ) : <>
          <Select aria-label={t("Group by")} value={rows} className="w-44" onChange={(e) => setGroup(e.target.value)}>
            {groups.map((g) => <option key={g.value} value={g.value}>{g.label}</option>)}
          </Select>
          {view === "pivot" && (
            <Select aria-label={t("Columns")} value={columns} className="w-40" onChange={(e) => setColumns(e.target.value)}>
              <option value="">{t("No columns")}</option>
              {groups.filter((g) => g.value !== rows).map((g) => <option key={g.value} value={g.value}>{g.label}</option>)}
            </Select>
          )}
          <Select aria-label={t("Measure")} value={measure} className="w-40" onChange={(e) => setMeasure(e.target.value)}>
            {measures.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </Select>
          {view === "chart" && (
            <Select aria-label={t("Chart")} value={mark} className="w-28" onChange={(e) => setMark(e.target.value as Mark)}>
              {([["bar", t("Bars")], ["line", t("Line")], ["area", t("Area")], ["arc", t("Pie")]] as const).map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </Select>
          )}
        </>}
        {!window && <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={archived} onChange={(e) => { setArchived(e.target.checked); setOffset(0); }} />archived</label>}
        {drilled && <Button size="sm" variant="ghost" onClick={() => { setDrilled(undefined); setOffset(0); }}>{t("Clear drill-down ×")}</Button>}
        {cards&&<span role="group" aria-label={t("Record layout")} className="flex gap-1">{(["list","grid"] as const).map(layout=><Button key={layout} size="sm" variant="ghost" aria-pressed={cardLayout===layout} onClick={()=>setCardChoice({identity:cardIdentity,layout})}>{t(layout==="grid"?"Grid":"List")}</Button>)}</span>}
        {toolbar}
        {onSave && <Button size="sm" variant="ghost" onClick={() => onSave({ view, search, sort, archived, drilled, group: rows, columns, measure, mark })}>{t("Save view…")}</Button>}
        {aggregate && (
          <span role="group" aria-label={t("View")} className="flex rounded-md border border-border">
            {(["list", "pivot", "chart"] as const).map((v) => (
              <button key={v} type="button" aria-pressed={view === v} onClick={() => setView(v)}
                className={`h-7 px-2 text-xs capitalize ${view === v ? "bg-row-selected font-medium" : "hover:bg-row-hover"}`}>{v}</button>
            ))}
          </span>
        )}
        {view === "list" && (
          <span className="ml-auto flex items-center gap-1 text-xs text-muted">
            {error ?? (total ? `${offset + 1}–${Math.min(offset + pageSize, total)} of ${total}` : "none")}
            <Button size="sm" variant="ghost" aria-label={t("Previous page")} disabled={offset === 0} onClick={() => window ? window.onChange({offset:Math.max(0,offset-pageSize)}) : setOffset(Math.max(0, offset - pageSize))}><ChevronLeft /></Button>
            <Button size="sm" variant="ghost" aria-label={t("Next page")} disabled={offset + pageSize >= total || !!window && offset+pageSize>window.maxOffset} onClick={() => window ? window.onChange({offset:offset+pageSize}) : setOffset(offset + pageSize)}><ChevronRight /></Button>
          </span>
        )}
      </div>
      {view === "list" && cards && (error?<p role="alert" className="text-sm text-danger">{humanizeKernelError(error)}</p>:!page?<p role="status" className="text-sm text-muted">{t("Loading…")}</p>:<RecordCards records={page.records} info={info} fields={fields} labelField={cards.labelField} layout={cardLayout} selected={selectedId} onSelect={onOpen} onNavigate={onNavigate}/>)}
      {view === "list" && !cards && (
        <EditableRecordGrid key={JSON.stringify([source.scope,type,info,inlineEdit?.schema,inlineEdit?.fields,inlineEdit?.scope,inlineEdit?.preview,domain,search,sort,offset,archived,error])} data={page?.records} columns={columnsOf as never} entity={entity} height={height} port={inlineEdit} selectionSet={selectionSet} onOpen={onOpen} loading={!page && !error} empty={error ? humanizeKernelError(error) : t("No {things}", { things: info.plural.toLowerCase() })}/>
      )}
      {view === "pivot" && aggregate && rows && (
        <Pivot source={{ aggregate, revision: source.revision }} type={type} query={query} rows={rows} columns={columns || undefined} measure={measure}
          onDrill={(d) => { setDrilled([...(drilled ?? []), ...d]); setOffset(0); setView("list"); }} />
      )}
      {view === "chart" && aggregate && rows && <Chart spec={spec} source={{ aggregate, revision: source.revision }} height={360} />}
    </div>
  );
}

/** A record's history from the journal: who changed what, newest last. Its
 *  page shows it, and so does a composed page's timeline (ADR-0035 16b). */
export function RecordHistory({ info, history = [], heading = true }: { info: EntityInfo; history?: RecordView["history"]; heading?: boolean }) {
  return (
    <section aria-label={t("History")}>
      {heading && <h2 className="mb-1 flex items-center gap-1 text-sm font-semibold"><HistoryIcon className="size-3.5" />{t("History")}</h2>}
      {(history ?? []).length === 0 && <p className="text-sm text-muted">{t("No changes yet.")}</p>}
      <ol className="grid gap-2">
        {(history ?? []).map((h, i) => (
          <li key={`${h.change}:${i}`} className="rounded-md border border-border bg-surface p-2 text-xs">
            <div className="flex gap-2"><span className="font-mono">{h.schema}</span><span className="text-muted">{h.by} · {new Date(h.at).toLocaleString()}</span></div>
            {h.fields.length > 0 && (
              <ul className="mt-1 grid gap-0.5">
                {h.fields.map((f) => {
                  const declared = info.fields.find((x) => x.name === f.field);
                  return <li key={f.field}><span className="text-muted">{declared?.title ?? f.field}</span>{" "}
                    {declared?.type === "lines" ? <LinesChange field={declared} before={f.before} after={f.after} />
                      : <>{f.before !== undefined && <><s className="text-muted">{shown(f.before)}</s> → </>}{shown(f.after)}</>}</li>;
                })}
              </ul>
            )}
          </li>
        ))}
      </ol>
    </section>
  );
}

const shown = (v: unknown) => (v === undefined || v === null || v === "" ? "—" : typeof v === "object" ? JSON.stringify(v) : String(v));

/** One record: its fields, the records that refer to it, and its history from the journal. */
export function RecordPage({ source, type, id, actions, onOpen, reload = 0, can, onTransition, files, comments, tasks, fields, work, detailOnly = false,detailPresentation,recordTabs,recordLinks,linksOnly=false,statusTracker,statusOnly=false }: {
  detailPresentation?:Api.PageDetailPresentation;recordTabs?:readonly string[];recordLinks?:readonly Api.PageRecordLink[];linksOnly?:boolean;statusTracker?:Api.PageStatusTracker;statusOnly?:boolean;
  /** App API composes declared record-specific work without another read path. */
  work?: (view: RecordView) => ReactNode;
  /** Answering the open tasks about the record from its page; without it they are listed only. */
  tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void> };
  source: RecordSource; type: string; id: string; actions?: (r: EntityRecord) => ReactNode;
  /** Presentation subset. No field omitted by source.entity can be restored here. */
  fields?: string[];
  /** A composed detail widget shows only this record's header and fields. */
  detailOnly?: boolean;
  /** Uploading a file to the record and downloading one (ADR-0028); without it the files are listed only. */
  files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void };
  /** Commenting and following (ADR-0028 D6); without it comments are listed only. */
  comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> };
  onOpen?: (type: string, r: EntityRecord) => void; reload?: number;
  /** The caller's catalog, and how to take a lifecycle transition (a decision on this record). */
  can?: (schema: string) => boolean; onTransition?: (schema: string, r: EntityRecord) => void;
}) {
  const tabIdentity=JSON.stringify([source.scope,type,id,recordTabs]);
  const [tabState,setTab]=useState<{identity:string;value:string}>();
  const info = source.entity(type);
  const [loaded, setLoaded] = useState<{ type: string; id: string; scope?: string; view: RecordView }>();
  const view = loaded?.type === type && loaded.id === id && loaded.scope === source.scope ? loaded.view : undefined;
  const [error, setError] = useState<string>();
  const request = useRef(0);
  const load = useCallback(() => {
    const epoch = ++request.current;
    const scope = source.scope;
    setError(undefined);
    source.get(type, id).then((value) => { if (request.current === epoch && source.scope === scope) setLoaded({ type, id, scope, view: value }); }, (e) => {
      if (request.current === epoch && source.scope === scope) setError(e instanceof Error ? e.message : String(e));
    });
  }, [source, source.scope, type, id]);
  useEffect(() => { load(); return () => { request.current++; }; }, [load, reload, source.revision]);
  const entity = useMemo(() => (info ? entityFrom(info) : undefined), [info]);
  if (error) {
    return (
      <div role="alert" className="grid max-w-xl gap-2 rounded-md border border-[var(--tone-danger)] bg-surface p-4 text-sm">
        <p className="font-medium text-[var(--tone-danger)]">{t("Could not load record.")}</p>
        <p className="font-mono text-xs text-muted">{humanizeKernelError(error)}</p>
        <div>
          <Button size="sm" onClick={load}>{t("Try again")}</Button>
        </div>
      </div>
    );
  }
  if (!info || !entity || !view) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const r = view.record;
  if(statusOnly){
   const l=info.lifecycle,value=statusTracker,field=info.fields.find(f=>f.name===value?.field);
   if(!l||!value||value.field!==l.field||!field||!value.stages.length||value.stages.some(name=>!l.states.some(s=>s.name===name))||typeof r[value.field]!=="string")return <p role="alert" className="text-sm text-muted">{t("Lifecycle status is unavailable.")}</p>;
   const states=value.stages.map(name=>l.states.find(s=>s.name===name)!);
   return <div className="grid min-w-0 gap-2"><StatusBar lifecycle={{...l,states}} state={r[value.field] as string} tracker/>{!value.stages.includes(r[value.field] as string)&&<p className="text-xs text-muted">{t("Current state is outside the displayed stages.")}</p>}</div>;
  }

  const header=(<header className="flex flex-wrap items-center gap-2">
        <h1 className="min-w-0 break-words text-lg font-semibold">{displayOf(info, r)}</h1>
        <span className="min-w-0 break-words font-mono text-xs text-muted">{info.title} · {r.id} {t("· rev")} {r.revision}</span>
        {r.archived && <Tag label="archived" />}
        {!detailOnly && <span className="ml-auto flex flex-wrap gap-1">{actions?.(r)}</span>}
      </header>);
  const properties=(<section className="rounded-md border border-border bg-surface p-3">
        <PropertyList columns={detailPresentation?.columns as 1|2|3|4|undefined} items={[...[...new Set(fields??info.fields.map(f=>f.name))].flatMap(name=>{const f=info.fields.find(f=>f.name===name);return !f||detailPresentation?.hideNull&&(r[name]===undefined||r[name]===null||r[name]==="")?[]:[[f.title,entity.fields[name]!.display(r[name] as never,r)] as [string,ReactNode]];}),
          ...(!detailOnly ? [[t("Created"), `${r.created.by ?? ""} · ${r.created.at ? new Date(r.created.at).toLocaleString() : ""}`],
            [t("Changed"), `${r.changed.by ?? ""} · ${r.changed.at ? new Date(r.changed.at).toLocaleString() : ""}`]] as [string, ReactNode][] : [])]} />
      </section>);
  const groups=detailOnly&&!recordTabs&&!linksOnly?[]:recordLinks===undefined?[...view.related,...(view.linked??[])].map(rel=>({rel,title:undefined as string|undefined})):recordLinks.flatMap(group=>{const target=source.entity(group.object.name),field=target?.fields.find(f=>f.name===group.field&&f.type==="reference"&&f.ref===type&&!!f.inverse),rel=view.related.find(r=>r.type===group.object.name&&r.field===group.field);return field&&target?.app===group.object.app&&rel?[{rel,title:group.title}]:[];});
  const related=detailOnly&&!recordTabs&&!linksOnly?[]:groups.flatMap(({rel,title}) => {
    const relInfo=source.entity(rel.type),relEntity=relInfo&&entityFrom(relInfo);if(!relEntity)return [];
    return [<section key={`${rel.type}.${rel.field}`}>
      <h2 className="mb-1 text-sm font-semibold">{title||relInfo.plural} <span className="font-normal text-muted">({rel.total}{rel.field === "link" ? t(", linked") : rel.relation ? <>{" · "}{rel.relation}</> : <>{t(", by")} {rel.field}</>})</span></h2>
      <DataTable data={rel.records} columns={[{id:"id",header:"ID",accessorKey:"id",meta:{width:130}},...columnsFor(relEntity,listed(relEntity))] as never} getRowId={(x:EntityRecord)=>x.id} height={Math.min(40+rel.records.length*28,260)} searchable={false} onRowClick={onOpen&&((x:EntityRecord)=>onOpen(rel.type,x))} empty={t("None")}/>
    </section>];
  });
  if(linksOnly)return <div className="grid min-w-0 gap-3">{related.length?related:<p className="text-sm text-muted">{t("No related records.")}</p>}</div>;
  if(recordTabs){
    const tabs=recordTabs.filter(tab=>(pageUIManifest.runtime.recordView.tabs as readonly string[]).includes(tab));
    const titles:Record<string,string>={overview:t("Overview"),properties:t("Properties"),links:t("Links"),history:t("History")};
    const overview=<div className="grid gap-3">{info.lifecycle&&<StatusBar lifecycle={info.lifecycle} state={String(r[info.lifecycle.field]??"")}/>}<PropertyList columns={2} items={info.fields.filter(f=>fields?.includes(f.name)&&["integer","decimal"].includes(f.type)).slice(0,4).map(f=>[f.title,entity.fields[f.name]!.display(r[f.name],r)])}/><PropertyList items={[...view.related,...(view.linked??[])].flatMap(rel=>{const target=source.entity(rel.type);return target?[[target.plural,String(rel.total)] as [string,ReactNode]]:[];})}/></div>;
    const panels:Record<string,ReactNode>={overview,properties,links:related.length?related:<p className="text-sm text-muted">{t("No related records.")}</p>,history:<RecordHistory info={info} history={view.history}/>};
    return <div className="grid min-w-0 gap-3">{header}<ContentTabs key={tabIdentity} label={t("Record view")} value={tabState?.identity===tabIdentity?tabState.value:tabs[0]} onChange={value=>setTab({identity:tabIdentity,value})} items={tabs.map(id=>({id,title:titles[id]!,content:panels[id]}))}/></div>;
  }
  return (
    <div className="grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-4">
      {header}
      {!detailOnly && info.lifecycle && <StatusBar lifecycle={info.lifecycle} state={String(r[info.lifecycle.field] ?? "")} can={can}
        onTransition={onTransition && ((schema) => onTransition(schema, r))} />}
      {!detailOnly && (view.tasks.length > 0 || view.approvals.length > 0 || view.processes.length > 0 || work) && <section aria-label={t("Work on this record")} className="grid grid-cols-[minmax(0,1fr)] gap-3 rounded-md border border-border bg-surface p-3">
        <h2 className="text-sm font-semibold">{t("Work on this record")}</h2>
        {view.tasks.length > 0 && <Tasks list={view.tasks} tasks={tasks} />}
        {view.approvals.length > 0 && <Approvals source={source} approvals={view.approvals} onOpen={onOpen} />}
        {view.processes.length > 0 && <Processes source={source} processes={view.processes} onOpen={onOpen} />}
        {work?.(view)}
      </section>}
      {!detailOnly && info.type === "work.approval" && <ApprovalGraph approval={r as unknown as Api.ApprovalRequest} />}
      {properties}
      {!detailOnly && (view.files.length > 0 || files) && <Files key={JSON.stringify([source.scope,type,id])} attached={view.files} files={files} />}
      {!detailOnly && (view.comments.length > 0 || comments) && <Comments key={JSON.stringify([source.scope,type,id])} list={view.comments} following={view.following} comments={comments} />}
      {!detailOnly && related}
      {!detailOnly && (view.activity?.length ?? 0) > 0 && (
        <section>
          <h2 className="mb-1 text-sm font-semibold">{t("Activity")}</h2>
          <ol className="grid gap-1 text-sm">
            {view.activity.map((n, i) => (
              <li key={i} className="flex flex-wrap items-baseline gap-2">
                <span className="text-xs text-muted">{new Date(n.at).toLocaleString()}</span>
                <Tag label={n.by} tone="info" />
                {n.title && <span className="font-medium">{n.title}</span>}
                <span className="text-xs text-muted">{n.text}</span>
              </li>
            ))}
          </ol>
        </section>
      )}
      {!detailOnly && <RecordHistory info={info} history={view.history} />}
    </div>
  );
}

/** A standalone projection of the same authorized record view and related windows. */
export function RecordLinks({source,type,id,groups,onOpen}:{source:RecordSource;type:string;id:string;groups:readonly Api.PageRecordLink[];onOpen?:(type:string,r:EntityRecord)=>void}){
 return <RecordPage source={source} type={type} id={id} recordLinks={groups} onOpen={onOpen} linksOnly/>;
}

/** The caller declares lifecycle presentation; this component never emits transitions. */
export function RecordStatus({source,type,id,config}:{source:RecordSource;type:string;id:string;config?:Api.PageStatusTracker}){
 return <RecordPage source={source} type={type} id={id} statusTracker={config} statusOnly/>;
}

type Row = Record<string, unknown>;

/** The cell of a line as people read it: money with its currency, the rest as shown elsewhere. */
const cellOf = (f: FieldInfo, v: unknown) => f.type === "money" && v && typeof v === "object"
  ? `${((v as Money).amount / 100).toFixed(2)} ${(v as Money).currency}` : shown(v);

/**
 * A changed lines field row by row (F-26, F-37): lines added, removed, and
 * changed in some cells. Lines are matched by the first column whose values
 * are unique on both sides (a booking, an account), else by position.
 */
export function LinesChange({ field, before, after }: { field: FieldInfo; before: unknown; after: unknown }) {
  const columns = field.fields ?? [];
  const was = Array.isArray(before) ? (before as Row[]) : [], is = Array.isArray(after) ? (after as Row[]) : [];
  const unique = (rows: Row[], name: string) => rows.every((r) => ["string", "number"].includes(typeof r[name]) && r[name] !== "")
    && new Set(rows.map((r) => r[name])).size === rows.length;
  const key = columns.find((c) => c.type !== "money" && unique(was, c.name) && unique(is, c.name))?.name;
  const id = (r: Row, i: number) => String(key ? r[key] : i);
  const text = (r: Row) => columns.filter((c) => r[c.name] !== undefined && r[c.name] !== "" && r[c.name] !== null).map((c) => `${c.title} ${cellOf(c, r[c.name])}`).join(" · ");
  const old = new Map(was.map((r, i) => [id(r, i), r])), now = new Map(is.map((r, i) => [id(r, i), r]));
  const lines: ReactNode[] = [];
  is.forEach((r, i) => {
    const k = id(r, i), prior = old.get(k);
    if (!prior) { lines.push(<li key={`+${k}`} className="text-[var(--tone-success)]">+ {text(r)}</li>); return; }
    const changed = columns.filter((c) => JSON.stringify(prior[c.name]) !== JSON.stringify(r[c.name]));
    if (changed.length) lines.push(<li key={`~${k}`}>{key ? `${r[key]}: ` : `${i + 1}: `}{changed.map((c, j) =>
      <span key={c.name}>{j > 0 && " · "}{c.title} <s className="text-muted">{cellOf(c, prior[c.name])}</s> → {cellOf(c, r[c.name])}</span>)}</li>);
  });
  was.forEach((r, i) => { const k = id(r, i); if (!now.has(k)) lines.push(<li key={`-${k}`} className="text-muted"><s>− {text(r)}</s></li>); });
  return lines.length ? <ul className="ml-3 grid gap-0.5">{lines}</ul> : <span className="text-muted">{t("unchanged")}</span>;
}

const size = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${Math.round(n / 1024)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);

/** A record's files: download each, add one. */
function Files({ attached, files }: { attached: AttachedFile[]; files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void } }) {
  const [file,setFile]=useState<File>(),[busy,setBusy]=useState(false),[error,setError]=useState<string>();const epoch=useRef(0),picked=useRef(file);picked.current=file;useEffect(()=>()=>{epoch.current++;},[]);
  const upload=async()=>{const submitted=picked.current,started=epoch.current;if(!submitted||!files||busy)return;setBusy(true);setError(undefined);try{await files.upload(submitted);if(epoch.current===started&&picked.current===submitted){picked.current=undefined;setFile(undefined);}}catch{if(epoch.current===started)setError(t("The attachment could not be confirmed. Your selected file is retained."));}finally{if(epoch.current===started)setBusy(false);}};
  return (
    <section>
      <h2 className="mb-1 text-sm font-semibold">{t("Files")}</h2>
      {files&&<RecordUploader label={t("Add file")} file={file} busy={busy} error={error} onFile={next=>{picked.current=next;setFile(next);setError(undefined);}} onUpload={upload} onClear={()=>{picked.current=undefined;setFile(undefined);setError(undefined);}}/>}
      {attached.length === 0 ? <p className="text-xs text-muted">{t("No files")}</p> :
        <ul className="grid gap-1">
          {attached.map((f) => (
            <li key={f.id} className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-1.5 text-sm">
              <button type="button" className="font-medium hover:underline" disabled={!files} onClick={() => files?.download(f)}>{f.name}</button>
              <span className="text-xs text-muted">{f.contentType} · {size(f.size)} · {f.by}</span>
            </li>
          ))}
        </ul>}
    </section>
  );
}

/** A record's comments, oldest first, and a box to add one; following tells of its changes. */
function Comments({ list, following, comments }: { list: RecordComment[]; following: boolean; comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> } }) {
  const [text,setText]=useState(""),[busy,setBusy]=useState(false),[followBusy,setFollowBusy]=useState(false),[error,setError]=useState<string>();const epoch=useRef(0),draft=useRef(text);draft.current=text;useEffect(()=>()=>{epoch.current++;},[]);
  const add=async()=>{const submitted=draft.current,started=epoch.current;if(!comments||busy||!submitted.trim())return;setBusy(true);setError(undefined);try{const confirmed=await comments.add(submitted);if(epoch.current!==started)return;if(confirmed&&draft.current===submitted){draft.current="";setText("");}else if(!confirmed)setError(t("The comment could not be confirmed. Your draft is retained."));}catch{if(epoch.current===started)setError(t("The comment could not be confirmed. Your draft is retained."));}finally{if(epoch.current===started)setBusy(false);}};
  const follow=async(on:boolean)=>{const started=epoch.current;if(!comments||followBusy)return;setFollowBusy(true);setError(undefined);try{await comments.follow(on);}catch{if(epoch.current===started)setError(t("Following this record could not be confirmed."));}finally{if(epoch.current===started)setFollowBusy(false);}};
  return <RecordComments list={list} text={text} following={following} busy={busy} followBusy={followBusy} error={error} onText={comments?next=>{draft.current=next;setText(next);}:undefined} onAdd={comments?add:undefined} onFollow={comments?follow:undefined}/>;
}

const processTone = (state: string) =>
  state === "done" ? "success" : state === "stuck" || state === "compensated" || state === "canceled" ? "danger" : state === "compensating" ? "warning" : "info";

const approvalTone = (state: string) => state === "approved" ? "success" : state === "pending" ? "warning" : state === "withdrawn" ? "neutral" : "danger";

/** The approvals asked for a record: each request, its state, whom it waits for, and who rejected it and why. */
function Approvals({ source, approvals, onOpen }: { source: RecordSource; approvals: Api.ApprovalRequest[]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("work.approval")?.fields.find((f) => f.name === "state");
  return (
    <section aria-label={t("Approvals")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Approvals")}</h2>
      {approvals[0] && <div className="mb-1.5"><ApprovalGraph approval={approvals[0]} /></div>}
      <ul className="grid gap-1">
        {approvals.map((a) => {
          const level = a.levels[a.level];
          const by = a.levels.flatMap((l) => l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m));
          return (
            <li key={a.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
              {onOpen ? <Button size="sm" variant="ghost" onClick={() => onOpen("work.approval", a)}>{a.title}</Button> : <span className="font-medium">{a.title}</span>}
              <Tag label={state?.choiceTitles?.[state.choices?.indexOf(a.state) ?? -1] ?? a.state} tone={approvalTone(a.state)} />
              {a.state === "pending" && level && <span className="text-xs text-muted">{t("waiting for {level}: {approvers}", { level: level.title, approvers: level.approvers.join(", ") })}</span>}
              {by.length > 0 && <span className="text-xs text-muted">{t("approved by {members}", { members: by.join(", ") })}</span>}
              {a.rejectedBy && <span className="text-xs">{t("rejected by {member}", { member: a.rejectedBy })}{a.outcome ? `: ${a.outcome}` : ""}</span>}
              {a.state === "refused" && a.outcome && <span className="text-xs">{a.outcome}</span>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** The open tasks about a record: what is asked, by when, and its answers as buttons. */
export function Tasks({ list, tasks }: { list: Api.InboxTask[]; tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void> } }) {
  const [busy, setBusy] = useState<string>();
  const answer = async (task: Api.InboxTask, a?: string) => { setBusy(task.id); try { await tasks?.answer(task, a); } finally { setBusy(undefined); } };
  return (
    <section aria-label={t("Waiting for you")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Waiting for you")}</h2>
      <ul className="grid gap-1">
        {list.map((task) => (
          <li key={task.id} className="grid gap-1.5 rounded-md border border-[var(--tone-warning)] bg-surface px-3 py-2 text-sm">
            <span className="font-medium">{task.title}</span>
            {task.body && <span className="whitespace-pre-wrap text-xs text-muted">{task.body}</span>}
            {tasks && <span className="flex flex-wrap gap-1.5">
              {(task.answers?.length ? task.answers : [undefined]).map((a, i) =>
                <Button key={a ?? "done"} size="sm" variant={a === task.answers?.[0] ? "primary" : undefined} disabled={busy === task.id} onClick={() => void answer(task, a)}>{task.answerTitles?.[i] ?? a ?? t("Done")}</Button>)}
            </span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** An approval chain drawn as a graph (#122): the requester, each level with its approvers and who decided, and how it ended. */
export function ApprovalGraph({ approval: a }: { approval: Api.ApprovalRequest }) {
  const nodes: GraphNode[] = [{ id: "requester", label: a.requester, detail: t("asked"), tone: "success" }];
  const edges: GraphEdge[] = [];
  let previous = "requester";
  a.levels.forEach((l, i) => {
    const decided = l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m);
    const rejectedHere = a.state === "rejected" && i === a.level;
    const here = a.state === "pending" && i === a.level;
    nodes.push({
      id: `level-${i}`, label: l.title, current: here,
      detail: rejectedHere ? t("rejected by {member}", { member: a.rejectedBy ?? "" }) : decided.length ? t("approved by {members}", { members: decided.join(", ") }) : l.approvers.join(", "),
      tone: rejectedHere ? "danger" : here ? "info" : i < a.level || a.state === "approved" || a.state === "refused" ? "success" : undefined,
    });
    edges.push({ from: previous, to: `level-${i}` });
    previous = `level-${i}`;
  });
  const ended = a.state !== "pending";
  const outcomes: Record<string, string> = { approved: t("Approved"), rejected: t("Rejected"), refused: t("Refused when run"), withdrawn: t("Withdrawn") };
  nodes.push({ id: "outcome", label: ended ? outcomes[a.state] ?? a.state : t("Outcome"), detail: a.outcome || undefined,
    tone: a.state === "approved" ? "success" : a.state === "rejected" || a.state === "refused" ? "danger" : a.state === "withdrawn" ? "neutral" : undefined });
  edges.push({ from: previous, to: "outcome", dashed: !ended });
  return <Graph nodes={nodes} edges={edges} height={150} label={t("Approvals")} />;
}

/** The processes about a record: each flow, its state, and the steps it stands at. */
function Processes({ source, processes, onOpen }: { source: RecordSource; processes: RecordView["processes"]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("flow.instance")?.fields.find((f) => f.name === "state");
  return (
    <section aria-label={t("Processes")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Processes")}</h2>
      <ul className="grid gap-1">
        {processes.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
            <button type="button" className="font-medium hover:underline" onClick={() => onOpen?.("flow.instance", p)}>{p.title}</button>
            <Tag label={state?.choiceTitles?.[state.choices?.indexOf(p.state) ?? -1] ?? p.state} tone={processTone(p.state)} />
            {!!p.tokens?.length && <span className="text-xs text-muted">{t("at {steps}", { steps: p.tokens.map((x) => x.step).join(", ") })}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** A task offered to a member; `answers` are what a flow's question offers (ADR-0020), none: done. */
export type InboxTask = Api.InboxTask;

/** A member's open tasks (ADR-0017), overdue first; each can open what it is about and offers the actions the app gives it. */
export function Inbox({ tasks, onOpen, actions, empty = t("Nothing for you") }: {
  tasks: InboxTask[]; onOpen?: (task: InboxTask) => void; actions?: (task: InboxTask) => ReactNode; empty?: string;
}) {
  if (tasks.length === 0) return <p className="text-sm text-muted">{empty}</p>;
  const now = Date.now();
  return (
    <ul className="grid max-w-3xl gap-2">
      {tasks.map((task) => {
        const late = !!task.due && Date.parse(task.due) < now;
        return (
          <li key={task.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className="text-left font-medium hover:underline" onClick={() => onOpen?.(task)}>{task.title}</button>
              {late && <Tag label={t("overdue")} tone="danger" />}
              {task.due && !late && <span className="text-xs text-muted">{t("due {when}", { when: new Date(task.due).toLocaleString() })}</span>}
              {task.assignee && <span className="text-xs text-muted">{t("taken by")} {task.assignee}</span>}
              <span className="ml-auto flex gap-1">{actions?.(task)}</span>
            </div>
            {task.body && <p className="mt-1 whitespace-pre-wrap text-xs text-muted">{task.body}</p>}
          </li>
        );
      })}
    </ul>
  );
}
