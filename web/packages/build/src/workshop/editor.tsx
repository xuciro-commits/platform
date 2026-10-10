import { useApplicationWorkspace } from "../projects/application-scope";
import {WidgetGlyph,LayoutGlyph} from "./page-editor/WidgetGlyph";
import {canvasModel,canvasDrop,canvasMove,canvasResize,canvasResetSize,canvasGroup,canvasEqualize,canvasUngroup,canvasRemove} from "./page-editor/canvas-layout";
import {addWidgetSlot} from "./page-editor/widget-slots";
import {queryInventoryBudget} from "@platform/app/query-inventory";
import {validActionDefaults} from "@platform/app/action-defaults";
import {validExternalFrame} from "@platform/ui/external-frame";
import {observationProblem} from "./page-editor/observation";
import {recordWorkProblem} from "./page-editor/record-work";
import {collectionAnalysisProblem} from "./page-editor/collection-analysis";
import {explorationViewProblem} from "./page-editor/exploration-views";
import {contextViewProblem} from "./page-editor/context";
import {collaborationRecordSource,recordInputOwner,historyViewProblem,isCollaborationWidget,requiresOriginalRecord} from "./page-editor/collaboration";
import {recordComparisonSource} from "./page-editor/record-comparison";
import {searchInputObjects} from "@platform/app/search";
import {interfaceQueryForSection,pageLayoutDiagnostics} from "@platform/app";
import { recordPaths } from "../ontology/record-paths";
// Application Studio page design (ADR-0046). Document history, UI selection
// and authorized runtime data have separate owners. Preview and operation use
// the same registered widgets; save and activation use the original Go path.
import { ApplicationPage, ComposedPage, SemanticObjectSelect, pageDocumentFromSections, parsePageDecimal, pageUIProfile, pageVariableContract, pageVariableDiagnostics, widgetContract, widgetContracts, useHost, useReadQuery, useRecordInventory, type PageVariableValue, type Definition } from "@platform/app";
import {
  validTimestampOffset, validChoiceInput, rangeGrid, gaugeModel, progressRatio, ganttRange, ActionMenu, Button, Card, CanvasEditor, CanvasRegion, ProblemList, Workbench, type WorkbenchCrumb, type WorkbenchProblem, type WorkbenchTab, Input, MarkdownEditor, Panel, Select, Textarea, Toggles, humanizeKernelError, notify, t, useUnsavedChanges,
  type CanvasCommand, type CanvasDrop, type CanvasPayload, type EntityInfo,
} from "@platform/ui";
import { Copy, Clipboard, Columns2, Download, ExternalLink, Rows3, Group, Ungroup, Archive, ChevronUp, Settings2, Equal, RotateCcw, Monitor, MoreHorizontal, Play, Plus, Smartphone, Tablet, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ModuleTree, type ModuleContext } from "./ModuleWorkbench";
import type { Route } from "@platform/ui";
import { WidgetLibrary } from "./page-editor/WidgetLibrary";
import { DraftStatus, PublishMenu, WorkbenchMessage, savingState, useAutoSave } from "../editor/workbench";
import {pageUIManifest,type Api as HostApi} from "@platform/kernel";
import { BindingEditor, WorkflowFormProblems } from "../automate/workflow-binding";
import { variableAccessible, overlayOwner, loopOwner, synchronizeLoopBindings, addOverlay, removeOverlay, appendWidget, layoutID, stashWidget, restoreWidget, setLayoutKind } from "./page-layout";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { VariablesPanel, NodeBindings } from "./page-editor/VariablesPanel";
import { InterfacePanel } from "./page-editor/InterfacePanel";
import { widgetInspector,widgetBindingInspector } from "./page-editor/widgets/registry";
import {InspectorFrame} from "./page-editor/widgets/InspectorFrame";
import {CompatibilityReview} from "./page-editor/CompatibilityReview";
import {applyProfileUpgrade,pageCompatibility} from "./page-editor/compatibility";
import { OverlayProperties } from "./page-editor/OverlayPanel";
import {LayoutTree} from "./page-editor/LayoutTree";
import {LayoutProperties,LayoutSizing} from "./page-editor/LayoutProperties";
import { DraftInputs, useDraftSession } from "../session/DraftSession";
import {copyLayout,copyLayoutIssue,pasteLayout,type LayoutClipboard,type ClipboardIssue} from "./page-editor/clipboard";
import type {AuthoringSection,PageDraft} from "./page-editor/draft";
import {ModuleImportDialog,type ImportPackage} from "./module-import/ModuleImportDialog";

type Api = NonNullable<Definition["page"]>;
type Section = NonNullable<Api["sections"]>[number];
/** The page as its record holds it: what the builder edits and submits. */
type PageRecord = {
  id: string; revision: number; name: string; title: string; description?: string; object: string; state: string;
  list?: string[]; detail?: string[]; actions?: string[];
  selections?: HostApi.SelectionVariable[];
  document?: HostApi.PageDocument;
  sections?: AuthoringSection[];
};
type DraftObjectMetadata = { name: string; title: string; plural?: string; fields: (Omit<HostApi.FieldInfo, "choices"> & { choices?: string })[] };
type Draft = NonNullable<PageRecord["sections"]>[number];


const widgets = widgetContracts.map((contract) => contract.componentID);
const widgetTitles: Record<string, () => string> = Object.fromEntries(widgetContracts.map((contract) => [contract.componentID, () => t(contract.title)]));

type StudioSelection = { kind: "page" | "variables" | "interface" | "queries" } | { kind: "widget" | "container"; id: string };
const emptyDraft = (): PageDraft => ({ sections: [], document: pageDocumentFromSections<Draft>([]).document, selections: [], title: "", description: "" });
const loadDraft = (record: PageRecord): PageDraft => {
  const shorthand: Draft[] = (record.list ?? []).length ? [
    { widget: "table", title: t("Table"), width: "half", fields: record.list },
    { widget: "detail", title: t("Detail"), width: "half", fields: record.detail },
    ...((record.actions ?? []).length ? [{ widget: "actions", title: t("Actions"), actions: record.actions }] : []),
  ] : [];
  const lifted = pageDocumentFromSections(record.sections?.length ? record.sections : shorthand);
  return { sections: record.document ? record.sections ?? [] : lifted.sections, document: record.document ?? lifted.document,
    selections: record.selections ?? [], title: record.title, description: record.description ?? "" };
};
/** The field types a filter offers: values that repeat (the host's platform.Filterable). */
const filterable = ["choice", "boolean", "reference"];

/** The page being composed, as the renderer takes it. */
const asPage = (record: PageRecord, sections: Draft[], document?: HostApi.PageDocument): Api => ({
  name: record.name, title: record.title, description: record.description, layout: "composed",
  object: { app: record.object.split(".")[0] ?? "", kind: "object", name: record.object },
  listFields: [], detailFields: [], actions: [],
  selections: record.selections,
  document,
  sections: sections.map((s) => ({actionDefaults:s.actionDefaults,
collectionBuilder:s.collectionBuilder,collectionOutputVariable:s.collectionOutputVariable,map:s.map,scene:s.scene,sceneSampleCollectionVariable:s.sceneSampleCollectionVariable,sceneSampleVariable:s.sceneSampleVariable,scenePartVariable:s.scenePartVariable,ai:s.ai,externalFrame:s.externalFrame,embedding:s.embedding,observation:s.observation,observationHistoryVariable:s.observationHistoryVariable,observationContextVariable:s.observationContextVariable,observationSignalVariable:s.observationSignalVariable,observationThresholdVariable:s.observationThresholdVariable,observationRowsVariable:s.observationRowsVariable,observationCountVariable:s.observationCountVariable,observationMeanVariable:s.observationMeanVariable,actionTable:s.actionTable,notepadVariable:s.notepadVariable,analysis:s.analysis,analysisXVariable:s.analysisXVariable,analysisYVariable:s.analysisYVariable,analysisCountVariable:s.analysisCountVariable,analysisMeanVariable:s.analysisMeanVariable,resourceList:s.resourceList,assetDirectory:s.assetDirectory,graphExplorer:s.graphExplorer,vertexGraph:s.vertexGraph,breadcrumb:s.breadcrumb,avatar:s.avatar,image:s.image,historyLimit:s.historyLimit,commentDraftVariable:s.commentDraftVariable,fileVariable:s.fileVariable,pdfPageVariable:s.pdfPageVariable,recordComparison:s.recordComparison,recordSetVariable:s.recordSetVariable,recordCard:s.recordCard,sparkline:s.sparkline,sparklineDecimalVariable:s.sparklineDecimalVariable,sparklineNumberVariable:s.sparklineNumberVariable,groupValueVariable:s.groupValueVariable,groupSetVariable:s.groupSetVariable,rowValueVariable:s.rowValueVariable,rowSetVariable:s.rowSetVariable,columnValueVariable:s.columnValueVariable,columnSetVariable:s.columnSetVariable,scatter:s.scatter,histogram:s.histogram,inputKind:s.inputKind,spacer:s.spacer,separator:s.separator,notice:s.notice,alertValueVariable:s.alertValueVariable,alertBanner:s.alertBanner,pickerValueVariable:s.pickerValueVariable,recordPicker:s.recordPicker,dateKind:s.dateKind,dateOffset:s.dateOffset,dateVariable:s.dateVariable,dateLabel:s.dateLabel,choiceSetVariable:s.choiceSetVariable,choiceVariable:s.choiceVariable,choiceInput:s.choiceInput,booleanVariant:s.booleanVariant,booleanVariable:s.booleanVariable,booleanLabel:s.booleanLabel,rangeInput:s.rangeInput,rangeMinVariable:s.rangeMinVariable,rangeMaxVariable:s.rangeMaxVariable,leaderboard:s.leaderboard,summaryField:s.summaryField,statisticsVariable:s.statisticsVariable,gauge:s.gauge,gaugeValueVariable:s.gaugeValueVariable,progressLabel:s.progressLabel,progressValueVariable:s.progressValueVariable,progressTotalVariable:s.progressTotalVariable,progressTotal:s.progressTotal,recordGantt:s.recordGantt,recordCalendar:s.recordCalendar,recordEvents:s.recordEvents,recordChart:s.recordChart,recordList:s.recordList,headingLevel:s.headingLevel,countVariable:s.countVariable,metricPresentation:s.metricPresentation,statusTracker:s.statusTracker,recordLinks:s.recordLinks,buttons:s.buttons,recordView:s.recordView,detailPresentation:s.detailPresentation,tablePresentation:s.tablePresentation,tableColumns:s.tableColumns,showSearch:s.showSearch,facets:s.facets,filterSearchVariable:s.filterSearchVariable,selectionSetVariable:s.selectionSetVariable,
    id: s.id, configVersion: s.configVersion, widget: s.widget, title: s.title, width: s.width, selection: s.selection, recordVariable: s.recordVariable, selectionVariable:s.selectionVariable, filterVariable:s.filterVariable, collectionVariable:s.collectionVariable, parentSelection: s.parentSelection, relation: s.relation, fields: s.fields, group: s.group, mark:s.mark,chartVariant:s.chartVariant, columnGroup: s.columnGroup,timeStart:s.timeStart,timeEnd:s.timeEnd,timeLabel:s.timeLabel,timeGroup:s.timeGroup,cardLabel:s.cardLabel, measure: s.measure, text: s.text,
    object: s.object ? { app: s.object.split(".")[0] ?? "", kind: "object", name: s.object } : undefined,
    query: s.query ? { app: s.query.split(".")[0] ?? "", kind: "query", name: s.query.split(".").slice(1).join(".") } : undefined,
    function: s.function ? { ref: { app: "build", kind: "function", name: s.function.name }, sourceVersion: `preview.function-${s.function.version}` } : undefined,
    operation: s.operation, inputs: s.inputs,
    inlineEdit:s.inlineEdit?{...s.inlineEdit,action:{app:s.inlineEdit.action.split(".")[0]??"",kind:"action",name:s.inlineEdit.action}}:undefined,
    actions: (s.actions ?? []).map((schema) => ({ app: schema.split(".")[0] ?? "", kind: "action", name: schema })),
  })) as Section[],
});

/** The pages of this organisation: open one to compose it. */
export function PageEditor({ id, module }: { id: string; module?: ModuleContext }) {
  const { decide, source, catalog, definitions } = useHost();
  const { open } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: PageRecord }>(`/v1/records/build.page/${encodeURIComponent(id)}`);
  const draftObjects = useRecordInventory<DraftObjectMetadata>("build.object");
  // Field pickers may describe a saved object before installation. These
  // authoring hints never replace runtime definitions, reads or permissions.
  const authoringEntity = (type: string): EntityInfo | undefined => {
    const installed = source.entity(type);
    if (installed) return installed;
    const object = draftObjects.data?.records.find(record => `build.${record.name}` === type);
    return object ? { type, app: "build", title: object.title, plural: object.plural || object.title, display: "id", standard: [],
      fields: [{ name: "id", title: "ID", type: "text", readOnly: true }, ...(object.fields ?? []).map(field => ({ ...field, choices: field.choices?.split(",").map(value => value.trim()) }))] } : undefined;
  };
  const page = query.data?.record;
  const session = useDraftSession<PageDraft,LayoutClipboard<Draft>>(emptyDraft(),JSON.stringify([id,source.scope]));
  const [clipboardNotice,setClipboardNotice]=useState<{scope:string;error?:boolean;text:string}>();
  const clipboardScope=JSON.stringify([id,source.scope]);
  useEffect(()=>{setClipboardNotice(undefined);setImportPackage(undefined);setImporting(false);setCompatibilityOpen(false);},[clipboardScope]);
  const { sections, document, selections, title, description } = session.draft;
  const { dirty } = session;
  const [selection, select] = useState<StudioSelection>({ kind: "page" });
  const [importing,setImporting]=useState(false);
  const [compatibilityOpen,setCompatibilityOpen]=useState(false);
  const [importPackage,setImportPackage]=useState<{scope:string;pack:ImportPackage}>();
  const [leftTab, setLeftTab] = useState("layers"), [preview, setPreview] = useState(false);
  const [inspectorTab, setInspectorTab] = useState<Record<string, string>>({});
  const [viewport, setViewport] = useState<"desktop" | "tablet" | "mobile">("desktop"), [zoom, setZoom] = useState(100);
  const [refused, setRefused] = useState<string>();
  const [saving, setSaving] = useState(false), [publishing, setPublishing] = useState(false);
  const lock = useRef(false), loaded = useRef(""), baseRevision = useRef(0);
  const busy = saving || publishing;
  const [formProblems, setFormProblems] = useState<Record<string, string>>({});
  const report = useCallback((id: string, problem: string) => setFormProblems((old) => old[id] === problem ? old : { ...old, [id]: problem }), []);
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    if (page) { session.load(loadDraft(page)); baseRevision.current = page.revision; loaded.current = `${page.id}:${page.revision}`; }
    select({ kind: "page" }); setRefused(undefined); setFormProblems({});
  });
  useEffect(() => {
    if (!page || dirty || busy || loaded.current === `${page.id}:${page.revision}`) return;
    session.load(loadDraft(page)); baseRevision.current = page.revision; loaded.current = `${page.id}:${page.revision}`;
  }, [page, dirty, busy, session.load]);
  useEffect(() => {
    if (selection.kind === "widget" && !sections.some((section) => section.id === selection.id) ||
      selection.kind === "container" && !document.nodes[selection.id]) select({ kind: "page" });
  }, [selection, sections, document]);
  const chosen = selection.kind === "widget" ? sections.findIndex((section) => section.id === selection.id) : selection.kind === "container" ? -2 : selection.kind === "variables" ? -3 : -1;
  const container = selection.kind === "container" ? selection.id : undefined;
  const choose = (index: number) => { select(index < 0 || !sections[index]?.id ? { kind: "page" } : { kind: "widget", id: sections[index]!.id! });  };
  const edit: typeof session.edit = (update, key) => { if (!lock.current) session.edit((old) => {
    const next = typeof update === "function" ? update(old) : { ...old, ...update };
    // Profile changes are explicit reviewed commands; ordinary editing retains
    // the original profile even when a layout helper returns the latest one.
    const document=key==="module-import"?next.document:{...next.document,uiProfile:old.document.uiProfile};
    return { ...next, ...synchronizeLoopBindings(document, next.sections) };
  }, key); };
  const history = (direction: "undo" | "redo") => { if (lock.current) return; session[direction](); setFormProblems({}); setRefused(undefined); };
  const selectionProblem = selections.some((v, i) => !/^[a-z][a-z0-9_-]{0,63}$/.test(v.name) || selections.some((other, at) => at !== i && other.name === v.name))
    ? t("Selection names must be unique lowercase identifiers.") : sections.some((s) =>
      s.selection && !selections.some((v) => v.name === s.selection && v.object.name === (s.object || page?.object)) ||
      s.parentSelection && !selections.some((v) => v.name === s.parentSelection))
      ? t("A widget references a missing selection or the wrong object type.") : "";
  const compatibility=pageCompatibility(session.draft);
  const incompatible=!compatibility.supported||compatibility.issues.length>0;
const overlayProblem = Object.values(document.overlays ?? {}).some((overlay) => !document.nodes[overlay.root]?.children?.length && !document.unusedWidgets?.some(entry=>entry.parent===overlay.root) || !overlay.title.trim()) || sections.some(section=>{const contract=widgetContract(section.widget);return contract&&"events" in contract&&contract.events.some(event=>event.required&&!document.events?.some(binding=>binding.source===section.id&&binding.event===event.id));}); const groupProblem=sections.some(s=>s.widget==="button-group"&&(!(s.buttons?.length)||s.buttons.length>pageVariableContract.buttonGroup.maxButtons||s.buttons.some((b,i)=>!b.title||new TextEncoder().encode(b.title).length>pageVariableContract.buttonGroup.maxTitleBytes||s.buttons?.some((other,j)=>i!==j&&b.id===other.id)||!document.events?.some(e=>e.source===s.id&&e.control===b.id&&e.event==="click"))));
  const loopProblem = Object.values(document.nodes).some((node) => node.kind === "loop" && (!node.loop || !document.variables?.[node.loop.collection]));
  const searchInputProblem=sections.some(s=>s.inputKind!==undefined&&(s.widget!=="input"||s.inputKind!=="search"&&s.inputKind!=="scan"||searchInputObjects(document,Object.values(document.nodes).find(n=>n.section===s.id)?.valueVariable).length===0));
  const inputProblem = Object.entries(document.nodes).some(([id, node]) => {
    if (!sections.some((s) => s.id === node.section && s.widget === "input")) return false;
    const variable = document.variables?.[node.valueVariable ?? ""];
    return ["search","scan"].includes(sections.find(s=>s.id===node.section)?.inputKind??"")&&searchInputObjects(document,node.valueVariable).length===0||!variable || !(variable.mode === "state" || variable.mode === "shared" && variable.writable) || !["string","decimal"].includes(variable.type) || !variableAccessible(variable, loopOwner(document, id), overlayOwner(document, id));
  });
  const variableProblems = pageVariableDiagnostics(document.variables ?? {});
  const queryProblem = !queryInventoryBudget(document,sections as unknown as HostApi.Section[],pageVariableContract.query).valid || Object.values(document.queries??{}).some((q)=>q.limit<1||q.limit>pageVariableContract.query.maxLimit||(q.conditions??[]).some((c)=>!c.field));
  const layoutProblems=pageLayoutDiagnostics(document,sections);
  const inlineProblem=sections.some(s=>s.widget==="inline-action"&&(s.actions?.length!==1||!!s.actionDefaults?.length&&!validActionDefaults(source.entity(s.object||page?.object||""),catalog.find(a=>a.schema===s.actions?.[0]),s.actionDefaults)));
  const tableEditProblem=sections.some(s=>s.inlineEdit&&(!s.inlineEdit.action||s.inlineEdit.fields.length===0||s.inlineEdit.fields.length>pageVariableContract.tableEditing.maxFields||s.inlineEdit.fields.some(f=>!s.fields?.includes(f))));
  const presentationLimits=pageVariableContract.tablePresentation;
  const tableControlsProblem=sections.some(s=>{const p=s.tablePresentation;if(!p)return false;const template=p.titleTemplate,source=document.variables?.[s.collectionVariable??""]?.source;return s.widget!=="table"||!["compact","normal"].includes(p.density)||typeof p.showToolbar!=="boolean"||template!==undefined&&(new TextEncoder().encode(template).length>256||/[{}]/.test(template.replace("{count}",""))||template.includes("{count}")&&source?.kind!=="plan");});
  const tablePresentationProblem=tableControlsProblem||sections.some(s=>(s.tableColumns?.length??0)>presentationLimits.maxColumns||s.tableColumns?.some((c,i)=>c.field!=="id"&&!s.fields?.includes(c.field)||s.tableColumns?.some((other,j)=>j!==i&&other.field===c.field)||new TextEncoder().encode(c.title??"").length>presentationLimits.maxTitleBytes||c.width!==undefined&&(!Number.isInteger(c.width)||c.width<presentationLimits.minWidth||c.width>presentationLimits.maxWidth)));
  const recordViewProblem=sections.some(s=>s.widget==="record-view"&&s.recordView?.tabs.length===0);
  const recordLinksProblem=sections.some(s=>s.widget==="record-links"&&(!s.recordLinks?.length||s.recordLinks.length>pageVariableContract.recordLinks.maxGroups||s.recordLinks.some((g,i)=>!g.field||!g.object.name||new TextEncoder().encode(g.title??"").length>pageVariableContract.recordLinks.maxTitleBytes||s.recordLinks?.some((h,j)=>i!==j&&g.object.name===h.object.name&&g.field===h.field))));
  const statusTrackerProblem=sections.some(s=>{if(s.widget!=="status-tracker")return false;const value=s.statusTracker,info=source.entity(s.object||page?.object||""),l=info?.lifecycle;return !value||!l||value.field!==l.field||!info?.fields.some(f=>f.name===value.field)||!value.stages.length||value.stages.length>pageVariableContract.statusTracker.maxStages||new Set(value.stages).size!==value.stages.length||value.stages.some(name=>!l.states.some(s=>s.name===name));});
  const metricAnnotationProblem=sections.some(s=>{const note=s.metricPresentation?.annotation;return !!note&&(s.widget!=="metric"||!["up","down","flat"].includes(note.direction)||new TextEncoder().encode(note.text).length>256);});
  const metricPresentationProblem=metricAnnotationProblem||sections.some(s=>s.metricPresentation&&(new TextEncoder().encode(s.metricPresentation.prefix??"").length>pageVariableContract.metricPresentation.maxUnitBytes||new TextEncoder().encode(s.metricPresentation.suffix??"").length>pageVariableContract.metricPresentation.maxUnitBytes));
  const titleProblem=sections.some(s=>s.widget==="heading"&&(!s.text?.trim()||new TextEncoder().encode(s.text).length>pageVariableContract.titles.maxTextBytes)||s.widget==="collection-title"&&(!s.collectionVariable||!s.countVariable));
  const leaderboardProblem=sections.some(s=>{if(s.widget!=="record-leaderboard")return false;const c=s.leaderboard,v=document.variables?.[s.collectionVariable??""],q=document.queries?.[v?.source?.query??""];return !c||!c.valueField||!c.labelField||!Number.isInteger(c.limit)||c.limit<1||c.limit>32||q?.limit!==c.limit||!!q?.offset||JSON.stringify(q?.sort)!==JSON.stringify([`${c.ascending?"":"-"}${c.valueField}`,"id"]);});
  const summaryProblem=sections.some(s=>{if(s.widget!=="summary-stats")return false;const set=document.variables?.[s.collectionVariable??""],v=document.variables?.[s.statisticsVariable??""];return !s.summaryField||!s.statisticsVariable||v?.type!=="statistics"||v.source?.measure!==s.summaryField||v.source.query!==set?.source?.query;});
  const pickerValueProblem=sections.some(s=>{if(s.widget!=="record-picker"||!s.pickerValueVariable)return false;const v=document.variables?.[s.pickerValueVariable],set=document.variables?.[s.collectionVariable??""];return !v||v.type!=="string"||v.mode!=="state"||v.scope!==set?.scope||v.owner!==set?.owner;});
  const pickerProblem=sections.some(s=>{if(s.widget!=="record-picker")return false;const v=document.variables?.[s.collectionVariable??""],q=document.queries?.[v?.source?.query??""];return !s.recordPicker?.labelField||v?.source?.kind!=="plan"||q?.limit!==20||!!q?.offset||JSON.stringify(q?.sort)!==JSON.stringify(["id"]);});
  const spacerProblem=sections.some(s=>s.widget==="spacer"&&(!s.spacer||typeof s.spacer.size!=="number"||!Number.isFinite(s.spacer.size)||s.spacer.size<0||s.spacer.size>pageUIManifest.layout.maxSize));
  const separatorProblem=sections.some(s=>s.widget==="separator"&&(!s.separator||new TextEncoder().encode(s.separator.label??"").length>pageVariableContract.separator.maxLabelBytes));
  const noticeProblem=sections.some(s=>s.widget==="notice"&&(!s.notice||!(pageVariableContract.notice.tones as readonly string[]).includes(s.notice.tone)||new TextEncoder().encode(s.notice.title??"").length>pageVariableContract.notice.maxTitleBytes||new TextEncoder().encode(s.notice.message).length>pageVariableContract.notice.maxMessageBytes));
  const alertProblem=sections.some(s=>{if(s.widget!=="alert-banner")return false;const c=s.alertBanner,v=document.variables?.[s.alertValueVariable??""],leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0];return !c||parsePageDecimal(c.threshold)?.value!==c.threshold||!(pageVariableContract.alertBanner.tones as readonly string[]).includes(c.tone)||!c.message||new TextEncoder().encode(c.message).length>pageVariableContract.alertBanner.maxMessageBytes||!v||v.type!=="decimal"||!["page","overlay"].includes(v.scope)||!leaf||!!loopOwner(document,leaf)||!variableAccessible(v,undefined,overlayOwner(document,leaf));});
  const dateProblem=sections.some(s=>{if(s.widget!=="date-input")return false;if(s.dateKind&&!["date","datetime"].includes(s.dateKind)||s.dateKind==="datetime"&&!validTimestampOffset(s.dateOffset??"")||s.dateKind!=="datetime"&&s.dateOffset)return true;const v=document.variables?.[s.dateVariable??""],leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0];return !v||v.type!=="string"||v.mode!=="state"||!["page","overlay"].includes(v.scope)||new TextEncoder().encode(s.dateLabel??"").length>1024||!leaf||!!loopOwner(document,leaf)||!variableAccessible(v,undefined,overlayOwner(document,leaf));});
  const choiceProblem=sections.some(s=>{if(s.widget!=="choice-input")return false;const multiple=s.choiceInput?.variant==="multiple",v=document.variables?.[(multiple?s.choiceSetVariable:s.choiceVariable)??""],leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0];return !s.choiceInput||!validChoiceInput(s.choiceInput)||!!(multiple?s.choiceVariable:s.choiceSetVariable)||!v||v.type!==(multiple?"string-set":"string")||(v.mode!=="state"&&(!["steps","tabs"].includes(s.choiceInput.variant)||v.mode!=="constant"))||!["page","overlay"].includes(v.scope)||!leaf||!!loopOwner(document,leaf)||!variableAccessible(v,undefined,overlayOwner(document,leaf))||["steps","tabs"].includes(s.choiceInput.variant)&&(v.scope!==(overlayOwner(document,leaf)?"overlay":"page")||v.owner!==overlayOwner(document,leaf));});
  const booleanProblem=sections.some(s=>{if(s.widget!=="boolean-input")return false;const v=document.variables?.[s.booleanVariable??""];return !!s.booleanVariant&&!(pageVariableContract.booleanInput.variants as readonly string[]).includes(s.booleanVariant)||!v||v.type!=="boolean"||v.mode!=="state"||!["page","overlay"].includes(v.scope)||new TextEncoder().encode(s.booleanLabel??"").length>1024||(()=>{const leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0];return !leaf||!!loopOwner(document,leaf)||!variableAccessible(v,undefined,overlayOwner(document,leaf));})();});
  const rangeProblem=sections.some(s=>{if(s.widget!=="range-input")return false;const a=document.variables?.[s.rangeMinVariable??""],b=document.variables?.[s.rangeMaxVariable??""],r=s.rangeInput;return !r||!rangeGrid(r.min,r.max,r.step)||!a||!b||s.rangeMinVariable===s.rangeMaxVariable||a.type!=="string"||b.type!=="string"||a.mode!=="state"||b.mode!=="state"||a.scope!==b.scope||a.owner!==b.owner||!["page","overlay"].includes(a.scope);});
  const gaugeProblem=sections.some(s=>s.widget==="gauge"&&(!s.gaugeValueVariable||document.variables?.[s.gaugeValueVariable]?.type!=="number"||!s.gauge||!gaugeModel(0,s.gauge.max,s.gauge.warnAt)));
  const progressProblem=sections.some(s=>{if(s.widget!=="progress")return false;return !s.progressValueVariable||document.variables?.[s.progressValueVariable]?.type!=="decimal"||(s.progressTotalVariable?document.variables?.[s.progressTotalVariable]?.type!=="decimal"||!!s.progressTotal:!progressRatio("0",s.progressTotal??""));});
  const recordGanttProblem=sections.some(s=>s.widget==="record-gantt"&&(!s.recordGantt?.startField||!s.recordGantt?.endField||!s.recordGantt?.titleField||!s.recordGantt?.statusField||!ganttRange(s.recordGantt?.rangeStart??"",s.recordGantt?.rangeEnd??"")||!document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""]?.sort?.length));
  const recordCalendarProblem=sections.some(s=>s.widget==="record-calendar"&&(!s.recordCalendar?.dateField||!s.recordCalendar?.labelField||!/^\d{4}-(0[1-9]|1[0-2])$/.test(s.recordCalendar?.initialMonth??"")||s.recordCalendar?.initialMonth.startsWith("0000")||!document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""]?.sort?.length));
  const recordEventsProblem=sections.some(s=>s.widget==="record-events"&&(!s.recordEvents?.timeField||!s.recordEvents?.titleField||!s.recordEvents?.severityField||!document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""]?.sort?.length));
  const explorationProblem=sections.some(s=>explorationViewProblem(document,sections,s,page?.object??"",type=>source.entity(type),definitions));
  const contextProblem=sections.some(s=>contextViewProblem(document,sections,s,page?.object??"",type=>source.entity(type),definitions));
  const workViewProblem=sections.some(s=>{if(!["approval-inbox","notification-feed"].includes(s.widget))return false;const leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0];return !leaf||!!loopOwner(document,leaf)||!!s.object||!!s.fields?.length||!!s.actions?.length||!!s.recordVariable||!!s.selection||!!s.collectionVariable||!!s.selectionVariable||!!s.selectionSetVariable||!!s.filterVariable||!!s.recordSetVariable||!!s.parentSelection||!!s.relation||!!s.query||!!s.inlineEdit||!!s.commentDraftVariable||!!s.fileVariable||!!s.pdfPageVariable;});
  const historyProblem=sections.some(s=>historyViewProblem(document,sections,s,page?.object??""));
  const collaborationProblem=sections.some(s=>{if(!isCollaborationWidget(s.widget))return false;const original=collaborationRecordSource(document,sections,s.recordVariable??"",s.id??"",page?.object??""),r=document.variables?.[s.recordVariable??""],bindingOwner=recordInputOwner(document,s),key=s.widget==="record-comments"?s.commentDraftVariable:s.fileVariable,value=document.variables?.[key??""],pdf=document.variables?.[s.pdfPageVariable??""];return !original||(s.object||page?.object)!==original.object||!!s.fields?.length||!!s.actions?.length||!!s.collectionVariable||!!s.selection||!!s.selectionVariable||!!s.selectionSetVariable||!!s.recordSetVariable||!!s.relation||!!s.parentSelection||!!s.query||!!s.inlineEdit||s.widget==="record-comments"&&!!(s.fileVariable||s.pdfPageVariable)||s.widget==="record-uploader"&&!!(s.commentDraftVariable||s.pdfPageVariable)||["media-preview","pdf-viewer","image-annotation","scene-3d"].includes(s.widget)&&!!s.commentDraftVariable||s.widget==="media-preview"&&!!s.pdfPageVariable||!value||value.type!=="string"||value.scope!==bindingOwner?.scope||value.owner!==bindingOwner?.owner||(value.mode!=="state"&&(!["media-preview","pdf-viewer","image-annotation","scene-3d"].includes(s.widget)||value.mode!=="constant"))||s.widget==="pdf-viewer"&&(!pdf||pdf.type!=="string"||pdf.mode!=="state"||pdf.scope!==r?.scope||pdf.owner!==r?.owner||s.fileVariable===s.pdfPageVariable);});
  const recordComparisonProblem=sections.some(s=>{if(s.widget!=="record-comparison")return false;const binding=recordComparisonSource(document,sections,s.recordSetVariable??"",s.id??"",page?.object??""),info=binding&&source.entity(binding.object),label=s.recordComparison?.labelField,fields=s.fields??[];return !binding||(s.object||page?.object)!==binding.object||!label||label!=="id"&&!info?.fields.some(f=>f.name===label&&["text","longtext","choice","reference"].includes(f.type))||!fields.length||fields.length>64||new Set(fields).size!==fields.length||fields.some(name=>!info?.fields.some(f=>f.name===name&&["text","longtext","choice","reference","integer","decimal","money","date","datetime","boolean"].includes(f.type)));});
  const recordCardProblem=sections.some(s=>{if(s.widget!=="record-card")return false;const v=document.variables?.[s.recordVariable??""];return !s.recordCard?.labelField||!s.recordCard.tone||(s.fields?.length??0)>4||!(v?.mode==="resource"&&v.source?.kind==="record"||Number(document.uiProfile.split(".").at(-1))>=98&&v?.mode==="shared"&&v.type==="record"&&v.source?.object?.name===(s.object||page?.object));});
  const sparklineProblem=sections.some(s=>{if(s.widget!=="sparkline-kpi")return false;const c=s.sparkline,q=document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""];return !c||(!s.sparklineDecimalVariable&&!s.sparklineNumberVariable)||!!s.sparklineDecimalVariable&&!!s.sparklineNumberVariable||!!s.collectionVariable&&(!c.field||!q?.sort?.length||!q.limit||q.limit>30)||!s.collectionVariable&&!!c.field;});
  const tagCountsProblem=sections.some(s=>{if(s.widget!=="tag-counts")return false;const v=document.variables?.[s.collectionVariable??""],output=document.variables?.[s.groupValueVariable??""],info=source.entity(s.object||page?.object||""),leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0],owner=leaf?overlayOwner(document,leaf):undefined;return !leaf||!!loopOwner(document,leaf)||v?.scope!==(owner?"overlay":"page")||v.owner!==owner|| !s.group||s.group==="count"||s.group.includes(":")||!info?.fields.some(f=>f.name===s.group&&["text","choice"].includes(f.type))||v?.mode!=="resource"||v.source?.kind!=="plan"||!!s.groupSetVariable||!!s.groupValueVariable&&(!output||output.type!=="string"||output.mode!=="state"||output.scope!==v.scope||output.owner!==v.owner);});
  const treemapProblem=sections.some(s=>s.widget==="treemap"&&(!s.group||s.group==="count"||s.group.includes(":")||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"||!!s.groupValueVariable&&!!s.groupSetVariable));
  const heatmapProblem=sections.some(s=>{if(s.widget!=="heatmap")return false;const v=document.variables?.[s.collectionVariable??""];return !s.group||!s.columnGroup||s.group===s.columnGroup||s.measure!=="count"||v?.mode!=="resource"||v.source?.kind!=="plan"||!!s.rowValueVariable&&!!s.rowSetVariable||!!s.columnValueVariable&&!!s.columnSetVariable;});
  const scatterProblem=sections.some(s=>{if(s.widget!=="record-scatter")return false;const c=s.scatter,v=document.variables?.[s.collectionVariable??""],q=document.queries?.[v?.source?.query??""];return !c?.xField||!c.yField||!c.colorField||!c.labelField||v?.mode!=="resource"||v.source?.kind!=="plan"||!q?.sort?.length||!q.limit||q.limit>100;});
  const recordChartProblem=sections.some(s=>s.widget==="record-chart"&&(!s.recordChart?.xField||!s.recordChart?.yField||!document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""]?.sort?.length));
  const recordListProblem=sections.some(s=>s.widget==="record-list"&&(!s.collectionVariable||!s.recordList||!s.cardLabel||(s.fields?.length??0)>pageVariableContract.recordList.maxFields));
  const sharedRecordSetOutputProblem=sections.some(s=>!!s.selectionSetVariable&&document.variables?.[s.selectionSetVariable]?.mode==="shared"&&(()=>{const v=document.variables![s.selectionSetVariable!],leaf=Object.entries(document.nodes).find(([,n])=>n.section===s.id)?.[0];return s.widget!=="table"||!leaf||!!overlayOwner(document,leaf)||!!loopOwner(document,leaf)||!v?.writable||v.type!=="record-set"||v.source?.object?.name!==(s.object||page?.object);})());
  const sharedRecordOutputProblem=sections.some(s=>["record-list","record-scatter","record-leaderboard","kanban","record-map"].includes(s.widget)&&!!s.selectionVariable&&(()=>{const v=document.variables?.[s.selectionVariable!],leaf=Object.entries(document.nodes).find(([,n])=>n.kind==='widget'&&n.section===s.id)?.[0];return !!s.selection||s.recordList?.layout==='tiles'||!leaf||!!overlayOwner(document,leaf)||!!loopOwner(document,leaf)||v?.type!=='record'||v.scope!=='application'||v.mode!=='shared'||!v.writable||v.source?.kind!=='application'||v.source.object?.name!==(s.object||page?.object);})());
  const observationInvalid=sections.some(s=>observationProblem(document,sections,s,page?.object??"",type=>source.entity(type),definitions));
  const analysisProblem=sections.some(s=>collectionAnalysisProblem(document,s,page?.object??"",type=>source.entity(type),definitions));
  const recordWorkInvalid=sections.some(s=>recordWorkProblem(document,s,page?.object??"",name=>source.entity(name),catalog,definitions));
  const invalid = sections.some(s=>s.widget==="external-frame"&&!validExternalFrame(s.externalFrame))||sections.some(s=>s.widget==="embedded-page"&&(!s.embedding||!/^page\.sha256\.[0-9a-f]{64}$/.test(s.embedding.contentVersion)))||observationInvalid||recordWorkInvalid||analysisProblem||sections.some(s=>s.widget==="histogram"&&(!s.histogram?.field||!Number.isInteger(s.histogram.bins)||s.histogram.bins<1||s.histogram.bins>64||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"))||sections.some(s=>s.widget==="term-counts"&&(!s.group||s.group==="count"||s.group.includes(":")||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"))||searchInputProblem||pickerValueProblem||spacerProblem||separatorProblem||noticeProblem||alertProblem||pickerProblem||dateProblem||choiceProblem||booleanProblem||rangeProblem||leaderboardProblem||summaryProblem||gaugeProblem||progressProblem||recordGanttProblem||recordCalendarProblem||recordEventsProblem||recordCardProblem||recordComparisonProblem||collaborationProblem||workViewProblem||contextProblem||explorationProblem||historyProblem||sparklineProblem||treemapProblem||tagCountsProblem||heatmapProblem||scatterProblem||recordChartProblem||recordListProblem||sharedRecordOutputProblem||sharedRecordSetOutputProblem||titleProblem||metricPresentationProblem||statusTrackerProblem||recordLinksProblem||groupProblem||recordViewProblem||tablePresentationProblem||tableEditProblem || inlineProblem || layoutProblems.length>0 || queryProblem || inputProblem || loopProblem || overlayProblem || variableProblems.length > 0 || Object.values({...formProblems,...session.inputProblems}).some(Boolean) || !!selectionProblem || incompatible;
  const relatedObjects = useMemo(() => definitions.filter((d) => d.ref.kind === "object" && d.entity && d.ref.name !== page?.object)
    .filter((d) => d.entity!.fields.some((f) => f.type === "reference" && [page?.object, ...selections.map((selection) => selection.object.name)].includes(f.ref))).map((d) => d.ref.name), [definitions, page?.object, selections]);
  const [queryPreviewOwner,setQueryPreviewOwner]=useState<string|undefined>(undefined);
  const [variableValues, setVariableValues] = useState<Record<string, PageVariableValue>>({});
  const saveRef = useRef<() => Promise<unknown>>(async () => false);
  useAutoSave({ dirty, invalid, busy, save: () => saveRef.current() });
  if (!page) return <Workbench storageKey="page" title={t("Page")}><WorkbenchMessage>{query.isError ? t("The page could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const info = authoringEntity(page.object);
const change = (index: number, patch: Partial<Draft>) => edit((old) => ({ ...old, sections: old.sections.map((s, at) => at === index ? { ...s, ...patch } : s) }), `widget:${sections[index]?.id}:${Object.keys(patch).join(",")}`);
  const add = (widget: string, destination?: { container: string; after?: string }, drop?:CanvasDrop) => {
    const contract = widgetContract(widget); if (!contract) return;
    const defaults = JSON.parse(JSON.stringify(contract.defaults)) as Partial<Draft>;
    if(widget==="record-calendar")defaults.recordCalendar={dateField:"",labelField:"id",initialMonth:new Date().toISOString().slice(0,7)};
    const section: Draft = { ...defaults, id: layoutID("section"), configVersion: contract.configVersion, widget, title: t(contract.title) };
    if (contract.fieldPreset === "list") section.fields = info?.fields.slice(0, 4).map((f) => f.name) ?? [];
    if (contract.fieldPreset === "filter") section.fields = info?.fields.filter((f) => filterable.includes(f.type)).slice(0, 2).map((f) => f.name) ?? [];
    if (contract.fieldPreset === "create") section.fields = info?.fields.filter((f) => f.required && !f.readOnly).map((f) => f.name) ?? [];
    edit((old) => {
      const dropParent=drop?(drop.kind==='insert'||drop.kind==='into'?drop.parent:Object.entries(old.document.nodes).find(([,n])=>n.children?.includes(drop.target))?.[0]):undefined;
      let document = appendWidget(old.document, section.id!, dropParent??destination?.container ?? container ?? old.document.root, drop?undefined:destination ? destination.after : chosen >= 0 ? sections[chosen]?.id : undefined);
      if(drop){const leaf=Object.keys(document.nodes).find(id=>document.nodes[id]?.section===section.id);const result=leaf&&canvasDrop(document,leaf,drop,[...old.sections,section]);if(!result)return old;document=result;}
      if (widget === "input") {
        const [nodeID, node] = Object.entries(document.nodes).find(([, node]) => node.section === section.id)!;
        const loop = loopOwner(document, nodeID), overlay = overlayOwner(document, nodeID), variable = layoutID("value");
        node.valueVariable = variable;
        document.variables = { ...document.variables, [variable]: { title: section.title, scope: loop ? "loop-item" : overlay ? "overlay" : "page", owner: loop ?? overlay, type: "string", mode: "state", initial: "" } };
      }
      return { ...old, document, sections: [...old.sections, section] };
    });
    select({ kind: "widget", id: section.id! }); 
  };
  const duplicate = (index=chosen) => {
    const section=sections[index],leaf=Object.keys(document.nodes).find(id=>document.nodes[id]?.section===section?.id);
    if(!leaf)return;
    const copied=copyLayout(session.draft,leaf,page.object);if(copied.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(copied.issue)});return;}
    const parent=Object.entries(document.nodes).find(([,n])=>n.children?.includes(leaf))?.[0]??document.unusedWidgets?.find(e=>e.node===leaf)?.parent??document.root;
    pasteContainer(parent,copied.value,leaf);
  };
  const clipboardError=(issue:ClipboardIssue)=>issue==="unsupported"?t("Copy a main-page Rows, Columns, Tabs, Flow, Toolbar or complete Loop layout."):issue==="scope"?t("Copy a complete main-page layout or an entire overlay root. Scoped fragments need their owner."):issue==="dependencies"?t("An external binding changed since this layout was copied. Copy it again before pasting."):issue==="budget"?t("This copy would exceed the page's layout or resource limits."):issue==="tab-binding"?t("Copied tabs need a private state selector in their layout scope. Shared selectors name the original panels."):issue==="overlay-entry"?t("An overlay copy needs a visible entry button."):t("This layout has missing or invalid references. Correct it before copying.");
  const copyContainer=(root:string)=>{
    if(lock.current)return;
    const result=copyLayout(session.draft,root,page.object);
    if(result.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(result.issue)});return;}
    session.copy(result.value);setClipboardNotice({scope:clipboardScope,text:t("Layout copied. Choose a page layout and paste. External shared bindings stay shared.")});
  };
  const pasteContainer=(target:string,clip=session.clipboard,after?:string)=>{
    if(lock.current||!clip)return;
    const overlay=clip.overlay?clip.draft.document.overlays?.[clip.overlay]:undefined,copyTitle=overlay?t("Copy of {title}",{title:overlay.title}):"",button=widgetContract("button");
    const options=overlay&&button?{title:copyTitle,entry:{...button.defaults,widget:"button",configVersion:button.configVersion,title:t("Open {title}",{title:copyTitle})} as Draft}:undefined;
    const result=pasteLayout(session.draft,clip,target,page.object,{...pageVariableContract,selectionWriters:widgetContracts.filter(w=>w.selectionMode==="write").map(w=>w.componentID),selectionWidgets:widgetContracts.filter(w=>w.selectionMode!=="none").map(w=>w.componentID),references:Object.fromEntries(definitions.filter(d=>d.entity).map(d=>[d.entity!.type,d.entity!.fields.filter(f=>f.type==="reference"&&f.ref).map(f=>f.ref!)]))},options);
    if(result.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(result.issue)});return;}
    let draft=result.value.draft;
    if(after){const parent=Object.entries(draft.document.nodes).find(([,n])=>n.children?.includes(after))?.[0],unused=document.unusedWidgets?.find(e=>e.node===after);if(parent){const index=(draft.document.nodes[parent]!.children??[]).indexOf(after)+1;const next=canvasDrop(draft.document,result.value.root,{kind:'insert',parent,index},draft.sections);if(!next){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError("invalid")});return;}draft={...draft,document:next};}else if(unused){const section=draft.document.nodes[result.value.root]?.section;if(section)draft={...draft,document:stashWidget(draft.document,section)};}}
    edit({...session.draft,...draft});const copied=draft.document.nodes[result.value.root];select(copied?.kind==='widget'?{kind:'widget',id:copied.section!}:{kind:'container',id:result.value.root});
    setClipboardNotice({scope:clipboardScope,text:result.value.shared.length?t("Layout pasted. External bindings kept: {bindings}",{bindings:result.value.shared.map(id=>document.variables?.[id]?.title||id).join(", ")}):t("Layout pasted with independent inputs and record selections.")});
  };
  const duplicateContainer=(root:string)=>{
    const result=copyLayout(session.draft,root,page.object);
    if(result.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(result.issue)});return;}
    const parent=Object.entries(document.nodes).find(([,node])=>node.children?.includes(root))?.[0]??document.root;
    pasteContainer(parent,result.value,root);
  };
  const save = async () => {
    if (invalid || lock.current) return false;
    lock.current = true; setRefused(undefined); setSaving(true);
    const submitted = session.draft;
    const expectedRevision = baseRevision.current;
    try {
      const ok = await decide("build.page.edit", { type: "build.page", id }, submitted, { expectedRevision, onRefused: setRefused });
      if (ok) {
        const result = await query.refetch();
        const confirmed = result.isSuccess && result.data?.record?.revision === expectedRevision + 1 ? result.data.record : undefined;
        baseRevision.current = expectedRevision + 1; loaded.current = `${id}:${baseRevision.current}`;
        session.saved(submitted, confirmed ? loadDraft(confirmed) : undefined); markSaved();
      }
      return ok;
    } catch { setRefused(t("The page could not be saved. Your draft is still here.")); return false; }
    finally { lock.current = false; setSaving(false); }
  };
  const problemList: WorkbenchProblem[] = [
    ...layoutProblems.map((issue, i) => ({ id: `layout:${i}`, text: t(issue.code), subject: issue.node, locate: () => canvasSelect(issue.node) })),
    ...variableProblems.map((issue, i) => ({ id: `variable:${i}`, text: t(issue.code), subject: issue.variable, locate: () => select({ kind: "variables" }) })),
    ...(selectionProblem ? [{ id: "selection", text: selectionProblem, locate: () => select({ kind: "page" }) }] : []),
    ...Object.entries({...formProblems,...session.inputProblems}).filter(([, problem]) => problem).map(([id, problem]) => ({ id: `form:${id}`, text: problem, subject: id })),
    ...(inputProblem ? [{ id: "input", text: t("Bind each input to a writable text or decimal state in its own scope.") }] : []),
    ...(queryProblem ? [{ id: "query", text: t("Each query needs a field for every condition and a limit within the budget.") }] : []),
    ...(inlineProblem ? [{ id: "inline", text: t("An inline action needs exactly one action and valid defaults.") }] : []),
    ...(tableEditProblem ? [{ id: "table-edit", text: t("Inline table editing needs an action and 1–{n} visible fields.", { n: pageVariableContract.tableEditing.maxFields }) }] : []),
    ...(incompatible ? [{ id: "profile", text: t("This draft needs a newer workspace version. Its saved content has been preserved."), locate: () => setCompatibilityOpen(true) }] : []),
    ...(loopProblem ? [{ id: "p0", text: t("Choose a query window for each loop before saving.") }] : []),
    ...(overlayProblem ? [{ id: "p1", text: t("Add content to each overlay and bind every button before saving.") }] : []),
    ...(leaderboardProblem ? [{ id: "p2", text: t("Bind ranking fields and a matching numeric/ID sorted Top-N query before saving.") }] : []),
    ...(recordWorkInvalid ? [{ id: "p3", text: t("Bind the original row action parameters, bounded tile/action window or note state before saving.") }] : []),
    ...(analysisProblem ? [{ id: "p4", text: t("Bind the original analysis fields, matching scalar or axis ports and query before saving.") }] : []),
    ...(summaryProblem ? [{ id: "p5", text: t("Bind the summary field, query and matching statistics before saving.") }] : []),
    ...(searchInputProblem ? [{ id: "p6", text: t("Bind search text to a query search in the same scope before saving.") }] : []),
    ...(pickerValueProblem ? [{ id: "p7", text: t("Bind picker ID output to original text state with the same query owner before saving.") }] : []),
    ...(pickerProblem ? [{ id: "p8", text: t("Bind a picker title and a 20-record ID-sorted query before saving.") }] : []),
    ...(dateProblem ? [{ id: "p9", text: t("Bind the date input to its original scoped text state before saving.") }] : []),
    ...(choiceProblem ? [{ id: "p10", text: t("Bind a scoped text state and valid static choices before saving.") }] : []),
    ...(booleanProblem ? [{ id: "p11", text: t("Bind a boolean state with its original page or overlay owner before saving.") }] : []),
    ...(rangeProblem ? [{ id: "p12", text: t("Bind distinct text states with one owner and a valid decimal range before saving.") }] : []),
    ...(gaugeProblem ? [{ id: "p13", text: t("Bind a number variable and positive gauge maximum before saving.") }] : []),
    ...(spacerProblem ? [{ id: "p14", text: t("Declare a finite nonnegative spacer size within the layout budget before saving.") }] : []),
    ...(separatorProblem ? [{ id: "p15", text: t("Declare a bounded plain separator label or leave it absent before saving.") }] : []),
    ...(noticeProblem ? [{ id: "p16", text: t("Choose a supported notice tone and bounded plain title and text before saving.") }] : []),
    ...(alertProblem ? [{ id: "p17", text: t("Bind an original decimal value and a valid threshold, tone and plain message before saving.") }] : []),
    ...(progressProblem ? [{ id: "p18", text: t("Bind a decimal numerator and exactly one positive fixed total or decimal denominator before saving.") }] : []),
    ...(recordGanttProblem ? [{ id: "p19", text: t("Bind Gantt fields, a valid fixed range and an explicitly sorted plan before saving.") }] : []),
    ...(recordCalendarProblem ? [{ id: "p20", text: t("Bind calendar fields, an initial month and an explicitly sorted plan before saving.") }] : []),
    ...(recordEventsProblem ? [{ id: "p21", text: t("Bind event fields and an explicitly sorted query plan before saving.") }] : []),
    ...(explorationProblem ? [{ id: "p22", text: t("Exploration views require original typed resources, published assets, retained relations and distinct object-specific output ports.") }] : []),
    ...(contextProblem ? [{ id: "p23", text: t("Context views need their original navigation, record or personnel-query bindings; image URL, caption and height must satisfy the static image profile.") }] : []),
    ...(workViewProblem||historyProblem ? [{ id: "p24", text: t("Work views use their original member services. Bounded history needs an original same-owner record resource and a window of 1–100 entries.") }] : []),
    ...(collaborationProblem ? [{ id: "p25", text: t("Bind the original record resource and collaboration state in the same page or overlay before saving.") }] : []),
    ...(recordComparisonProblem ? [{ id: "p26", text: t("Bind an original multi-selection resource, title and 1–64 comparison fields before saving.") }] : []),
    ...(recordCardProblem ? [{ id: "p27", text: t("Bind an original record resource, title and at most four card properties before saving.") }] : []),
    ...(sparklineProblem ? [{ id: "p28", text: t("Bind one original sparkline scalar and an optional ordered 30-record numeric window before saving.") }] : []),
    ...(observationInvalid ? [{ id: "p29", text: t("Bind original observation time, signals, owned windows and matching record or statistic ports before saving.") }] : []),
    ...(treemapProblem ? [{ id: "p30", text: t("Bind a visible treemap group, original query window and exclusive output type before saving.") }] : []),
    ...(heatmapProblem ? [{ id: "p31", text: t("Bind two distinct heatmap axes, complete count and the original query set before saving.") }] : []),
    ...(scatterProblem ? [{ id: "p32", text: t("Bind visible scatter fields and an ordered original window of at most 100 records before saving.") }] : []),
    ...(recordChartProblem ? [{ id: "p33", text: t("Bind visible record chart axes and an explicitly sorted query plan before saving.") }] : []),
    ...(recordListProblem ? [{ id: "p34", text: t("Bind a record card window, title and bounded summary fields before saving.") }] : []),
    ...(titleProblem ? [{ id: "p35", text: t("Enter bounded heading text or bind the collection and its original count before saving.") }] : []),
    ...(metricPresentationProblem ? [{ id: "p36", text: t("Metric units and static notes need supported directions and bounded text.") }] : []),
    ...(statusTrackerProblem ? [{ id: "p37", text: t("Choose an original lifecycle field and valid stages before saving.") }] : []),
    ...(recordLinksProblem ? [{ id: "p38", text: t("Choose valid related groups before saving.") }] : []),
    ...(recordViewProblem ? [{ id: "p39", text: t("Choose at least one record tab before saving.") }] : []),
    ...(groupProblem ? [{ id: "p40", text: t("Give every group button a bounded title and its own click binding.") }] : []),
    ...(tablePresentationProblem ? [{ id: "p41", text: t("Table columns and controls need supported formats, density and bounded titles.") }] : []),
    ...(incompatible ? [{ id: "p42", text: t("This draft needs a newer workspace version. Its saved content has been preserved.") }] : []),
  ];
  const nothing = sections.length === 0;
  saveRef.current = save;
  const publish = async () => {
    if (invalid || nothing || lock.current || dirty && !await save()) return;
    lock.current = true; setRefused(undefined); setPublishing(true);
    try {
      if (await decide("build.page.publish", { type: "build.page", id }, {}, { onRefused: setRefused })) notify.success(t("The page is in the workspace."));
    } catch { setRefused(t("The page could not be installed.")); }
    finally { lock.current = false; setPublishing(false); }
  };
  const review = async () => { if (nothing || invalid || lock.current || dirty && !await save()) return; open({ view: "release-review", params: { kind: "page", id } }); };
  const canvasSelection = sections[chosen];
  const nodeID = container ?? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection?.id && node.kind === "widget")?.[0];
  const patchNode = (id: string, patch: Partial<HostApi.PageLayoutNode>) => edit((old) => ({ ...old, document: { ...old.document, nodes: { ...old.document.nodes, [id]: { ...old.document.nodes[id]!, ...patch } } } }), `node:${id}:${Object.keys(patch).join(",")}`);
  const canvasSelect=(id:string)=>{const node=document.nodes[id];if(!node)return;if(node.kind==='widget'){const index=sections.findIndex(s=>s.id===node.section);if(index>=0)choose(index);}else{select({kind:'container',id});}};
  const canvasEdit=(next:HostApi.PageDocument|undefined)=>{if(next){for(const key of Object.keys(session.inputs.values)){const node=key.match(/^\/layout:([^/]+)\//)?.[1];if(node&&!next.nodes[node])session.inputs.set(key);}edit({document:next});}};
  const canvasCommitDrop=(payload:CanvasPayload,target:CanvasDrop)=>{if(lock.current)return;if(payload.kind==='new')add(payload.type,undefined,target);else{const next=canvasDrop(document,payload.id,target,sections);if(next){canvasEdit(next);canvasSelect(payload.id);}else notify.error(t("This move would change a protected scope or exceed the layout limits."));}};
  const canvasCommitSizes=(sizes:Record<string,HostApi.PageLayoutSize>)=>{if(lock.current)return;const next=structuredClone(document);for(const [id,size]of Object.entries(sizes))if(next.nodes[id])next.nodes[id]!.size=Object.keys(size).length?size:undefined;if(!pageLayoutDiagnostics(next,sections).length)canvasEdit(next);};
  const canvasCommands=(id:string):CanvasCommand[]=>{
    const node=document.nodes[id];if(!node)return [];const parent=Object.entries(document.nodes).find(([,n])=>n.children?.includes(id))?.[0],root=id===document.root||Object.values(document.overlays??{}).some(o=>o.root===id),unused=document.unusedWidgets?.find(e=>e.node===id),at=sections.findIndex(s=>s.id===node.section);
    const children=parent?document.nodes[parent]!.children??[]:[],index=children.indexOf(id),horizontal=parent&&['columns','toolbar'].includes(document.nodes[parent]!.kind),copyIssue=copyLayoutIssue(session.draft,id),ungrouped=canvasUngroup(document,id,sections);
    const commands:CanvasCommand[]=[
      {id:'configure',label:t('Configure selection'),icon:<Settings2/>,primary:true,run:()=>canvasSelect(id)},
      {id:'parent',label:t('Select parent layout'),icon:<ChevronUp/>,disabled:!parent,run:()=>parent&&canvasSelect(parent)},
      {id:'previous',label:t(horizontal?'Move left':'Move up'),disabled:index<=0||!!node.slot,shortcut:horizontal?'Alt+←':'Alt+↑',run:()=>canvasEdit(canvasMove(document,id,-1,sections))},
      {id:'next',label:t(horizontal?'Move right':'Move down'),disabled:index<0||index>=children.length-1||!!node.slot,shortcut:horizontal?'Alt+→':'Alt+↓',run:()=>canvasEdit(canvasMove(document,id,1,sections))},
      {id:'copy',label:t('Copy selection'),icon:<Copy/>,primary:true,disabled:!!copyIssue,shortcut:'⌘C',separatorBefore:true,run:()=>copyContainer(id)},
      {id:'paste',label:t('Paste into layout'),icon:<Clipboard/>,disabled:!session.clipboard,shortcut:'⌘V',run:()=>pasteContainer(node.kind==='widget'?parent??document.root:id)},
      {id:'duplicate',label:t('Duplicate selection'),icon:<Copy/>,primary:true,disabled:root||!!copyIssue,shortcut:'⌘D',run:()=>node.kind==='widget'?duplicate(at):duplicateContainer(id)},
      ...(['rows','columns']as const).map(kind=>({id:'group-'+kind,label:t(kind==='columns'?'Group in columns':'Group in rows'),icon:kind==='columns'?<Columns2/>:<Rows3/>,primary:kind==='columns',disabled:root||!!node.slot,run:()=>{const result=canvasGroup(document,id,kind,sections);if(result){canvasEdit(result.document);select({kind:'container',id:result.id});}}})),
      {id:'ungroup',label:t('Ungroup layout'),icon:<Ungroup/>,disabled:!ungrouped,run:()=>{if(ungrouped){canvasEdit(ungrouped);if(parent)canvasSelect(parent);}}},
      {id:'equalize',label:t('Equalize layout shares'),icon:<Equal/>,disabled:!canvasEqualize(document,id,sections),separatorBefore:true,run:()=>canvasEdit(canvasEqualize(document,id,sections))},
      {id:'reset-size',label:t('Reset region sizing'),icon:<RotateCcw/>,run:()=>patchNode(id,{size:undefined,gap:undefined})},
    ];
    if(node.kind==='widget'){
      if(unused)commands.push({id:'restore',label:t('Put back in original layout'),icon:<Archive/>,run:()=>edit({document:restoreWidget(document,node.section!,unused.parent)})});
      else commands.push({id:'stash',label:t('Move to unused widgets'),icon:<Archive/>,primary:true,run:()=>edit({document:stashWidget(document,node.section!)})});
      const contract=sections[at]&&widgetContract(sections[at]!.widget);
      if(contract&&'slots'in contract)for(const slot of contract.slots)commands.push({id:'slot-'+slot.id,label:t('Edit {slot} slot',{slot:t(slot.title)}),icon:<Group/>,run:()=>{const result=addWidgetSlot(document,node.section!,sections[at]!.widget,slot.id);if(result.id){canvasEdit(result.document);select({kind:'container',id:result.id});}else setCompatibilityOpen(true);}});
    }
    commands.push({id:'delete',label:t(node.kind==='widget'?'Delete widget':'Delete layout'),icon:<Trash2/>,primary:true,danger:true,separatorBefore:true,disabled:root||!!node.slot,run:()=>{const result=canvasRemove(document,id);if(result){edit(old=>({...old,document:result.document,sections:old.sections.filter(s=>!result.sections.has(s.id??''))}));select({kind:'page'});}}});
    return commands;
  };
  const selectionCommands=nodeID?canvasCommands(nodeID):[];
  const selectionCommand=(name:string)=>selectionCommands.find(command=>command.id===name);
  const runSelectionCommand=(name:string)=>{const command=selectionCommand(name);if(command&&!command.disabled&&!busy&&!incompatible)command.run();};
  const canvasRoute: Route = module ? { view: "module", params: { id: module.project.id, application: module.project.id, page: id } } : { view: "module", params: { page: id } };
  const objectRef = { app: page.object.split(".")[0]!, kind: "object", name: page.object };
  const overlayEntries = container ? Object.entries(document.overlays ?? {}).filter(([, overlay]) => overlay.root === container) : [];
  // --- Inspector tabs follow what is in hand: the page, a layout, or a widget (ADR-0053 §5.3).
  const pageTabs: WorkbenchTab[] = [
    { id: "page", title: t("Page"), content: <Settings value={{ title, description }} object={info?.title ?? page.object} device={document.device ?? ""} onDevice={(device) => edit({ document: { ...document, device: device || undefined } }, "settings:device")}
      selections={selections} objects={definitions.filter((d) => d.ref.kind === "object" && d.entity).map((d) => d.ref).sort((a, b) => Number(b.name === page.object) - Number(a.name === page.object))}
      onSelections={(next, rename) => edit((old) => ({ ...old, selections: next, sections: rename ? old.sections.map((s) => ({ ...s, selection: s.selection === rename.from ? rename.to : s.selection, parentSelection: s.parentSelection === rename.from ? rename.to : s.parentSelection })) : old.sections }))}
      onChange={(patch) => edit(patch, `settings:${Object.keys(patch).join(",")}`)} /> },
    { id: "variables", title: t("Variables"), badge: Object.keys(document.variables ?? {}).length || undefined, content: <VariablesPanel object={objectRef} document={document} sections={sections} values={variableValues} onChange={(document) => edit({ document })} /> },
    { id: "queries", title: t("Queries"), badge: Object.keys(document.queries ?? {}).length || undefined, content: <QueriesPanel sections={sections} onPreviewOwner={setQueryPreviewOwner} document={document} object={objectRef} values={variableValues} onChange={(document) => edit({ document })} /> },
    { id: "interface", title: t("Inputs and outputs"), content: <InterfacePanel document={document} object={objectRef} onChange={(document) => edit({ document })} /> },
  ];
  const containerTabs: WorkbenchTab[] = container ? [
    { id: "layout", title: t("Layout"), content: <div className="grid content-start gap-2 p-2">
      {overlayEntries.map(([id, overlay]) => <OverlayProperties key={id} overlay={overlay}
        onChange={(patch) => edit({ document: { ...document, overlays: { ...document.overlays, [id]: { ...overlay, ...patch } } } })}
        onRemove={() => { const result = removeOverlay(document, id); edit({ document: result.document, sections: sections.filter((section) => !result.sections.has(section.id!)) }); select({ kind: "page" }); }} />)}
      <DraftInputs.Provider value={session.inputs}><LayoutProperties sections={sections} document={document} id={container}
        onPatch={patchNode} onChange={(kind) => edit((old) => ({ ...old, document: setLayoutKind(old.document, container, kind) }))}
        onUngroup={() => canvasCommands(container).find((command) => command.id === "ungroup")?.run()} ungroupDisabled={!canvasUngroup(document, container, sections)} /></DraftInputs.Provider>
    </div> },
    { id: "appearance", title: t("Appearance"), content: <div className="grid content-start gap-2 p-2">
      <DraftInputs.Provider value={session.inputs}><LayoutSizing document={document} id={container} onPatch={patchNode} /></DraftInputs.Provider>
      <NodeBindings document={document} id={container} button={false} input={false} onChange={(patch) => patchNode(container, patch)} />
    </div> },
  ] : [];
  const widgetTabs: WorkbenchTab[] = canvasSelection ? widgetInspectorTabs({
    section: canvasSelection, sections, document, info: authoringEntity(canvasSelection.object || page.object), object: page.object, selections, relatedObjects,
    catalog: catalog.map((a) => ({ schema: a.schema, title: a.title, target: a.target })), onChange: (patch) => change(chosen, patch),
    onResourcesChange: (patch, variables) => edit((old) => ({ ...old, document: { ...old.document, variables }, sections: old.sections.map((s) => s.id === canvasSelection.id ? { ...s, ...patch } : s) }), "graph-resources"),
    events: (() => { const Inspector = widgetInspector(canvasSelection.widget, canvasSelection.configVersion ?? 0)?.events; return <InspectorFrame id={canvasSelection.id ?? String(chosen)} widget={canvasSelection.widget} version={canvasSelection.configVersion ?? 0} part="events">{Inspector && <Inspector buttons={canvasSelection.buttons} onGroupChange={(buttons, document) => edit({ document, sections: sections.map((s) => s.id === canvasSelection.id ? { ...s, buttons } : s) })} document={document} section={canvasSelection.id!} owner={nodeID ? loopOwner(document, nodeID) : undefined} overlay={nodeID ? overlayOwner(document, nodeID) : undefined} onChange={(document) => edit({ document })} />}</InspectorFrame>; })(),
    appearance: <>
      {Object.keys(document.overlays ?? {}).length > 0 && <Card className="grid gap-2 p-3"><label className="grid gap-1 text-xs">{t("Move widget to")}<Select value="" onChange={(event) => { if (event.target.value && nodeID) canvasCommitDrop({ kind: "move", id: nodeID, label: canvasSelection.title ?? nodeID }, { kind: "into", parent: event.target.value }); }}><option value="">{t("Choose a layout root")}</option><option value={document.root}>{t("Main page")}</option>{Object.entries(document.overlays ?? {}).map(([id, overlay]) => <option key={id} value={overlay.root}>{overlay.title}</option>)}</Select></label></Card>}
      {nodeID && <DraftInputs.Provider value={session.inputs}><LayoutSizing document={document} id={nodeID} onPatch={patchNode} /></DraftInputs.Provider>}
      {nodeID && <NodeBindings document={document} id={nodeID} button={!!widgetContract(canvasSelection.widget)?.inputPorts.some((port) => port.bindingField === "enabledWhen") && canvasSelection.widget !== "input"} input={canvasSelection.widget === "input"} onChange={(patch) => patchNode(nodeID, patch)} />}
    </>,
  }) : [];
  const inspectorTabs = canvasSelection ? widgetTabs : container ? containerTabs : pageTabs;
  const inspectorKey = canvasSelection ? `widget:${canvasSelection.id}` : container ? `container:${container}` : "page";
  const inspectorValue = selection.kind === "variables" ? "variables" : selection.kind === "queries" ? "queries" : selection.kind === "interface" ? "interface" : inspectorTab[inspectorKey.split(":")[0]!];
  const liveValues = Object.entries(variableValues);
  const crumbs: WorkbenchCrumb[] = module ? [
    { label: t("Projects"), onClick: () => open({ view: "projects" }) },
    { label: module.project.title || module.project.name, onClick: () => open({ view: "project", params: { id: module.project.id } }) },
    { label: t("Module"), onClick: () => module.openModule() },
  ] : [{ label: t("Pages"), onClick: () => open({ view: "module" }) }];
  return <WorkflowFormProblems.Provider value={report}>
    <Workbench storageKey="page" crumbs={crumbs} title={title || page.title}
      status={<DraftStatus state={page.state} problems={problemList.length} />} saving={savingState(dirty, saving, refused)}
      history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: () => history("undo"), redo: () => history("redo") }}
      actions={<>
        <Button size="sm" variant={preview ? "primary" : "ghost"} aria-pressed={preview} onClick={() => { setPreview(!preview); select({ kind: "page" }); }}><Play />{t("Preview")}</Button>
        <ActionMenu label={t("More page commands")} icon={<MoreHorizontal />} commands={[
          { id: "import", label: t("Import Workshop module…"), icon: <Download />, disabled: busy, run: () => setImporting(true) },
          { id: "compatibility", label: t("Review page compatibility…"), disabled: busy, run: () => setCompatibilityOpen(true) },
          ...(page.state === "published" ? [{ id: "open", label: t("Open published page"), icon: <ExternalLink />, run: () => open({ view: "page", params: { app: "build", kind: "page", name: page.name } }) }] : []),
        ]} />
        <PublishMenu type="build.page" record={page} dirty={dirty} busy={busy} invalid={invalid} empty={nothing} onReview={() => void review()} onInstall={() => void publish()} onDiscard={discardChanges} route={canvasRoute} />
      </>}
      onKeyDown={(event) => {
        const command = event.metaKey || event.ctrlKey;
        const typing = (event.target as HTMLElement).closest("input,textarea,select,[contenteditable=true]");
        if (command && event.key.toLowerCase() === "s") { event.preventDefault(); if (dirty && !busy) void save(); }
        if (!typing && !busy && !incompatible && event.altKey && nodeID && ["ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"].includes(event.key)) { event.preventDefault(); canvasEdit(canvasMove(document, nodeID, event.key === "ArrowUp" || event.key === "ArrowLeft" ? -1 : 1, sections)); return; }
        if (!typing && !busy && nodeID && (event.key === "Delete" || event.key === "Backspace")) { event.preventDefault(); runSelectionCommand("delete"); return; }
        if (!typing && event.key === "Escape" && nodeID) { const parent = Object.entries(document.nodes).find(([, n]) => n.children?.includes(nodeID))?.[0]; if (parent && parent !== document.root) canvasSelect(parent); else select({ kind: "page" }); return; }
        if (!command || typing || busy) return;
        if (event.key.toLowerCase() === "z") { event.preventDefault(); history(event.shiftKey ? "redo" : "undo"); }
        if (event.key.toLowerCase() === "y") { event.preventDefault(); history("redo"); }
        if (event.key.toLowerCase() === "d") { event.preventDefault(); runSelectionCommand("duplicate"); }
        if (event.key.toLowerCase() === "c" && nodeID && !window.getSelection()?.toString()) { event.preventDefault(); runSelectionCommand("copy"); }
        if (event.key.toLowerCase() === "v" && nodeID && session.clipboard) { event.preventDefault(); runSelectionCommand("paste"); }
      }}
      left={{ label: t("Page structure"), value: leftTab, onChange: setLeftTab, tabs: [
        { id: "layers", title: t("Layers"), content: <div className="grid content-start">
          {module && <ModuleTree context={module} compact />}
          <LayoutTree document={document} sections={sections} chosen={chosen} container={container} title={title || page.title}
            widgetTitles={widgetTitles} onChoose={choose} onContainer={(id) => canvasSelect(id)}
            onGroup={(kind) => { if (!nodeID) return; const result = canvasGroup(document, nodeID, kind, sections); if (result) { canvasEdit(result.document); select({ kind: "container", id: result.id }); } }}
            onAddOverlay={() => { const result = addOverlay(document, t("Overlay {n}", { n: Object.keys(document.overlays ?? {}).length + 1 })); canvasEdit(result.document); select({ kind: "container", id: result.root }); }}
            commandsForNode={canvasCommands} />
        </div> },
        { id: "widgets", title: t("Widgets"), content: <WidgetLibrary widgets={widgets} widgetTitles={widgetTitles} onAdd={(widget) => add(widget)} /> },
      ] }}
      right={preview ? undefined : { label: t("Inspector"), value: inspectorValue, onChange: (next) => { if (["variables", "queries", "interface"].includes(next) && !canvasSelection && !container) select({ kind: next as "variables" | "queries" | "interface" }); else if (next === "page") select({ kind: "page" }); setInspectorTab((old) => ({ ...old, [inspectorKey.split(":")[0]!]: next })); },
        tabs: [{ id: "__head", title: <span className="flex items-center gap-1">{canvasSelection ? <WidgetGlyph widget={canvasSelection.widget} /> : container ? <LayoutGlyph kind={document.nodes[container]?.kind ?? "rows"} /> : <Settings2 className="size-3" />}<span className="max-w-24 truncate">{canvasSelection ? canvasSelection.title || widgetTitles[canvasSelection.widget]?.() : container ? t("Layout") : t("Page")}</span></span>, content: null }, ...inspectorTabs].filter((tab) => tab.id !== "__head") }}
      dock={{ label: t("Page dock"), tabs: [
        { id: "problems", title: t("Problems"), badge: problemList.length, content: <ProblemList problems={problemList} empty={t("No problems. The page can be published.")} /> },
        { id: "variables", title: t("Variables"), badge: liveValues.length || undefined, content: liveValues.length ? <table className="w-full text-xs"><thead><tr className="text-left text-muted"><th className="px-3 py-1 font-medium">{t("Variable")}</th><th className="px-3 py-1 font-medium">{t("Type")}</th><th className="px-3 py-1 font-medium">{t("Current value")}</th></tr></thead>
          <tbody>{liveValues.map(([name, value]) => <tr key={name} className="border-t border-border"><td className="px-3 py-1 font-mono">{document.variables?.[name]?.title || name}</td><td className="px-3 py-1 text-muted">{document.variables?.[name]?.type}</td><td className="max-w-md truncate px-3 py-1 font-mono">{JSON.stringify(value)}</td></tr>)}</tbody></table> : <p className="p-3 text-xs text-muted">{t("Variables show their live values here once the page renders.")}</p> },
      ] }}>
      {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {!source.entity(page.object) && info && <p className="px-3 pt-2 text-xs text-warning" role="status">{t("Field choices come from a saved object draft. Business data is available after joint activation.")}</p>}
      {importing && <ModuleImportDialog key={clipboardScope} open retained={importPackage?.scope === clipboardScope ? importPackage.pack : undefined} object={page.object} profile={pageUIProfile} onClose={() => setImporting(false)} onApply={(draft, pack) => { setImportPackage({ scope: clipboardScope, pack }); edit(draft, "module-import"); select({ kind: "page" }); setFormProblems({}); setRefused(undefined); }} />}
      {compatibilityOpen && <CompatibilityReview key={clipboardScope} draft={session.draft} busy={busy} onClose={() => setCompatibilityOpen(false)} onLocate={(id) => { select({ kind: "widget", id }); setCompatibilityOpen(false); }} onApply={(review) => { if (lock.current) return; const next = applyProfileUpgrade(session.draft, review); if (!next) return; session.edit(next, "profile-upgrade"); setCompatibilityOpen(false); setFormProblems({}); setRefused(undefined); }} />}
      <fieldset disabled={busy || incompatible} className="flex min-h-0 min-w-0 flex-1 flex-col">
        <CanvasEditor model={canvasModel(document, sections, { ...Object.fromEntries(Object.entries(widgetTitles).map(([id, title]) => [id, title()])), rows: t("Rows"), columns: t("Columns"), tabs: t("Tabs"), flow: t("Flow layout"), toolbar: t("Toolbar"), loop: t("Loop") })} selected={preview ? undefined : nodeID} revision={session.draft} zoom={zoom / 100} disabled={busy || incompatible || preview}
          commands={selectionCommands} icon={canvasSelection ? <WidgetGlyph widget={canvasSelection.widget} /> : <LayoutGlyph kind={document.nodes[nodeID ?? ""]?.kind ?? "rows"} />}
          onSelect={canvasSelect} onDrop={canvasCommitDrop} onMove={(id, delta) => canvasEdit(canvasMove(document, id, delta, sections))}
          resize={(id, axis, pixels, rects) => canvasResize(document, id, axis, pixels, rects)} onResize={canvasCommitSizes}
          onResetSize={(id, axis) => { const sizes = canvasResetSize(document, id, axis); if (sizes) canvasCommitSizes(sizes); }}>
          <div className="flex flex-wrap items-center gap-1 border-b border-border px-2 py-1">
            {([["duplicate", "Duplicate selection", Copy], ["copy", "Copy selection", Copy], ["paste", "Paste into layout", Clipboard]] as const).map(([cid, label, Icon]) => <Button key={cid} size="sm" variant="ghost" title={t(label)} aria-label={t(label)} disabled={busy || incompatible || preview || !selectionCommand(cid) || selectionCommand(cid)?.disabled} onClick={() => runSelectionCommand(cid)}><Icon /></Button>)}
            <span className="mx-1 h-4 w-px bg-border" />
            {(["rows", "columns", "tabs"] as const).map((kind) => <Button key={kind} size="sm" variant="ghost" disabled={!nodeID || preview} title={t(kind === "rows" ? "Group in rows" : kind === "columns" ? "Group in columns" : "Group in tabs")} aria-label={t(kind === "rows" ? "Group in rows" : kind === "columns" ? "Group in columns" : "Group in tabs")} onClick={() => { if (!nodeID) return; const result = canvasGroup(document, nodeID, kind, sections); if (result) { canvasEdit(result.document); select({ kind: "container", id: result.id }); } }}>{kind === "rows" ? <Rows3 /> : kind === "columns" ? <Columns2 /> : <Group />}</Button>)}
            {clipboardNotice?.scope === clipboardScope && <span role={clipboardNotice.error ? "alert" : "status"} className={"ml-2 truncate text-xs " + (clipboardNotice.error ? "text-danger" : "text-muted")}>{clipboardNotice.text}</span>}
            <span className="ml-auto" />
            {([["desktop", "Desktop preview", Monitor], ["tablet", "Tablet preview", Tablet], ["mobile", "Mobile preview", Smartphone]] as const).map(([device, label, Icon]) => <Button key={device} size="sm" variant="ghost" aria-label={t(label)} aria-pressed={viewport === device} onClick={() => setViewport(device)}><Icon /></Button>)}
            <Select aria-label={t("Canvas zoom")} value={zoom} onChange={(event) => setZoom(Number(event.target.value))} className="w-20">{[50, 75, 100, 125].map((value) => <option key={value} value={value}>{value}%</option>)}</Select>
          </div>
          <div data-canvas-scroll className="min-h-[24rem] flex-1 overflow-auto bg-canvas p-4">
            <div className="mx-auto" style={{ width: viewport === "desktop" ? "100%" : viewport === "tablet" ? 768 : 390, zoom: zoom / 100 }}>
              <ApplicationPage pageRef={{ app: "build", kind: "page", name: page.name }} route={canvasRoute} preview><ComposedPage editingRoot={preview ? undefined : (() => {
                if (selection.kind === "queries") return queryPreviewOwner ? document.overlays?.[queryPreviewOwner]?.root : undefined;
                const node = container ?? (canvasSelection?.id ? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection.id)?.[0] : undefined);
                return Object.values(document.overlays ?? {}).find((overlay) => {
                  const includes = (id: string): boolean => id === node || [...(document.nodes[id]?.children ?? []), ...(document.unusedWidgets ?? []).filter((entry) => entry.parent === id).map((entry) => entry.node)].some(includes);
                  return includes(overlay.root);
                })?.root;
              })()} onVariableValues={setVariableValues} page={asPage({ ...page, title, description, selections }, sections, document)} live={preview} chosen={preview ? -1 : chosen} onChoose={preview ? () => {} : choose}
                notice={nothing && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted"><p>{t("This page is empty.")}</p><p className="mt-1 text-xs">{t("Drag a widget from the library, or pick one to add it here.")}</p><Button className="mt-3" size="sm" onClick={() => setLeftTab("widgets")}><Plus />{t("Add widget")}</Button></div>}
                wrapLayout={preview ? undefined : (id, _node, body) => <CanvasRegion key={id} id={id}>{body}</CanvasRegion>} /></ApplicationPage>
            </div>
          </div>
          <div className="flex items-center gap-2 border-t border-border px-3 py-1 text-[11px] text-muted" role="status">{preview ? t("Preview: your records as they are; actions run for real.") : t("Design: your records as they are. Actions do not run while you compose.")}</div>
        </CanvasEditor>
      </fieldset>
    </Workbench>
  </WorkflowFormProblems.Provider>;
}

/** The panel that configures the widget in hand: only what that widget binds. */
type PropertiesProps = {
  section?: Draft; sections:Draft[]; document: HostApi.PageDocument; info?: EntityInfo; object: string; relatedObjects?: string[];
  selections: HostApi.SelectionVariable[];
  catalog: { schema: string; title: string; target: string }[];
  onChange: (patch: Partial<Draft>) => void;
  onResourcesChange:(patch:Partial<Draft>,variables:Record<string,HostApi.PageVariable>)=>void;
};

/** Inspector navigation is local UI; every panel edits the same DraftSession. */
/** The inspector tabs of the widget in hand (ADR-0053 §5.3): Properties, Data, Events, Appearance. */
function widgetInspectorTabs(props: PropertiesProps & { section: Draft; events: ReactNode; appearance: ReactNode }): WorkbenchTab[] {
  const { section, sections, document, info, object, onChange, onResourcesChange } = props;
  const leaf = Object.entries(document.nodes).find(([, node]) => node.section === section.id)?.[0];
  const overlay = leaf ? overlayOwner(document, leaf) : undefined;
  const Inspector = widgetBindingInspector(section.widget, section.configVersion ?? 0);
  const contract = widgetContract(section.widget);
  return [
    { id: "properties", title: t("Properties"), content: <div className="grid content-start gap-2 p-2">
      <Card className="grid gap-3 p-3">
        <label className="grid gap-1 text-xs">{t("Title")}<Input value={section.title ?? ""} onChange={(event) => onChange({ title: event.target.value })} /></label>
        {section.widget === "input" && <label className="grid gap-1 text-xs">{t("Input presentation")}
          <Select value={section.inputKind ?? ""} onChange={(event) => onChange({ inputKind: event.target.value || undefined })}>
            <option value="">{t("Text input")}</option><option value="search">{t("Scoped record search")}</option><option value="scan">{t("Barcode scan (search by scanner or camera)")}</option>
          </Select>
        </label>}
        {section.widget === "text" && <div className="grid gap-1 text-xs">
          <span className="font-medium text-muted">{t("Words")}</span>
          <MarkdownEditor value={section.text ?? ""} onChange={(text) => onChange({ text })} rows={6} placeholder={t("Write markdown here…")} />
        </div>}
      </Card>
      {Inspector && <Card className="grid gap-3 p-3">
        <InspectorFrame id={section.id ?? "selected"} widget={section.widget} version={section.configVersion ?? 0} part="bindings">
          <Inspector section={section} document={document} object={object} info={info} overlay={overlay}
            itemOwner={leaf ? loopOwner(document, leaf) : undefined} sections={sections}
            onResourcesChange={onResourcesChange} onChange={onChange} />
        </InspectorFrame>
      </Card>}
    </div> },
    { id: "data", title: t("Data"), content: <div className="grid content-start gap-2 p-2"><Properties {...props} /></div> },
    ...(contract && "events" in contract && contract.events.length ? [{ id: "events", title: t("Events"), badge: document.events?.filter((binding) => binding.source === section.id).length || undefined, content: <div className="grid content-start gap-2 p-2">{props.events}</div> }] : []),
    { id: "appearance", title: t("Appearance"), content: <div className="grid content-start gap-2 p-2">{props.appearance}</div> },
  ];
}

function Properties({ section, sections, document, info, catalog, object, selections, relatedObjects = [], onChange }: PropertiesProps) {
  const { definitions, source } = useHost();
  const functionRecords = useRecordInventory<{ name: string; title: string; object: string; versions?: string[] }>("build.function");
  const codeRecords = useRecordInventory<{ versions?: string[] }>("build.code");
  const computations = (codeRecords.data?.records ?? []).flatMap((record) => (record.versions ?? []).flatMap((raw) => {
    try { const code = JSON.parse(raw) as { name: string; title: string; version: number; input: HostApi.ValueSchema };
      return [{ binding: { ref: { app: "build", kind: "compute" as const, name: code.name }, sourceVersion: `1.compute-${code.version}` }, ...code }]; } catch { return []; }
  })).concat(definitions.filter((item) => item.ref.kind === "compute" && item.source === "code" && item.operation).map((item) => ({
    binding: { ref: { ...item.ref, kind: "compute" as const }, sourceVersion: item.version }, name: item.ref.name, title: item.operation!.title, version: 0, input: item.operation!.input,
  })));
  const functions = (functionRecords.data?.records ?? []).flatMap((record) => (record.versions ?? []).flatMap((raw) => {
    try { const version = JSON.parse(raw) as { name: string; title: string; object: string; version: number };
      return version.object === object ? [version] : []; } catch { return []; }
  }));
  // Named queries of an object (ADR-0040 21c), as "<app>.<name>".
  const queriesOf = (obj: string) => (definitions ?? [])
    .filter((d) => d.ref.kind === "query" && d.query?.object === obj)
    .map((d) => ({ key: `${d.ref.app}.${d.ref.name}`, title: d.query?.title ?? d.ref.name }));
  if (!section) return <Card className="p-3 text-xs text-muted">{t("Choose a section to configure it.")}</Card>;
  const contract = widgetContract(section.widget);
  const allows = (kind: string) => contract?.bindingKinds.some((binding) => binding === kind) ?? false;
  const fields = info?.fields ?? [];
  const parentType = section.parentSelection ? selections.find((selection) => selection.name === section.parentSelection)?.object.name ?? "" : object;
  const relations = fields.filter((field) => field.type === "reference" && field.ref === parentType && field.inverse).map((field) => field.inverse!);
  const actions = catalog.filter((a) => a.target === (section.object || object));
  const leaf=Object.entries(document.nodes).find(([,n])=>n.section===section.id)?.[0],overlay=leaf?overlayOwner(document,leaf):undefined;
  const accessible=(v:HostApi.PageVariable)=>variableAccessible(v,undefined,overlay);
  const measures = ["count", ...fields.filter((f) => f.type === "integer" || f.type === "decimal" || f.type === "money").flatMap((f) => [`sum:${f.name}`, `avg:${f.name}`])];
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{widgetTitles[section.widget]?.() ?? section.widget}</div>
      {relatedObjects.length > 0 && allows("object") && !interfaceQueryForSection({document,sections},section) && (
        <label className="grid gap-1 text-xs">{t("Object")}
          <SemanticObjectSelect label={t("Object")} value={section.object ?? object} filter={(definition) => definition.ref.name === object || relatedObjects.includes(definition.ref.name)}
            onChange={(ref) => { if (ref) onChange({ object: ref.name === object ? undefined : ref.name,
              selection: undefined,countVariable:undefined,statusTracker:undefined,recordLinks:undefined,tableColumns:undefined, collectionVariable:undefined, parentSelection: undefined, relation: undefined, query: undefined, inputs: undefined, timeStart:undefined,timeEnd:undefined,timeLabel:section.widget==="record-timeline"?"id":undefined,timeGroup:undefined,cardLabel:section.widget==="kanban"?"id":undefined, fields: [], actions: [] }); }} />
        </label>
      )}
      {allows("filter-variable")&&!(leaf&&loopOwner(document,leaf))&&(!overlay||section.widget!=="filter")&&!section.collectionVariable&&<label className="grid gap-1 text-xs">{t("Shared filter binding")}<Select value={section.filterVariable??""} onChange={(e)=>onChange({filterVariable:e.target.value||undefined})}><option value="">{t("Keep filters in this page")}</option>{Object.entries(document.variables??{}).filter(([,v])=>v.mode==="shared"&&v.type==="filter"&&v.source?.object?.name===(section.object||object)&&(section.widget!=="filter"||v.writable)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {section.widget==="metric" && <label className="grid gap-1 text-xs">{t("Aggregate query set")}<Select value={section.collectionVariable??""} onChange={(event)=>{const variable=document.variables?.[event.target.value],query=variable?.source?.query?document.queries?.[variable.source.query]:undefined,shared=variable?.source?.object;onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...((query?.object||shared)?{object:(query?.object||shared)!.name===object?undefined:(query?.object||shared)!.name}:{} )});}}><option value="">{t("Use the widget's own aggregate")}</option>{Object.entries(document.variables??{}).filter(([,v])=>accessible(v)&&v.type==="object-set"&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {allows("record-set-variable")&&<label className="grid gap-1 text-xs">{t("Input record set binding")}<Select value={section.recordSetVariable??""} onChange={e=>{const binding=recordComparisonSource(document,sections,e.target.value,section.id??"",object),changed=binding&&binding.object!==(section.object||object);onChange({recordSetVariable:e.target.value||undefined,...(binding?{object:binding.object===object?undefined:binding.object}:{}),...(changed?{fields:[],recordComparison:{labelField:"id"}}:{})});}}><option value="">{t("Choose an original multi-selection")}</option>{Object.entries(document.variables??{}).filter(([id])=>!!recordComparisonSource(document,sections,id,section.id??"",object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {allows("record-variable") && <label className="grid gap-1 text-xs">{t("Input record binding")}<Select value={document.variables?.[section.recordVariable ?? ""]?.source?.kind === "record" || document.variables?.[section.recordVariable ?? ""]?.mode === "input" || document.variables?.[section.recordVariable ?? ""]?.mode === "shared" ? section.recordVariable : ""} onChange={(e) => {const original=requiresOriginalRecord(section)?collaborationRecordSource(document,sections,e.target.value,section.id??"",object):undefined;onChange({recordVariable:e.target.value||undefined,selection:undefined,...(section.widget==="breadcrumb"&&!e.target.value?{object:undefined,breadcrumb:section.breadcrumb?{...section.breadcrumb,labelField:undefined}:undefined}:{}),...(original?{object:original.object===object?undefined:original.object,fields:[],actions:[]}:{})});}}><option value="">{t(requiresOriginalRecord(section)?"Choose an original record resource":"Use page selection")}</option>{!requiresOriginalRecord(section)&&Object.entries(document.interface?.inputs ?? {}).filter(([, p]) => p.type === "record").map(([id, p]) => <option key={id} value={p.variable}>{id}</option>)}{Object.entries(document.variables??{}).filter(([id,v])=>requiresOriginalRecord(section)?!!collaborationRecordSource(document,sections,id,section.id??"",object):v.type==="record"&&(v.source?.kind==="record"||v.mode==="shared")&&accessible(v)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {section.recordVariable && <p className="text-xs text-muted">{t(document.variables?.[section.recordVariable]?.mode === "input" ? "This widget reads the input record." : document.variables?.[section.recordVariable]?.source?.kind==="record" ? "This widget reads the bound record selection." : "This widget reads the current loop record.")}</p>}
      {!interfaceQueryForSection({document,sections},section) && !requiresOriginalRecord(section) && !section.recordVariable && (selections.length > 0 || section.selection) && contract?.selectionMode !== "none" &&
        <label className="grid gap-1 text-xs">{contract?.selectionMode === "write" ? t("Writes selection") : t("Reads selection")}
          <Select value={section.selection ?? ""} onChange={(e) => onChange({ selection: e.target.value || undefined })}>
            <option value="">{t("Shared selection for this object")}</option>
            {section.selection && !selections.some((v) => v.name === section.selection && v.object.name === (section.object || object)) &&
              <option value={section.selection}>{t("Unavailable selection: {name}", { name: section.selection })}</option>}
            {selections.filter((v) => v.object.name === (section.object || object)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {(selections.length > 0 || section.parentSelection) && section.object && relatedObjects.includes(section.object) &&
        !section.collectionVariable && allows("relation") &&
        <label className="grid gap-1 text-xs">{t("Parent selection")}
          <Select value={section.parentSelection ?? ""} onChange={(e) => { const nextType = e.target.value ? selections.find((selection) => selection.name === e.target.value)?.object.name : object; onChange({ parentSelection: e.target.value || undefined, ...(nextType !== parentType ? { relation: undefined, inputs: undefined } : {}) }); }}>
            <option value="">{t("Page's shared selection")}</option>
            {section.parentSelection && !selections.some((v) => v.name === section.parentSelection && fields.some((field) => field.ref === v.object.name)) &&
              <option value={section.parentSelection}>{t("Unavailable selection: {name}", { name: section.parentSelection })}</option>}
            {selections.filter((v) => fields.some((field) => field.type === "reference" && field.ref === v.object.name)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {section.object && relations.length > 0 && !section.collectionVariable && allows("relation") && (
        <label className="grid gap-1 text-xs">{t("Through")}
          <Select value={section.relation ?? ""} onChange={(e) => {
            const parent = fields.find((f) => f.type === "reference" && f.ref === parentType && f.inverse === e.target.value);
            onChange({ relation: e.target.value || undefined,
              ...(section.widget === "form" && parent && e.target.value ? { fields: section.fields?.filter((name) => name !== parent.name) } : {}) });
          }}>
            <option value="">{section.widget === "form" ? t("Choose the parent in the form") : t("Any reference to this page's object")}</option>
            {relations.map((name) => <option key={name} value={name}>{name}</option>)}
          </Select>
        </label>
      )}
      {!section.collectionVariable && allows("query") && queriesOf(section.object || object).length > 0 && (
        <label className="grid gap-1 text-xs">{t("Query")}
          <Select value={section.query ?? ""} onChange={(e) => onChange({ query: e.target.value || undefined })}>
            <option value="">{t("All records it may read")}</option>
            {queriesOf(section.object || object).map((q) => <option key={q.key} value={q.key}>{q.title}</option>)}
          </Select>
        </label>
      )}
      {section.widget === "function" && <>
        <label className="grid gap-1 text-xs">{t("Published function version")}
          <Select value={section.function ? `${section.function.name}:${section.function.version}` : ""} onChange={(e) => {
            const selected = functions.find((f) => `${f.name}:${f.version}` === e.target.value);
            onChange({ function: selected ? { name: selected.name, version: selected.version } : undefined });
          }}><option value="">{t("Choose a published function")}</option>
            {functions.map((f) => <option key={`${f.name}:${f.version}`} value={`${f.name}:${f.version}`}>{f.title} · {t("Version")} {f.version}</option>)}
          </Select></label>
        <p className="text-xs text-muted">{t("The selected record supplies the function input. Its typed answer stays in a separate call record.")}</p>
      </>}
      {section.widget === "compute" && <>
        <label className="grid gap-1 text-xs">{t("Published code function version")}
          <Select value={section.operation ? `${section.operation.ref.app}/${section.operation.ref.name}:${section.operation.sourceVersion}` : ""} onChange={(event) => {
            const selected = computations.find((item) => `${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}` === event.target.value);
            onChange({ operation: selected?.binding, inputs: undefined });
          }}><option value="">{t("Choose a published code function")}</option>{computations.map((item) => <option key={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`} value={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`}>{item.title} · {item.binding.ref.app} · {item.binding.sourceVersion}</option>)}</Select>
        </label>
        {(() => {
          const schema = computations.find((item) => item.binding.sourceVersion === section.operation?.sourceVersion && item.name === section.operation?.ref.name && item.binding.ref.app === section.operation?.ref.app)?.input;
          if (schema?.type !== "object") return <p className="text-xs text-muted">{t("The operator supplies the complete typed input.")}</p>;
          return <><Button size="sm" onClick={() => onChange({ inputs: section.inputs ? undefined : Object.fromEntries(Object.keys(schema.properties ?? {}).map((name) => [name, { source: "input", path: [name] }])) })}>{section.inputs ? t("Use operator input") : t("Bind calculation inputs")}</Button>
            {section.inputs && Object.entries(schema.properties ?? {}).map(([name, field]) => <BindingEditor key={name} label={name} schema={field} value={section.inputs?.[name]} steps={[]} sources={["literal", "input", "subject"]} optional={!schema.required?.includes(name)}
              onChange={(value) => { const inputs = { ...section.inputs }; if (value) inputs[name] = value; else delete inputs[name]; onChange({ inputs }); }} />)}</>;
        })()}
        <p className="text-xs text-muted">{t("Record inputs are read by the host with the operator's permissions and retain their sources.")}</p>
      </>}
      {section.widget === "filter" && !section.collectionVariable && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it filters by")}</legend>
          <Toggles options={fields.filter((f) => filterable.includes(f.type)).map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} empty={t("This object has no choice, yes/no or reference field to filter by.")} />
          <p className="text-muted">{t("Tables, charts and metrics over the same object show only what it lets through.")}</p>
        </fieldset>
      )}
      {section.widget === "form" && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it asks for")}</legend>
          <Toggles options={fields.filter((f) => !f.readOnly && !section.inputs?.[f.name] && !(section.relation && f.type === "reference" && f.ref === parentType && f.inverse === section.relation)).map((f) => ({ value: f.name, label: f.required ? `${f.title} *` : f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
          <p className="text-muted">{section.relation
            ? t("The selected parent supplies its reference. Choose the remaining fields; creation still uses the object's own action.")
            : t("It makes a new record through the object's own create action; fields marked * are needed.")}</p>
        </fieldset>
      )}
      {section.widget === "form" && <FormInputBindings fields={fields.filter((field) => !field.readOnly && !(section.relation && field.ref === parentType && field.inverse === section.relation))}
        parentType={section.relation ? parentType : undefined} entity={source.entity} inputs={section.inputs ?? {}} onChange={(inputs, field, bound) => onChange({ inputs,
          fields: bound ? section.fields?.filter((name) => name !== field) : [...new Set([...(section.fields ?? []), field])] })} />}
      {(section.widget === "timeline" || section.widget === "tasks") && (
        <p className="text-xs text-muted">{section.widget === "timeline"
          ? t("It shows what happened to the record selected in a table.")
          : t("It shows what waits on the record selected in a table, for whoever opens the page.")}</p>
      )}
      {["detail","record-view"].includes(section.widget) && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it shows")}</legend>
          <Toggles options={fields.map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
        </fieldset>
      )}
      {["actions","record-view"].includes(section.widget) && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Actions it offers")}</legend>
          <Toggles options={actions.map((a) => ({ value: a.schema, label: a.title }))} value={section.actions ?? []}
            onChange={(value) => onChange({ actions: value })} empty={t("No action of this object is offered to you.")} />
        </fieldset>
      )}
      {section.widget === "metric" && (
        <label className="grid gap-1 text-xs">{t("Measure")}
          <Select value={section.measure ?? "count"} onChange={(e) => onChange({ measure: e.target.value })}>
            {measures.map((m) => <option key={m} value={m}>{m}</option>)}
          </Select>
        </label>
      )}
    </Card>
  );
}

/** The page's own settings: what people call it and what it is for. Its name
 *  and its object are its identity — pages, applications and links name them. */
function Settings({ value, object, selections, objects, onSelections, onChange, device, onDevice }: {
  value: { title: string; description: string }; object: string; device: string; onDevice: (device: string) => void;
  selections: HostApi.SelectionVariable[]; objects: HostApi.AssetRef[];
  onSelections: (next: HostApi.SelectionVariable[], rename?: { from: string; to: string }) => void;
  onChange: (patch: Partial<{ title: string; description: string }>) => void;
}) {
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{t("Page settings")}</div>
      <label className="grid gap-1 text-xs">{t("What people call it")}
        <Input value={value.title} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      <label className="grid gap-1 text-xs">{t("What people do on this page")}
        <Textarea rows={4} value={value.description} onChange={(e) => onChange({ description: e.target.value })} />
      </label>
      <p className="text-xs text-muted">{t("It shows {object}. Its name and object stay as they are: applications and links name them.", { object })}</p>
      <label className="grid gap-1 text-xs">{t("Device")}
        <Select value={device} onChange={(e) => onDevice(e.target.value)}>
          <option value="">{t("Desk: columns as laid out")}</option>
          {pageVariableContract.device.kinds.map((kind) => <option key={kind} value={kind}>{t(kind === "handheld" ? "Handheld terminal: one column, large targets" : kind)}</option>)}
        </Select>
      </label>
      <fieldset className="grid gap-3 border-t border-border pt-3">
        <legend className="text-xs font-semibold">{t("Record selections")}</legend>
        <p className="text-xs text-muted">{t("A table writes a selection; details and actions read it. Each selection holds records of one object.")}</p>
        {selections.map((v, i) => <div key={i} className="grid gap-2 rounded-sm border border-border p-2">
          <label className="grid gap-1 text-xs">{t("Selection name")}
            <Input value={v.name} onChange={(e) => { onSelections(selections.map((row, at) => at === i ? { ...row, name: e.target.value } : row), { from: v.name, to: e.target.value }); }} />
          </label>
          <label className="grid gap-1 text-xs">{t("Selection object")}
            <Select value={v.object.name} onChange={(e) => {
              const object = objects.find((ref) => ref.name === e.target.value);
              if (object) onSelections(selections.map((row, at) => at === i ? { ...row, object } : row));
            }}>{objects.map((ref) => <option key={ref.name} value={ref.name}>{ref.name}</option>)}</Select>
          </label>
          <Button size="sm" variant="ghost" onClick={() => onSelections(selections.filter((_, at) => at !== i))}><Trash2 />{t("Remove selection")}</Button>
        </div>)}
        <Button size="sm" disabled={!objects.length} onClick={() => {
          let n = 1; while (selections.some((v) => v.name === `selection${n}`)) n++;
          onSelections([...selections, { name: `selection${n}`, object: objects[0]! }]);
        }}><Plus />{t("Add record selection")}</Button>
      </fieldset>
    </Card>
  );
}

/** Configure native form inputs without requiring an expression or TSX. */
function FormInputBindings({ fields, parentType, entity, inputs, onChange }: {
  fields: EntityInfo["fields"]; parentType?: string; entity: (type: string) => EntityInfo | undefined;
  inputs: Record<string, HostApi.Binding>; onChange: (inputs: Record<string, HostApi.Binding>, field: string, bound: boolean) => void;
}) {
  const paths = (target: EntityInfo["fields"][number]) => parentType ? recordPaths(parentType, entity).filter(({ field }) =>
    (field.type === target.type || field.type === "integer" && target.type === "decimal") && (field.type !== "reference" || field.ref === target.ref)) : [];
  const update = (field: string, value?: HostApi.Binding) => {
    const next = { ...inputs }; if (value) next[field] = value; else delete next[field];
    onChange(next, field, !!value);
  };
  return <fieldset className="grid gap-2 text-xs"><legend className="mb-1">{t("Supplied form inputs")}</legend>
    {fields.map((field) => {
      const options = paths(field), binding = inputs[field.name];
      const choice = binding?.source === "subject" ? `path:${binding.path?.join(".")}` : binding ? "literal" : "manual";
      const schema: HostApi.ValueSchema = { type: field.type === "integer" ? "integer" : field.type === "decimal" ? "number" : field.type === "boolean" ? "boolean" : "string", ...(field.choices?.length ? { enum: field.choices } : {}) };
      return <div key={field.name} className="grid gap-1"><label className="grid gap-1">{field.title}
        <Select aria-label={t("Input source for {field}", { field: field.title })} value={choice} onChange={(event) => {
          const value = event.target.value;
          update(field.name, value === "manual" ? undefined : value === "literal" ? { source: "literal", value: schema.type === "integer" || schema.type === "number" ? 0 : schema.type === "boolean" ? false : "" } : { source: "subject", path: value.slice(5).split(".") });
        }}><option value="manual">{t("Operator input")}</option><option value="literal">{t("Constant")}</option>
          {binding?.source === "subject" && !options.some((option) => `path:${option.path.join(".")}` === choice) && <option value={choice}>{t("Unavailable record path")}</option>}
          {options.map((option) => <option key={option.path.join(".")} value={`path:${option.path.join(".")}`}>{option.label}</option>)}
        </Select></label>
        {binding?.source === "literal" && <BindingEditor label={field.title} value={binding} schema={schema} sources={["literal"]} steps={[]} onChange={(value) => update(field.name, value)} />}
      </div>;
    })}
    <p className="text-muted">{t("Bound fields are read-only here. The host reads record paths with the operator's permissions when creating the record.")}</p>
  </fieldset>;
}
