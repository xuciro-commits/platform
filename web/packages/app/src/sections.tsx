import {confirmObservationRow} from "./widgets/observation-selection";
import {usePageComputations} from "./runtime/PageComputations";
import {aiRecordSlot} from "./widgets/ai-context";
import {CollectionBuilderRenderer} from "./widgets/CollectionBuilder";
import {RecordMapRenderer} from "./widgets/RecordMap";
import {AIWidget} from "./widgets/AIWidget";
import {PageEmbeddingBoundary,EmbeddedPageRenderer,ExternalDocumentRenderer} from "./widgets/EmbeddedPage";
import {avatarCollectionVariable,confirmedContext,originalContextSlot,currentContextRead} from "./widgets/context-views";
import {searchInputObjects} from "./widgets/search-input";
import {tableEditableFields} from "./widgets/table-edit";
import {Facets} from "./widgets/Facets";
import {isStringSet,isDecimal,scalarAssignable,type ScalarValue} from "./runtime/decimal";
// A composed page (ADR-0035): sections laid out in order, each holding one
// widget bound to what this tenant has. A table says which record is selected;
// a detail and the actions read it. Every widget renders through the owner that
// already has it — the kit's record list and record page, the action catalog,
// the aggregate chart — so a code page and a composed page look and behave the
// same, and nothing here interprets data of its own.
import {
  Button,ButtonGroup, CollectionTitle, PageHeader, Card, RegionPresentation, LayoutRegion, LayoutStack, ContentTabs, Dialog, FlowLayout, Sheet, Input, SearchInput, Markdown, Panel, PropertyList, RecordHistory, RecordList, RecordLookup, RecordPage, RecordLinks, RecordStatus, Select, Tasks, cn, t, useViewVisible, type ChartSpec, type EntityRecord, type RecordSource, type RecordView,
} from "@platform/ui";
import { Component, lazy, Suspense, useEffect, useId, useMemo, useRef, useState, type ReactNode } from "react";
import { NewActions, RecordActions, InlineActionForm, prefixOf } from "./actions";
import { GeneratedForm, findDefinition, newId, useHost,useOpenRecord, useInvokeCapability, type Definition } from "./index";
import { ComputeCall } from "./capability";
import type { Api } from "@platform/kernel";
import { createWidgetRegistry, supportsPageUIProfile } from "./widgets/registry";
import { useApplicationContext, useApplicationVariables } from "./runtime/ApplicationRuntime";
import { usePageQueries } from "./runtime/PageQueries";
import { planKey, variablePlan } from "./runtime/query-plans";
import { compileChartSpec } from "./widgets/chart-spec";
import { inputSlot, usePageInputs, usePageNavigation } from "./runtime/PageNavigation";
import { NestedLoopRuntime } from "./runtime/NestedLoopRuntime";
import { LoopRuntime, type LoopContext } from "./runtime/LoopRuntime";
import {recordReadReference, type PageSessionStore} from "./runtime/Session";
import { recordSlot, filterOwner, filtersForOwner, filterSessionBindings, selectionSetSlot,selectionSlot,recordOutputSlot,recordOutputObject,recordResourceSlot, overlaySessionScopes, resourceVariables } from "./runtime/resources";
import { evaluateVariables, type VariableResult } from "./runtime/variables";
import { pageVariableContract, usePageVariables, usePageSession } from "./runtime/PageRuntime";

const ExplorationRenderer=lazy(()=>import("./widgets/Exploration").then(module=>({default:module.ExplorationRenderer})));
const ActionTableRenderer=lazy(()=>import("./widgets/RecordWork").then(m=>({default:m.ActionTableRenderer})));
const RecordTilesRenderer=lazy(()=>import("./widgets/RecordWork").then(m=>({default:m.RecordTilesRenderer})));
const NotepadRenderer=lazy(()=>import("./widgets/RecordWork").then(m=>({default:m.NotepadRenderer})));
const ObservationRenderer=lazy(()=>import("./widgets/Observation").then(module=>({default:module.ObservationRenderer})));
const CollectionAnalysisRenderer=lazy(()=>import("./widgets/CollectionAnalysis").then(module=>({default:module.CollectionAnalysisRenderer})));
const ResourceListRenderer=lazy(()=>import("./widgets/Exploration").then(module=>({default:module.ResourceListRenderer})));
const AssetDirectoryRenderer=lazy(()=>import("./widgets/Exploration").then(module=>({default:module.AssetDirectoryRenderer})));
const BreadcrumbRenderer=lazy(()=>import("./widgets/ContextViews").then(module=>({default:module.BreadcrumbRenderer})));
const AvatarStackRenderer=lazy(()=>import("./widgets/ContextViews").then(module=>({default:module.AvatarStackRenderer})));
const StaticImageRenderer=lazy(()=>import("./widgets/ContextViews").then(module=>({default:module.StaticImageRenderer})));
const HistogramRenderer=lazy(()=>import("./widgets/Histogram").then(module=>({default:module.HistogramRenderer})));
const TermCountsRenderer=lazy(()=>import("./widgets/TermCounts").then(module=>({default:module.TermCountsRenderer})));
const ChartRenderer=lazy(()=>import("./widgets/Chart").then(module=>({default:module.ChartRenderer})));
const RecordPickerRenderer=lazy(()=>import("./widgets/RecordPicker").then(module=>({default:module.RecordPickerRenderer})));
const SpacerRenderer=lazy(()=>import("./widgets/Spacer").then(module=>({default:module.SpacerRenderer})));
const SeparatorRenderer=lazy(()=>import("./widgets/Separator").then(module=>({default:module.SeparatorRenderer})));
const NoticeRenderer=lazy(()=>import("./widgets/Notice").then(module=>({default:module.NoticeRenderer})));
const AlertRenderer=lazy(()=>import("./widgets/Alert").then(module=>({default:module.AlertRenderer})));
const DateInputRenderer=lazy(()=>import("./widgets/DateInput").then(module=>({default:module.DateInputRenderer})));
const ChoiceInputRenderer=lazy(()=>import("./widgets/ChoiceInput").then(module=>({default:module.ChoiceInputRenderer})));
const BooleanInputRenderer=lazy(()=>import("./widgets/BooleanInput").then(module=>({default:module.BooleanInputRenderer})));
const RangeRenderer=lazy(()=>import("./widgets/Range").then(module=>({default:module.RangeRenderer})));
const LeaderboardRenderer=lazy(()=>import("./widgets/Leaderboard").then(module=>({default:module.LeaderboardRenderer})));
const SummaryRenderer=lazy(()=>import("./widgets/Summary").then(module=>({default:module.SummaryRenderer})));
const RecordCollaborationRenderer=lazy(()=>import("./widgets/RecordCollaboration").then(module=>({default:module.RecordCollaborationRenderer})));
const WorkViewsRenderer=lazy(()=>import("./widgets/WorkViews").then(module=>({default:module.WorkViewsRenderer})));
const RecordComparisonRenderer=lazy(()=>import("./widgets/RecordComparison").then(module=>({default:module.RecordComparisonRenderer})));
const RecordCardRenderer=lazy(()=>import("./widgets/RecordCard").then(module=>({default:module.RecordCardRenderer})));
const SparklineRenderer=lazy(()=>import("./widgets/Sparkline").then(module=>({default:module.SparklineRenderer})));
const GaugeRenderer=lazy(()=>import("./widgets/Gauge").then(module=>({default:module.GaugeRenderer})));
const ProgressRenderer=lazy(()=>import("./widgets/Progress").then(module=>({default:module.ProgressRenderer})));
const RecordGanttRenderer=lazy(()=>import("./widgets/RecordGantt").then(module=>({default:module.RecordGanttRenderer})));
const RecordCalendarRenderer=lazy(()=>import("./widgets/RecordCalendar").then(module=>({default:module.RecordCalendarRenderer})));
const RecordEventsRenderer=lazy(()=>import("./widgets/RecordEvents").then(module=>({default:module.RecordEventsRenderer})));
const RecordScatterRenderer=lazy(()=>import("./widgets/RecordScatter").then(module=>({default:module.RecordScatterRenderer})));
const RecordChartRenderer=lazy(()=>import("./widgets/RecordChart").then(module=>({default:module.RecordChartRenderer})));
const TableRenderer=lazy(()=>import("./widgets/Table").then(module=>({default:module.TableRenderer})));
const RecordTimelineRenderer=lazy(()=>import("./widgets/RecordTimeline").then(module=>({default:module.RecordTimelineRenderer})));
const KanbanRenderer=lazy(()=>import("./widgets/Kanban").then(module=>({default:module.KanbanRenderer})));
const PivotRenderer=lazy(()=>import("./widgets/Pivot").then(module=>({default:module.PivotRenderer})));
const ButtonRenderer=lazy(()=>import("./widgets/Button").then(module=>({default:module.ButtonRenderer})));

import type {QueryWindow} from "./widgets/QueryWindowFrame";
type Page = NonNullable<Definition["page"]>;
type Section = NonNullable<Page["sections"]>[number];

/** The page's second variable (16b): the conditions each filter set, by the
 *  object they narrow. A table, chart or metric over that object reads them. */
type Narrowed = Record<string, Record<string, unknown>>;

/** What a section is bound to, and what the page has selected and narrowed to. */
type Bound = {builder?:ReturnType<typeof usePageQueries>["builders"][string];sceneWindow?:QueryWindow;embeddingInputs?:Record<string,VariableResult>;embeddingReturn?:(values:Record<string,unknown>)=>void;observationHistory?:QueryWindow;observationContext?:QueryWindow;observationAsset?:EntityRecord;observationSelected?:string;onObservationRow?:(record:EntityRecord)=>Promise<void>;notepadValue?:VariableResult;onNotepad?:(value:string)=>void;explorationRoot?:EntityRecord;explorationStatus?:"empty"|"pending"|"value"|"error";explorationIdentity?:string;explorationActive?:()=>boolean;onGraphOutput?:(object:string,record:EntityRecord)=>Promise<boolean>;graphSelected?:{object:string;id:string};contextReadCurrent?:boolean;avatarContextStatus?:"empty"|"pending"|"value"|"error";onClearContext?:()=>void;collaborationRecord?:EntityRecord;collaborationReference?:import("./runtime/Session").RecordReference;collaborationStatus?:"empty"|"pending"|"value"|"error";collaborationSlot?:string;commentDraft?:VariableResult;fileValue?:VariableResult;pdfPageValue?:VariableResult;onCommentDraft?:(value:string)=>void;onFileID?:(value:string)=>void;onPdfPage?:(value:string)=>void;comparisonRecords?:EntityRecord[];comparisonStatus?:"empty"|"pending"|"value"|"error";confirmedRecord?:EntityRecord;recordStatus?:"empty"|"pending"|"value"|"error";sparklineValue?:VariableResult;groupValue?:VariableResult;onGroupFilter?:(value?:string)=>void;onHeatmap?:(row?:string,column?:string)=>void;pickerConfirmation?:"empty"|"pending"|"value"|"error";pickerValue?:VariableResult;onPickerID?:(record?:EntityRecord)=>void;alertValue?:VariableResult;dateValue?:VariableResult;onDate?:(value:string)=>void;choiceSetValue?:VariableResult;onChoiceSet?:(value:string[])=>void;choiceValue?:VariableResult;onChoice?:(value:string)=>void;booleanInput?:VariableResult;onBoolean?:(checked:boolean)=>void;rangeLower?:VariableResult;rangeUpper?:VariableResult;onRange?:(lower:string,upper:string)=>void;statisticsValue?:VariableResult;gaugeValue?:VariableResult;progressValue?:VariableResult;progressTotal?:VariableResult;countValue?:string;countError?:string;
  actionReady?:boolean;onRecordOpen?:(type:string,record:EntityRecord)=>void;onControl?:(id:string)=>void;controlBound?:(id:string)=>boolean;
  page: Page; section: Section; selected?: EntityRecord; onSelect: (record?: EntityRecord) => void; live: boolean;
  master?: EntityRecord;
  session?: PageSessionStore;
  window?: NonNullable<Parameters<typeof RecordList>[0]["window"]>;
 collection?:VariableResult; aggregateScope?:string;
  selectionSet?:import("@platform/ui").RecordSelectionPort;
  keepActive?:boolean;
  facetValues?:Record<string,VariableResult>;onFacet?:(id:string,value:ScalarValue)=>void;
  inputScopes?:string[];onClick?: () => void; numeric?:boolean; valueError?:string; value?: string; onValue?: (value: string) => void; enabled?: boolean; readSource?: RecordSource;
  sharedFilter?:Record<string,unknown>; narrowed: Narrowed; onNarrow: (object: string, field: string, value: unknown) => void;
};

/** Composing: the section in hand, and choosing another by clicking it. */
type Composing = { chosen?: number; onChoose?: (at: number) => void; at?: number; nested?: boolean;
  wrapLayout?: (id: string, node: Api.PageLayoutNode, body: ReactNode) => ReactNode };

const objectOf = (page: Page, section: Section) => section.object?.name || page.object.name;
const parentTypeOf = (page: Page, section: Section) => section.parentSelection
  ? page.selections?.find((selection) => selection.name === section.parentSelection)?.object.name ?? "" : page.object.name;
// Named and unnamed selections use one typed slot model.
const selectionKey = recordSlot;

/** The filters' conditions over an object, as the host's domain (ADR-0019). */
const domainOf = (narrowed: Narrowed, object: string): unknown[] =>
  Object.entries(narrowed[object] ?? {}).filter(([, v]) => v !== undefined && v !== "").map(([field, v]) => [field, "=", v]);

/** The reference that ties a section's object to the page's selected record:
 *  the declared relation when the section names one (ADR-0040 21b), else the
 *  first reference to the page's object. */
const relatedField = (fields: { name: string; title: string; type: string; ref?: string; inverse?: string; readOnly?: boolean }[] | undefined, page: Page, section: Section) =>
  fields?.find((f) => f.type === "reference" && f.ref === parentTypeOf(page, section) && (!section.relation || f.inverse === section.relation));

/** The records of an object, as a list; selecting one fills the rest of the page. */
function TableAdapter({ page, section, onSelect, selected, master, narrowed, sharedFilter, session, window,collection,live,aggregateScope,selectionSet,keepActive }: Bound) {
  const openRecord=useOpenRecord();
  const { source, definitions,catalog,decide } = useHost();
  const type = objectOf(page, section);
  const isMaster = type === parentTypeOf(page, section) && !section.parentSelection && !section.relation;
  const info = source.entity(type);
  // A named query (ADR-0040 21c): its declared conditions, run for the selected
  // record through its reference; the list is still the member's own read.
  const query = section.query?.name ? findDefinition(definitions, section.query)?.query : undefined;
  const refField = !isMaster
    ? (query?.by ? info?.fields.find((f) => f.name === query.by) : relatedField(info?.fields, page, section))
    : undefined;

  const editAction=section.inlineEdit?catalog.find(a=>a.schema===section.inlineEdit!.action.name):undefined,editFields=tableEditableFields(info,editAction,section.inlineEdit?.fields??[]);
  const inlineEdit=editAction&&editFields.length?{schema:editAction.schema,fields:editFields,scope:JSON.stringify([aggregateScope,editAction]),preview:!live,maxRows:pageVariableContract.tableEditing.maxRows,submit:async(record:EntityRecord,patch:Record<string,unknown>)=>{let error:string|undefined;const accepted=await decide(editAction.schema,{type,id:record.id},patch,{expectedRevision:record.revision,quiet:true,onRefused:reason=>error=reason});return {accepted,error};}}:undefined;
  if(section.collectionVariable&&collection?.status==="error")return <Panel role="alert">{t(collection.code)}</Panel>;
  const status = section.collectionVariable ? !window ? "missing-window" : undefined
    : (section.relation || section.parentSelection) && !refField ? "invalid-reference" : refField && !master ? "missing-parent" : undefined;
  const relationDomain = refField && master ? [[refField.name, "=", master.id]] : [];
  const domain = [...((query?.domain as unknown[] | undefined) ?? []), ...domainOf(narrowed, type), ...domainOf({[type]:sharedFilter??{}},type), ...relationDomain];
  return <TableRenderer onNavigate={section.widget==="record-list"&&live?record=>openRecord({type,id:record.id}):undefined} cards={section.widget==="record-list"?{layout:section.recordList?.layout as "grid"|"list"??"grid",labelField:section.cardLabel??"id"}:undefined} key={section.collectionVariable ? type : refField ? `${type}/${refField.name}/${master?.id}` : type}
    source={section.collectionVariable ? source : session?.querySource(section.id ?? `section:${page.sections?.indexOf(section)}`) ?? source}
    presentation={section.tablePresentation} columns={section.tableColumns} showSearch={section.showSearch} keepActive={section.widget==="record-list"||keepActive} selectionSet={selectionSet} inlineEdit={inlineEdit} object={type} fields={section.fields} domain={section.collectionVariable ? undefined : domain} window={window}
    selected={selected} onSelect={onSelect} status={status} plural={info?.plural?.toLowerCase()}/>;
}

function RecordTimelineAdapter({page,section,onSelect,selected,window}:Bound) {
 const {source}=useHost(),info=source.entity(objectOf(page,section));
 const start=info?.fields.find(f=>f.name===section.timeStart),end=section.timeEnd?info?.fields.find(f=>f.name===section.timeEnd):undefined;
 const label=(name?:string)=>name==="id"||!!info?.fields.some(f=>f.name===name&&["text","longtext","choice","reference"].includes(f.type));
 const valid=start&&["date","datetime"].includes(start.type)&&(!section.timeEnd||end?.type===start.type)&&label(section.timeLabel)&&(!section.timeGroup||label(section.timeGroup));
 return <RecordTimelineRenderer window={window} fields={valid?{start:section.timeStart!,end:section.timeEnd,label:section.timeLabel!,group:section.timeGroup,kind:start.type as "date"|"datetime"}:undefined} selected={selected} onSelect={onSelect} title={section.title||t("Record timeline")}/>;
}
function RecordChartAdapter({page,section,window}:Bound) {
 const {source}=useHost(),info=source.entity(objectOf(page,section));
 if(!info)return <Panel role="alert">{t("Record chart fields or values are unavailable or incompatible.")}</Panel>;
 return <RecordChartRenderer window={window} info={info} fields={{mark:section.recordChart?.mark as "bar"|"line"??"line",xField:section.recordChart?.xField??"",yField:section.recordChart?.yField??""}}/>;
}
function KanbanAdapter({page,section,onSelect,selected,window,live}:Bound) {
 const {source,catalog}=useHost(),object=objectOf(page,section),info=source.entity(object);
 const allowed=new Set((section.actions??[]).map(a=>a.name));
 const moves=(info?.lifecycle?.transitions??[]).filter(m=>allowed.has(m.schema)&&catalog.some(a=>a.schema===m.schema&&a.target===object)&&(!m.toInput&&m.to.length===1||!!m.toInput&&Number(page.document?.uiProfile.split(".").at(-1))>=99&&catalog.some(a=>a.schema===m.schema&&a.payload.some(p=>p.name===m.toInput&&p.type==="string"&&m.to.every(state=>p.choices?.includes(state)))))).flatMap(m=>m.to.map(to=>({schema:m.schema,title:m.title,from:m.from,to,input:m.toInput})));
 return <KanbanRenderer key={JSON.stringify([object,window?.query,source.scope])} object={object} info={info} window={window} cardLabel={section.cardLabel??""} fields={section.fields} selected={selected} onSelect={onSelect} moves={moves} live={live} title={section.title||t("Kanban board")}/>;
}

/** The record the page has selected, with the fields the builder chose. */
function DetailWidget({ page, section, selected, readSource }: Bound) {
  const host = useHost(), source = readSource ?? host.source;
  const type = objectOf(page, section);
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
  // The fields alone: what people do with it is the actions widget's (ADR-0035 D2).
  return <RecordPage key={`${type}/${selected.id}`} source={source} type={type} id={selected.id} fields={section.fields} detailPresentation={section.detailPresentation} detailOnly />;
}

function StatusTrackerWidget({page,section,selected,readSource}:Bound){
 const host=useHost(),source=readSource??host.source,type=objectOf(page,section);
 if(!selected)return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
 return <RecordStatus key={JSON.stringify([source.scope,type,selected.id])} source={source} type={type} id={selected.id} config={section.statusTracker}/>;
}

function RecordLinksWidget({page,section,selected,readSource,live,onRecordOpen}:Bound){
 const host=useHost(),open=useOpenRecord(),source=readSource??host.source,type=objectOf(page,section);
 if(!selected)return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
 return <RecordLinks key={JSON.stringify([source.scope,type,selected.id])} source={source} type={type} id={selected.id} groups={section.recordLinks??[]} onOpen={live?onRecordOpen??((type,record)=>open({type,id:record.id})):undefined}/>;
}

function RecordViewWidget({page,section,selected,readSource,live,onRecordOpen}:Bound){
 const host=useHost(),open=useOpenRecord(),source=readSource??host.source,type=objectOf(page,section);
 if(!selected)return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
 return <RecordPage key={JSON.stringify([source.scope,type,selected.id])} source={source} type={type} id={selected.id} fields={section.fields??[]} recordTabs={section.recordView?.tabs??pageVariableContract.recordView.tabs} onOpen={live?onRecordOpen??((type,record)=>open({type,id:record.id})):undefined} actions={record=>live?<RecordActions type={type} record={record} allowed={(section.actions??[]).map(a=>a.name)} steps/>:<p className="text-xs text-muted">{t("Actions do not run while you compose.")}</p>}/>;
}

/** The actions the builder chose, on what is selected (Workshop's button group). */
function ActionsWidget({ page, section, selected, live }: Bound) {
  const type = objectOf(page, section);
  const allowed = (section.actions ?? []).map((ref) => ref.name);
  if (!live) return <p className="text-sm text-muted">{t("Actions do not run while you compose.")}</p>;
  return (
    <div className="flex flex-wrap gap-2">
      <NewActions type={type} allowed={allowed} />
      {selected
        ? <RecordActions type={type} record={selected} allowed={allowed} steps />
        : <span className="self-center text-sm text-muted">{t("Select a record to act on it.")}</span>}
    </div>
  );
}

/** An aggregate of the object: grouped and measured, drawn by the kit (ADR-0019). */
function chartSpec(page: Page, section: Section, kpi: boolean, domain: unknown[]): ChartSpec {
  return {...compileChartSpec({object:objectOf(page,section),title:section.title,group:section.group,measure:section.measure,mark:section.mark,chartVariant:section.chartVariant,kpi,domain}),metric:kpi?section.metricPresentation:undefined};
}

function ChartWidget({ page, section, kpi, pivot, narrowed, sharedFilter, master, window, collection, aggregateScope,onHeatmap,enabled }: Bound & { kpi: boolean; pivot?:boolean }) {
  const { source } = useHost();
  const aggregate = source.aggregate;
  const render=(spec:ChartSpec,readScope?:string)=>pivot?<PivotRenderer filterAxes={{row:!!(section.rowValueVariable||section.rowSetVariable),column:!!(section.columnValueVariable||section.columnSetVariable)}} heatmap={section.widget==="heatmap"} enabled={enabled} onCellFilter={onHeatmap} onClearFilters={onHeatmap?()=>onHeatmap():undefined} object={objectOf(page,section)} query={"entity" in spec.data?{domain:spec.data.domain,search:spec.data.search,set:spec.data.set,traversal:spec.data.traversal,archived:spec.data.archived}:{}} rows={section.group??""} columns={section.columnGroup} measure={section.measure??"count"} source={aggregate?{aggregate,scope:readScope??source.scope,revision:source.revision}:undefined}/>:<ChartRenderer spec={spec} height={kpi?120:240} source={aggregate?{aggregate,scope:readScope??source.scope,revision:source.revision}:undefined}/>;
  if(section.collectionVariable){
    if(!window)return <Panel role={collection?.status==="error"?"alert":"status"}>{t(collection?.status==="error"?collection.code:"Query window is unavailable.")}</Panel>;
    if(window.error)return <Panel role="alert">{t(window.error)}</Panel>;
    const {domain,search,set,archived,traversal}=window.query;
    const spec=chartSpec(page,section,kpi,domain??[]);spec.data={entity:objectOf(page,section),domain,search,set,archived,traversal};
    return render(spec,aggregateScope);
  }
  const type = objectOf(page, section);
  const isMaster = type === parentTypeOf(page, section) && !section.parentSelection && !section.relation;
  const info = source.entity(type);
  const refField = !isMaster ? relatedField(info?.fields, page, section) : undefined;
  if ((section.relation || section.parentSelection) && !refField) return <p role="alert" className="text-sm text-danger">
    {t("This section's parent reference is unavailable.")}</p>;
  if (refField && !master) return <p className="text-sm text-muted">
    {t("Select a record to see related {records}.", { records: info?.plural?.toLowerCase() ?? type })}
  </p>;
  const relationDomain = refField && master ? [[refField.name, "=", master.id]] : [];
  const domain = [...domainOf(narrowed, type), ...domainOf({[type]:sharedFilter??{}},type), ...relationDomain];

  return render(chartSpec(page, section, kpi, domain),aggregateScope);
}

/** The filter (16b): a value to narrow the object's records by, for each field
 *  the builder chose. What it sets is the page's second variable. */
function FilterWidget({ page, section, narrowed, onNarrow,facetValues,onFacet,window,aggregateScope }: Bound) {
  const { source } = useHost();
  const prefix = useId();
  const type = objectOf(page, section);
  const info = source.entity(type);
  const set = narrowed[type] ?? {};
  if(section.facets?.length||section.filterSearchVariable)return <Facets section={section} source={source} object={type} window={window} values={facetValues??{}} onChange={onFacet??(()=>{})} scope={aggregateScope??""}/>;
  return (
    <div role="search" aria-label={section.title || t("Filter")} className="flex flex-wrap items-end gap-3">
      {(section.fields ?? []).map((name) => {
        const f = info?.fields.find((x) => x.name === name);
        if (!f) return null; // not a field this member reads
        const id = `${prefix}-${type}-${name}`;
        const value = set[name];
        return (
          <label key={name} htmlFor={id} className="grid gap-1 text-xs text-muted">{f.title}
            {f.type === "reference" && f.ref
              ? <RecordLookup id={id} source={source} type={f.ref} value={value as string | undefined} onChange={(v) => onNarrow(type, name, v)} />
              : <Select id={id} aria-label={f.title} className="w-40" value={value === undefined ? "" : String(value)}
                  onChange={(e) => onNarrow(type, name, e.target.value === "" ? undefined : f.type === "boolean" ? e.target.value === "true" : e.target.value)}>
                  <option value="">{t("Any")}</option>
                  {f.type === "boolean"
                    ? <><option value="true">{t("Yes")}</option><option value="false">{t("No")}</option></>
                    : (f.choices ?? []).map((c, i) => <option key={c} value={c}>{f.choiceTitles?.[i] ?? c}</option>)}
                </Select>}
          </label>
        );
      })}
      {Object.values(set).some((v) => v !== undefined && v !== "") &&
        <Button size="sm" variant="ghost" onClick={() => Object.keys(set).forEach((name) => onNarrow(type, name, undefined))}>{t("Clear")}</Button>}
    </div>
  );
}

/** The form (16b): a new record of the object, made through its own create
 *  action with the fields the builder chose; the host checks it like any other. */
function FormWidget({ page, section, live, master }: Bound) {
  const { decide, source } = useHost();
  const invoke = useInvokeCapability();
  const type = objectOf(page, section), parentType = parentTypeOf(page, section);
  const refField = section.relation ? relatedField(source.entity(type)?.fields, page, section) : undefined;
  const [round, setRound] = useState(0), [error, setError] = useState("");
  const bindings = section.inputs ?? {};
  const bindingKey = JSON.stringify([parentType, master?.id, bindings]);
  const [bound, setBound] = useState<{ key: string; values?: Record<string, unknown>; error?: string }>({ key: "" });
  useEffect(() => {
    let current = true;
    setBound({ key: bindingKey });
    Promise.all(Object.entries(bindings).map(async ([name, binding]) => {
      if (binding.source === "literal") return [name, binding.value] as const;
      if (binding.source !== "subject" || !master || !binding.path?.length) throw new Error(t("The bound record input is unavailable."));
      let typ = parentType, record = (await source.get(typ, master.id)).record;
      for (const [index, part] of binding.path.entries()) {
        const field = source.entity(typ)?.fields.find((field) => field.name === part);
        const value = record[part];
        if (!field || value === undefined) throw new Error(t("The bound record input is unavailable."));
        if (index === binding.path.length - 1) return [name, value] as const;
        if (field.type !== "reference" || !field.ref || typeof value !== "string" || !value) throw new Error(t("The bound record input is unavailable."));
        typ = field.ref; record = (await source.get(typ, value)).record;
      }
      throw new Error(t("The bound record input is unavailable."));
    })).then((values) => { if (current) setBound({ key: bindingKey, values: Object.fromEntries(values) }); }, () => {
      if (current) setBound({ key: bindingKey, error: t("The bound record input is unavailable.") });
    });
    return () => { current = false; };
  }, [bindingKey, source, source.revision]);
  if (section.relation && (!refField || refField.readOnly)) return <p role="alert" className="text-sm text-danger">
    {t("This form's parent reference is unavailable.")}</p>;
  if (refField && !master) return <p className="text-sm text-muted">{t("Select a parent record before creating a related record.")}</p>;
  const fields = (section.fields ?? source.entity(type)?.fields.map((field) => field.name) ?? [])
    .filter((name) => name !== refField?.name && !bindings[name]);
  const parentInfo = source.entity(parentType);
  const ready = bound.key === bindingKey && bound.values !== undefined;
  const supplied = ready ? Object.entries(bound.values!).map(([name, value]) => [source.entity(type)?.fields.find((field) => field.name === name)?.title ?? name, String(value)] as [string, string]) : [];
  return <div className="grid gap-2">
    {!live && <p className="text-xs text-muted">{t("The form does not submit while you compose.")}</p>}
    {refField && master && <PropertyList items={[[refField.title, String(master[parentInfo?.display ?? "id"] ?? master.id)]]} />}
    {supplied.length > 0 && <PropertyList items={supplied} />}
    {!ready && Object.keys(bindings).length > 0 && <p role={bound.error ? "alert" : "status"} className="text-xs text-muted">{bound.error ?? t("Loading bound inputs…")}</p>}
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    <fieldset disabled={!live || !ready}>
      <GeneratedForm key={round} type={type} fields={fields} submitLabel={t("Create")} onCancel={() => { setError(""); setRound((r) => r + 1); }}
        onSubmit={async (values) => {
          if (!live) return;
          setError("");
          const payload = refField && master ? { ...values, [refField.name]: master.id } : values;
          const id = newId(prefixOf(type));
          try {
            if (Object.keys(bindings).length > 0) {
              await invoke({ ref: { app: type.split(".")[0]!, kind: "action", name: `${type}.create` }, target: id, key: crypto.randomUUID(),
                inputs: payload, bindings, record: Object.values(bindings).some((binding) => binding.source === "subject") && master ? `${parentType}/${master.id}` : undefined, expectedRevision: 0 });
              setRound((r) => r + 1);
            } else if (await decide(`${type}.create`, { type, id }, payload, { expectedRevision: 0 })) setRound((r) => r + 1);
          } catch (failure) { setError(failure instanceof Error ? failure.message : t("The related record could not be created.")); }
        }} />
    </fieldset>
  </div>;
}

/** The selected record as its page reads it: history, tasks waiting on it. */
function useRecordView(type: string, id?: string, readSource?: RecordSource) {
  const host = useHost(), source = readSource ?? host.source;
  const key=JSON.stringify([host.source.scope,source.scope,source.revision,type,id]),[result,setResult]=useState<{key:string;view?:RecordView;error?:string}>();
  useEffect(() => {
    let current = true;
    setResult(undefined);
    if(id&&source.scope===host.source.scope)void source.get(type,id).then(value=>{if(value.record.id!==id)throw Error("Record identity mismatch");if(current)setResult({key,view:value});}).catch(error=>{if(current)setResult({key,error:error instanceof Error?error.message:String(error)});});
    return () => { current = false; };
  }, [key]);
  return result?.key===key?result:undefined;
}

/** The timeline (16b): the selected record's history from the journal. */
function TimelineWidget({ page, section, selected, readSource,session,confirmedRecord,recordStatus }: Bound) {
  const { source } = useHost();
  const type = objectOf(page, section);
  const info = source.entity(type);
  const original=section.historyLimit?confirmedRecord:selected,result=useRecordView(type,original?.id,readSource??session?.readSource());
  if(section.historyLimit&&recordStatus==="error")return <Panel role="alert">{t("The original record history could not be read.")}</Panel>;
  if(section.historyLimit&&recordStatus==="pending")return <p role="status">{t("Confirming record access…")}</p>;
  if (!original) return <p role="status" className="text-sm text-muted">{t("Select a record to see what happened to it.")}</p>;
  if(result?.error)return <Panel role="alert">{t("The original record history could not be read.")}</Panel>;
  if (!result?.view || !info) return <p role="status" className="text-sm text-muted">{t("Loading…")}</p>;
  return <RecordHistory info={info} history={result.view.history} heading={false} limit={section.historyLimit||undefined} total={result.view.history.length} recordID={original.id} recordRevision={result.view.record.revision}/>;
}

/** The tasks (16b): what waits on the selected record for this member — approvals
 *  and flow steps from the work app — answered where they are. */
function TasksWidget({ page, section, selected, live, readSource }: Bound) {
  const { can, decide } = useHost();
  const type = objectOf(page, section);
  const result = useRecordView(type, selected?.id, readSource),view=result?.view;
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see what waits on it.")}</p>;
  if(result?.error)return <Panel role="alert">{t("The original record work could not be read.")}</Panel>;
  if (!view) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  if (view.tasks.length === 0) return <p className="text-sm text-muted">{t("Nothing waits on it.")}</p>;
  const answer = live && can("work.task.complete")
    ? { answer: async (task: RecordView["tasks"][number], a?: string) => { await decide("work.task.complete", { type: "work.task", id: task.id }, a ? { answer: a } : {}); } }
    : undefined;
  return <Tasks list={view.tasks} tasks={answer} />;
}

/** The builder's published function is called by its ordinary action. The
 * saved call record remains the only answer and permission surface. */
function FunctionWidget({ page, section, selected, live }: Bound) {
  const { source, can, decide } = useHost();
  const [callID, setCallID] = useState("");
  const [reload, setReload] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [measured, setMeasured] = useState<EntityRecord>();
  const name = section.function?.ref.name ?? "";
  const version = Number(section.function?.sourceVersion.match(/\.function-(\d+)$/)?.[1] ?? 0);
  useEffect(() => { setCallID(""); setError(""); }, [selected?.id, name, version]);
  useEffect(() => {
    if (!live || !callID) { setMeasured(undefined); return; }
    let current = true;
    source.get("build.function-call", callID).then((view) => { if (current) setMeasured(view.record); }, () => { if (current) setMeasured(undefined); });
    return () => { current = false; };
  }, [source, live, callID, reload]);
  if (!name || !version) return <p role="alert" className="text-sm text-danger">{t("Choose a published function for this page.")}</p>;
  return <div className="grid gap-3">
    <p className="text-xs text-muted">{name} · {t("Version")} {version}</p>
    {!selected ? <p className="text-sm text-muted">{t("Select a record to request advice.")}</p> : <>
      <div className="flex flex-wrap gap-2">
        <Button disabled={!live || busy || !can("build.function-call.start")} onClick={async () => {
          setBusy(true); setError("");
          const id = newId("CALL");
          try {
            if (await decide("build.function-call.start", { type: "build.function-call", id },
              { name, version, source: selected.id }, { quiet: true, onRefused: setError })) { setCallID(id); setReload((n) => n + 1); }
          } finally { setBusy(false); }
        }}>{busy ? t("Requesting advice…") : t("Request advice")}</Button>
        <Button disabled={!live} onClick={() => setReload((n) => n + 1)}>{t("Refresh advice")}</Button>
      </div>
      {!live && <p className="text-xs text-muted">{t("Advice calls do not run while you compose.")}</p>}
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      {live && <RecordList key={reload} source={source} type="build.function-call" fields={["function", "version", "state", "source"]}
        domain={[["source", "=", `${page.object.name}/${selected.id}`], ["function", "=", name], ["version", "=", version]]}
        onOpen={(record) => setCallID(record.id)} />}
      {live && callID && measured?.metered === true && <Card className="grid gap-2 p-3">
        <h3 className="text-sm font-semibold">{t("Measured model call")}</h3>
        <PropertyList items={[
          [t("Input tokens"), measured.tokensReported ? String(measured.inputTokens ?? 0) : t("Not reported")],
          [t("Output tokens"), measured.tokensReported ? String(measured.outputTokens ?? 0) : t("Not reported")],
          [t("Model latency"), `${measured.latencyMillis ?? 0} ms`],
          [t("Reported USD cost"), measured.costReported ? `$${Number(measured.costUsd ?? 0).toFixed(6)}` : t("Not reported")],
          [t("Requested model"), String(measured.model ?? t("Not reported"))],
          [t("Served model"), measured.servedModel ? String(measured.servedModel) : t("Not reported")],
        ]} />
      </Card>}
      {live && callID && measured?.state !== "pending" && measured?.metered === false &&
        <p className="text-xs text-muted">{t("No model call was measured for this result.")}</p>}
      {live && callID && <RecordPage source={source} type="build.function-call" id={callID} fields={["state", "output", "code", "reason"]} reload={reload} />}
    </>}
  </div>;
}

/** One section: its title, and the widget it holds. While a page is being
 *  composed, clicking it takes it in hand. */
function CollaborationWidget({page,section,facetValues,onFacet,sceneWindow,collaborationRecord,collaborationReference,collaborationStatus,collaborationSlot,session,commentDraft,fileValue,pdfPageValue,onCommentDraft,onFileID,onPdfPage,enabled,live}:Bound) {
 const host=useHost(),source=session?.readSource()??host.source,sampleSlot=recordResourceSlot(page,section.sceneSampleVariable),sampleRecord=sampleSlot?session?.confirmedSelected(sampleSlot):undefined,sampleReference=sampleSlot?session?.snapshot().records[sampleSlot]:undefined,sampleInfo=sampleReference?.status==="value"?source.entity(sampleReference.value.object):undefined;
 return <RecordCollaborationRenderer sceneWindow={sceneWindow} sceneSampleObject={page.document?.queries?.[page.document?.variables?.[section.sceneSampleCollectionVariable??""]?.source?.query??""]?.object.name} scene={section.scene} sample={sampleRecord&&sampleInfo?{record:sampleRecord,info:sampleInfo}:undefined} part={facetValues?.[section.scenePartVariable??""]} onPart={value=>{if(section.scenePartVariable)onFacet?.(section.scenePartVariable,value);}} kind={section.widget} label={section.title||t("Record collaboration")} record={collaborationRecord} reference={collaborationReference} status={collaborationStatus} slot={collaborationSlot} bindingEpoch={collaborationSlot?session?.recordBindingEpoch(collaborationSlot):undefined} session={session} readSource={session?.readSource()} draft={commentDraft} fileValue={fileValue} pageValue={pdfPageValue} onDraft={onCommentDraft} onFileID={onFileID} onPage={onPdfPage} enabled={enabled} live={live}/>;
}
function ExplorerWidget({page,section,session,explorationRoot,explorationStatus,explorationIdentity,explorationActive,onGraphOutput,graphSelected,enabled,contextReadCurrent}:Bound){
 const {source,definitions}=useHost();return <ExplorationRenderer kind={section.widget as "graph-explorer"|"vertex-graph"} config={section.graphExplorer} vertex={section.vertexGraph} root={explorationRoot} status={explorationStatus} rootObject={section.object?.name?section.object:page.object} source={session?.readSource()??source} definitions={definitions} identity={explorationIdentity??""} isActive={explorationActive??(()=>false)} onOutput={onGraphOutput} selected={graphSelected} enabled={enabled} label={section.title||t("Record exploration")} readCurrent={contextReadCurrent}/>;
}
const widgets = createWidgetRegistry<Bound>({
 "record-map":({page,section,window,selected,enabled,onSelect,pickerConfirmation})=>{const {source}=useHost();return <RecordMapRenderer window={window} info={source.entity(objectOf(page,section))} config={section.map} scope={source.scope??""} confirmation={pickerConfirmation} selected={selected} enabled={enabled} onSelect={onSelect}/>;},
 "image-annotation":CollaborationWidget,
 "scene-3d":CollaborationWidget,
 "collection-builder":CollectionBuilderRenderer,
 "ai-assistant":bound=><AIWidget section={bound.section} record={bound.explorationRoot} status={bound.explorationStatus} identity={bound.explorationIdentity??""} active={bound.explorationActive??(()=>false)} live={bound.live} enabled={bound.enabled} question={bound.facetValues?.[bound.section.ai?.questionVariable??""]} onQuestion={value=>{const id=bound.section.ai?.questionVariable;if(id)bound.onFacet?.(id,value);}}/>,
 "external-frame":({section,contextReadCurrent})=><ExternalDocumentRenderer section={section} readCurrent={contextReadCurrent}/>,
 "embedded-page":bound=><EmbeddedPageRenderer section={bound.section} values={bound.embeddingInputs??bound.facetValues??{}} scope={bound.aggregateScope??""} live={bound.live} readCurrent={bound.contextReadCurrent} enabled={bound.enabled} onReturn={bound.embeddingReturn}/>,
 "action-table":({page,section,window,session,aggregateScope,enabled,live,contextReadCurrent})=><ActionTableRenderer section={section} object={objectOf(page,section)} window={window} session={session} scope={aggregateScope??""} enabled={enabled} live={live} readCurrent={contextReadCurrent}/>,
 notepad:({section,notepadValue,onNotepad,enabled,contextReadCurrent})=><NotepadRenderer value={notepadValue} onChange={onNotepad} enabled={enabled} label={section.title||t("Session notepad")} readCurrent={contextReadCurrent}/>,
 observation:bound=><ObservationRenderer page={bound.page} section={bound.section} window={bound.window} history={bound.observationHistory} context={bound.observationContext} asset={bound.observationAsset} selectedRow={bound.observationSelected} onRow={bound.onObservationRow} values={bound.facetValues??{}} onState={bound.onFacet} session={bound.session} scope={bound.aggregateScope??""} enabled={bound.enabled} readCurrent={bound.contextReadCurrent} live={bound.live}/>,
 "collection-analysis":({page,section,window,aggregateScope,facetValues,onFacet,enabled})=>{const {source}=useHost();return <CollectionAnalysisRenderer config={section.analysis} object={objectOf(page,section)} info={source.entity(objectOf(page,section))} window={window} source={source.aggregate?{aggregate:source.aggregate,scope:aggregateScope??source.scope,revision:source.revision}:undefined} label={section.title||t("Collection analysis")} values={facetValues??{}} xVariable={section.analysisXVariable} yVariable={section.analysisYVariable} countVariable={section.analysisCountVariable} meanVariable={section.analysisMeanVariable} onAxis={onFacet} enabled={enabled}/>;},
 "resource-list":({page,section,window,collection,selected,onSelect,enabled,contextReadCurrent})=>{const {source}=useHost();return <ResourceListRenderer config={section.resourceList} window={window} collection={collection} info={source.entity(objectOf(page,section))} selected={selected} onSelect={onSelect} enabled={enabled} label={section.title||t("Resource list")} readCurrent={contextReadCurrent}/>;},
 "asset-directory":({section,onControl,controlBound,enabled})=>{const {definitions}=useHost(),pages=section.assetDirectory?.items.filter(i=>i.asset.ref.kind==="page")??[],canOpen=pages.some(i=>controlBound?.(i.id));return <AssetDirectoryRenderer config={section.assetDirectory} definitions={definitions} onOpen={canOpen?id=>{if(controlBound?.(id))onControl?.(id);}:undefined} canOpen={controlBound} enabled={enabled} label={section.title||t("Asset directory")}/>;},
 "graph-explorer":ExplorerWidget,"vertex-graph":ExplorerWidget,
 breadcrumb:({page,section,confirmedRecord,recordStatus,onControl,onClearContext,enabled,contextReadCurrent})=>{const {source}=useHost();return <BreadcrumbRenderer readCurrent={contextReadCurrent} config={section.breadcrumb} record={confirmedRecord} status={recordStatus} info={source.entity(objectOf(page,section))} onHome={()=>onControl?.("home")} onClear={onClearContext} enabled={enabled} label={section.title||t("Breadcrumbs")}/>;},
 "avatar-stack":({page,section,window,collection,avatarContextStatus,contextReadCurrent})=>{const {source}=useHost();return <AvatarStackRenderer readCurrent={contextReadCurrent} config={section.avatar} contextStatus={avatarContextStatus} window={window} collection={collection} info={source.entity(objectOf(page,section))} label={section.title||t("Personnel avatars")}/>;},
 "static-image":({section})=><StaticImageRenderer config={section.image} label={section.title||t("Image")}/>,
 "approval-inbox":({section,session,enabled,live})=><WorkViewsRenderer kind="approval-inbox" label={section.title||t("Approval inbox")} readSource={session?.readSource()} enabled={enabled} live={live}/>,
 "notification-feed":({section,session,enabled,live})=><WorkViewsRenderer kind="notification-feed" label={section.title||t("Notifications")} readSource={session?.readSource()} enabled={enabled} live={live}/>,
 "record-comments":CollaborationWidget,"record-uploader":CollaborationWidget,"media-preview":CollaborationWidget,"pdf-viewer":CollaborationWidget,
 histogram:({page,section,window,aggregateScope})=>{const {source}=useHost();return <HistogramRenderer object={objectOf(page,section)} window={window} fields={section.histogram} label={section.title||t("Histogram")} info={source.entity(objectOf(page,section))} source={source.aggregate?{aggregate:source.aggregate,scope:aggregateScope??source.scope,revision:source.revision}:undefined}/>;},
 "sparkline-kpi":({page,section,window,sparklineValue})=>{const {source}=useHost();return <SparklineRenderer value={sparklineValue} window={window} info={source.entity(objectOf(page,section))} fields={section.sparkline} title={section.title||t("Sparkline KPI")}/>;},
 "tag-counts":({page,section,window,aggregateScope,groupValue,enabled,onGroupFilter})=>{const {source}=useHost(),selected=groupValue?.status==="value"?typeof groupValue.value==="string"?[groupValue.value]:isStringSet(groupValue.value)?groupValue.value.values:[]:[];return <TermCountsRenderer tags selected={selected} enabled={enabled} onSelect={onGroupFilter} object={objectOf(page,section)} window={window} field={section.group??""} label={section.title||t("Tag counts")} info={source.entity(objectOf(page,section))} source={source.aggregate?{aggregate:source.aggregate,scope:aggregateScope??source.scope,revision:source.revision}:undefined}/>;},
 "treemap":({page,section,window,aggregateScope,groupValue,enabled,onGroupFilter})=>{const {source}=useHost(),selected=groupValue?.status==="value"?typeof groupValue.value==="string"?[groupValue.value]:isStringSet(groupValue.value)?groupValue.value.values:[]:[];return <TermCountsRenderer treemap selected={selected} enabled={enabled} onSelect={onGroupFilter} object={objectOf(page,section)} window={window} field={section.group??""} label={section.title||t("Treemap")} info={source.entity(objectOf(page,section))} source={source.aggregate?{aggregate:source.aggregate,scope:aggregateScope??source.scope,revision:source.revision}:undefined}/>;},
 "term-counts":({page,section,window,aggregateScope})=>{const {source}=useHost();return <TermCountsRenderer object={objectOf(page,section)} window={window} field={section.group??""} label={section.title||t("Term counts")} info={source.entity(objectOf(page,section))} source={source.aggregate?{aggregate:source.aggregate,scope:aggregateScope??source.scope,revision:source.revision}:undefined}/>;},
 "record-picker":({page,section,window,selected,enabled,onSelect,pickerValue,onPickerID,pickerConfirmation})=><RecordPickerRenderer type={objectOf(page,section)} window={window} fields={section.recordPicker} title={section.title||t("Record picker")} selected={selected} enabled={enabled} confirmation={section.pickerValueVariable?pickerConfirmation:undefined} value={section.pickerValueVariable?pickerValue:undefined} onSelect={onPickerID??onSelect}/>,
 spacer:({section})=><SpacerRenderer config={section.spacer}/>,
 separator:({section})=><SeparatorRenderer config={section.separator} name={section.title||t("Separator")}/>,
 notice:({section})=><NoticeRenderer config={section.notice} label={section.title||t("Notice")}/>,
 "alert-banner":({section,alertValue})=><AlertRenderer value={alertValue} config={section.alertBanner} title={section.title||t("Alert banner")}/>,
 "date-input":({section,dateValue,onDate,enabled})=><DateInputRenderer value={dateValue} kind={section.dateKind} offset={section.dateOffset} label={section.dateLabel} title={section.title||t("Date input")} enabled={enabled} onChange={onDate}/>,
 "choice-input":({section,choiceValue,choiceSetValue,onChoice,onChoiceSet,enabled})=><ChoiceInputRenderer setValue={choiceSetValue} onSet={onChoiceSet} value={choiceValue} fields={section.choiceInput} title={section.title||t("Choice input")} enabled={enabled} onChange={onChoice}/>,
 "boolean-input":({section,booleanInput,onBoolean,enabled})=><BooleanInputRenderer value={booleanInput} label={section.booleanLabel} variant={section.booleanVariant} title={section.title||t("Boolean switch")} enabled={enabled} onChange={onBoolean}/>,
 "range-input":({section,rangeLower,rangeUpper,onRange})=><RangeRenderer lower={rangeLower} upper={rangeUpper} fields={section.rangeInput} title={section.title||t("Range input")} onChange={onRange}/>,
 "record-leaderboard":({page,section,window,selected,onSelect})=>{const {source}=useHost();return <LeaderboardRenderer window={window} info={source.entity(objectOf(page,section))} fields={section.leaderboard} selected={selected} onSelect={onSelect}/>;},
 "summary-stats":({page,section,statisticsValue})=>{const {source}=useHost();return <SummaryRenderer value={statisticsValue} info={source.entity(objectOf(page,section))} field={section.summaryField}/>;},
 gauge:({section,gaugeValue})=><GaugeRenderer value={gaugeValue} fields={section.gauge} title={section.title||t("Gauge")}/>,
 progress:({section,progressValue,progressTotal})=><ProgressRenderer value={progressValue} total={progressTotal} fixedTotal={section.progressTotalVariable?undefined:section.progressTotal} title={section.progressLabel??section.title??""}/>,
 "record-gantt":({page,section,window})=>{const {source}=useHost();return <RecordGanttRenderer window={window} info={source.entity(objectOf(page,section))} fields={section.recordGantt}/>;},
 "record-calendar":({page,section,window,selected,onSelect})=>{const {source}=useHost(),object=objectOf(page,section);return <RecordCalendarRenderer key={JSON.stringify([source.scope,object,section.recordCalendar,window?.query])} window={window} info={source.entity(object)} fields={section.recordCalendar} selected={selected} onSelect={onSelect}/>;},
 "record-events":({page,section,window})=>{const {source}=useHost();return <RecordEventsRenderer window={window} info={source.entity(objectOf(page,section))} fields={section.recordEvents}/>;},
 "record-scatter":({page,section,window,selected,enabled,onSelect,pickerConfirmation})=>{const {source}=useHost();return <RecordScatterRenderer window={window} info={source.entity(objectOf(page,section))} fields={section.scatter} confirmation={pickerConfirmation} selected={selected} enabled={enabled} onSelect={onSelect}/>;},
 "record-chart":RecordChartAdapter,
 "record-list":(bound)=>bound.section.recordList?.layout==="tiles"?<RecordTilesRenderer window={bound.window} object={objectOf(bound.page,bound.section)} labelField={bound.section.cardLabel??"id"} selected={bound.selected?.id} onSelect={bound.enabled===false||!bound.section.selection?undefined:record=>bound.onSelect(record)} label={bound.section.title||t("Record tiles")} readCurrent={bound.contextReadCurrent}/>:<TableAdapter {...bound}/>,
 heading:({section})=><PageHeader compact level={Number(section.headingLevel?.slice(1)??2) as 1|2|3} title={section.text||t("Heading")}/>,
 "collection-title":({section,countValue,countError})=><CollectionTitle title={section.title||t("Collection title")} value={countValue} error={countError}/>,
 "button-group":({section,onControl,controlBound,enabled})=><ButtonGroup buttons={section.buttons??[]} label={section.title||t("Button group")} onActivate={id=>onControl?.(id)} isBound={controlBound??(()=>false)} enabled={enabled}/>,
 "status-tracker":StatusTrackerWidget,
 "record-links":RecordLinksWidget,
 "record-comparison":({page,section,comparisonRecords,comparisonStatus})=>{const {source}=useHost();return <RecordComparisonRenderer records={comparisonRecords??[]} status={comparisonStatus} info={source.entity(objectOf(page,section))} fields={section.fields??[]} config={section.recordComparison}/>;},
 "record-card":({page,section,confirmedRecord,recordStatus})=>{const {source}=useHost();return <RecordCardRenderer record={confirmedRecord} status={recordStatus} info={source.entity(objectOf(page,section))} fields={section.fields??[]} config={section.recordCard}/>;},
 "record-view":RecordViewWidget,
  kanban:KanbanAdapter,
  "record-timeline":RecordTimelineAdapter,
  input: ({ section, value, onValue, enabled,numeric,valueError,inputScopes }) => <div className="grid gap-1">{section.inputKind==="search"?<SearchInput value={value??""} onChange={onValue??(()=>{})} disabled={!onValue||enabled===false} aria-label={section.title||t("Search records")} scope={inputScopes??[]}/>:<Input inputMode={numeric?"decimal":undefined} maxLength={numeric?pageVariableContract.decimal.maxBytes:undefined} aria-invalid={!!valueError} aria-label={section.title || t("Text input")} value={value ?? ""} disabled={!onValue || enabled === false} onChange={(event) => onValue?.(event.target.value)} />}{valueError&&<p role="alert" className="text-xs text-danger">{t(valueError)}</p>}</div>,
  button: ({ section, onClick, enabled }) => <ButtonRenderer title={section.title} onClick={onClick} enabled={enabled}/>,
  "inline-action":({page,section,selected,live,aggregateScope,actionReady})=><InlineActionForm type={objectOf(page,section)} schema={section.actions?.[0]?.name??""} record={selected} live={live} scope={aggregateScope} defaults={section.actionDefaults} ready={actionReady}/>,
  table: TableAdapter, detail: DetailWidget, actions: ActionsWidget,
  heatmap:(props)=><ChartWidget {...props} kpi={false} pivot/>,
  pivot: (bound) => <ChartWidget {...bound} kpi={false} pivot/>,
  chart: (bound) => <ChartWidget {...bound} kpi={false} />,
  metric: (bound) => <ChartWidget {...bound} kpi />,
  text: ({ section }) => <Markdown content={section.text} className="text-sm" />,
  filter: FilterWidget,
  form: (bound) => <FormWidget key={`${objectOf(bound.page, bound.section)}/${bound.section.relation ?? ""}/${bound.section.parentSelection ?? ""}/${bound.section.relation ? bound.master?.id ?? "" : ""}`} {...bound} />,
  timeline: TimelineWidget, tasks: TasksWidget, function: FunctionWidget,
  compute: (bound) => <ComputeCall binding={bound.section.operation} bindings={bound.section.inputs} record={bound.selected} recordType={bound.page.object.name} live={bound.live} />,
});

class WidgetBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <Panel role="alert">{t("This widget could not be displayed.")}</Panel> : this.props.children; }
}

export function SectionView(bound: Bound & Composing) {
  const { section, chosen, onChoose, at, nested } = bound;
  const implementation = widgets.resolveDefinition(section.widget, section.configVersion ?? (bound.page.document ? 0 : 1));
  const Renderer=implementation?.Renderer;
  const body = Renderer ? <Suspense fallback={<p role="status">{t("Loading…")}</p>}><Renderer {...bound}/></Suspense> : <p role="alert" className="text-sm text-danger">{t("This widget is unavailable.")}</p>;
  const template=section.tablePresentation?.titleTemplate,total=bound.window?.error||!["value","empty"].includes(bound.collection?.status??"")?undefined:bound.window?.page?.total;
  const title=template===undefined?section.title:template.replace("{count}",total===undefined||!Number.isSafeInteger(total)||total<0?"…":total.toLocaleString());
  const inHand = onChoose !== undefined && chosen === at;
  if (implementation?.contract.layoutPreferences.frame === "inline") return <div onClick={onChoose && at !== undefined ? () => onChoose(at) : undefined} className={cn("min-w-0", inHand && "outline outline-2 outline-primary rounded")}><WidgetBoundary key={`${section.id ?? at}/${section.widget}/${section.configVersion}/${JSON.stringify(section)}`}>{body}</WidgetBoundary></div>;
  return (
    <Card onClick={onChoose && at !== undefined ? () => onChoose(at) : undefined}
      className={cn("grid min-w-0 grid-cols-1 content-start gap-2 p-3", nested ? "w-full" : section.width === "half" ? "md:col-span-1" : "md:col-span-2",
        onChoose && "cursor-pointer", inHand && "outline outline-2 outline-primary")}>
      {title && <h3 className="text-sm font-semibold">{title}</h3>}
      <WidgetBoundary key={`${section.id ?? at}/${section.widget}/${section.configVersion}/${JSON.stringify(section)}`}>{body}</WidgetBoundary>
    </Card>
  );
}

/**
 * A composed page as people use it: the sections in order, sharing what is
 * selected for each typed slot. Related lists follow their declared parent;
 * detail/actions read their own object's selection. `live` false is the builder's canvas — the same widgets over the
 * same records, with nothing that writes.
 */
type ComposedPageProps = {
  page: Page; live?: boolean; notice?: ReactNode; definitionKey?: string;pageCall?:import("./runtime/PageNavigation").PageCall;
  editingRoot?: string;
  onVariableValues?: (values: Record<string, VariableResult>) => void;
} & Composing;
export function ComposedPage(props: ComposedPageProps) {
  const { me } = useHost();const application=useApplicationContext();
  return <PageEmbeddingBoundary page={props.page}><PageSession key={JSON.stringify([me, props.definitionKey, props.page, application?.identity, application?.session.readScope])} {...props} /></PageEmbeddingBoundary>;
}

function PageSession({ page, live = true, notice, chosen, onChoose, wrapLayout, onVariableValues, editingRoot,pageCall }: ComposedPageProps) {
  const { source } = useHost();
  const openRecord=useOpenRecord();
  const viewVisible = useViewVisible();
  const pageFocus = useRef<HTMLDivElement>(null), callers = useRef<Record<string, HTMLElement | null>>({});
  const initialVariables = useMemo(() => {
    const values = { ...page.document?.variables };
    for (const node of Object.values(page.document?.nodes ?? {})) {
      const id = node.activeVariable, variable = id ? values[id] : undefined;
      if (id && node.kind === "tabs" && node.children?.length && variable?.mode === "state" && variable.type === "string" && !node.children.includes(String(variable.initial))) {
        values[id] = { ...variable, initial: node.children[0] };
      }
    }
    return values;
  }, [page.document]);
  const masterType = page.object.name;
  const slots = new Map([
    [selectionKey(masterType), masterType],
    ...(page.sections ?? []).map((section) => [selectionSlot(page,section), objectOf(page, section)] as const),
    ...(page.sections??[]).flatMap(section=>(section.graphExplorer?.outputs??[]).map(output=>[recordOutputSlot(page,section,output.id),output.object.name] as const)),
    ...(page.sections??[]).flatMap(section=>section.observation?.kind==="table"?["row","asset"].flatMap(port=>{const object=recordOutputObject(page,section,port);return object?[[recordOutputSlot(page,section,port),object.name] as const]:[]}):[]),
    ...(page.sections??[]).filter(s=>s.selectionSetVariable).map(s=>[selectionSetSlot(page,s),objectOf(page,s)] as const),
    ...Object.values(page.document?.interface?.inputs ?? {}).filter((port) => port.type === "record" && port.object).map((port) => [inputSlot(port.variable), port.object!.name] as const),
    ...Object.entries(initialVariables).filter(([,v])=>v.mode==="shared"&&["record","record-set"].includes(v.type)&&v.source?.object).map(([id,v])=>[inputSlot(id),v.source!.object!.name] as const),
    ...(page.selections ?? []).map((variable) => [selectionKey(variable.object.name, variable.name), variable.object.name] as const),
  ]);
  const children = new Map<string, Set<string>>();
  const queryParents = new Map<string, string>();
  for (const section of page.sections ?? []) {
    const type = objectOf(page, section);
    if (section.collectionVariable || section.widget !== "table" || type === masterType && !section.parentSelection || !(section.relation || section.parentSelection || relatedField(source.entity(type)?.fields, page, section))) continue;
    const parent = selectionSlot(page,section,true), child = selectionSlot(page,section);
    if (!children.has(parent)) children.set(parent, new Set());
    children.get(parent)!.add(child);
    queryParents.set(section.id ?? `section:${page.sections!.indexOf(section)}`, parent);
  }
  const applicationVariable=(id:string)=>{const v=initialVariables[id];if(v?.mode==="shared"&&v.type==="object-set")return id;if(v?.source?.kind==="query"){const section=page.sections?.find((s)=>s.id===v.source?.section);const bound=initialVariables[section?.collectionVariable??""];if(bound?.mode==="shared"&&bound.type==="object-set")return section?.collectionVariable;}return undefined;};
  const querySelections = new Map<string,Set<string>>();
  for(const section of page.sections??[]){if(!["table","record-timeline","kanban","record-list","resource-list","record-calendar","record-picker","record-leaderboard","record-scatter","record-map"].includes(section.widget))continue;const variable=page.document?.variables?.[section.collectionVariable??""];if(variable?.source?.kind==="plan"){const key=planKey(variable.source.query??"");if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSlot(page,section));if(section.selectionSetVariable)querySelections.get(key)!.add(selectionSetSlot(page,section));}}
  for(const section of page.sections??[]){if(!["table","record-timeline","kanban","record-list","resource-list","record-calendar","record-picker","record-leaderboard","record-scatter","record-map"].includes(section.widget))continue;const id=section.collectionVariable;if(id&&applicationVariable(id)){const key=`application/${id}`;if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSlot(page,section));if(section.selectionSetVariable)querySelections.get(key)!.add(selectionSetSlot(page,section));}}
  for(const section of page.sections??[]){if(section.filterVariable&&section.widget==="table"){const key=section.id??`section:${page.sections!.indexOf(section)}`;if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSlot(page,section));if(section.selectionSetVariable)querySelections.get(key)!.add(selectionSetSlot(page,section));}}
  for(const section of page.sections??[]){if(section.widget!=="table"||!section.selectionSetVariable||section.collectionVariable)continue;const key=section.id??"";if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSetSlot(page,section));}
  for(const [id,q] of Object.entries(page.document?.queries??{})){const v=initialVariables[q.for?.variable??""],producer=page.sections?.find(s=>s.id===v?.source?.section);if(!["query","link-type"].includes(q.query?.ref.kind??"")||v?.source?.kind!=="record"||!producer)continue;const parent=recordResourceSlot(page,q.for?.variable);if(!parent)continue;queryParents.set(planKey(id),parent);for(const child of querySelections.get(planKey(id))??[]){if(!children.has(parent))children.set(parent,new Set());(children.get(parent) as Set<string>).add(child);}}
  for(const graph of page.sections??[]){if(graph.widget!=="graph-explorer")continue;const parent=recordResourceSlot(page,graph.recordVariable);if(!parent)continue;if(!children.has(parent))children.set(parent,new Set());for(const output of graph.graphExplorer?.outputs??[])children.get(parent)!.add(recordOutputSlot(page,graph,output.id));}
  for(const section of page.sections??[]){const c=section.observation;if(c?.kind!=="table")continue;const v=initialVariables[section.collectionVariable??""],key=planKey(v?.source?.query??"");if(!querySelections.has(key))querySelections.set(key,new Set());for(const port of ["row","asset"])if(recordOutputObject(page,section,port))querySelections.get(key)!.add(recordOutputSlot(page,section,port));if(c.rowOutput&&c.assetOutput){const row=recordOutputSlot(page,section,"row");if(!children.has(row))children.set(row,new Set());children.get(row)!.add(recordOutputSlot(page,section,"asset"));}}
  const recordScalars=new Map<string,Map<string,ScalarValue>>();
  for(const section of page.sections??[]){if(!["record-comments","record-uploader","media-preview","pdf-viewer","image-annotation","scene-3d","ai-assistant"].includes(section.widget))continue;const slot=section.widget==="ai-assistant"?aiRecordSlot(page,section.recordVariable):recordResourceSlot(page,section.recordVariable);if(!slot)continue;if(!recordScalars.has(slot))recordScalars.set(slot,new Map());for(const id of [section.commentDraftVariable,section.fileVariable,section.pdfPageVariable,section.ai?.questionVariable,section.scenePartVariable]){if(id&&initialVariables[id]?.mode==="state")recordScalars.get(slot)!.set(id,id===section.pdfPageVariable?String(initialVariables[id]!.initial??"1"):"");}}
  const { session, snapshot } = usePageSession(source, {recordScalars, maxSelectionSetRecords:pageVariableContract.recordSelection.maxRecords,objects: slots, children, queryParents, querySelections, ...(Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=13?filterSessionBindings(page):{}), overlayScopes:overlaySessionScopes(page) });
  const resourceKey = JSON.stringify([page.object, page.document?.variables, page.sections]);
  const resources = useMemo(() => resourceVariables(page, snapshot), [resourceKey, snapshot]);
  const incoming = usePageInputs(page, session, snapshot,pageCall);
  const application = useApplicationVariables(initialVariables);
  const computations=usePageComputations(page,session,snapshot,application,live);
  const [recordProducers]=useState(()=>new Map<string,symbol>());
  const recordProducer=(section:string)=>{if(!recordProducers.has(section))recordProducers.set(section,Symbol(section));return recordProducers.get(section)!;};
  const applicationRecords=JSON.stringify(Object.entries(initialVariables).filter(([,v])=>v.mode==="shared"&&v.type==="record").map(([id])=>[id,application.resources[id],application.recordReferences[id]]));
  useEffect(()=>{for(const [id,v] of Object.entries(initialVariables)){if(v.mode!=="shared"||v.type!=="record")continue;const ref=application.recordReferences[id],current=recordReadReference(session.snapshot().records[inputSlot(id)]);if(current?.object!==ref?.object||current?.id!==ref?.id)session.selectReference(inputSlot(id),ref);}},[session,applicationRecords]);
  const applicationSets=JSON.stringify(application.recordSetReferences);
  useEffect(()=>{for(const [id,v] of Object.entries(initialVariables)){if(v.mode!=="shared"||v.type!=="record-set")continue;const refs=application.recordSetReferences[id]??[],slot=inputSlot(id),current=session.selectionSetReferences(slot);if(JSON.stringify(current)!==JSON.stringify(refs))void session.selectSetReferences(slot,refs);}},[session,applicationSets]);
  const previousSets=useRef<Record<string,boolean>>({});
  useEffect(()=>{for(const section of page.sections??[]){const variable=section.selectionSetVariable;if(!variable||initialVariables[variable]?.mode!=="shared")continue;const slot=selectionSetSlot(page,section),state=snapshot.recordSets[slot];if(previousSets.current[slot]&&(state?.status==="empty"||state?.status==="error"))application.selectSet(variable,undefined,recordProducer(section.id??""),true);if(state?.status==="pending"||state?.status==="value")previousSets.current[slot]=true;else delete previousSets.current[slot];}},[snapshot.recordSets]);
  const previousSelections=useRef<Record<string,string>>({});
  useEffect(()=>{for(const section of page.sections??[]){if(!section.selectionVariable)continue;const slot=selectionSlot(page,section),state=snapshot.records[slot],id=state?.status==="value"?state.value.id:undefined;if(previousSelections.current[slot]&&state?.status==="empty"&&!id)application.select(section.selectionVariable,undefined,recordProducer(section.id??""),true);if(id)previousSelections.current[slot]=id;else if(state?.status!=="pending")delete previousSelections.current[slot];}},[snapshot.records]);

  const previousObservationAssets=useRef<Record<string,boolean>>({});
  useEffect(()=>{for(const section of page.sections??[]){const output=section.observation?.assetOutput;if(section.observation?.kind!=="table"||!output||initialVariables[output]?.mode!=="shared")continue;const slot=recordOutputSlot(page,section,"asset"),status=snapshot.records[slot]?.status;if(previousObservationAssets.current[slot]&&(status==="empty"||status==="error"))application.select(output,undefined,recordProducer(section.id??""),true);if(status==="value"||status==="pending")previousObservationAssets.current[slot]=true;else delete previousObservationAssets.current[slot];}},[snapshot.records]);

  useEffect(()=>{for(const [id,signature] of Object.entries(application.signatures)){const result=application.resources[id],window=result&&(result.status==="value"||result.status==="empty")&&typeof result.value==="object"&&result.value?.kind==="object-set"?result.value.window:undefined;session.reconcileExternalWindow(`application/${id}`,result?.status==="error"?"":signature,window?.records.map((r)=>r.id));}},[session,JSON.stringify(application.signatures),JSON.stringify(application.resources)]);
  const aliases=Object.fromEntries(Object.entries(initialVariables).flatMap(([id,v])=>{const sharedFilter=v.source?.kind==="filter"?page.sections?.find((s)=>s.id===v.source?.section)?.filterVariable:undefined;const source=sharedFilter??applicationVariable(id);return source&&source!==id?[[id,application.resources[source]??{status:"empty" as const}]]:[]}));
  const inputResources = useMemo(() => ({ ...resources, ...incoming.inputs, ...application.resources,...aliases,...computations }), [resources, JSON.stringify(incoming.inputs), JSON.stringify(application.resources),JSON.stringify(aliases),JSON.stringify(computations)]);
  const inputValues = useMemo(() => evaluateVariables(initialVariables, snapshot.scalars, pageVariableContract, inputResources,undefined,undefined,session.property), [initialVariables,snapshot.scalars,inputResources]);
  const overlayForRoot = (root: string) => Object.entries(page.document?.overlays ?? {}).find(([, overlay]) => overlay.root === root)?.[0];
  const overlayInputs=Object.fromEntries(Object.keys(page.document?.overlays??{}).map((id)=>[id,evaluateVariables(initialVariables,snapshot.scalars,pageVariableContract,inputResources,undefined,id,session.property)]));
  const queries = usePageQueries(page,inputValues,session,snapshot,overlayInputs,editingRoot?overlayForRoot(editingRoot):undefined);
  const allResources = useMemo(() => ({ ...inputResources,...queries.resources }), [inputResources,JSON.stringify(queries.resources)]);
  const variables = usePageVariables(initialVariables, snapshot.scalars, session, allResources);
  const overlayValues = useMemo(() => Object.fromEntries(Object.keys(page.document?.overlays ?? {}).map((id) => [id, evaluateVariables(initialVariables, snapshot.scalars, pageVariableContract, allResources, undefined, id,session.property)])), [initialVariables, snapshot.scalars, allResources]);

  const collectionInputs={...queries.collectionInputs,...application.collectionInputs,...Object.fromEntries(Object.keys(initialVariables).flatMap(id=>{const shared=applicationVariable(id);return shared?[[id,application.collectionInputs[shared]??{status:"empty" as const}]]:[]}))};
  const navigation = usePageNavigation(page, live, {...variables.values,...collectionInputs}, variables.set,pageCall);
  useEffect(() => { onVariableValues?.({...variables.values,...Object.fromEntries(Object.entries(initialVariables).filter(([,v])=>v.scope==="overlay").map(([id,v])=>[id,overlayValues[v.owner??""]?.[id]??{status:"empty" as const}]))}); }, [onVariableValues, variables.values, overlayValues]);
  const selectionQuery=(section:Section)=>{if(!(Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=15))return undefined;const v=initialVariables[section.collectionVariable??""];return v?.source?.kind==="plan"?planKey(v.source.query??""):v?.mode==="shared"?`application/${section.collectionVariable}`:section.id;};
  const onSelect = (key: string, record?: EntityRecord, query?:string) => session.select(key, record,query);
  const indexed = new Map((page.sections ?? []).map((section, i) => [section.id, { section, i }]));
  const writeState = (id: string, value: ScalarValue, owner?: string) => {
    const variable = initialVariables[id];
    if (variable?.mode === "shared") { application.set(id,value); return; }
    if (variable?.mode !== "state" || !(variable.scope === "page" || variable.scope === "overlay" && variable.owner === owner) || !scalarAssignable(variable.type,value,pageVariableContract.maxStringBytes,pageVariableContract.decimal.maxBytes)) return;
    const entries = Object.entries(page.document?.overlays ?? {}), target = entries.find(([, overlay]) => overlay.openVariable === id);
    const changes: Record<string, ScalarValue> = { [id]: value }, reset: string[] = [];
    if (target) {
      if (value === true) callers.current[id] = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      for (const [owner, overlay] of entries) {
        const closing = owner === target[0] ? value === false : value === true;
        if (value === true) changes[overlay.openVariable] = owner === target[0];
        if (closing) {
          session.endOverlay(owner);
          reset.push(...Object.entries(initialVariables).filter(([, v]) => v.scope === "overlay" && v.owner === owner && v.mode === "state").map(([key]) => key));
        }
      }
    }
    session.setScalars(changes, reset);
  };
  const setContextState = (id: string, value: ScalarValue, context?: LoopContext, overlay?: string) => initialVariables[id]?.scope === "loop-item" && context ? context.set(id, value) : writeState(id, value, overlay);
  const booleanValue = (id: string) => { const result = variables.values[id]; return result?.status === "value" && result.value === true; };
  const renderSection = (section: Section, i: number, nested: boolean, enabled = true, context?: LoopContext, overlay?: string, valueVariable?: string) => {
    const values = context?.values ?? (overlay ? overlayValues[overlay] : variables.values) ?? {}, value = values[valueVariable ?? ""];
    const shared=section.filterVariable?application.resources[section.filterVariable]:undefined;
    if(section.filterVariable&&(!shared||shared.status==="error"||shared.status==="pending"||!("value" in shared)||typeof shared.value!=="object"||shared.value.kind!=="filter"))return <Panel role="alert">{t("Shared filter is unavailable.")}</Panel>;
    const sharedFilter=shared&&"value" in shared&&typeof shared.value==="object"&&shared.value.kind==="filter"?shared.value.fields:undefined;
    const epoch = overlay ? session.overlayEpoch(overlay) : undefined;
    const binding=(name:string,control?:string)=>page.document?.events?.find(event=>event.source===section.id&&event.event===name&&(event.control??"")===(control??""));
    const emit=(name:string,control?:string)=>{
      const event=binding(name,control);if(!event||overlay&&session.overlayEpoch(overlay)!==epoch)return;
      if(event.navigate||event.return){navigation.emit(event,{values,set:(id,value)=>setContextState(id,value,context,overlay),isActive:()=> (!overlay||session.overlayEpoch(overlay)===epoch)&&(!context||context.session.hasLoopItem(context.owner,context.key)&&context.session.querySignature(context.queryKey)===context.signature)});return;}
      if(typeof event.value==="string"||typeof event.value==="boolean"||isDecimal(event.value)||isStringSet(event.value))setContextState(event.target,event.value,context,overlay);
    };
    const avatarContext=section.widget==="avatar-stack"&&section.avatar?.contextVariable?confirmedContext(page,section,section.avatar.contextVariable,session,snapshot):undefined;
    const collectionID=section.widget==="avatar-stack"?avatarCollectionVariable(section.avatar,avatarContext?.status,section.collectionVariable):section.collectionVariable;
    const explorationSlot=section.widget==="ai-assistant"?aiRecordSlot(page,section.recordVariable):recordResourceSlot(page,section.recordVariable),explorationRoot=explorationSlot?session.confirmedSelected(explorationSlot):undefined,rootLease=explorationSlot?session.recordBindingEpoch(explorationSlot):undefined;
    const sharedAI=section.widget==="ai-assistant"&&initialVariables[section.recordVariable??""]?.mode==="shared";
    const sharedContext=(["graph-explorer","vertex-graph","breadcrumb","record-card","scene-3d"].includes(section.widget)||section.widget==="observation"&&section.observation?.kind==="statistics")&&initialVariables[section.recordVariable??""]?.mode==="shared";
    const sharedContextStatus=sharedContext?application.resources[section.recordVariable??""]?.status:undefined;
    const contextReference=(sharedAI||sharedContext)?application.recordReferences[section.recordVariable??""]:recordReadReference(explorationSlot?snapshot.records[explorationSlot]:undefined);
    const explorationActive=()=>{
      if(!currentContextRead(source.scope,session.snapshotScope())||!explorationSlot||session.recordBindingEpoch(explorationSlot)!==rootLease||overlay&&session.overlayEpoch(overlay)!==epoch)return false;
      if(sharedContext&&(sharedContextStatus!=="value"||application.recordReferences[section.recordVariable??""]?.id!==explorationRoot?.id||application.recordReferences[section.recordVariable??""]?.object!==(section.observation?.kind==="statistics"?section.observation.assetObject?.name:objectOf(page,section))))return false;
      if(section.widget!=="ai-assistant")return session.confirmedSelected(explorationSlot)?.id===explorationRoot?.id;
      const current=recordReadReference(session.snapshot().records[explorationSlot]);
      return !!current&&!!contextReference&&current.object===contextReference.object&&current.id===contextReference.id;
    };

    const observationOutput=section.observation?.kind==='table'?section.observation.assetOutput:undefined,observationShared=!!observationOutput&&initialVariables[observationOutput]?.mode==='shared',observationRecord=observationOutput?session.confirmedSelected(inputSlot(observationOutput)):undefined,observationReference=observationOutput?application.recordReferences[observationOutput]:undefined;
    const observationReady=observationShared&&currentContextRead(source.scope,session.snapshotScope())&&application.resources[observationOutput!]?.status==='value'&&!!observationRecord&&observationRecord.id===observationReference?.id&&observationReference.object===section.observation?.assetObject?.name;

    const mapVariable=section.widget==="record-map"?section.selectionVariable:undefined,mapShared=!!mapVariable&&initialVariables[mapVariable]?.mode==="shared",mapRecord=mapVariable?session.confirmedSelected(inputSlot(mapVariable)):undefined,mapReference=mapVariable?application.recordReferences[mapVariable]:undefined;
    const mapReady=mapShared&&currentContextRead(source.scope,session.snapshotScope())&&application.resources[mapVariable!]?.status==="value"&&!!mapRecord&&mapRecord.id===mapReference?.id&&mapReference.object===objectOf(page,section);
    const actionVariable=initialVariables[section.recordVariable??""],actionSlot=actionVariable?.mode==="shared"?inputSlot(section.recordVariable??""):recordResourceSlot(page,section.recordVariable)??(section.widget==="inline-action"&&!section.recordVariable?selectionSlot(page,section):undefined),actionRef=actionSlot?recordReadReference(snapshot.records[actionSlot]):undefined;
    const applicationAction=actionVariable?.mode==="shared"?application.recordReferences[section.recordVariable??""]:actionRef;
    const actionRecord=actionSlot&&actionRef?.object===applicationAction?.object&&actionRef?.id===applicationAction?.id?session.selected(actionSlot):undefined;
    const actionReady=(!actionVariable||actionVariable.mode!=="shared"||application.resources[section.recordVariable??""]?.status==="value")&&(!actionSlot||snapshot.records[actionSlot]?.status==="value")&&enabled!==false;
    return (
      <SectionView builder={queries.builders[section.id??""]} key={section.id || i} page={page} section={section} session={session} readSource={context?.source} actionReady={actionReady} selected={mapShared?(mapReady?mapRecord:undefined):section.widget==="inline-action"&&actionSlot? actionRecord:section.selectionVariable?session.selected(inputSlot(section.selectionVariable)):section.recordVariable ? context ? context.record : initialVariables[section.recordVariable]?.mode==="resource"&&initialVariables[section.recordVariable]?.source?.kind==="record" ? (()=>{const slot=recordResourceSlot(page,section.recordVariable);return slot?session.selected(slot):undefined})() : snapshot.records[inputSlot(section.recordVariable)]?.status === "value" ? session.selected(inputSlot(section.recordVariable)) : undefined : session.selected(selectionSlot(page,section))}
        keepActive={!!binding("select")} master={session.selected(selectionSlot(page,section,true))} onSelect={(record) => {if((section.widget==="record-scatter"||section.widget==="record-map")||section.recordList?.layout==="tiles"){if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;const query=selectionQuery(section);if(!record){session.select(selectionSlot(page,section),undefined);if(section.selectionVariable)application.select(section.selectionVariable,undefined,recordProducer(section.id??""),true);return;}if(query){const complete=section.selectionVariable?application.beginSelect(section.selectionVariable,recordProducer(section.id??"")):undefined;void session.confirmSelection(selectionSlot(page,section),record,query).then(confirmed=>{if(!currentContextRead(source.scope,session.snapshotScope())||overlay&&session.overlayEpoch(overlay)!==epoch||confirmed&&session.confirmedSelected(selectionSlot(page,section))!==confirmed){complete?.(undefined);return;}complete?.(confirmed?{object:objectOf(page,section),id:confirmed.id}:undefined);});}return;}onSelect(selectionSlot(page,section),record,selectionQuery(section));if(section.selectionVariable)application.select(section.selectionVariable,record?{object:section.object?.name||page.object.name,id:record.id}:undefined,recordProducer(section.id??""));if(record)emit("select");}} live={live} narrowed={section.widget==="filter"&&section.filterVariable?{[objectOf(page,section)]:sharedFilter??{}}:filtersForOwner(snapshot.filters,filterOwner(page,section))} sharedFilter={sharedFilter} onNarrow={(object,field,value)=>section.filterVariable&&section.widget==="filter"?application.filter(section.filterVariable,field,value):session.filter(object,field,value,filterOwner(page,section))}
        chosen={chosen} onChoose={onChoose} at={i} nested={nested} enabled={enabled}
        window={collectionID?applicationVariable(collectionID)?application.windows[applicationVariable(collectionID)!]:queries.windows[initialVariables[collectionID]?.source?.query??""]:undefined}
        onRecordOpen={(type,record)=>{
          if(!live||enabled===false||!currentContextRead(source.scope,session.snapshotScope())||overlay&&session.overlayEpoch(overlay)!==epoch)return;
          // Retire the modal owner before opening the workspace's original record window.
          if(overlay){const frame=page.document?.overlays?.[overlay];if(!frame)return;writeState(frame.openVariable,false);}
          openRecord({type,id:record.id});
        }}
        selectionSet={section.selectionSetVariable?{selectedIDs:(initialVariables[section.selectionSetVariable]?.mode==="shared"&&snapshot.recordSets[selectionSetSlot(page,section)]?.status!=="pending"?application.recordSetReferences[section.selectionSetVariable]??[]:session.selectionSetReferences(selectionSetSlot(page,section))).map(r=>r.id),records:session.selectedSet(selectionSetSlot(page,section)),status:snapshot.recordSets[selectionSetSlot(page,section)]?.status??"empty",maxRecords:pageVariableContract.recordSelection.maxRecords,onChange:ids=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;const slot=selectionSetSlot(page,section),pending=session.selectSet(slot,ids,selectionQuery(section)??section.id??"");if(!pending||initialVariables[section.selectionSetVariable??""]?.mode!=="shared")return;const complete=application.beginSetSelect(section.selectionSetVariable!,recordProducer(section.id??""));void pending.then(refs=>{const read=session.snapshot().recordSets[slot];if(!currentContextRead(source.scope,session.snapshotScope())||overlay&&session.overlayEpoch(overlay)!==epoch||refs?.length&&(read?.status!=="value"||read.value!==refs)){complete?.(undefined);return;}complete?.(refs);});}}:undefined}
        notepadValue={values[section.notepadVariable??""]} onNotepad={section.notepadVariable?value=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;writeState(section.notepadVariable!,value,overlay);}:undefined}
        facetValues={values} onFacet={(id,value)=>setContextState(id,value,context,overlay)}
        explorationRoot={explorationRoot} explorationStatus={(sharedAI||sharedContext)&&application.resources[section.recordVariable??""]?.status!=="value"?application.resources[section.recordVariable??""]?.status??"empty":explorationSlot?snapshot.records[explorationSlot]?.status??"empty":undefined} explorationIdentity={JSON.stringify([explorationSlot,rootLease,overlay,epoch,sharedAI?application.identity:sharedContext?[application.identity,application.readScope,contextReference,sharedContextStatus]:undefined])} explorationActive={explorationActive}
        graphSelected={section.graphExplorer?.outputs?.flatMap(output=>{const slot=recordOutputSlot(page,section,output.id),record=session.confirmedSelected(slot);return record?[{object:output.object.name,id:record.id}]:[];})[0]}
        onGraphOutput={section.widget==="graph-explorer"?async(object,record)=>{if(context||enabled===false||!explorationActive())return false;const output=section.graphExplorer?.outputs?.find(output=>output.object.name===object&&output.object.app===source.entity(object)?.app);const variable=output?initialVariables[output.variable]:undefined,rootVariable=initialVariables[section.recordVariable??""];const localScope=overlay?"overlay":"page",localOwner=overlay;const outputScope=rootVariable?.mode==="shared"?localScope:rootVariable?.scope,outputOwner=rootVariable?.mode==="shared"?localOwner:rootVariable?.owner;if(!output||!variable||variable.type!=="record"||variable.mode!=="resource"||variable.scope!==outputScope||variable.owner!==outputOwner||variable.source?.kind!=="record"||variable.source.section!==section.id||variable.source.port!==output.id)return false;for(const other of section.graphExplorer?.outputs??[])session.select(recordOutputSlot(page,section,other.id),undefined);const slot=recordOutputSlot(page,section,output.id);const confirmed=await session.selectReference(slot,{object:output.object.name,id:record.id});return !!confirmed&&explorationActive()&&session.confirmedSelected(slot)?.id===record.id;}:undefined}
        embeddingInputs={{...values,...collectionInputs}} embeddingReturn={result=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;for(const [port,id] of Object.entries(section.embedding?.results??{})){const value=result[port];if(typeof value==="string"||typeof value==="boolean"||isDecimal(value))writeState(id,value,overlay);}}} observationHistory={queries.windows[variablePlan(page,section.observationHistoryVariable??"")??""]} observationContext={queries.windows[variablePlan(page,section.observationContextVariable??"")??""]} observationAsset={observationShared?(observationReady?observationRecord:undefined):section.widget==="observation"&&(!sharedContext||explorationActive())?explorationRoot:undefined} observationSelected={section.observation?.rowOutput?session.confirmedSelected(recordOutputSlot(page,section,"row"))?.id:undefined} onObservationRow={section.observation?.kind==="table"?async record=>{const c=section.observation!,query=selectionQuery(section),signature=query?session.querySignature(query):undefined;const active=()=>!context&&enabled!==false&&currentContextRead(source.scope,session.snapshotScope())&&(!overlay||session.overlayEpoch(overlay)===epoch)&&(!query||session.querySignature(query)===signature);if(!query||!active())return;await confirmObservationRow(page,section,record,session,query,active,initialVariables[c.assetOutput??'']?.mode==='shared'?()=>application.beginSelect(c.assetOutput!,recordProducer(section.id??'')):undefined);}:undefined} collection={values[collectionID??""]} avatarContextStatus={avatarContext?.status} contextReadCurrent={currentContextRead(source.scope,session.snapshotScope())} onClearContext={section.widget==="breadcrumb"&&!!section.recordVariable?()=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;if(initialVariables[section.recordVariable!]?.mode==="shared"){application.select(section.recordVariable!,undefined,recordProducer(section.id??""));return;}const slot=originalContextSlot(page,section,section.recordVariable);if(slot)session.select(slot,undefined);}:undefined} aggregateScope={JSON.stringify([source.scope,applicationVariable(section.collectionVariable??"")?[application.identity,application.readScope]:undefined,overlay,epoch,context?[context.owner,context.key,context.signature]:undefined])}
        numeric={initialVariables[valueVariable??""]?.type==="decimal"||!!valueVariable&&Object.values(page.document?.queries??{}).some(q=>q.conditions?.some(c=>c.asDecimal&&c.value.variable===valueVariable))} valueError={value?.status==="error"?value.code:undefined} value={value?.status==="error"?value.draft:value?.status==="value"?value.draft??(isDecimal(value.value)?value.value.value:typeof value.value==="string"?value.value:undefined):undefined} onValue={valueVariable ? (value) => setContextState(valueVariable,initialVariables[valueVariable]?.type==="decimal"?{kind:"decimal",value}:value, context, overlay) : undefined}
        inputScopes={searchInputObjects(page.document,valueVariable).flatMap(object=>{const info=source.entity(object);return info?[info.plural||info.title]:[];})} pickerConfirmation={mapShared?(mapReady?"value":application.resources[mapVariable!]?.status??"empty"):snapshot.records[selectionSlot(page,section)]?.status} pickerValue={values[section.pickerValueVariable??""]} onPickerID={section.pickerValueVariable?(record)=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;const variable=section.pickerValueVariable!,scalarState=session.snapshot().scalars,query=selectionQuery(section);if(!record){session.select(selectionSlot(page,section),undefined);writeState(variable,"",overlay);return;}if(!query)return;void session.confirmSelection(selectionSlot(page,section),record,query).then(confirmed=>{if(!confirmed||overlay&&session.overlayEpoch(overlay)!==epoch||session.snapshot().scalars!==scalarState||session.selected(selectionSlot(page,section))?.id!==confirmed.id)return;writeState(variable,confirmed.id,overlay);});}:undefined}
        alertValue={values[section.alertValueVariable??""]}
        dateValue={values[section.dateVariable??""]} onDate={section.widget==="date-input"&&section.dateVariable?(value)=>{if(context||enabled===false||typeof value!=="string"||overlay&&session.overlayEpoch(overlay)!==epoch)return;writeState(section.dateVariable!,value,overlay);}:undefined}
        choiceSetValue={values[section.choiceSetVariable??""]} onChoiceSet={section.widget==="choice-input"&&section.choiceInput?.variant==="multiple"&&section.choiceSetVariable?(next)=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;const current=values[section.choiceSetVariable!];if(current?.status!=="value"||!isStringSet(current.value)||!isStringSet({kind:"string-set",values:next}))return;const previous=current.value.values,delta=[...next.filter(item=>!previous.includes(item)),...previous.filter(item=>!next.includes(item))];if(!(section.choiceInput?.clearable&&next.length===0&&previous.length>0)&&(delta.length!==1||!section.choiceInput?.options.includes(delta[0]!)))return;writeState(section.choiceSetVariable!,{kind:"string-set",values:next},overlay);}:undefined}
        choiceValue={values[section.choiceVariable??""]} onChoice={section.widget==="choice-input"&&section.choiceVariable&&initialVariables[section.choiceVariable]?.mode==="state"?(value)=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch||typeof value!=="string"||!(section.choiceInput?.options.includes(value)||value===""&&section.choiceInput?.variant==="select"))return;writeState(section.choiceVariable!,value,overlay);}:undefined}
        booleanInput={values[section.booleanVariable??""]} onBoolean={section.widget==="boolean-input"&&section.booleanVariable?(checked)=>{if(context||enabled===false||typeof checked!=="boolean"||overlay&&session.overlayEpoch(overlay)!==epoch)return;writeState(section.booleanVariable!,checked,overlay);}:undefined}
        rangeLower={values[section.rangeMinVariable??""]} rangeUpper={values[section.rangeMaxVariable??""]}
        sceneWindow={section.sceneSampleCollectionVariable?queries.windows[initialVariables[section.sceneSampleCollectionVariable]?.source?.query??""]:undefined}
        collaborationSlot={(()=>{if(!["record-comments","record-uploader","media-preview","pdf-viewer","image-annotation","scene-3d"].includes(section.widget))return;return recordResourceSlot(page,section.recordVariable);})()}
        collaborationRecord={(()=>{if(!["record-comments","record-uploader","media-preview","pdf-viewer","image-annotation","scene-3d"].includes(section.widget))return;const slot=recordResourceSlot(page,section.recordVariable);return slot&&(!sharedContext||explorationActive())?session.confirmedSelected(slot):undefined;})()}
        collaborationReference={(()=>{if(!["record-comments","record-uploader","media-preview","pdf-viewer","image-annotation","scene-3d"].includes(section.widget))return;const slot=recordResourceSlot(page,section.recordVariable),state=slot?snapshot.records[slot]:undefined;return state&&(!sharedContext||explorationActive())&&(state.status==="value"||state.status==="pending")?state.value:undefined;})()}
        collaborationStatus={(()=>{if(!["record-comments","record-uploader","media-preview","pdf-viewer","image-annotation","scene-3d"].includes(section.widget))return;const slot=recordResourceSlot(page,section.recordVariable);if(sharedContext&&sharedContextStatus!=="value")return sharedContextStatus??"empty";return slot?snapshot.records[slot]?.status:undefined;})()}
        commentDraft={values[section.commentDraftVariable??""]} fileValue={values[section.fileVariable??""]} pdfPageValue={values[section.pdfPageVariable??""]}
        onCommentDraft={section.commentDraftVariable?value=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;writeState(section.commentDraftVariable!,value,overlay);}:undefined}
        onFileID={section.fileVariable&&initialVariables[section.fileVariable]?.mode==="state"?value=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;writeState(section.fileVariable!,value,overlay);}:undefined}
        onPdfPage={section.pdfPageVariable?value=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;writeState(section.pdfPageVariable!,value,overlay);}:undefined}
        comparisonRecords={section.widget==="record-comparison"?(()=>{const v=initialVariables[section.recordSetVariable??""],producer=page.sections?.find(s=>s.id===v?.source?.section);const shared=v?.mode==="shared"&&v.type==="record-set";if(!producer&&!shared)return [];const slot=shared?inputSlot(section.recordSetVariable!):selectionSetSlot(page,producer!);return (!shared||application.resources[section.recordSetVariable!]?.status==="value")&&snapshot.recordSets[slot]?.status==="value"?session.selectedSet(slot):[];})():undefined} comparisonStatus={section.widget==="record-comparison"?(()=>{const v=initialVariables[section.recordSetVariable??""],producer=page.sections?.find(s=>s.id===v?.source?.section);if(v?.mode==="shared"&&v.type==="record-set"){const status=application.resources[section.recordSetVariable!]?.status;return status!=="value"?status:snapshot.recordSets[inputSlot(section.recordSetVariable!)]?.status;}return producer?snapshot.recordSets[selectionSetSlot(page,producer)]?.status:undefined;})():undefined}
        confirmedRecord={(section.widget==="breadcrumb"&&!!section.recordVariable)||section.widget==="record-card"||section.widget==="timeline"&&!!section.historyLimit?(()=>{const slot=recordResourceSlot(page,section.recordVariable);return slot&&(!sharedContext||explorationActive())?session.confirmedSelected(slot):undefined;})():undefined} recordStatus={(section.widget==="breadcrumb"&&!!section.recordVariable)||section.widget==="record-card"||section.widget==="timeline"&&!!section.historyLimit?(()=>{const slot=recordResourceSlot(page,section.recordVariable);return sharedContext&&sharedContextStatus!=="value"?sharedContextStatus??"empty":slot?snapshot.records[slot]?.status:undefined;})():undefined}
        sparklineValue={values[section.sparklineDecimalVariable??section.sparklineNumberVariable??""]}
        groupValue={values[section.groupValueVariable??section.groupSetVariable??""]} onGroupFilter={["treemap","tag-counts"].includes(section.widget)&&(section.groupValueVariable||section.groupSetVariable)?value=>{if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;const id=section.groupValueVariable??section.groupSetVariable!,v=initialVariables[id],next=v?.type==="string-set"?{kind:"string-set" as const,values:value===undefined?[]:[value]}:value??"";if(!v||v.mode!=="state"||!(v.scope==="page"||v.scope==="overlay"&&v.owner===overlay)||!scalarAssignable(v.type,next,pageVariableContract.maxStringBytes,pageVariableContract.decimal.maxBytes))return;writeState(id,next,overlay);}:undefined}
        onHeatmap={section.widget==="heatmap"&&(section.rowValueVariable||section.rowSetVariable||section.columnValueVariable||section.columnSetVariable)?(row,column)=>{
          if(context||enabled===false||overlay&&session.overlayEpoch(overlay)!==epoch)return;
          const entries:[[string|undefined,string|undefined],[string|undefined,string|undefined]]=[[section.rowValueVariable??section.rowSetVariable,row],[section.columnValueVariable??section.columnSetVariable,column]],changes:Record<string,ScalarValue>={};
          for(const [id,value] of entries){if(!id)continue;const v=initialVariables[id];if(!v||v.mode!=="state"||!(v.scope==="page"||v.scope==="overlay"&&v.owner===overlay))return;const next=v.type==="string-set"?{kind:"string-set" as const,values:value===undefined?[]:[value]}:value??"";if(!scalarAssignable(v.type,next,pageVariableContract.maxStringBytes,pageVariableContract.decimal.maxBytes))return;changes[id]=next;}
          session.setScalars(changes);
        }:undefined}
        onRange={section.widget==="range-input"?(lower,upper)=>{
          if(context || overlay&&session.overlayEpoch(overlay)!==epoch)return;
          const min=section.rangeMinVariable,max=section.rangeMaxVariable;if(!min||!max||min===max)return;
          const a=initialVariables[min],b=initialVariables[max];
          if(!a||!b||a.type!=="string"||b.type!=="string"||a.mode!=="state"||b.mode!=="state"||a.scope!==b.scope||a.owner!==b.owner||!(a.scope==="page"||a.scope==="overlay"&&a.owner===overlay)||!scalarAssignable("string",lower,pageVariableContract.maxStringBytes)||!scalarAssignable("string",upper,pageVariableContract.maxStringBytes))return;
          session.setScalars({[min]:lower,[max]:upper});
        }:undefined}
        statisticsValue={values[section.statisticsVariable??""]}
        gaugeValue={values[section.gaugeValueVariable??""]}
        progressValue={values[section.progressValueVariable??""]} progressTotal={values[section.progressTotalVariable??""]}
        countValue={(()=>{const value=values[section.countVariable??""];return value?.status==="value"&&isDecimal(value.value)?value.value.value:undefined;})()} countError={(()=>{const value=values[section.countVariable??""];return value?.status==="error"?t(value.code):undefined;})()}
        onControl={id=>emit("click",id)} controlBound={id=>!!binding("click",id)} onClick={binding("click")?()=>emit("click"):undefined} />
    );
  };
  const placed=(id:string,seen=new Set<string>()):boolean=>{if(seen.has(id))return false;seen.add(id);const n=page.document?.nodes[id];return !!n&&(n.kind==="widget"?indexed.has(n.section??""):(n.children??[]).some(child=>placed(child,seen)));};
  const renderNode = (id: string, ancestors: Set<string>, context?: LoopContext, overlay?: string, parentKind?:string, parentHeight=false): ReactNode => {
    if (!page.document || ancestors.has(id)) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const node = page.document.nodes[id];
    if (!node) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    if(!wrapLayout&&!placed(id))return null;
    const values = context?.values ?? (overlay ? overlayValues[overlay] : variables.values) ?? {};
    if (node.visibleWhen) {
      const visible = values[node.visibleWhen];
      if (!visible || visible.status === "error") return <Panel role="alert">{t("This page variable could not be evaluated.")} {node.visibleWhen}</Panel>;
      if (visible.status === "pending") return <Panel role="status">{t("Loading page variable…")}</Panel>;
      if (visible.value !== true) return wrapLayout ? wrapLayout(id, node, <Panel>{t("Hidden by page variable")}: {node.visibleWhen}</Panel>) : null;
    }
    const bounded=node.size?.height!==undefined||parentHeight&&(parentKind==="columns"||parentKind==="rows"&&node.size?.weight!==undefined);
    const frame=(body:ReactNode)=><LayoutRegion key={id} size={node.size} parent={parentKind} fillHeight={parentKind==="columns"&&parentHeight} fillWidth={(parentKind==="flow"||parentKind==="toolbar")&&node.kind!=="widget"}>{wrapLayout?wrapLayout(id,node,present(body)):present(body)}</LayoutRegion>;
    const present=(body:ReactNode)=>node.presentation?<RegionPresentation key={`${id}/${overlay?session.overlayEpoch(overlay):"page"}`} title={node.title} presentation={node.presentation}>{body}</RegionPresentation>:body;
    if (node.kind === "widget") {
      const item = indexed.get(node.section);
      if (!item) return null; // server filtered this widget for the reader
      if (context && !([...pageVariableContract.loop.recordWidgets, ...pageVariableContract.loop.presentationWidgets] as readonly string[]).includes(item.section.widget)) return <Panel role="alert">{t("This widget is not supported in a loop.")}</Panel>;
      const body = renderSection(item.section, item.i, true, !node.enabledWhen || (() => { const result = values[node.enabledWhen]; return result?.status === "value" && result.value === true; })(), context, overlay, node.valueVariable);
      return frame(body);
    }
    if (!["rows", "columns", "tabs", "flow", "toolbar", "loop"].includes(node.kind)) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const next = new Set(ancestors); next.add(id);
    if (node.kind === "loop") {
      if(context)return frame(<NestedLoopRuntime page={page} node={node} owner={id} parent={context} overlay={overlay}>{item=><>{node.children?.map(child=><div key={child} className="min-w-0">{renderNode(child,next,item,overlay)}</div>)}</>}</NestedLoopRuntime>);
      if (!node.loop) return <Panel role="alert">{t("Choose a loop query window.")}</Panel>;
      const collection = initialVariables[node.loop.collection]?.source, queryID=variablePlan(page,node.loop.collection);
      const shared=applicationVariable(node.loop.collection);const queryKey = shared?`application/${shared}`:queryID!==undefined?planKey(queryID):collection?.section??"";
      const body = <LoopRuntime page={page} queryKey={queryKey} expectedSignature={shared?application.signatures[shared]??"":queryID!==undefined ? queries.signatures[queryID]??"" : undefined} owner={id} loop={node.loop} label={node.title || t("Repeated records")} result={values[node.loop.collection]} session={session} snapshot={snapshot} variables={initialVariables} resources={allResources} overlay={overlay}>
        {(item) => <>{node.children?.map((child) => <div key={child} className="min-w-0">{renderNode(child, next, item, overlay)}</div>)}</>}
      </LoopRuntime>;
      return frame(body);
    }
    if (node.kind === "tabs") {
      const active = values[node.activeVariable ?? ""];
      if (!active || active.status !== "value" || typeof active.value !== "string") return <Panel role="alert">{t("This page variable could not be evaluated.")} {node.activeVariable}</Panel>;
      const body = <ContentTabs label={node.title || t("Page tabs")} value={active.value} onChange={(value) => setContextState(node.activeVariable!, value, context, overlay)}
        items={(node.children ?? []).map((child, index) => ({ id: child,
          title: page.document!.nodes[child]?.title || indexed.get(page.document!.nodes[child]?.section)?.section.title || t("Tab {n}", { n: index + 1 }), content: renderNode(child, next, context, overlay) }))} />;
      return frame(body);
    }
    if (node.kind === "flow" || node.kind === "toolbar") {
      const body = <FlowLayout toolbar={node.kind === "toolbar"} label={node.title || t("Toolbar")} align={node.align}>{node.children?.map(child=>renderNode(child,next,context,overlay,node.kind))}</FlowLayout>;
      return frame(body);
    }
    const body = <LayoutStack direction={node.kind as "rows"|"columns"} gap={node.gap}>
      {node.children?.map(child=>renderNode(child,next,context,overlay,node.kind,bounded))}
    </LayoutStack>;
    return frame(body);
  };
  const queryErrors=(owner?:string)=>Object.entries(page.document?.queries??{}).filter(([,plan])=>(plan.owner??"")===(owner??"")).map(([id,plan])=>{
    const result=Object.entries(initialVariables).find(([,v])=>(v.source?.kind==="plan"||v.mode==="aggregate")&&v.source?.query===id),status=result&&queries.resources[result[0]];
    return status?.status==="error"?<Panel key={id} role="alert" className="flex flex-wrap items-center gap-2">{plan.title||id}: {t(status.code)}<Button onClick={()=>queries.retry(id)}>{t("Retry query")}</Button></Panel>:null;
  });
  return (
    <div ref={pageFocus} tabIndex={-1} className="@container/page grid min-w-0 grid-cols-1 gap-3 outline-none">
      {notice}
      {queryErrors(editingRoot ? overlayForRoot(editingRoot) : undefined)}
      {Object.entries(application.resources).filter(([id,result])=>result.status==="error"&&["object-set","decimal"].includes(initialVariables[id]?.type??"")&&!application.error).map(([id,result])=><Panel key={id} role="alert" className="flex gap-2">{result.status==="error"?t(result.code):""}<Button onClick={()=>application.retry(id)}>{t("Retry query")}</Button></Panel>)}
      {Object.entries(application.resources).filter(([id,result])=>result.status==="error"&&initialVariables[id]?.type==="record"&&!application.error).map(([id,result])=><Panel key={id} role="alert">{result.status==="error"?t(result.code):""}</Panel>)}
      {application.error && <Panel role="alert">{t(application.error)}</Panel>}
      {(incoming.error || navigation.error) && <Panel role="alert">{t(incoming.error ?? navigation.error!)}</Panel>}
      {incoming.error && !onChoose ? null : page.document ? page.document.formatVersion !== 2 || !supportsPageUIProfile(page.document.uiProfile)
        ? <Panel role="alert">{t("This page needs a newer workspace version. Refresh after updating the workspace.")}</Panel>
        : renderNode(editingRoot ?? page.document.root, new Set(), undefined, editingRoot ? overlayForRoot(editingRoot) : undefined) : <div className="grid gap-3 md:grid-cols-2">
        {(page.sections ?? []).map((section, i) => (
          renderSection(section, i, false)
        ))}
      </div>}
      {!editingRoot && page.document && supportsPageUIProfile(page.document.uiProfile) && Object.entries(page.document.overlays ?? {}).map(([id, overlay]) => {
        const open = booleanValue(overlay.openVariable);
        const Frame = overlay.kind === "drawer" ? Sheet : Dialog;
        const sections: string[] = [];
        const collect = (id: string, seen = new Set<string>()) => {
          if (seen.has(id)) return; seen.add(id);
          const node = page.document!.nodes[id]; if (!node) return;
          if (node.section) sections.push(node.section);
          node.children?.forEach((child) => collect(child, seen));
        };
        collect(overlay.root);
        const presentation=overlay.presentation,width=presentation?.size==="custom"?presentation.customWidth:presentation?({small:320,medium:overlay.kind==="drawer"?420:560,large:overlay.kind==="drawer"?560:880} as Record<string,number>)[presentation.size]:undefined;
        return <Frame width={width} side={presentation?.side as "left"|"right"|undefined} backdrop={presentation?.backdrop} closeOnBackdrop={presentation?.closeOnBackdrop} closeOnEsc={presentation?.closeOnEsc} key={id} open={open} suspended={!viewVisible} title={overlay.title} onOpenChange={(open) => writeState(overlay.openVariable, open)} returnFocus={callers.current[overlay.openVariable]} fallbackFocus={pageFocus.current}>
          {open && <OverlayBody session={session} sectionIDs={sections} owner={id}>{queryErrors(id)}{renderNode(overlay.root, new Set(), undefined, id)}</OverlayBody>}
        </Frame>;
      })}
      {(page.sections ?? []).length === 0 && <Panel role="status" className="text-sm text-muted">{t("Nothing is on this page yet.")}</Panel>}
    </div>
  );
}

function OverlayBody({ children, session, sectionIDs, owner }: { children: ReactNode; session: PageSessionStore; sectionIDs: string[]; owner:string }) {
  const key = JSON.stringify(sectionIDs);
  useEffect(() => () => session.endOverlay(owner), [session, key, owner]);
  return <div className="@container grid min-w-0 grid-cols-1 gap-3">{children}</div>;
}

/** Whether a page is composed of sections (ADR-0035) rather than the list-detail shorthand. */
export const isComposed = (page: Page) => page.layout === "composed";
