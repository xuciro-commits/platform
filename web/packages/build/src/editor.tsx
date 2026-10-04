import {validActionDefaults} from "@platform/app/action-defaults";
import {validExternalFrame} from "@platform/ui/external-frame";
import {observationProblem} from "./page-editor/observation";
import {recordWorkProblem} from "./page-editor/record-work";
import {collectionAnalysisProblem} from "./page-editor/collection-analysis";
import {explorationViewProblem} from "./page-editor/exploration-views";
import {contextViewProblem} from "./page-editor/context";
import {collaborationRecordSource,historyViewProblem,isCollaborationWidget,requiresOriginalRecord} from "./page-editor/collaboration";
import {recordComparisonSource} from "./page-editor/record-comparison";
import {searchInputObjects} from "@platform/app/search";
import {pageLayoutDiagnostics} from "@platform/app";
import { recordPaths } from "./record-paths";
import { AssetControls } from "./asset-controls";
// Application Studio page design (ADR-0046). Document history, UI selection
// and authorized runtime data have separate owners. Preview and operation use
// the same registered widgets; save and activation use the original Go path.
import { ApplicationPage, ComposedPage, NewActions, SemanticObjectSelect, pageDocumentFromSections, parsePageDecimal, pageUIProfile, pageVariableContract, supportsPageUIProfile, pageVariableDiagnostics, widgetContract, widgetContracts, useHost, useReadQuery, useRecordInventory, type PageVariableValue, type Definition } from "@platform/app";
import {
  validTimestampOffset, validChoiceInput, rangeGrid, gaugeModel, progressRatio, ganttRange, Button, Card, CommandMenu, EditorWorkbench, Input, MarkdownEditor, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, Toggles, defineStatuses, humanizeKernelError, notify, t, useWorkspace, useUnsavedChanges,
  type EntityInfo,
} from "@platform/ui";
import { Copy, Monitor, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Plus, Redo2, Smartphone, Tablet, Trash2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {pageUIManifest,type Api as HostApi} from "@platform/kernel";
import { BindingEditor, WorkflowFormProblems } from "./workflow-binding";
import { variableAccessible, overlayOwner, loopOwner, synchronizeLoopBindings, addOverlay, removeOverlay, appendWidget, groupWidget, layoutID, moveWidget, relocateWidget, stashWidget, restoreWidget, removeWidget, setLayoutKind, ungroup } from "./page-layout";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { VariablesPanel, NodeBindings } from "./page-editor/VariablesPanel";
import { InterfacePanel } from "./page-editor/InterfacePanel";
import { widgetInspector } from "./page-editor/widgets/registry";
import { OverlayProperties } from "./page-editor/OverlayPanel";
import { LayoutProperties, LayoutSizing, LayoutTree } from "./page-editor/LayoutTree";
import { useDraftSession } from "./session/DraftSession";
import {copyLayout,pasteLayout,type LayoutClipboard,type ClipboardIssue} from "./page-editor/clipboard";
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
type Draft = NonNullable<PageRecord["sections"]>[number];

const pageStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });

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
export function PagesList() {
  const { source } = useHost();
  const { open } = useWorkspace();
  return (
    <div className="grid gap-3">
      <PageHeader title={t("Pages")} description={t("Compose pages over your objects, then review a candidate to release them together.")}
        actions={<NewActions type="build.page" />} />
      <RecordList source={source} type="build.page" fields={["title", "name", "object", "state"]}
        onOpen={(record) => open({ view: "compose", params: { id: record.id } })} />
    </div>
  );
}

export function PageEditor({ id }: { id: string }) {
  const { decide, source, catalog, definitions } = useHost();
  const { open } = useWorkspace();
  const query = useReadQuery<{ record?: PageRecord }>(`/v1/records/build.page/${encodeURIComponent(id)}`);
  const page = query.data?.record;
  const session = useDraftSession<PageDraft,LayoutClipboard<Draft>>(emptyDraft(),JSON.stringify([id,source.scope]));
  const [clipboardNotice,setClipboardNotice]=useState<{scope:string;error?:boolean;text:string}>();
  const clipboardScope=JSON.stringify([id,source.scope]);
  useEffect(()=>{setClipboardNotice(undefined);setImportPackage(undefined);setImporting(false);},[clipboardScope]);
  const { sections, document, selections, title, description } = session.draft;
  const { dirty } = session;
  const [selection, select] = useState<StudioSelection>({ kind: "page" });
  const [importing,setImporting]=useState(false);
  const [importPackage,setImportPackage]=useState<{scope:string;pack:ImportPackage}>();
  const [leftOpen, setLeftOpen] = useState(true), [rightOpen, setRightOpen] = useState(true);
  const [viewport, setViewport] = useState<"desktop" | "tablet" | "mobile">("desktop"), [zoom, setZoom] = useState(100);
  const [dropTarget, setDropTarget] = useState<string>();
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
  const choose = (index: number) => { select(index < 0 || !sections[index]?.id ? { kind: "page" } : { kind: "widget", id: sections[index]!.id! }); setRightOpen(true); };
  const edit: typeof session.edit = (update, key) => { if (!lock.current) session.edit((old) => {
    const next = typeof update === "function" ? update(old) : { ...old, ...update };
    return { ...next, ...synchronizeLoopBindings(next.document, next.sections) };
  }, key); };
  const history = (direction: "undo" | "redo") => { if (lock.current) return; session[direction](); setFormProblems({}); setRefused(undefined); };
  const selectionProblem = selections.some((v, i) => !/^[a-z][a-z0-9_-]{0,63}$/.test(v.name) || selections.some((other, at) => at !== i && other.name === v.name))
    ? t("Selection names must be unique lowercase identifiers.") : sections.some((s) =>
      s.selection && !selections.some((v) => v.name === s.selection && v.object.name === (s.object || page?.object)) ||
      s.parentSelection && !selections.some((v) => v.name === s.parentSelection))
      ? t("A widget references a missing selection or the wrong object type.") : "";
  const incompatible = document.formatVersion !== 2 || !supportsPageUIProfile(document.uiProfile) || sections.some((s) => !widgetContract(s.widget) || s.configVersion !== widgetContract(s.widget)?.configVersion);
const overlayProblem = Object.values(document.overlays ?? {}).some((overlay) => !document.nodes[overlay.root]?.children?.length && !document.unusedWidgets?.some(entry=>entry.parent===overlay.root) || !overlay.title.trim()) || sections.some(section=>{const contract=widgetContract(section.widget);return contract&&"events" in contract&&contract.events.some(event=>event.required&&!document.events?.some(binding=>binding.source===section.id&&binding.event===event.id));}); const groupProblem=sections.some(s=>s.widget==="button-group"&&(!(s.buttons?.length)||s.buttons.length>pageVariableContract.buttonGroup.maxButtons||s.buttons.some((b,i)=>!b.title||new TextEncoder().encode(b.title).length>pageVariableContract.buttonGroup.maxTitleBytes||s.buttons?.some((other,j)=>i!==j&&b.id===other.id)||!document.events?.some(e=>e.source===s.id&&e.control===b.id&&e.event==="click"))));
  const loopProblem = Object.values(document.nodes).some((node) => node.kind === "loop" && (!node.loop || !document.variables?.[node.loop.collection]));
  const searchInputProblem=sections.some(s=>s.inputKind!==undefined&&(s.widget!=="input"||s.inputKind!=="search"||searchInputObjects(document,Object.values(document.nodes).find(n=>n.section===s.id)?.valueVariable).length===0));
  const inputProblem = Object.entries(document.nodes).some(([id, node]) => {
    if (!sections.some((s) => s.id === node.section && s.widget === "input")) return false;
    const variable = document.variables?.[node.valueVariable ?? ""];
    return sections.find(s=>s.id===node.section)?.inputKind==="search"&&searchInputObjects(document,node.valueVariable).length===0||!variable || !(variable.mode === "state" || variable.mode === "shared" && variable.writable) || !["string","decimal"].includes(variable.type) || !variableAccessible(variable, loopOwner(document, id), overlayOwner(document, id));
  });
  const variableProblems = pageVariableDiagnostics(document.variables ?? {});
  const queryProblem = Object.values(document.queries??{}).some((q)=>q.limit<1||q.limit>pageVariableContract.query.maxLimit||(q.conditions??[]).some((c)=>!c.field));
  const layoutProblems=pageLayoutDiagnostics(document);
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
  const collaborationProblem=sections.some(s=>{if(!isCollaborationWidget(s.widget))return false;const original=collaborationRecordSource(document,sections,s.recordVariable??"",s.id??"",page?.object??""),r=document.variables?.[s.recordVariable??""],key=s.widget==="record-comments"?s.commentDraftVariable:s.fileVariable,value=document.variables?.[key??""],pdf=document.variables?.[s.pdfPageVariable??""];return !original||(s.object||page?.object)!==original.object||!!s.fields?.length||!!s.actions?.length||!!s.collectionVariable||!!s.selection||!!s.selectionVariable||!!s.selectionSetVariable||!!s.recordSetVariable||!!s.relation||!!s.parentSelection||!!s.query||!!s.inlineEdit||s.widget==="record-comments"&&!!(s.fileVariable||s.pdfPageVariable)||s.widget==="record-uploader"&&!!(s.commentDraftVariable||s.pdfPageVariable)||["media-preview","pdf-viewer","image-annotation","scene-3d"].includes(s.widget)&&!!s.commentDraftVariable||s.widget==="media-preview"&&!!s.pdfPageVariable||!value||value.type!=="string"||value.scope!==r?.scope||value.owner!==r?.owner||(value.mode!=="state"&&(!["media-preview","pdf-viewer","image-annotation","scene-3d"].includes(s.widget)||value.mode!=="constant"))||s.widget==="pdf-viewer"&&(!pdf||pdf.type!=="string"||pdf.mode!=="state"||pdf.scope!==r?.scope||pdf.owner!==r?.owner||s.fileVariable===s.pdfPageVariable);});
  const recordComparisonProblem=sections.some(s=>{if(s.widget!=="record-comparison")return false;const binding=recordComparisonSource(document,sections,s.recordSetVariable??"",s.id??"",page?.object??""),info=binding&&source.entity(binding.object),label=s.recordComparison?.labelField,fields=s.fields??[];return !binding||(s.object||page?.object)!==binding.object||!label||label!=="id"&&!info?.fields.some(f=>f.name===label&&["text","longtext","choice","reference"].includes(f.type))||!fields.length||fields.length>64||new Set(fields).size!==fields.length||fields.some(name=>!info?.fields.some(f=>f.name===name&&["text","longtext","choice","reference","integer","decimal","money","date","datetime","boolean"].includes(f.type)));});
  const recordCardProblem=sections.some(s=>{if(s.widget!=="record-card")return false;const v=document.variables?.[s.recordVariable??""];return !s.recordCard?.labelField||!s.recordCard.tone||(s.fields?.length??0)>4||v?.mode!=="resource"||v.source?.kind!=="record";});
  const sparklineProblem=sections.some(s=>{if(s.widget!=="sparkline-kpi")return false;const c=s.sparkline,q=document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""];return !c||(!s.sparklineDecimalVariable&&!s.sparklineNumberVariable)||!!s.sparklineDecimalVariable&&!!s.sparklineNumberVariable||!!s.collectionVariable&&(!c.field||!q?.sort?.length||!q.limit||q.limit>30)||!s.collectionVariable&&!!c.field;});
  const tagCountsProblem=sections.some(s=>{if(s.widget!=="tag-counts")return false;const v=document.variables?.[s.collectionVariable??""],output=document.variables?.[s.groupValueVariable??""],info=source.entity(s.object||page?.object||""),leaf=Object.entries(document.nodes).find(([,n])=>n.kind==="widget"&&n.section===s.id)?.[0],owner=leaf?overlayOwner(document,leaf):undefined;return !leaf||!!loopOwner(document,leaf)||v?.scope!==(owner?"overlay":"page")||v.owner!==owner|| !s.group||s.group==="count"||s.group.includes(":")||!info?.fields.some(f=>f.name===s.group&&["text","choice"].includes(f.type))||v?.mode!=="resource"||v.source?.kind!=="plan"||!!s.groupSetVariable||!!s.groupValueVariable&&(!output||output.type!=="string"||output.mode!=="state"||output.scope!==v.scope||output.owner!==v.owner);});
  const treemapProblem=sections.some(s=>s.widget==="treemap"&&(!s.group||s.group==="count"||s.group.includes(":")||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"||!!s.groupValueVariable&&!!s.groupSetVariable));
  const heatmapProblem=sections.some(s=>{if(s.widget!=="heatmap")return false;const v=document.variables?.[s.collectionVariable??""];return !s.group||!s.columnGroup||s.group===s.columnGroup||s.measure!=="count"||v?.mode!=="resource"||v.source?.kind!=="plan"||!!s.rowValueVariable&&!!s.rowSetVariable||!!s.columnValueVariable&&!!s.columnSetVariable;});
  const scatterProblem=sections.some(s=>{if(s.widget!=="record-scatter")return false;const c=s.scatter,v=document.variables?.[s.collectionVariable??""],q=document.queries?.[v?.source?.query??""];return !c?.xField||!c.yField||!c.colorField||!c.labelField||v?.mode!=="resource"||v.source?.kind!=="plan"||!q?.sort?.length||!q.limit||q.limit>100;});
  const recordChartProblem=sections.some(s=>s.widget==="record-chart"&&(!s.recordChart?.xField||!s.recordChart?.yField||!document.queries?.[document.variables?.[s.collectionVariable??""]?.source?.query??""]?.sort?.length));
  const recordListProblem=sections.some(s=>s.widget==="record-list"&&(!s.collectionVariable||!s.recordList||!s.cardLabel||(s.fields?.length??0)>pageVariableContract.recordList.maxFields));
  const observationInvalid=sections.some(s=>observationProblem(document,sections,s,page?.object??"",type=>source.entity(type),definitions));
  const analysisProblem=sections.some(s=>collectionAnalysisProblem(document,s,page?.object??"",type=>source.entity(type),definitions));
  const recordWorkInvalid=sections.some(s=>recordWorkProblem(document,s,page?.object??"",name=>source.entity(name),catalog,definitions));
  const invalid = sections.some(s=>s.widget==="external-frame"&&!validExternalFrame(s.externalFrame))||sections.some(s=>s.widget==="embedded-page"&&(!s.embedding||!/^page\.sha256\.[0-9a-f]{64}$/.test(s.embedding.contentVersion)))||observationInvalid||recordWorkInvalid||analysisProblem||sections.some(s=>s.widget==="histogram"&&(!s.histogram?.field||!Number.isInteger(s.histogram.bins)||s.histogram.bins<1||s.histogram.bins>64||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"))||sections.some(s=>s.widget==="term-counts"&&(!s.group||s.group==="count"||s.group.includes(":")||document.variables?.[s.collectionVariable??""]?.source?.kind!=="plan"))||searchInputProblem||pickerValueProblem||spacerProblem||separatorProblem||noticeProblem||alertProblem||pickerProblem||dateProblem||choiceProblem||booleanProblem||rangeProblem||leaderboardProblem||summaryProblem||gaugeProblem||progressProblem||recordGanttProblem||recordCalendarProblem||recordEventsProblem||recordCardProblem||recordComparisonProblem||collaborationProblem||workViewProblem||contextProblem||explorationProblem||historyProblem||sparklineProblem||treemapProblem||tagCountsProblem||heatmapProblem||scatterProblem||recordChartProblem||recordListProblem||titleProblem||metricPresentationProblem||statusTrackerProblem||recordLinksProblem||groupProblem||recordViewProblem||tablePresentationProblem||tableEditProblem || inlineProblem || layoutProblems.length>0 || queryProblem || inputProblem || loopProblem || overlayProblem || variableProblems.length > 0 || Object.values(formProblems).some(Boolean) || !!selectionProblem || incompatible;
  const relatedObjects = useMemo(() => definitions.filter((d) => d.ref.kind === "object" && d.entity && d.ref.name !== page?.object)
    .filter((d) => d.entity!.fields.some((f) => f.type === "reference" && [page?.object, ...selections.map((selection) => selection.object.name)].includes(f.ref))).map((d) => d.ref.name), [definitions, page?.object, selections]);
  const [queryPreviewOwner,setQueryPreviewOwner]=useState<string|undefined>(undefined);
  const [variableValues, setVariableValues] = useState<Record<string, PageVariableValue>>({});
  if (!page) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const info = source.entity(page.object);
const change = (index: number, patch: Partial<Draft>) => edit((old) => ({ ...old, sections: old.sections.map((s, at) => at === index ? { ...s, ...patch } : s),document:"actionDefaults" in patch||(sections[index]?.widget==="ai-assistant"&&"recordVariable" in patch)||patch.mark||"map" in patch||"collectionBuilder" in patch||"collectionOutputVariable" in patch||"scene" in patch||"sceneSampleCollectionVariable" in patch||"sceneSampleVariable" in patch||"scenePartVariable" in patch||"ai" in patch||"externalFrame" in patch||"embedding" in patch||"observation" in patch||"observationHistoryVariable" in patch||"observationContextVariable" in patch||"observationSignalVariable" in patch||"observationThresholdVariable" in patch||"observationRowsVariable" in patch||"observationCountVariable" in patch||"observationMeanVariable" in patch||"actionTable" in patch||"notepadVariable" in patch||"analysis" in patch||"analysisXVariable" in patch||"analysisYVariable" in patch||"analysisCountVariable" in patch||"analysisMeanVariable" in patch||"resourceList" in patch||"assetDirectory" in patch||"graphExplorer" in patch||"vertexGraph" in patch||"breadcrumb" in patch||"avatar" in patch||"image" in patch||"historyLimit" in patch||"commentDraftVariable" in patch||"fileVariable" in patch||"pdfPageVariable" in patch||"recordCard" in patch||"sparkline" in patch||"sparklineDecimalVariable" in patch||"sparklineNumberVariable" in patch||"groupValueVariable" in patch||"groupSetVariable" in patch||"rowValueVariable" in patch||"rowSetVariable" in patch||"columnValueVariable" in patch||"columnSetVariable" in patch||"scatter" in patch||"histogram" in patch||"inputKind" in patch||"spacer" in patch||"separator" in patch||"notice" in patch||"alertBanner" in patch||"alertValueVariable" in patch||"pickerValueVariable" in patch||"recordPicker" in patch||"dateKind" in patch||"dateOffset" in patch||"dateVariable" in patch||"dateLabel" in patch||"choiceInput" in patch||"choiceSetVariable" in patch||"choiceVariable" in patch||"booleanVariant" in patch||"booleanVariable" in patch||"booleanLabel" in patch||"rangeInput" in patch||"rangeMinVariable" in patch||"rangeMaxVariable" in patch||"leaderboard" in patch||"summaryField" in patch||"statisticsVariable" in patch||"gauge" in patch||"gaugeValueVariable" in patch||"progressLabel" in patch||"progressValueVariable" in patch||"progressTotalVariable" in patch||"progressTotal" in patch||"recordGantt" in patch||"recordCalendar" in patch||"recordEvents" in patch||"chartVariant" in patch||"recordChart" in patch||"recordList" in patch||"headingLevel" in patch||"countVariable" in patch||"metricPresentation" in patch||"statusTracker" in patch||"recordLinks" in patch||"buttons" in patch||"recordView" in patch||"detailPresentation" in patch||"tablePresentation" in patch||"tableColumns" in patch||"showSearch" in patch?{...old.document,uiProfile:pageUIProfile}:old.document }), `widget:${sections[index]?.id}:${Object.keys(patch).join(",")}`);
  const move = (index: number, by: -1 | 1) => { const section = sections[index]; if (section?.id) edit((old) => ({ ...old, document: moveWidget(old.document, section.id!, by) })); };
  const add = (widget: string, destination?: { container: string; after?: string }) => {
    const contract = widgetContract(widget); if (!contract) return;
    const defaults = JSON.parse(JSON.stringify(contract.defaults)) as Partial<Draft>;
    if(widget==="record-calendar")defaults.recordCalendar={dateField:"",labelField:"id",initialMonth:new Date().toISOString().slice(0,7)};
    const section: Draft = { ...defaults, id: layoutID("section"), configVersion: contract.configVersion, widget, title: t(contract.title) };
    if (contract.fieldPreset === "list") section.fields = info?.fields.slice(0, 4).map((f) => f.name) ?? [];
    if (contract.fieldPreset === "filter") section.fields = info?.fields.filter((f) => filterable.includes(f.type)).slice(0, 2).map((f) => f.name) ?? [];
    if (contract.fieldPreset === "create") section.fields = info?.fields.filter((f) => f.required && !f.readOnly).map((f) => f.name) ?? [];
    edit((old) => {
      const document = appendWidget({ ...old.document, uiProfile: pageUIProfile }, section.id!, destination?.container ?? container ?? old.document.root, destination ? destination.after : chosen >= 0 ? sections[chosen]?.id : undefined);
      if (widget === "input") {
        const [nodeID, node] = Object.entries(document.nodes).find(([, node]) => node.section === section.id)!;
        const loop = loopOwner(document, nodeID), overlay = overlayOwner(document, nodeID), variable = layoutID("value");
        node.valueVariable = variable;
        document.variables = { ...document.variables, [variable]: { title: section.title, scope: loop ? "loop-item" : overlay ? "overlay" : "page", owner: loop ?? overlay, type: "string", mode: "state", initial: "" } };
      }
      return { ...old, document, sections: [...old.sections, section] };
    });
    select({ kind: "widget", id: section.id! }); setRightOpen(true);
  };
  const duplicate = (index=chosen) => {
    const source = sections[index]; if (!source?.id) return;
    const copy = { ...structuredClone(source), id: layoutID("section") };
    edit((old) => {
      const document = appendWidget(old.document, copy.id, old.document.root, source.id);
      const original = Object.values(document.nodes).find((node) => node.section === source.id);
      const leaf = Object.keys(document.nodes).find((id) => document.nodes[id]?.section === copy.id);
      if (original && leaf) document.nodes[leaf] = { ...structuredClone(original), section: copy.id };
      const unused=old.document.unusedWidgets?.find(entry=>old.document.nodes[entry.node]?.section===source.id);
      if(unused&&leaf){document.nodes[document.root]!.children=document.nodes[document.root]!.children?.filter(id=>id!==leaf);document.unusedWidgets=[...(document.unusedWidgets??[]),{node:leaf,parent:unused.parent}];}
      if(original?.valueVariable&&leaf){const value=document.variables?.[original.valueVariable];if(value?.mode==="state"&&value.scope!=="application"){const variable=layoutID("value");document.variables={...document.variables,[variable]:structuredClone(value)};document.nodes[leaf]!.valueVariable=variable;}}
      document.events = [...(old.document.events ?? []), ...(old.document.events ?? []).filter((event) => event.source === source.id).map((event) => ({ ...event, source: copy.id }))];
      return { ...old, sections: [...old.sections, copy], document };
    });
    select({ kind: "widget", id: copy.id });
  };
  const clipboardError=(issue:ClipboardIssue)=>issue==="unsupported"?t("Copy a main-page Rows, Columns, Tabs, Flow, Toolbar or complete Loop layout."):issue==="scope"?t("Copy a complete main-page layout or an entire overlay root. Scoped fragments need their owner."):issue==="dependencies"?t("An external binding changed since this layout was copied. Copy it again before pasting."):issue==="budget"?t("This copy would exceed the page's layout or resource limits."):issue==="tab-binding"?t("Copied tabs need a private state selector in their layout scope. Shared selectors name the original panels."):issue==="overlay-entry"?t("An overlay copy needs a visible entry button."):t("This layout has missing or invalid references. Correct it before copying.");
  const copyContainer=(root:string)=>{
    if(lock.current)return;
    const result=copyLayout(session.draft,root,page.object);
    if(result.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(result.issue)});return;}
    session.copy(result.value);setClipboardNotice({scope:clipboardScope,text:t("Layout copied. Choose a page layout and paste. External shared bindings stay shared.")});
  };
  const pasteContainer=(target:string,clip=session.clipboard)=>{
    if(lock.current||!clip)return;
    const overlay=clip.overlay?clip.draft.document.overlays?.[clip.overlay]:undefined,copyTitle=overlay?t("Copy of {title}",{title:overlay.title}):"",button=widgetContract("button");
    const options=overlay&&button?{title:copyTitle,entry:{...button.defaults,widget:"button",configVersion:button.configVersion,title:t("Open {title}",{title:copyTitle})} as Draft}:undefined;
    const result=pasteLayout(session.draft,clip,target,page.object,{...pageVariableContract,selectionWriters:widgetContracts.filter(w=>w.selectionMode==="write").map(w=>w.componentID),selectionWidgets:widgetContracts.filter(w=>w.selectionMode!=="none").map(w=>w.componentID),references:Object.fromEntries(definitions.filter(d=>d.entity).map(d=>[d.entity!.type,d.entity!.fields.filter(f=>f.type==="reference"&&f.ref).map(f=>f.ref!)]))},options);
    if(result.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(result.issue)});return;}
    edit({...session.draft,...result.value.draft});select({kind:"container",id:result.value.root});setRightOpen(true);
    setClipboardNotice({scope:clipboardScope,text:result.value.shared.length?t("Layout pasted. External bindings kept: {bindings}",{bindings:result.value.shared.map(id=>document.variables?.[id]?.title||id).join(", ")}):t("Layout pasted with independent inputs and record selections.")});
  };
  const duplicateContainer=(root:string)=>{
    const result=copyLayout(session.draft,root,page.object);
    if(result.issue){setClipboardNotice({scope:clipboardScope,error:true,text:clipboardError(result.issue)});return;}
    const parent=Object.entries(document.nodes).find(([,node])=>node.children?.includes(root))?.[0]??document.root;
    pasteContainer(parent,result.value);
  };
  const layoutCommands=(root:string)=>[{id:"copy",label:t("Copy layout"),run:()=>copyContainer(root)},{id:"paste",label:t("Paste layout"),disabled:!session.clipboard,run:()=>pasteContainer(root)},{id:"duplicate",label:t("Duplicate layout"),run:()=>duplicateContainer(root)}];
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
  const nothing = sections.length === 0;
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
  const patchNode = (id: string, patch: Partial<HostApi.PageLayoutNode>) => edit((old) => ({ ...old, document: { ...old.document, uiProfile: pageUIProfile, nodes: { ...old.document.nodes, [id]: { ...old.document.nodes[id]!, ...patch } } } }), `node:${id}:${Object.keys(patch).join(",")}`);
  return <WorkflowFormProblems.Provider value={report}>
    <div className="flex flex-col gap-2 lg:h-[calc(100dvh-8rem)] lg:min-h-0" tabIndex={-1} onKeyDown={(event) => {
      const command = event.metaKey || event.ctrlKey;
      const typing = (event.target as HTMLElement).closest("input,textarea,select,[contenteditable=true]");
      if (command && event.key.toLowerCase() === "s") { event.preventDefault(); if (dirty && !busy) void save(); }
      if (!command || typing || busy) return;
      if (event.key.toLowerCase() === "z") { event.preventDefault(); history(event.shiftKey ? "redo" : "undo"); }
      if (event.key.toLowerCase() === "y") { event.preventDefault(); history("redo"); }
      if (event.key.toLowerCase() === "d") { event.preventDefault(); if(container)duplicateContainer(container);else duplicate(); }
      if (event.key.toLowerCase() === "c"&&container&&!window.getSelection()?.toString()) { event.preventDefault(); copyContainer(container); }
      if (event.key.toLowerCase() === "v"&&container&&session.clipboard) { event.preventDefault(); pasteContainer(container); }
    }}>
      <PageHeader title={title || page.title} description={t("Compose what people see, save your draft, then review its release candidate.")}
        actions={<><StatusTag status={page.state} registry={pageStates} />{page.state === "published" && <Button onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: page.name } })}>{t("Open published page")}</Button>}</>} />
      <Card role="toolbar" aria-label={t("Page design actions")} className="flex flex-wrap items-center gap-1 px-2 py-1.5">
        <Button variant="ghost" aria-label={t("Toggle widget library")} onClick={() => setLeftOpen(!leftOpen)}>{leftOpen ? <PanelLeftClose /> : <PanelLeftOpen />}</Button>
        <Button variant="ghost" aria-label={t("Undo")} title={t("Undo")} disabled={!session.canUndo || busy} onClick={() => history("undo")}><Undo2 /></Button>
        <Button variant="ghost" aria-label={t("Redo")} title={t("Redo")} disabled={!session.canRedo || busy} onClick={() => history("redo")}><Redo2 /></Button>
        <Button variant="ghost" disabled={!container&&chosen < 0 || busy} onClick={()=>container?duplicateContainer(container):duplicate()}><Copy />{t(container?"Duplicate layout":"Duplicate widget")}</Button>
        <Button variant="ghost" disabled={!container||busy} onClick={()=>container&&copyContainer(container)}>{t("Copy layout")}</Button>
        <Button variant="ghost" disabled={!container||!session.clipboard||busy} onClick={()=>container&&pasteContainer(container)}>{t("Paste layout")}</Button>
        <Button variant="ghost" disabled={busy} onClick={()=>setImporting(true)}>{t("Import Workshop module")}</Button>
        <span className="mx-1 h-4 w-px bg-border" />
        <AssetControls type="build.page" record={page} dirty={dirty} busy={busy} onCancel={discardChanges} route={{ view: "compose", params: { id } }} />
        <span className="ml-auto text-xs text-muted" role="status">{dirty ? t("Unsaved") : t("Saved")}</span>
        <Button onClick={() => void save()} disabled={!dirty || busy || invalid}>{saving ? t("Saving…") : t("Save")}</Button>
        <Button onClick={() => void publish()} disabled={nothing || busy || invalid} title={t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}>{publishing ? t("Installing…") : t("Direct install")}</Button>
        <Button variant="primary" onClick={() => void review()} disabled={nothing || busy || invalid}>{t("Review release")}</Button>
        <Button variant="ghost" aria-label={t("Toggle inspector")} onClick={() => setRightOpen(!rightOpen)}>{rightOpen ? <PanelRightClose /> : <PanelRightOpen />}</Button>
      </Card>
      {refused && <Panel role="alert" className="text-sm text-danger">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {importing&&<ModuleImportDialog key={clipboardScope} open retained={importPackage?.scope===clipboardScope?importPackage.pack:undefined} object={page.object} profile={pageUIProfile} onClose={()=>setImporting(false)} onApply={(draft,pack)=>{setImportPackage({scope:clipboardScope,pack});edit(draft);select({kind:"page"});setFormProblems({});setRefused(undefined);}}/>}
      {clipboardNotice?.scope===clipboardScope&&<Panel role={clipboardNotice.error?"alert":"status"}>{clipboardNotice.text}</Panel>}
      {layoutProblems.length>0&&<Panel role="alert">{layoutProblems.map((issue,i)=><p key={i}>{issue.node}: {t(issue.code)}</p>)}</Panel>}
      {variableProblems.length > 0 && <Panel role="alert" className="text-xs text-danger">{variableProblems.map((issue, index) => <p key={index}>{issue.variable}: {t(issue.code)}</p>)}</Panel>}
      {loopProblem && <Panel role="status" className="text-xs text-muted">{t("Choose a query window for each loop before saving.")}</Panel>}
      {overlayProblem && <Panel role="status" className="text-xs text-muted">{t("Add content to each overlay and bind every button before saving.")}</Panel>}
      {leaderboardProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind ranking fields and a matching numeric/ID sorted Top-N query before saving.")}</Panel>}
      {recordWorkInvalid&&<Panel role="alert" className="text-xs text-danger">{t("Bind the original row action parameters, bounded tile/action window or note state before saving.")}</Panel>}
      {analysisProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind the original analysis fields, matching scalar or axis ports and query before saving.")}</Panel>}
      {summaryProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind the summary field, query and matching statistics before saving.")}</Panel>}
      {searchInputProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind search text to a query search in the same scope before saving.")}</Panel>}
      {pickerValueProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind picker ID output to original text state with the same query owner before saving.")}</Panel>}
      {pickerProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a picker title and a 20-record ID-sorted query before saving.")}</Panel>}
      {dateProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind the date input to its original scoped text state before saving.")}</Panel>}
      {choiceProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a scoped text state and valid static choices before saving.")}</Panel>}
      {booleanProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a boolean state with its original page or overlay owner before saving.")}</Panel>}
      {rangeProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind distinct text states with one owner and a valid decimal range before saving.")}</Panel>}
      {gaugeProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a number variable and positive gauge maximum before saving.")}</Panel>}
      {spacerProblem&&<Panel role="alert" className="text-xs text-danger">{t("Declare a finite nonnegative spacer size within the layout budget before saving.")}</Panel>}
      {separatorProblem&&<Panel role="alert" className="text-xs text-danger">{t("Declare a bounded plain separator label or leave it absent before saving.")}</Panel>}
      {noticeProblem&&<Panel role="alert" className="text-xs text-danger">{t("Choose a supported notice tone and bounded plain title and text before saving.")}</Panel>}
      {alertProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind an original decimal value and a valid threshold, tone and plain message before saving.")}</Panel>}
      {progressProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a decimal numerator and exactly one positive fixed total or decimal denominator before saving.")}</Panel>}
      {recordGanttProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind Gantt fields, a valid fixed range and an explicitly sorted plan before saving.")}</Panel>}
      {recordCalendarProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind calendar fields, an initial month and an explicitly sorted plan before saving.")}</Panel>}
      {recordEventsProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind event fields and an explicitly sorted query plan before saving.")}</Panel>}
      {explorationProblem&&<Panel role="alert" className="text-xs text-danger">{t("Exploration views require original typed resources, published assets, retained relations and distinct object-specific output ports.")}</Panel>}
      {contextProblem&&<Panel role="alert" className="text-xs text-danger">{t("Context views need their original navigation, record or personnel-query bindings; image URL, caption and height must satisfy the static image profile.")}</Panel>}
      {(workViewProblem||historyProblem)&&<Panel role="alert" className="text-xs text-danger">{t("Work views use their original member services. Bounded history needs an original same-owner record resource and a window of 1–100 entries.")}</Panel>}
      {collaborationProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind the original record resource and collaboration state in the same page or overlay before saving.")}</Panel>}
      {recordComparisonProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind an original multi-selection resource, title and 1–64 comparison fields before saving.")}</Panel>}
      {recordCardProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind an original record resource, title and at most four card properties before saving.")}</Panel>}
      {sparklineProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind one original sparkline scalar and an optional ordered 30-record numeric window before saving.")}</Panel>}
      {observationInvalid&&<Panel role="alert" className="text-xs text-danger">{t("Bind original observation time, signals, owned windows and matching record or statistic ports before saving.")}</Panel>}
      {treemapProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a visible treemap group, original query window and exclusive output type before saving.")}</Panel>}
      {heatmapProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind two distinct heatmap axes, complete count and the original query set before saving.")}</Panel>}
      {scatterProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind visible scatter fields and an ordered original window of at most 100 records before saving.")}</Panel>}
      {recordChartProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind visible record chart axes and an explicitly sorted query plan before saving.")}</Panel>}
      {recordListProblem&&<Panel role="alert" className="text-xs text-danger">{t("Bind a record card window, title and bounded summary fields before saving.")}</Panel>}
      {titleProblem&&<Panel role="alert" className="text-xs text-danger">{t("Enter bounded heading text or bind the collection and its original count before saving.")}</Panel>}
      {metricPresentationProblem&&<Panel role="alert" className="text-xs text-danger">{t("Metric units and static notes need supported directions and bounded text.")}</Panel>}
      {statusTrackerProblem&&<Panel role="alert" className="text-xs text-danger">{t("Choose an original lifecycle field and valid stages before saving.")}</Panel>}
      {recordLinksProblem&&<Panel role="alert" className="text-xs text-danger">{t("Choose valid related groups before saving.")}</Panel>}
      {recordViewProblem&&<Panel role="alert" className="text-xs text-danger">{t("Choose at least one record tab before saving.")}</Panel>}
      {groupProblem&&<Panel role="alert" className="text-xs text-danger">{t("Give every group button a bounded title and its own click binding.")}</Panel>}
      {tablePresentationProblem&&<Panel role="alert" className="text-xs text-danger">{t("Table columns and controls need supported formats, density and bounded titles.")}</Panel>}
      {selectionProblem && <Panel role="alert" className="text-xs text-danger">{selectionProblem}</Panel>}
      {incompatible && <Panel role="alert" className="text-xs text-danger">{t("This draft needs a newer workspace version. Its saved content has been preserved.")}</Panel>}
      <fieldset disabled={busy || incompatible} className="flex min-w-0 flex-col lg:min-h-0 lg:flex-1">
        <EditorWorkbench leftLabel={t("Widgets and layout")} centerLabel={t("The page")} rightLabel={t("The widget in hand")}
          left={leftOpen && <><Button className="m-3" aria-pressed={selection.kind === "queries"} onClick={() => { select({kind:"queries"});setRightOpen(true); }}>{t("Query plans")}</Button><Button className="m-3" aria-pressed={selection.kind === "interface"} onClick={() => { select({ kind: "interface" }); setRightOpen(true); }}>{t("Page interface")}</Button><Button className="m-3" aria-pressed={selection.kind === "variables"} onClick={() => { select({ kind: "variables" }); setRightOpen(true); }}>{t("Page variables")}</Button><LayoutTree document={document} sections={sections} chosen={chosen} container={container} widgetTitles={widgetTitles} widgets={widgets}
            onChoose={choose} onContainer={(id) => { select({ kind: "container", id }); setRightOpen(true); }} title={title || page.title} onAdd={add} onMove={move}
            onInsert={(widget, container, after) => add(widget, { container, after })}
            onRelocate={(section, target, after) => edit((old) => ({ ...old, document: relocateWidget(old.document, section, target, after) }))}
            onGroup={(kind) => { const section = sections[chosen]; if (!section?.id) return; const result = groupWidget(document, section.id, kind); edit({ document: result.document }); if (result.id) select({ kind: "container", id: result.id }); }}
            onAddOverlay={() => { const result = addOverlay(document, t("Overlay {n}", { n: Object.keys(document.overlays ?? {}).length + 1 })); edit({ document: result.document }); select({ kind: "container", id: result.root }); setRightOpen(true); }}
            onDuplicate={duplicate} onStash={index=>{const section=sections[index];if(section?.id)edit({document:stashWidget(document,section.id)});}}
            onCopyLayout={copyContainer} onPasteLayout={pasteContainer} onDuplicateLayout={duplicateContainer} canPasteLayout={!!session.clipboard}
            onRestore={(index,target)=>{const section=sections[index];if(section?.id)edit({document:restoreWidget(document,section.id,target)});}}
            onRemove={(index) => { const section = sections[index]; if (!section?.id) return; edit((old) => ({ ...old, document: removeWidget(old.document, section.id!), sections: old.sections.filter((s) => s.id !== section.id) })); select({ kind: "page" }); }} /></>}
          right={rightOpen && (selection.kind === "queries" ? <QueriesPanel sections={sections} onPreviewOwner={setQueryPreviewOwner} document={document} object={{app:page.object.split(".")[0]!,kind:"object",name:page.object}} values={variableValues} onChange={(document)=>edit({document})}/> : selection.kind === "interface" ? <InterfacePanel document={document} object={{ app: page.object.split(".")[0]!, kind: "object", name: page.object }} onChange={(document) => edit({ document })} /> : selection.kind === "variables" ? <VariablesPanel object={{app:page.object.split(".")[0]!,kind:"object",name:page.object}} document={document} sections={sections} values={variableValues} onChange={(document) => edit({ document })} /> : <div className="grid content-start gap-2">{container && Object.entries(document.overlays ?? {}).filter(([, overlay]) => overlay.root === container).map(([id, overlay]) => <OverlayProperties key={id} overlay={overlay}
            onChange={(patch) => edit({ document: { ...document, ...(patch.presentation?{uiProfile:pageUIProfile}:{}), overlays: { ...document.overlays, [id]: { ...overlay, ...patch } } } })}
            onRemove={() => { const result = removeOverlay(document, id); edit({ document: result.document, sections: sections.filter((section) => !result.sections.has(section.id!)) }); select({ kind: "page" }); }} />)}{container ? <LayoutProperties document={document} id={container}
            onPatch={patchNode} onChange={(kind) => edit((old) => ({ ...old, document: setLayoutKind(old.document, container, kind) }))}
            onUngroup={() => { edit({ document: ungroup(document, container) }); select({ kind: "page" }); }} /> :
          chosen < 0 ? <Settings value={{ title, description }} object={info?.title ?? page.object}
            selections={selections} objects={definitions.filter((d) => d.ref.kind === "object" && d.entity).map((d) => d.ref).sort((a, b) => Number(b.name === page.object) - Number(a.name === page.object))}
            onSelections={(next, rename) => edit((old) => ({ ...old, selections: next, sections: rename ? old.sections.map((s) => ({ ...s, selection: s.selection === rename.from ? rename.to : s.selection, parentSelection: s.parentSelection === rename.from ? rename.to : s.parentSelection })) : old.sections }))}
            onChange={(patch) => edit(patch, `settings:${Object.keys(patch).join(",")}`)} /> :
          <Properties onResourcesChange={(patch,variables)=>edit(old=>({...old,document:{...old.document,uiProfile:pageUIProfile,variables},sections:old.sections.map(s=>s.id===canvasSelection?.id?{...s,...patch}:s)}),"graph-resources")} sections={sections} section={canvasSelection} info={source.entity(canvasSelection?.object || page.object)} catalog={catalog.map((a) => ({ schema: a.schema, title: a.title, target: a.target }))}
            document={document} object={page.object} selections={selections} relatedObjects={relatedObjects} onChange={(patch) => change(chosen, patch)} />}
            {chosen >= 0 && sections[chosen]?.id && Object.keys(document.overlays ?? {}).length > 0 && <Card className="grid gap-2 p-3"><label className="grid gap-1 text-xs">{t("Move widget to")}<Select value="" onChange={(event) => { if (event.target.value) edit({ document: relocateWidget(document, sections[chosen]!.id!, event.target.value) }); }}><option value="">{t("Choose a layout root")}</option><option value={document.root}>{t("Main page")}</option>{Object.entries(document.overlays ?? {}).map(([id, overlay]) => <option key={id} value={overlay.root}>{overlay.title}</option>)}</Select></label></Card>}
{(() => { const Inspector=canvasSelection&&widgetInspector(canvasSelection.widget,canvasSelection.configVersion??0)?.events;return Inspector&&canvasSelection?<Inspector buttons={canvasSelection.buttons} onGroupChange={(buttons,document)=>edit({document,sections:sections.map(s=>s.id===canvasSelection.id?{...s,buttons}:s)})} document={document} section={canvasSelection.id!} owner={nodeID?loopOwner(document,nodeID):undefined} overlay={nodeID?overlayOwner(document,nodeID):undefined} onChange={document=>edit({document})}/>:null; })()}
            {nodeID&&<LayoutSizing document={document} id={nodeID} onPatch={patchNode}/>}
            {nodeID && <NodeBindings document={document} id={nodeID} button={!!canvasSelection&&!!widgetContract(canvasSelection.widget)?.inputPorts.some(port=>port.bindingField==="enabledWhen")&&canvasSelection.widget!=="input"} input={canvasSelection?.widget === "input"} onChange={(patch) => patchNode(nodeID, patch)} />}</div>)}>
          <div className="flex flex-wrap items-center gap-1 border-b border-border px-3 py-1.5">
            <span className="mr-auto truncate text-xs font-medium">{title || page.title}</span>
            {([["desktop", "Desktop preview", Monitor], ["tablet", "Tablet preview", Tablet], ["mobile", "Mobile preview", Smartphone]] as const).map(([device, label, Icon]) => <Button key={device} size="sm" variant="ghost" aria-label={t(label)} aria-pressed={viewport === device} onClick={() => setViewport(device)}><Icon /></Button>)}
            <Select aria-label={t("Canvas zoom")} value={zoom} onChange={(event) => setZoom(Number(event.target.value))} className="w-20">{[50, 75, 100, 125].map((value) => <option key={value} value={value}>{value}%</option>)}</Select>
          </div>
          <div className="min-h-[24rem] flex-1 overflow-auto bg-canvas p-4">
            <div className="mx-auto" style={{ width: viewport === "desktop" ? "100%" : viewport === "tablet" ? 768 : 390, zoom: zoom / 100 }}>
              <ApplicationPage pageRef={{app:"build",kind:"page",name:page.name}} route={{view:"compose",params:{id}}} preview><ComposedPage editingRoot={(() => {
                if(selection.kind==="queries")return queryPreviewOwner?document.overlays?.[queryPreviewOwner]?.root:undefined;
                const node = container ?? (canvasSelection?.id ? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection.id)?.[0] : undefined);
                return Object.values(document.overlays ?? {}).find((overlay) => {
                  const includes = (id: string): boolean => id === node || [...(document.nodes[id]?.children ?? []),...(document.unusedWidgets??[]).filter(entry=>entry.parent===id).map(entry=>entry.node)].some(includes);
                  return includes(overlay.root);
                })?.root;
              })()} onVariableValues={setVariableValues} page={asPage({ ...page, title, description, selections }, sections, document)} live={false} chosen={chosen} onChoose={choose}
                notice={nothing && <Panel role="status" className="text-xs text-muted">{t("Add at least one widget before installing or reviewing a release.")}</Panel>}
                wrapLayout={(id, node, body) => <div key={id} data-layout-node={id} className={`relative flex min-h-0 min-w-0 flex-1 flex-col rounded ${dropTarget === id || container === id ? "outline outline-2 outline-primary" : ""}`}
                  onDragOver={(event) => { if (!event.dataTransfer.types.some((type) => type === "application/platform-page-widget" || type === "application/platform-page-section")) return; event.preventDefault(); event.stopPropagation(); setDropTarget(id); }}
                  onDragLeave={() => setDropTarget(undefined)} onDrop={(event) => {
                    const widget = event.dataTransfer.getData("application/platform-page-widget"), section = event.dataTransfer.getData("application/platform-page-section");
                    if (!widget && !section) return; event.preventDefault(); event.stopPropagation(); setDropTarget(undefined);
                    const destination = { container: node.kind === "widget" ? document.root : id, after: node.kind === "widget" ? node.section : undefined };
                    if (widget) add(widget, destination); else edit((old) => ({ ...old, document: relocateWidget(old.document, section, destination.container, destination.after) }));
                  }}>
                  {container === id && <div className="absolute -top-3 left-2 z-10"><CommandMenu label={t("Layout commands")} commands={layoutCommands(id)}><Button size="sm" variant="primary" onClick={() => select({ kind: "container", id })}>{t(node.kind === "tabs" ? "Tabs" : node.kind === "columns" ? "Columns" : node.kind === "flow" ? "Flow layout" : node.kind === "toolbar" ? "Toolbar" : node.kind === "loop" ? "Loop" : "Rows")}</Button></CommandMenu></div>}
                  {body}
                </div>} /></ApplicationPage>
            </div>
          </div>
          <div className="flex items-center gap-2 border-t border-border px-3 py-1.5 text-[11px] text-muted" role="status">{t("Your records, as they are. Actions do not run while you compose.")}</div>
        </EditorWorkbench>
      </fieldset>
    </div>
  </WorkflowFormProblems.Provider>;
}

/** The panel that configures the widget in hand: only what that widget binds. */
function Properties({ section, sections, document, info, catalog, object, selections, relatedObjects = [], onChange,onResourcesChange }: {
  section?: Draft; sections:Draft[]; document: HostApi.PageDocument; info?: EntityInfo; object: string; relatedObjects?: string[];
  selections: HostApi.SelectionVariable[];
  catalog: { schema: string; title: string; target: string }[];
  onChange: (patch: Partial<Draft>) => void;
  onResourcesChange:(patch:Partial<Draft>,variables:Record<string,HostApi.PageVariable>)=>void;
}) {
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
      {relatedObjects.length > 0 && allows("object") && (
        <label className="grid gap-1 text-xs">{t("Object")}
          <SemanticObjectSelect label={t("Object")} value={section.object ?? object} filter={(definition) => definition.ref.name === object || relatedObjects.includes(definition.ref.name)}
            onChange={(ref) => { if (ref) onChange({ object: ref.name === object ? undefined : ref.name,
              selection: undefined,countVariable:undefined,statusTracker:undefined,recordLinks:undefined,tableColumns:undefined, collectionVariable:undefined, parentSelection: undefined, relation: undefined, query: undefined, inputs: undefined, timeStart:undefined,timeEnd:undefined,timeLabel:section.widget==="record-timeline"?"id":undefined,timeGroup:undefined,cardLabel:section.widget==="kanban"?"id":undefined, fields: [], actions: [] }); }} />
        </label>
      )}
      {allows("filter-variable")&&!(leaf&&loopOwner(document,leaf))&&(!overlay||section.widget!=="filter")&&!section.collectionVariable&&<label className="grid gap-1 text-xs">{t("Shared filter binding")}<Select value={section.filterVariable??""} onChange={(e)=>onChange({filterVariable:e.target.value||undefined})}><option value="">{t("Keep filters in this page")}</option>{Object.entries(document.variables??{}).filter(([,v])=>v.mode==="shared"&&v.type==="filter"&&v.source?.object?.name===(section.object||object)&&(section.widget!=="filter"||v.writable)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {(() => {const Inspector=widgetInspector(section.widget,section.configVersion??0)?.bindings;return Inspector?<Inspector section={section} document={document} object={object} info={info} overlay={overlay} itemOwner={leaf?loopOwner(document,leaf):undefined} sections={sections} onResourcesChange={onResourcesChange} onChange={onChange}/>:null;})()}
      {section.widget==="metric" && <label className="grid gap-1 text-xs">{t("Aggregate query set")}<Select value={section.collectionVariable??""} onChange={(event)=>{const variable=document.variables?.[event.target.value],query=variable?.source?.query?document.queries?.[variable.source.query]:undefined,shared=variable?.source?.object;onChange({collectionVariable:event.target.value||undefined,filterVariable:undefined,query:undefined,parentSelection:undefined,relation:undefined,...((query?.object||shared)?{object:(query?.object||shared)!.name===object?undefined:(query?.object||shared)!.name}:{} )});}}><option value="">{t("Use the widget's own aggregate")}</option>{Object.entries(document.variables??{}).filter(([,v])=>accessible(v)&&v.type==="object-set"&&(v.source?.kind==="plan"||v.mode==="shared"&&!!v.source?.object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {allows("record-set-variable")&&<label className="grid gap-1 text-xs">{t("Input record set binding")}<Select value={section.recordSetVariable??""} onChange={e=>{const binding=recordComparisonSource(document,sections,e.target.value,section.id??"",object),changed=binding&&binding.object!==(section.object||object);onChange({recordSetVariable:e.target.value||undefined,...(binding?{object:binding.object===object?undefined:binding.object}:{}),...(changed?{fields:[],recordComparison:{labelField:"id"}}:{})});}}><option value="">{t("Choose an original multi-selection")}</option>{Object.entries(document.variables??{}).filter(([id])=>!!recordComparisonSource(document,sections,id,section.id??"",object)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {allows("record-variable") && <label className="grid gap-1 text-xs">{t("Input record binding")}<Select value={document.variables?.[section.recordVariable ?? ""]?.source?.kind === "record" || document.variables?.[section.recordVariable ?? ""]?.mode === "input" || document.variables?.[section.recordVariable ?? ""]?.mode === "shared" ? section.recordVariable : ""} onChange={(e) => {const original=requiresOriginalRecord(section)?collaborationRecordSource(document,sections,e.target.value,section.id??"",object):undefined;onChange({recordVariable:e.target.value||undefined,selection:undefined,...(section.widget==="breadcrumb"&&!e.target.value?{object:undefined,breadcrumb:section.breadcrumb?{...section.breadcrumb,labelField:undefined}:undefined}:{}),...(original?{object:original.object===object?undefined:original.object,fields:[],actions:[]}:{})});}}><option value="">{t(requiresOriginalRecord(section)?"Choose an original record resource":"Use page selection")}</option>{!requiresOriginalRecord(section)&&Object.entries(document.interface?.inputs ?? {}).filter(([, p]) => p.type === "record").map(([id, p]) => <option key={id} value={p.variable}>{id}</option>)}{Object.entries(document.variables??{}).filter(([id,v])=>requiresOriginalRecord(section)?!!collaborationRecordSource(document,sections,id,section.id??"",object):v.type==="record"&&(v.source?.kind==="record"||v.mode==="shared")&&accessible(v)).map(([id,v])=><option key={id} value={id}>{v.title||id}</option>)}</Select></label>}
      {section.recordVariable && <p className="text-xs text-muted">{t(document.variables?.[section.recordVariable]?.mode === "input" ? "This widget reads the input record." : document.variables?.[section.recordVariable]?.source?.kind==="record" ? "This widget reads the bound record selection." : "This widget reads the current loop record.")}</p>}
      {!requiresOriginalRecord(section) && !section.recordVariable && (selections.length > 0 || section.selection) && contract?.selectionMode !== "none" &&
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
      <label className="grid gap-1 text-xs">{t("Title")}
        <Input value={section.title ?? ""} onChange={(e) => onChange({ title: e.target.value })} />
      </label>
      {section.widget==="input"&&<label className="grid gap-1 text-xs">{t("Input presentation")}<Select value={section.inputKind??""} onChange={e=>onChange({inputKind:e.target.value||undefined})}><option value="">{t("Text input")}</option><option value="search">{t("Scoped record search")}</option></Select></label>}
      <p className="text-xs text-muted">{t("Use layout groups to arrange this widget in rows or columns.")}</p>
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
      {section.widget === "text" && (
        <div className="grid gap-1 text-xs">
          <span className="font-medium text-muted">{t("Words")}</span>
          <MarkdownEditor value={section.text ?? ""} onChange={(text) => onChange({ text })} rows={6} placeholder={t("Write markdown here…")} />
        </div>
      )}
    </Card>
  );
}

/** The page's own settings: what people call it and what it is for. Its name
 *  and its object are its identity — pages, applications and links name them. */
function Settings({ value, object, selections, objects, onSelections, onChange }: {
  value: { title: string; description: string }; object: string;
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
