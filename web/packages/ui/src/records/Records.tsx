import type { TablePresentationOptions } from "../presentation";
import { ChevronLeft, ChevronRight } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useViewVisible } from "../shell/ViewVisibility";
import { RecordCards } from "./RecordCards";
import { EditableRecordGrid, type RecordEditPort, type RecordSelectionPort } from "./EditableRecordGrid";
import { presentRecordColumns, type RecordColumnPresentation } from "./ColumnPresentation";
import { flexRender, type CellContext } from "@tanstack/react-table";
import { FacetChoices } from "../fields/FacetChoices";
import { FormatPill } from "./ObjectView";
import type { Tone } from "../components/StatusTag";
import { columnsFor, valueOf } from "../fields/entity";
import { RecordKanban, type KanbanLane, type KanbanMove } from "./RecordKanban";
import { Button } from "../primitives/button";
import { Input, Select } from "../primitives/input";
import { Chart } from "../charts/Chart";
import { Pivot } from "../charts/Pivot";
import type { AggregateData, AggregateQuery, ChartSpec, Mark } from "../charts/spec";
import { t } from "../i18n";
import { humanizeKernelError } from "../lib/errors";
import type { Api } from "@platform/kernel";
import {entityFrom, groupable, listed, measurable} from "./RecordEntity";

export type FieldInfo = Api.FieldInfo;
export type State = Api.State;
export type Lifecycle = Api.LifecycleInfo;
export type EntityInfo = Api.EntityInfo;
type Stamp = Api.Stamp;
export type EntityRecord = { id: string; revision: number; created: Stamp; changed: Stamp; archived?: boolean } & Record<string, unknown>;
export type RecordQuery = Omit<Api.Query,"domain"> & {domain?:unknown[];format?:boolean};
export type RecordPageData = Omit<Api.RecordPage, "records"> & { records: EntityRecord[] };
export type InterfaceRecordIdentity = Pick<Api.InterfaceRecord, "type" | "id">;
export type InterfaceRecordData = Omit<Api.InterfaceRecord, "record"> & { record: EntityRecord };
export type InterfaceRecordPageData = Omit<Api.InterfaceRecordPage, "records"> & { records: InterfaceRecordData[] };
export type RecordChange = Api.RecordChange;
export type AttachedFile = EntityRecord & { name: string; size: number; contentType: string; by: string; regions?:Api.ImageRegion[] };

export type RecordComment = EntityRecord & { text: string; by: string; mentions?: string[] };

export type RecordView = Omit<Api.RecordView, "record" | "related" | "linked" | "activity" | "processes" | "approvals" | "tasks" | "files" | "comments"> & {
  tasks: Api.InboxTask[];
  approvals: Api.ApprovalRequest[];
  files: AttachedFile[];
  comments: RecordComment[];
  record: EntityRecord; related: RelatedRecords[];
  linked: RelatedRecords[];
  activity: Api.Note[];
  processes: (EntityRecord & { title: string; state: string; tokens?: { step: string }[] })[];
};
type RelatedRecords = Omit<Api.Related, "records"> & { records: EntityRecord[] };
export type Money = { amount: number; currency: string };

export type RecordSource = {
	enterpriseQuery?: (stereotypes: string[], search: string) => Promise<{ elements: Api.EnterpriseQueryElement[] }>;
	enterpriseResolve?: (ids: string[]) => Promise<{ elements: Api.EnterpriseQueryElement[] }>;
  watchEnterprise?: (changed: () => void) => () => void;
  scope?: string;
  ruleIdentity?: {me:string;timezone:string};
  entity: (type: string) => EntityInfo | undefined;
  list: (type: string, query: RecordQuery) => Promise<RecordPageData>;
  exportCSV?: (type: string, query: RecordQuery) => Promise<Blob>;
  importCSV?: (type: string, csv: string, preview: boolean) => Promise<Api.ImportRow[]>;
  get: (type: string, id: string) => Promise<RecordView>;
  provenance?: (type: string, id: string) => Promise<Api.RecordProvenance>;
  checkAction?: (schema: string, target: { type: string; id: string }, inputs?: unknown) => Promise<Api.ActionCheckResult>;
  aggregate?: (type: string, query: AggregateQuery) => Promise<AggregateData>;
  interfaceList?: (name: string, query: RecordQuery) => Promise<InterfaceRecordPageData>;
  watchInterfaceList?: (name: string, query: RecordQuery, changed: () => void) => () => void;
  namedInterfaceList?: (binding: Api.AssetBinding, query: RecordQuery) => Promise<InterfaceRecordPageData>;
  watchNamedInterfaceList?: (binding: Api.AssetBinding, query: RecordQuery, changed: () => void) => () => void;
  revision?: number;
  watchList?: (type: string, query: RecordQuery, changed: () => void) => () => void;
  watchRecord?: (type: string, id: string, changed: () => void) => () => void;
  watchAggregate?: (type: string, query: AggregateQuery, changed: () => void) => () => void;
};

type ListView = "list" | "kanban" | "pivot" | "chart";
export type ListState = {
  view?: ListView; search?: string; sort?: string; archived?: boolean; drilled?: unknown[];
  group?: string; columns?: string; measure?: string; mark?: Mark;
  baseQuery?: RecordQuery; searchLocked?:boolean; sortLocked?:boolean; filtersLocked?:boolean;
  filters?: Record<string, string[]>;
};

export function RecordList({ source, type, onOpen, onOpenReference, toolbar, height = "calc(100dvh - 230px)", pageSize = 100, domain: fixed, initial = {}, onSave, fields, window,inlineEdit,selectionSet,columnPresentation,showSearch=true,tablePresentation,cards,selectedId,onNavigate, kanban, onMove, moves = [] }: {
  onOpenReference?: (type: string, id: string) => void;
  tablePresentation?:TablePresentationOptions;onNavigate?:(record:EntityRecord)=>void;cards?:{layout:"grid"|"list";labelField:string};selectedId?:string;columnPresentation?:RecordColumnPresentation[];showSearch?:boolean;
  selectionSet?:RecordSelectionPort;
  inlineEdit?:RecordEditPort;
  source: RecordSource; type: string; onOpen?: (r: EntityRecord) => void; toolbar?: ReactNode; height?: number | string; pageSize?: number;
  fields?: string[];
  window?: { query:RecordQuery; baseQuery?:RecordQuery; facetQuery?:(field:string)=>RecordQuery|undefined; page?:RecordPageData; error?:string; searchLocked?:boolean; sortLocked?:boolean; inputSearch?:string; maxOffset:number;
    filters?:Record<string,string[]>; filtersLocked?:boolean;
    onChange:(change:{search?:string;sort?:string[];offset?:number;filters?:Record<string,string[]>})=>void };
  domain?: unknown[];
  initial?: ListState; onSave?: (state: ListState) => void;
  kanban?: { lanes?: KanbanLane[]; stateField?: string; labelField?: string; properties?: (record: EntityRecord) => [string, ReactNode][] };
  onMove?: (record: EntityRecord, schema: string, destination?: string) => void;
  moves?: KanbanMove[];
}) {
  const densityIdentity=JSON.stringify([source.scope,type,tablePresentation]),[densityChoice,setDensityChoice]=useState<{identity:string;density:string}>();
  const density=densityChoice?.identity===densityIdentity?densityChoice.density:tablePresentation?.density??"compact",showToolbar=tablePresentation?.showToolbar!==false;
  const cardIdentity=JSON.stringify([source.scope,type,cards]),[cardChoice,setCardChoice]=useState<{identity:string;layout:"grid"|"list"}>();
  const cardLayout=cardChoice?.identity===cardIdentity?cardChoice.layout:cards?.layout??"grid";
  const info = source.entity(type);
  const [localSearch, setSearch] = useState(initial.search ?? initial.baseQuery?.search ?? "");
  const sortStorageKey = `records:${type}:query`;
  const readSort = () => {
    if (initial.sort) return [initial.sort];
    if (initial.baseQuery?.sort) return initial.baseQuery.sort;
    if (!window && !initial.sortLocked) {
      try {
        const saved: unknown = JSON.parse(localStorage.getItem(sortStorageKey) ?? "null");
        if (Array.isArray(saved) && saved.length > 0 && saved.every(value => typeof value === "string")) return saved as string[];
      } catch {   }
    }
    return ["-changed"];
  };
  const [sortChoice, setSortChoice] = useState(() => ({ key: sortStorageKey, value: readSort() }));
  if (sortChoice.key !== sortStorageKey) setSortChoice({ key: sortStorageKey, value: readSort() });
  const localSort = sortChoice.value, setSort = (value: string[]) => setSortChoice({ key: sortStorageKey, value });
  useEffect(() => {
    if (window || initial.sortLocked) return;
    try { localStorage.setItem(sortStorageKey, JSON.stringify(localSort)); } catch {   }
  }, [sortStorageKey, localSort, !!window, initial.sortLocked]);
  const [localOffset, setOffset] = useState(0);
  const [archived, setArchived] = useState(initial.archived ?? initial.baseQuery?.archived ?? false);
  const [loadedPage, setLoadedPage] = useState<{ source:string|RecordSource;key: string; page: RecordPageData }>();
  const baseQuery=window?window.baseQuery??window.query:initial.baseQuery??{};
  const searchLocked=window?window.searchLocked:initial.searchLocked,sortLocked=window?window.sortLocked:initial.sortLocked,filtersLocked=window?window.filtersLocked:initial.filtersLocked;
  const search = window ? window.inputSearch ?? window.query.search ?? "" : localSearch, sort = window ? window.query.sort?.[0] ?? "id" : localSort[0]??"id", offset = window ? window.query.offset ?? 0 : localOffset;
  if (window) pageSize = window.query.limit ?? pageSize;
  else if(baseQuery.limit!==undefined)pageSize=Math.min(pageSize,baseQuery.limit);
  const windowKey = JSON.stringify(window?.query);
  const [localError, setError] = useState<{source:string|RecordSource;key:string;message:string}>();
  const [localView, setView] = useState<ListView>(initial.view ?? "list");
  const [drilled, setDrilled] = useState<unknown[] | undefined>(initial.drilled);
  const groups = useMemo(() => (info ? groupable(info) : []), [info]);
  const measures = useMemo(() => (info ? measurable(info) : []), [info]);
  const [group, setGroup] = useState(initial.group ?? "");
  const [columns, setColumns] = useState(initial.columns ?? "");
  const [measure, setMeasure] = useState(initial.measure ?? "count");
  const [mark, setMark] = useState<Mark>(initial.mark ?? "bar");
  const rows = group || (info?.lifecycle?.field ?? groups[0]?.value ?? "");
  const lifecycle = info?.lifecycle;
  const kanbanStateField = kanban?.stateField ?? lifecycle?.field;
  const choiceField = kanbanStateField ? info?.fields.find((f) => f.name === kanbanStateField) : undefined;
  const kanbanLanes: KanbanLane[] = useMemo(() => {
    if (!choiceField) return [];
    if (kanban?.lanes) return kanban.lanes;
    if (lifecycle?.states?.length && lifecycle.field === kanbanStateField) {
      return lifecycle.states.map((s) => ({ name: s.name, title: s.title, tone: s.tone as Tone }));
    }
    if (choiceField?.choices?.length) {
      return choiceField.choices.map((c, i) => ({ name: c, title: choiceField.choiceTitles?.[i] ?? c }));
    }
    return [];
  }, [kanban?.lanes, lifecycle, choiceField, kanbanStateField]);
  const aggregate = window ? undefined : source.aggregate;
  const availableViews = useMemo<ListView[]>(() => [
    "list",
    ...(kanbanLanes.length > 0 && kanbanStateField ? ["kanban" as const] : []),
    ...(aggregate && rows ? ["pivot" as const, "chart" as const] : []),
  ], [kanbanLanes.length, kanbanStateField, aggregate, rows]);
  const view = window || !availableViews.includes(localView) ? "list" : localView;
  const fixedKey = JSON.stringify(fixed ?? []);
  useEffect(() => { setOffset(0); }, [fixedKey, type]);
  const [facetPanel, setFacetPanel] = useState(false);
  const [localFilters, setLocalFilters] = useState(initial.filters);
  const facetSelection = window ? window.filters ?? {} : localFilters ?? {};
  const staleFilters=Object.keys(facetSelection).filter(name=>{const field=info?.fields.find(f=>f.name===name&&f.type==="choice");return !field||facetSelection[name]!.some(value=>!field.choices?.includes(value));});
  const facetFields = useMemo(() => {
    if (!info) return [];
    const fields = info.fields.filter((f) => f.type === "choice" && f.choices?.length);
    const shown = fields.slice(0, 4);
    for (const f of fields.slice(4)) if ((facetSelection[f.name] ?? []).length > 0 && !shown.includes(f)) shown.push(f);
    return shown;
  }, [info, facetSelection]);
  const canClearFilters = !searchLocked && !!search || !filtersLocked && Object.values(facetSelection).some(values=>values.length>0);
  const facetTerms = useMemo(() => Object.entries(facetSelection).flatMap(([field, values]) => values.length ? [[field, "in", values]] : []), [facetSelection]);
  const baseDomainKey=JSON.stringify(baseQuery.domain??[]);
  const domain = useMemo(() => [...JSON.parse(baseDomainKey),...JSON.parse(fixedKey), ...(drilled ?? []), ...facetTerms], [baseDomainKey,fixedKey, drilled, facetTerms]);
  const applyFilters = (next: Record<string, string[]>) => {
    const usable = Object.fromEntries(Object.entries(next).filter(([, values]) => values.length > 0));
    if (window) window.onChange({ filters: usable, offset: 0 });
    else { setLocalFilters(Object.keys(usable).length ? usable : undefined); setOffset(0); }
  };
  const entity = useMemo(() => (info ? entityFrom(info,{},source,onOpenReference) : undefined), [info,source,onOpenReference]);
  const infoKey = JSON.stringify(info);
  const pageKey = JSON.stringify([source.scope, type, infoKey, baseQuery,domain, search, localSort,sort, offset, archived, pageSize]);
  const from = useRef(source);
  from.current = source;
  const sourceIdentity = source.scope ?? source;
  const taskIdentity=source.ruleIdentity?.me??sourceIdentity,editTask=useRef({identity:taskIdentity,generation:0});
  if(editTask.current.identity!==taskIdentity)editTask.current={identity:taskIdentity,generation:editTask.current.generation+1};
  const visible = useViewVisible();
  const error = staleFilters.length?t("Saved filters on {fields} are unavailable. Clear them to continue.",{fields:staleFilters.join(", ")}):window ? window.error : localError?.source===sourceIdentity&&localError.key===pageKey?localError.message:undefined;
  const page = staleFilters.length?undefined:window ? window.page : loadedPage?.source===sourceIdentity&&loadedPage.key === pageKey ? loadedPage.page : undefined;
  const sortField = sort.replace(/^-/, "");
  const knownSort = ["id", "created", "changed"].includes(sortField) || info?.fields.some((f) => f.name === sortField);
  const recordQuery: RecordQuery = window?.query ?? { ...baseQuery,domain, search, sort: knownSort ? localSort : ["id"], offset, limit: pageSize, archived };
  const listQuery: RecordQuery = { ...recordQuery, format: true };
  const facetQueries=facetFields.map(field=>[field.name,window?window.facetQuery?.(field.name):{...recordQuery,domain:[...JSON.parse(baseDomainKey),...JSON.parse(fixedKey),...(drilled??[]),...facetTerms.filter(term=>term[0]!==field.name)]}] as const);
  const facetKey=JSON.stringify([type,facetQueries]);
  type FacetCounts=Record<string,{value:string;count:number}[]|"unavailable">;
  const [countResult,setCountResult]=useState<{source:typeof sourceIdentity;key:string;counts:FacetCounts}>();
  const facetCounts=countResult?.source===sourceIdentity&&countResult.key===facetKey?countResult.counts:undefined;
  useEffect(()=>{
    if(!facetPanel||!visible||staleFilters.length)return;
    let active=true;
    const source=from.current,stops:(()=>void)[]=[];
    setCountResult({source:sourceIdentity,key:facetKey,counts:{}});
    for(const [field,query] of facetQueries){
      let epoch=0;
      const set=(counts:FacetCounts[string])=>{if(active)setCountResult(current=>({source:sourceIdentity,key:facetKey,counts:{...current?.counts,[field]:counts}}));};
      if(!query||!source.aggregate){set("unavailable");continue;}
      const {offset:_,limit:__,sort:___,...predicate}=query;
      const aggregateQuery={...predicate,groups:[field],measures:["count"]};
      const load=()=>{const request=++epoch;void source.aggregate!(type,aggregateQuery).then(data=>{if(active&&request===epoch)set(data.rows.map(row=>({value:String(row[field]??""),count:Number(row.count??0)})));},()=>{if(active&&request===epoch)set("unavailable");});};
      const stop=source.watchAggregate?.(type,aggregateQuery,load);if(stop)stops.push(stop);
      load();
    }
    return ()=>{active=false;stops.forEach(stop=>stop());};
  },[sourceIdentity,source.revision,facetKey,facetPanel,visible,staleFilters.length]);
  const [exporting, setExporting] = useState(false);
  const [exportError, setExportError] = useState<string>();
  const exportIdentity = JSON.stringify([source.scope, type, recordQuery]);
  const exportCurrent = useRef({ identity: exportIdentity, source: sourceIdentity, active: true });
  exportCurrent.current.identity = exportIdentity;
  exportCurrent.current.source = sourceIdentity;
  useEffect(() => {
    exportCurrent.current.active = true;
    return () => { exportCurrent.current.active = false; };
  }, []);
  const exportCSV = async () => {
    if (!source.exportCSV||staleFilters.length) return;
    const current = () => exportCurrent.current.active && exportCurrent.current.identity === exportIdentity && exportCurrent.current.source === sourceIdentity;
    setExporting(true);
    setExportError(undefined);
    try {
      const file = await source.exportCSV(type, recordQuery);
      if (!current()) return;
      const url = URL.createObjectURL(file);
      Object.assign(document.createElement("a"), { href: url, download: `${type}.csv` }).click();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (error) {
      if (current()) setExportError(humanizeKernelError(error instanceof Error ? error.message : String(error)));
    } finally {
      if (exportCurrent.current.active) setExporting(false);
    }
  };
  useEffect(() => {
    if (!visible || window || !info || staleFilters.length || (view !== "list" && view !== "kanban")) return;
    let current = true,epoch=0;
    const source = from.current;
    setError(undefined);
    const load = () => {
      const request=++epoch;
      source.list(type, listQuery)
        .then((p) => { if (current&&request===epoch) { setLoadedPage({ source:sourceIdentity,key: pageKey, page: p }); setError(undefined); } }, (e) => { if (current&&request===epoch) { setLoadedPage(undefined); setError({source:sourceIdentity,key:pageKey,message:String(e)}); } });
    };
    const stop = source.watchList?.(type, listQuery, load);
    const handle = setTimeout(load, 150);
    return () => { current = false; stop?.(); clearTimeout(handle); };
  }, [sourceIdentity, source.revision, pageKey, view, windowKey, visible,staleFilters.length]);
  if (!info || !entity) return <p className="rounded-md border border-dashed border-border p-3 text-sm text-muted">{t("No activated object {type} here yet. Its records appear once a release that includes it is active; a draft shows its fields in the object model.", { type })}</p>;
  const presented = presentRecordColumns([{ id: "id", header: "ID", accessorKey: "id", enableSorting: false,
    meta: { width: 130, querySort: sortLocked?undefined:{ direction: sort === "id" ? "asc" as const : sort === "-id" ? "desc" as const : undefined,
      onChange: (next) => { const key = next === "asc" ? "id" : "-id"; if (window) window.onChange({ sort: [key], offset: 0 }); else { setSort([key]); setOffset(0); } } } },
    cell: (c: any) => <span className="font-mono text-xs">{c.getValue()}</span> },
    ...columnsFor(entity,[...new Set(fields??listed(entity))].filter(name=>listed(entity).includes(name))).map((c) => ({ ...c, enableSorting: false }))],entity,info,columnPresentation);
  const pageFormats = page?.formats;
  const columnsOf = pageFormats ? presented.map((column) => {
    const name = column.id ?? "";
    if (!entity.fields[name]) return column;
    return { ...column, cell: (context: CellContext<EntityRecord, unknown>) => {
      const format = pageFormats[context.row.original.id]?.[name];
      return format ? <FormatPill format={format} text={entity.fields[name]!.text(context.getValue() as never)} /> : flexRender(column.cell, context);
    } };
  }) : presented;
  const total = page?.total ?? 0;
  const {offset:_,limit:__,sort:___,...query}=recordQuery;
  const measureEncoding = measure === "count" ? { type: "quantitative" as const, aggregate: "count" as const }
    : { type: "quantitative" as const, aggregate: measure.split(":")[0] as "sum" | "avg", field: measure.split(":")[1] };
  const groupEncoding = { field: rows.split(":")[0], type: rows.includes(":") ? "temporal" as const : "nominal" as const, ...(rows.includes(":") ? { timeUnit: rows.split(":")[1] as "month" } : {}) };
  const spec: ChartSpec = {
    data: { entity: type, ...query }, mark,
    encoding: mark === "arc" ? { theta: measureEncoding, color: groupEncoding } : { x: groupEncoding, y: measureEncoding },
  };
  return (
    <div className="grid min-w-0 grid-cols-1 gap-2" data-density={tablePresentation||densityChoice?.identity===densityIdentity?density==="normal"?"comfortable":"compact":undefined}>
      <div className="flex flex-wrap items-center gap-2 text-sm">
        {showToolbar&&<>
        {tablePresentation&&<Select aria-label={t("Record density")} value={density} onChange={e=>setDensityChoice({identity:densityIdentity,density:e.target.value})}><option value="compact">{t("Compact")}</option><option value="normal">{t("Comfortable")}</option></Select>}
        {showSearch&&<Input aria-label={t("Search")} placeholder={t("Search {things}", { things: info.plural.toLowerCase() })} value={search} disabled={searchLocked} className="w-56"
          onChange={(e) => { if(window)window.onChange({search:e.target.value,offset:0});else {setSearch(e.target.value);setOffset(0);} }} />}
        {(view === "list" || view === "kanban") ? (
          <Select aria-label={t("Sort")} value={sort} disabled={sortLocked} className="w-48" onChange={(e) => { if(window)window.onChange({sort:[e.target.value],offset:0});else {setSort([e.target.value]);setOffset(0);} }}>
            {[["-changed", t("Recently changed")], ["id", t("ID")], ...info.fields.filter((f) => !["references","tags","lines","json"].includes(f.type)).flatMap((f) =>
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
        {!filtersLocked && facetFields.length > 0 && <Button size="sm" aria-expanded={facetPanel} aria-pressed={facetPanel} onClick={() => setFacetPanel(!facetPanel)}>{t("Filters")}{Object.values(facetSelection).some((values) => values.length) ? <span className="tabular-nums">·</span> : null}</Button>}
        {!window && <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={archived} onChange={(e) => { setArchived(e.target.checked); setOffset(0); }} />{t("Archived")}</label>}
        {drilled && <Button size="sm" variant="ghost" onClick={() => { setDrilled(undefined); setOffset(0); }}>{t("Clear drill-down ×")}</Button>}
        {cards&&<span role="group" aria-label={t("Record layout")} className="flex gap-1">{(["list","grid"] as const).map(layout=><Button key={layout} size="sm" variant="ghost" aria-pressed={cardLayout===layout} onClick={()=>setCardChoice({identity:cardIdentity,layout})}>{t(layout==="grid"?"Grid":"List")}</Button>)}</span>}
        {source.exportCSV && <Button size="sm" disabled={exporting||staleFilters.length>0} onClick={() => void exportCSV()}>{t("Export CSV")}</Button>}
        {exportError && <span role="alert" className="text-xs text-danger">{exportError}</span>}
        {toolbar}
        {onSave && <Button size="sm" variant="ghost" disabled={staleFilters.length>0} onClick={() => onSave({ view, search, sort:recordQuery.sort?.length===1?sort:undefined, archived:recordQuery.archived, drilled, group: rows, columns, measure, mark,
          baseQuery:{...baseQuery,domain:[...(baseQuery.domain??[]),...JSON.parse(fixedKey)],sort:recordQuery.sort,limit:recordQuery.limit},searchLocked,sortLocked,filtersLocked,
          filters: Object.values(facetSelection).some((values) => values.length) ? facetSelection : undefined })}>{t("Save view…")}</Button>}
        {availableViews.length > 1 && (
          <span role="group" aria-label={t("View")} className="flex rounded-md border border-border">
            {availableViews.map((v) => (
              <button key={v} type="button" aria-pressed={view === v} onClick={() => setView(v)}
                className={`h-7 px-2 text-xs capitalize ${view === v ? "bg-row-selected font-medium" : "hover:bg-row-hover"}`}>
                {v === "list" ? t("List") : v === "kanban" ? t("Board") : v === "pivot" ? t("Pivot") : t("Chart")}
              </button>
            ))}
          </span>
        )}
        </>}
        {(view === "list" || view === "kanban") && (
          <span className="ml-auto flex items-center gap-1 text-xs text-muted">
            {error ?? (total ? `${offset + 1}–${Math.min(offset + pageSize, total)} of ${total}` : "none")}
            <Button size="sm" variant="ghost" aria-label={t("Previous page")} disabled={offset === 0} onClick={() => window ? window.onChange({offset:Math.max(0,offset-pageSize)}) : setOffset(Math.max(0, offset - pageSize))}><ChevronLeft /></Button>
            <Button size="sm" variant="ghost" aria-label={t("Next page")} disabled={offset + pageSize >= total || !!window && offset+pageSize>window.maxOffset} onClick={() => window ? window.onChange({offset:offset+pageSize}) : setOffset(offset + pageSize)}><ChevronRight /></Button>
          </span>
        )}
      </div>
      {staleFilters.length > 0 && <p role="alert" className="text-xs text-danger">{error} <Button size="sm" variant="ghost" onClick={()=>applyFilters({})}>{t("Clear filters")}</Button></p>}
      {facetPanel && !filtersLocked && facetFields.length > 0 && <div role="group" aria-label={t("Filter by values")} className="flex flex-wrap items-start gap-x-6 gap-y-3 rounded-md border border-border p-3">
        {facetFields.map((field) => {
          const counts = facetCounts?.[field.name];
          const unavailable = counts === "unavailable";
          const options = (field.choices ?? []).map(value=>({value,count:counts&&!unavailable?counts.find(option=>option.value===value)?.count??0:undefined}))
            .filter((option) => (facetSelection[field.name]?.includes(option.value)) || unavailable || !counts || (option.count ?? 0) > 0);
          return <FacetChoices key={field.name} title={field.title} histogram={!!counts && !unavailable} options={options} value={facetSelection[field.name] ?? []}
            onChange={(values) => applyFilters({ ...facetSelection, [field.name]: values })} />;
        })}
        {Object.values(facetCounts ?? {}).includes("unavailable") && <p role="status" className="text-xs text-muted self-center">{t("Counts are unavailable right now; choices come from the object model.")}</p>}
        <Button size="sm" variant="ghost" onClick={() => applyFilters({})}>{t("Clear filters")}</Button>
      </div>}
      {view === "list" && cards && (error?<p role="alert" className="text-sm text-danger">{humanizeKernelError(error)}</p>:!page?<p role="status" className="text-sm text-muted">{t("Loading…")}</p>:<RecordCards records={page.records} info={info} fields={fields} labelField={cards.labelField} layout={cardLayout} selected={selectedId} onSelect={onOpen} onNavigate={onNavigate} source={source} onOpenReference={onOpenReference}/>)}
      {view === "list" && !cards && (
        <EditableRecordGrid readError={error?humanizeKernelError(error):undefined} storageKey={`records:${type}`} totalRows={page?.total} key={JSON.stringify([editTask.current.generation,type,info,inlineEdit?.schema,inlineEdit?.fields,inlineEdit?.preview])} data={page?.records} columns={columnsOf as never} entity={entity} height={height} showToolbar={showToolbar} port={inlineEdit} selectedId={selectedId} selectionSet={selectionSet} onOpen={onOpen} loading={!page&&!error} empty={{title:t("No {things}", { things: info.plural.toLowerCase() }),hint:canClearFilters?t("No records match these filters."):t("Records appear here after they are created or imported."),action:canClearFilters?<Button size="sm" onClick={()=>{if(window)window.onChange({...!searchLocked?{search:""}:{},...!filtersLocked?{filters:{}}:{},offset:0});else{if(!searchLocked)setSearch("");setOffset(0);if(!filtersLocked)applyFilters({});}}}>{t("Clear filters")}</Button>:undefined}}/>
      )}
      {view === "kanban" && kanbanLanes.length > 0 && kanbanStateField && (
        error ? <p role="alert" className="text-sm text-danger">{humanizeKernelError(error)}</p>
        : !page ? <p role="status" className="text-sm text-muted">{t("Loading…")}</p>
        : <RecordKanban
            records={page.records}
            lanes={kanbanLanes}
            stateField={kanbanStateField}
            labelField={kanban?.labelField ?? cards?.labelField ?? (info.display !== "id" && info.display ? info.display : "title" in entity.fields ? "title" : "id")}
            selected={selectedId}
            onSelect={(r) => { if (r && onOpen) onOpen(r); }}
            moves={moves}
            onMove={onMove}
            properties={kanban?.properties ?? (r => (fields ?? listed(entity)).slice(0, 4).filter(k => k !== kanbanStateField && k !== (info.display ?? "id")).map(k => [entity.fields[k]?.label ?? k, entity.fields[k]?.display(valueOf(entity, k, r), r)]))}
            label={info.plural ?? type}
          />
      )}
      {view === "pivot" && aggregate && rows && (
        <Pivot source={{ aggregate, scope: source.scope, revision: source.revision, watchAggregate: source.watchAggregate }} type={type} query={query} rows={rows} columns={columns || undefined} measure={measure}
          onDrill={(d) => { setDrilled([...(drilled ?? []), ...d]); setOffset(0); setView("list"); }} />
      )}
      {view === "chart" && aggregate && rows && <Chart spec={spec} source={{ aggregate, scope: source.scope, revision: source.revision, watchAggregate: source.watchAggregate }} height={360} />}
    </div>
  );
}

export {choiceTone, setCurrency, entityFrom, StatusBar, groupable, measurable, type Options} from "./RecordEntity";
export {RecordHistory, type RecordHistoryProps} from "./RecordHistory";
export {useRecordView, RecordPage, RecordDeliveries, RecordLinks, RecordStatus} from "./RecordPage";
export {Tasks, Inbox, type InboxTask} from "./RecordPanels";
