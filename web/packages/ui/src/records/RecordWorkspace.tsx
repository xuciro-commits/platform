import {useEffect,useMemo,useRef,useState,type ReactNode} from "react";
import {ChevronLeft,ChevronRight} from "lucide-react";
import {type Api} from "@platform/kernel";
import {Button} from "../primitives/button";
import {Input,Select} from "../primitives/input";
import {PageHeader} from "../components/PageHeader";
import {useViewVisible} from "../shell/ViewVisibility";
import {Chart} from "../charts/Chart";
import {Pivot} from "../charts/Pivot";
import type {ChartSpec,Mark} from "../charts/spec";
import {t} from "../i18n";
import {type Tone} from "../components/StatusTag";
import {columnsFor,valueOf} from "../fields/entity";
import {humanizeKernelError} from "../lib/errors";
import {RecordCards} from "./RecordCards";
import {EditableRecordGrid,type RecordEditPort,type RecordSelectionPort} from "./EditableRecordGrid";
import {presentRecordColumns,type RecordColumnPresentation} from "./ColumnPresentation";
import {RecordKanban,type KanbanLane,type KanbanMove} from "./RecordKanban";
import {entityFrom,groupable,listed,measurable,type EntityRecord,type RecordPageData,type RecordQuery,type RecordSource} from "./record-presentation";
type ListView = "list" | "kanban" | "pivot" | "chart";
export type ListState = {
  view?: ListView; search?: string; sort?: string; archived?: boolean; drilled?: unknown[];
  group?: string; columns?: string; measure?: string; mark?: Mark;
};
export function RecordList({ source, type, onOpen, toolbar, height = "calc(100dvh - 230px)", pageSize = 100, domain: fixed, initial = {}, onSave, fields, window,inlineEdit,selectionSet,columnPresentation,showSearch=true,tablePresentation,cards,selectedId,onNavigate, kanban, onMove, moves = [] }: {
  tablePresentation?:Api.PageTablePresentation;onNavigate?:(record:EntityRecord)=>void;cards?:{layout:"grid"|"list";labelField:string};selectedId?:string;columnPresentation?:RecordColumnPresentation[];showSearch?:boolean;
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
  kanban?: { lanes?: KanbanLane[]; stateField?: string; labelField?: string; properties?: (record: EntityRecord) => [string, ReactNode][] };
  onMove?: (record: EntityRecord, schema: string, destination?: string) => void;
  /** Commands already offered by the application owner; absent or empty is read-only. */
  moves?: KanbanMove[];
}) {
  const densityIdentity=JSON.stringify([source.scope,type,tablePresentation]),[densityChoice,setDensityChoice]=useState<{identity:string;density:string}>();
  const density=densityChoice?.identity===densityIdentity?densityChoice.density:tablePresentation?.density??"compact",showToolbar=tablePresentation?.showToolbar!==false;
  const cardIdentity=JSON.stringify([source.scope,type,cards]),[cardChoice,setCardChoice]=useState<{identity:string;layout:"grid"|"list"}>();
  const cardLayout=cardChoice?.identity===cardIdentity?cardChoice.layout:cards?.layout??"grid";
  const info = source.entity(type);
  const [localSearch, setSearch] = useState(initial.search ?? "");
  const [localSort, setSort] = useState(initial.sort ?? "-changed");
  const [localOffset, setOffset] = useState(0);
  const [archived, setArchived] = useState(initial.archived ?? false);
  const [loadedPage, setLoadedPage] = useState<{ key: string; page: RecordPageData }>();
  const search = window ? window.inputSearch ?? window.query.search ?? "" : localSearch, sort = window ? window.query.sort?.[0] ?? "id" : localSort, offset = window ? window.query.offset ?? 0 : localOffset;
  if (window) pageSize = window.query.limit ?? pageSize;
  const windowKey = JSON.stringify(window?.query);
  const [localError, setError] = useState<string>();
  const error = window ? window.error : localError;
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
  const domain = useMemo(() => [...JSON.parse(fixedKey), ...(drilled ?? [])], [fixedKey, drilled]);
  const entity = useMemo(() => (info ? entityFrom(info,{},source) : undefined), [info,source]);
  const infoKey = JSON.stringify(info);
  const pageKey = JSON.stringify([source.scope, type, infoKey, domain, search, sort, offset, archived, pageSize]);
  const page = window ? window.page : loadedPage?.key === pageKey ? loadedPage.page : undefined;
  const from = useRef(source);
  from.current = source;
  // Scoped sources may be wrapped anew during ordinary parent renders. Their
  // scope, metadata, query and revision determine when a read is necessary.
  const sourceIdentity = source.scope ?? source;
  const visible = useViewVisible();
  const sortField = sort.replace(/^-/, "");
  const knownSort = ["id", "created", "changed"].includes(sortField) || info?.fields.some((f) => f.name === sortField);
  const recordQuery: RecordQuery = window?.query ?? { domain, search, sort: knownSort ? [sort] : ["id"], offset, limit: pageSize, archived };
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
    if (!source.exportCSV) return;
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
    if (!visible || window || !info || (view !== "list" && view !== "kanban")) return;
    let current = true;
    const source = from.current;
    setError(undefined);
    const load = () => {
      source.list(type, recordQuery)
        .then((p) => { if (current) { setLoadedPage({ key: pageKey, page: p }); setError(undefined); } }, (e) => { if (current) { setLoadedPage(undefined); setError(String(e)); } });
    };
    const stop = source.watchList?.(type, recordQuery, load);
    const handle = setTimeout(load, 150);
    return () => { current = false; stop?.(); clearTimeout(handle); };
  }, [sourceIdentity, source.revision, pageKey, view, windowKey, visible]);
  if (!info || !entity) return <p className="rounded-md border border-dashed border-border p-3 text-sm text-muted">{t("No activated object {type} here yet. Its records appear once a release that includes it is active; a draft shows its fields in the object model.", { type })}</p>;
  const columnsOf = presentRecordColumns([{ id: "id", header: "ID", accessorKey: "id", meta: { width: 130 }, cell: (c: any) => <span className="font-mono text-xs">{c.getValue()}</span> },
    ...columnsFor(entity,[...new Set(fields??listed(entity))].filter(name=>listed(entity).includes(name))).map((c) => ({ ...c, enableSorting: false }))],entity,info,columnPresentation);
  const total = page?.total ?? 0;
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
        {showToolbar&&<>
        {tablePresentation&&<Select aria-label={t("Record density")} value={density} onChange={e=>setDensityChoice({identity:densityIdentity,density:e.target.value})}><option value="compact">{t("Compact")}</option><option value="normal">{t("Comfortable")}</option></Select>}
        {showSearch&&<Input aria-label={t("Search")} placeholder={t("Search {things}", { things: info.plural.toLowerCase() })} value={search} disabled={window?.searchLocked} className="w-56"
          onChange={(e) => { if(window)window.onChange({search:e.target.value,offset:0});else {setSearch(e.target.value);setOffset(0);} }} />}
        {(view === "list" || view === "kanban") ? (
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
        {!window && <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={archived} onChange={(e) => { setArchived(e.target.checked); setOffset(0); }} />{t("Archived")}</label>}
        {drilled && <Button size="sm" variant="ghost" onClick={() => { setDrilled(undefined); setOffset(0); }}>{t("Clear drill-down ×")}</Button>}
        {cards&&<span role="group" aria-label={t("Record layout")} className="flex gap-1">{(["list","grid"] as const).map(layout=><Button key={layout} size="sm" variant="ghost" aria-pressed={cardLayout===layout} onClick={()=>setCardChoice({identity:cardIdentity,layout})}>{t(layout==="grid"?"Grid":"List")}</Button>)}</span>}
        {source.exportCSV && <Button size="sm" disabled={exporting} onClick={() => void exportCSV()}>{t("Export CSV")}</Button>}
        {exportError && <span role="alert" className="text-xs text-danger">{exportError}</span>}
        {toolbar}
        {onSave && <Button size="sm" variant="ghost" onClick={() => onSave({ view, search, sort, archived, drilled, group: rows, columns, measure, mark })}>{t("Save view…")}</Button>}
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
      {view === "list" && cards && (error?<p role="alert" className="text-sm text-danger">{humanizeKernelError(error)}</p>:!page?<p role="status" className="text-sm text-muted">{t("Loading…")}</p>:<RecordCards records={page.records} info={info} fields={fields} labelField={cards.labelField} layout={cardLayout} selected={selectedId} onSelect={onOpen} onNavigate={onNavigate}/>)}
      {view === "list" && !cards && (
        <EditableRecordGrid key={JSON.stringify([source.scope,type,info,inlineEdit?.schema,inlineEdit?.fields,inlineEdit?.scope,inlineEdit?.preview,domain,search,sort,offset,archived,error])} data={page?.records} columns={columnsOf as never} entity={entity} height={height} rowHeight={density==="normal"?36:28} showToolbar={showToolbar} port={inlineEdit} selectedId={selectedId} selectionSet={selectionSet} onOpen={onOpen} loading={!page && !error} empty={error ? humanizeKernelError(error) : t("No {things}", { things: info.plural.toLowerCase() })}/>
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
export function RecordWorkspace({ title, description, source, type, listFields, selected, onSelect, detail, actions, notice }: {
  title: string; description?: string; source: RecordSource; type: string; listFields?: string[];
  selected?: string; onSelect: (id?: string) => void; detail: (id: string) => ReactNode;
  actions?: ReactNode; notice?: ReactNode;
}) {
  return <>
    <PageHeader title={title} description={description} actions={actions} />
    {notice}
    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(20rem,45%)_minmax(0,1fr)]">
      <section aria-label={t("Records in this page")} className={selected ? "hidden min-w-0 lg:block" : "min-w-0"}>
        <RecordList source={source} type={type} fields={listFields} selectedId={selected} onOpen={(r) => onSelect(r.id)} height="calc(100dvh - 255px)" />
      </section>
      <section aria-label={t("Selected record")} className={!selected ? "hidden min-w-0 lg:block" : "min-w-0"}>
        {selected ? <>
          <Button size="sm" variant="ghost" className="mb-2 lg:hidden" onClick={() => onSelect(undefined)}>{t("Back to list")}</Button>
          {detail(selected)}
        </> : <p className="rounded-md border border-border bg-surface p-6 text-sm text-muted">{t("Select a record to see its details.")}</p>}
      </section>
    </div>
  </>;
}
