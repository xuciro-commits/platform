import {widgetDeclarationIssues} from "./page-editor/widget-declaration";
import {pageEditingProfile} from "./page-editor/compatibility";
import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue, declarationChanges } from "../editor/declaration";
import {recordComparisonSource} from "./page-editor/record-comparison";
import {pageDraftDiagnostics} from "./page-editor/diagnostics";
import {CompositeBindingInspector} from "./page-editor/CompositeBindingInspector";
import {ModelBindingInspector} from "./page-editor/ModelBindingInspector";
import {materializeModelSection,canonicalModelSection,foldModelPorts,modelEntity} from "@platform/app/model";
import {InputDraftProvider} from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useAuthoringModel } from "../ontology/authoring-model";
import {WidgetGlyph,LayoutGlyph} from "./page-editor/WidgetGlyph";
import {canvasModel,canvasDrop,canvasMove,canvasResize,canvasResetSize,canvasGroup,canvasEqualize,canvasUngroup,canvasRemove} from "./page-editor/canvas-layout";
import {addWidgetSlot} from "./page-editor/widget-slots";
import {collaborationRecordSource,requiresOriginalRecord} from "./page-editor/collaboration";
import {interfaceQueryForSection,pageLayoutDiagnostics} from "@platform/app";
import { recordPaths } from "../ontology/record-paths";
// Application Studio page design. Document history, UI selection
// and authorized runtime data have separate owners. Preview and operation use
// the same registered widgets; save and activation use the original Go path.
import { ApplicationPage, ComposedPage, SemanticObjectSelect, pageDocumentFromSections, pageUIProfile, pageVariableContract, widgetContract, widgetContracts, useHost, useProvidedCapabilities, useReadQuery, type PageVariableValue, type Definition } from "@platform/app";
import {
  ActionMenu, Button, CanvasEditor, CanvasRegion, DataTable, FlowLayout, InspectorSection, ProblemList, Workbench, type WorkbenchCrumb, type WorkbenchProblem, type WorkbenchTab, Input, MarkdownEditor, Panel, Select, Textarea, Toggles, humanizeKernelError, notify, t, useUnsavedChanges,
  type CanvasCommand, type CanvasDrop, type CanvasPayload, type EntityInfo,
} from "@platform/ui";
import { Copy, Clipboard, Columns2, Download, ExternalLink, Rows3, Group, Ungroup, Archive, ChevronUp, Settings2, Equal, RotateCcw, Monitor, MoreHorizontal, Play, Plus, Smartphone, Tablet, Trash2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { ModuleTree, type ModuleContext } from "./ModuleWorkbench";
import type { Route } from "@platform/ui";
import { WidgetLibrary } from "./page-editor/WidgetLibrary";
import { DraftStatus, ResourceControls, WorkbenchMessage, savingState, useAutoSave } from "../editor/workbench";
import {type Api as HostApi} from "@platform/kernel";
import { BindingEditor } from "../automate/workflow-binding";
import { variableAccessible, overlayOwner, loopOwner, synchronizeLoopBindings, addOverlay, removeOverlay, appendWidget, layoutID, stashWidget, restoreWidget, setLayoutKind } from "./page-layout";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { VariablesPanel, NodeBindings } from "./page-editor/VariablesPanel";
import { InterfacePanel } from "./page-editor/InterfacePanel";
import { widgetInspector,widgetBindingInspector } from "./page-editor/widgets/registry";
import {InspectorFrame} from "./page-editor/widgets/InspectorFrame";
import {CompatibilityReview} from "./page-editor/CompatibilityReview";
import {applyProfileUpgrade} from "./page-editor/compatibility";
import { OverlayProperties } from "./page-editor/OverlayPanel";
import {LayoutTree} from "./page-editor/LayoutTree";
import {LayoutProperties,LayoutSizing} from "./page-editor/LayoutProperties";
import { useDraftSession } from "../session/DraftSession";
import {copyLayout,copyLayoutIssue,pasteLayout,type LayoutClipboard,type ClipboardIssue} from "./page-editor/clipboard";
import {previewSection,type AuthoringSection,type PageDraft} from "./page-editor/draft";
import {ModuleImportDialog,type ImportPackage} from "./module-import/ModuleImportDialog";

type Api = NonNullable<Definition["page"]>;
/** The page as its record holds it: what the builder edits and submits. */
type PageRecord = {
  id: string; revision: number; name: string; title: string; description?: string; object: string; state: string;
  list?: string[]; detail?: string[]; actions?: string[];
  selections?: HostApi.SelectionVariable[];
  document?: HostApi.PageDocument;
  sections?: AuthoringSection[];
};
type Draft = NonNullable<PageRecord["sections"]>[number];


const widgets = widgetContracts.map((contract) => contract.componentID);
const widgetTitles: Record<string, () => string> = Object.fromEntries(widgetContracts.map((contract) => [contract.componentID, () => t(contract.title)]));

type StudioSelection = { kind: "page" | "variables" | "interface" | "queries" } | { kind: "widget" | "container"; id: string };
const emptyDraft = (): PageDraft => ({ sections: [], document: pageDocumentFromSections<Draft>([]).document, selections: [], title: "", description: "" });
const loadDraft = (record: PageRecord,lookup:(type:string)=>EntityInfo|undefined=()=>undefined, fields: HostApi.FieldInfo[] = []): PageDraft => {
  const shorthand: Draft[] = (record.list ?? []).length ? [
    { widget: "table", title: t("Table"), width: "half", fields: record.list },
    { widget: "detail", title: t("Detail"), width: "half", fields: record.detail },
    ...((record.actions ?? []).length ? [{ widget: "actions", title: t("Actions"), actions: record.actions }] : []),
  ] : [];
  const lifted = pageDocumentFromSections(record.sections?.length ? record.sections : shorthand);
  return { ...declarationValue(record, fields), sections: (record.document ? record.sections ?? [] : lifted.sections).map(s=>materializeModelSection(s,lookup,true)), document: record.document ?? lifted.document,
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
  sections: sections.map(previewSection),
});

/** The pages of this organisation: open one to compose it. */
export function PageEditor({ id, module }: { id: string; module?: ModuleContext }) {
  const { decide, source, catalog, definitions } = useHost();
  const { open } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: PageRecord }>(`/v1/records/build.page/${encodeURIComponent(id)}`);
  const model = useAuthoringModel(), authoringEntity = model.entity;
  const savedPage = query.data?.record;
  const declarationFields = source.entity("build.page")?.fields ?? [], trial = useActionTrial();
  const [sourceApplied, setSourceApplied] = useState(false);
  const session = useDraftSession<PageDraft,LayoutClipboard<Draft>>(emptyDraft(),JSON.stringify([id,source.scope]));
  const page = savedPage ? { ...savedPage, ...session.draft } : undefined;
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
  const [leftOpen, setLeftOpen] = useState<boolean>();
  const [inspectorTab, setInspectorTab] = useState<Record<string, string>>({});
  const [viewport, setViewport] = useState<"desktop" | "tablet" | "mobile">("desktop"), [zoom, setZoom] = useState(100);
  const [refused, setRefused] = useState<string>();
  const [saving, setSaving] = useState(false), [publishing, setPublishing] = useState(false);
  const lock = useRef(false), loaded = useRef(""), baseRevision = useRef(0);
  const busy = saving || publishing;
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    if (page) { session.load(loadDraft(savedPage!,authoringEntity,declarationFields)); baseRevision.current = page.revision; loaded.current = `${page.id}:${page.revision}`; }
    select({ kind: "page" }); setRefused(undefined);
  }, busy);
  useEffect(() => {
    if (!page || dirty || busy || loaded.current === `${page.id}:${page.revision}`) return;
    session.load(loadDraft(savedPage!,authoringEntity,declarationFields)); baseRevision.current = page.revision; loaded.current = `${page.id}:${page.revision}`;
  }, [page, dirty, busy, session.load]);
  useEffect(() => {
    if (selection.kind === "widget" && !sections.some((section) => section.id === selection.id) ||
      selection.kind === "container" && !document.nodes[selection.id]) select({ kind: "page" });
  }, [selection, sections, document]);
  const chosen = selection.kind === "widget" ? sections.findIndex((section) => section.id === selection.id) : selection.kind === "container" ? -2 : selection.kind === "variables" ? -3 : -1;
  const container = selection.kind === "container" ? selection.id : undefined;
  const choose = (index: number) => { select(index < 0 || !sections[index]?.id ? { kind: "page" } : { kind: "widget", id: sections[index]!.id! });  };
  // A native input event can already be in flight when autosave locks the UI.
  // The draft history preserves it when the older submission is acknowledged.
  const edit: typeof session.edit = (update, key) => { setSourceApplied(false); session.edit((old) => {
    const next = typeof update === "function" ? update(old) : { ...old, ...update };
    // Profile changes are explicit reviewed commands; ordinary editing retains
    // the original profile even when a layout helper returns the latest one.
    const document=key==="module-import"?next.document:{...next.document,uiProfile:old.document.uiProfile};
    const sections=next.sections.map(section=>{
      if(section.composite)return materializeModelSection(canonicalModelSection(section),authoringEntity,true);
      if(!section.model)return section;
      const previous=old.sections.find(s=>s.id===section.id);
      const canonical=JSON.stringify(previous?.model)!==JSON.stringify(section.model)?canonicalModelSection(section):foldModelPorts(section,authoringEntity);
      return materializeModelSection(canonical,authoringEntity,true);
    });
    return { ...next, ...synchronizeLoopBindings({...document,uiProfile:pageEditingProfile({...next,document,sections})}, sections) };
  }, key); };
  const history = (direction: "undo" | "redo") => { if (lock.current) return; session[direction](); setRefused(undefined); };
  const diagnostics = pageDraftDiagnostics(session.draft, { object: page?.object ?? "", entity: authoringEntity, catalog, definitions });
  const problemList: WorkbenchProblem[] = [
    ...diagnostics.map(issue => ({ id: issue.id, text: t(issue.code, issue.values), subject: issue.subject, locate: () => {
      const target = issue.target;
      if (target.kind === "compatibility") setCompatibilityOpen(true);
      else if (target.kind === "node") canvasSelect(target.id);
      else if (target.kind === "widget") { const at = sections.findIndex(section => section.id === target.id); if (at >= 0) choose(at); }
      else select({ kind: target.kind });
    } })),
    ...Object.entries(session.inputProblems).filter(([, problem]) => problem).map(([id, problem]) => ({ id: `form:${id}`, text: problem, subject: id })),
  ];
  const invalid = problemList.length > 0;
  const incompatible = diagnostics.some(issue => issue.target.kind === "compatibility");
  const relatedObjects = model.entities.filter(entity => entity.type !== page?.object)
    .filter(entity => entity.fields.some(field => field.type === "reference" && [page?.object, ...selections.map(selection => selection.object.name)].includes(field.ref))).map(entity => entity.type);
  const [queryPreviewOwner,setQueryPreviewOwner]=useState<string|undefined>(undefined);
  const [variableValues, setVariableValues] = useState<Record<string, PageVariableValue>>({});
  const saveRef = useRef<() => Promise<unknown>>(async () => false);
  useAutoSave({ enabled: !sourceApplied, generation:JSON.stringify(session.draft), dirty, invalid, busy, save: () => saveRef.current() });
  if (!page) return <Workbench storageKey="page" title={t("Page")}><WorkbenchMessage>{query.isError ? t("The page could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const info = authoringEntity(page.object);
const change = (index: number, patch: Partial<Draft>) => edit((old) => ({ ...old, sections: old.sections.map((s, at) => at === index ? { ...s, ...patch } : s) }), `widget:${sections[index]?.id}:${Object.keys(patch).join(",")}`);
  const add = (widget: string, destination?: { container: string; after?: string }, drop?:CanvasDrop) => {
    const contract = widgetContract(widget); if (!contract) return;
    const defaults = JSON.parse(JSON.stringify(contract.defaults)) as Partial<Draft>;
    if(contract.bindingKinds.some(k=>k==="composite"))defaults.composite={bindings:{}};
    if(widget==="record-calendar")defaults.recordCalendar={dateField:"",labelField:"id",initialMonth:new Date().toISOString().slice(0,7)};
    const section: Draft = { ...defaults,...contract.bindingKinds.some(k=>k==="object")?{model:{type:page.object}}:{}, id: layoutID("section"), configVersion: contract.configVersion, widget, title: t(contract.title) };
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
      const payload={...submitted,document:{...submitted.document,uiProfile:pageEditingProfile(submitted)},sections:submitted.sections.map(canonicalModelSection)};
      const ok = await decide("build.page.edit", { type: "build.page", id }, declarationChanges(payload, declarationValue(savedPage!, declarationFields)), { expectedRevision, onRefused: setRefused });
      if (ok) {
        const result = await query.refetch();
        const confirmed = result.isSuccess && result.data?.record?.revision === expectedRevision + 1 ? result.data.record : undefined;
        baseRevision.current = expectedRevision + 1; loaded.current = `${id}:${baseRevision.current}`;
        session.saved(submitted, confirmed ? loadDraft(confirmed,authoringEntity,declarationFields) : undefined); markSaved();
      }
      return ok;
    } catch { setRefused(t("The page could not be saved. Your draft is still here.")); return false; }
    finally { lock.current = false; setSaving(false); }
  };
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
    commands.push({id:'delete',label:t(node.kind==='widget'?'Delete widget':'Delete layout'),icon:<Trash2/>,primary:true,danger:true,separatorBefore:true,disabled:root||!!node.slot,run:()=>{const result=canvasRemove(document,id);if(result){for(const [removed,node] of Object.entries(document.nodes))if(!result.document.nodes[removed]){session.inputs.clear(`${session.inputs.scope}/container:${removed}/`);session.inputs.clear(`${session.inputs.scope}/layout:${removed}/`);if(node.section)session.inputs.clear(`${session.inputs.scope}/widget:${node.section}/`);}edit(old=>({...old,document:result.document,sections:old.sections.filter(s=>!result.sections.has(s.id??''))}));select({kind:'page'});}}});
    return commands;
  };
  const selectionCommands=nodeID?canvasCommands(nodeID):[];
  const selectionCommand=(name:string)=>selectionCommands.find(command=>command.id===name);
  const runSelectionCommand=(name:string)=>{const command=selectionCommand(name);if(command&&!command.disabled&&!busy&&!incompatible)command.run();};
  const canvasRoute: Route = module ? { view: "module", params: { id: module.project.id, application: module.project.id, page: id } } : { view: "module", params: { page: id } };
  const objectRef = { app: page.object.split(".")[0]!, kind: "object", name: page.object };
  const overlayEntries = container ? Object.entries(document.overlays ?? {}).filter(([, overlay]) => overlay.root === container) : [];
  // --- Inspector tabs follow what is in hand: the page, a layout, or a widget.
  const pageTabs: WorkbenchTab[] = [
    { id: "page", title: t("Page"), content: <Settings value={{ title, description }} object={info?.title ?? page.object} device={document.device ?? ""} onDevice={(device) => edit({ document: { ...document, device: device || undefined } }, "settings:device")}
      selections={selections} objects={definitions.filter((d) => d.ref.kind === "object" && d.entity).map((d) => d.ref).sort((a, b) => Number(b.name === page.object) - Number(a.name === page.object))}
      onSelections={(next, rename) => edit((old) => ({ ...old, selections: next, sections: rename ? old.sections.map((s) => ({ ...s, selection: s.selection === rename.from ? rename.to : s.selection, parentSelection: s.parentSelection === rename.from ? rename.to : s.parentSelection })) : old.sections }))}
      onChange={(patch) => edit(patch, `settings:${Object.keys(patch).join(",")}`)} /> },
    { id: "variables", title: t("Variables"), badge: Object.keys(document.variables ?? {}).length || undefined, content: <VariablesPanel object={objectRef} entity={authoringEntity} document={document} sections={sections} values={variableValues} onChange={(document) => edit({ document })} /> },
    { id: "queries", title: t("Queries"), badge: Object.keys(document.queries ?? {}).length || undefined, content: <QueriesPanel sections={sections} onPreviewOwner={setQueryPreviewOwner} document={document} object={objectRef} values={variableValues} onChange={(document) => edit({ document })} /> },
    { id: "interface", title: t("Inputs and outputs"), content: <InterfacePanel document={document} object={objectRef} onChange={(document) => edit({ document })} /> },
  ];
  const containerTabs: WorkbenchTab[] = container ? [
    { id: "layout", title: t("Layout"), content: <div className="grid content-start">
      {overlayEntries.map(([id, overlay]) => <OverlayProperties key={id} overlay={overlay}
        onChange={(patch) => edit({ document: { ...document, overlays: { ...document.overlays, [id]: { ...overlay, ...patch } } } })}
        onRemove={() => { const result = removeOverlay(document, id);for(const removed of result.sections)session.inputs.clear(`${session.inputs.scope}/widget:${removed}/`);edit({ document: result.document, sections: sections.filter((section) => !result.sections.has(section.id!)) }); select({ kind: "page" }); }} />)}
      <InputDraftProvider scope="layout" reveal={()=>select(selection)}><LayoutProperties sections={sections} document={document} id={container}
        onPatch={patchNode} onChange={(kind) => edit((old) => ({ ...old, document: setLayoutKind(old.document, container, kind) }))}
        onUngroup={() => canvasCommands(container).find((command) => command.id === "ungroup")?.run()} ungroupDisabled={!canvasUngroup(document, container, sections)} /></InputDraftProvider>
    </div> },
    { id: "appearance", title: t("Appearance"), content: <div className="grid content-start">
      <InputDraftProvider scope="layout" reveal={()=>select(selection)}><LayoutSizing document={document} id={container} onPatch={patchNode} /></InputDraftProvider>
      <NodeBindings document={document} id={container} button={false} input={false} onChange={(patch) => patchNode(container, patch)} />
    </div> },
  ] : [];
  const widgetTabs: WorkbenchTab[] = canvasSelection ? widgetInspectorTabs({
    section: canvasSelection, sections, document, info: modelEntity(authoringEntity(canvasSelection.model?.type||canvasSelection.object || page.object),canvasSelection.model,authoringEntity), object: page.object, selections, relatedObjects,
    catalog: model.actions, onChange: (patch) => change(chosen, patch),
    onResourcesChange: (patch, variables) => edit((old) => ({ ...old, document: { ...old.document, variables }, sections: old.sections.map((s) => s.id === canvasSelection.id ? { ...s, ...patch } : s) }), "graph-resources"),
    events: (() => { const Inspector = widgetInspector(canvasSelection.widget, canvasSelection.configVersion ?? 0)?.events; return <InspectorFrame id={canvasSelection.id ?? String(chosen)} widget={canvasSelection.widget} version={canvasSelection.configVersion ?? 0} part="events">{Inspector && <Inspector buttons={canvasSelection.buttons} onGroupChange={(buttons, document) => edit({ document, sections: sections.map((s) => s.id === canvasSelection.id ? { ...s, buttons } : s) })} document={document} section={canvasSelection.id!} owner={nodeID ? loopOwner(document, nodeID) : undefined} overlay={nodeID ? overlayOwner(document, nodeID) : undefined} onChange={(document) => edit({ document })} />}</InspectorFrame>; })(),
    appearance: <>
      {Object.keys(document.overlays ?? {}).length > 0 && <InspectorSection title={t("Placement")}><label className="grid gap-1 text-xs">{t("Move widget to")}<Select value="" onChange={(event) => { if (event.target.value && nodeID) canvasCommitDrop({ kind: "move", id: nodeID, label: canvasSelection.title ?? nodeID }, { kind: "into", parent: event.target.value }); }}><option value="">{t("Choose a layout root")}</option><option value={document.root}>{t("Main page")}</option>{Object.entries(document.overlays ?? {}).map(([id, overlay]) => <option key={id} value={overlay.root}>{overlay.title}</option>)}</Select></label></InspectorSection>}
      {nodeID && <InputDraftProvider scope="layout" reveal={()=>select(selection)}><LayoutSizing document={document} id={nodeID} onPatch={patchNode} /></InputDraftProvider>}
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
  return <>
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
        <ResourceControls type="build.page" record={page} dirty={dirty} busy={busy} invalid={invalid} empty={nothing} onSave={() => void save()} onReview={() => void review()} onInstall={() => void publish()} onCancel={discardChanges} route={canvasRoute} />
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
      left={{ label: t("Page structure"), open: leftOpen, onOpenChange: setLeftOpen, value: leftTab, onChange: setLeftTab, tabs: [
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
      right={preview ? undefined : { label: t("Inspector"), heading: <span className="flex min-w-0 items-center gap-1.5">{canvasSelection ? <WidgetGlyph widget={canvasSelection.widget} /> : container ? <LayoutGlyph kind={document.nodes[container]?.kind ?? "rows"} /> : <Settings2 className="size-3.5 shrink-0" />}<span className="truncate" title={canvasSelection ? canvasSelection.title || widgetTitles[canvasSelection.widget]?.() : container ? t("Layout") : t("Page")}>{canvasSelection ? canvasSelection.title || widgetTitles[canvasSelection.widget]?.() : container ? t("Layout") : t("Page")}</span></span>, scope:inspectorKey,locate:()=>select(selection), value: inspectorValue, onChange: (next) => { if (["variables", "queries", "interface"].includes(next) && !canvasSelection && !container) select({ kind: next as "variables" | "queries" | "interface" }); else if (next === "page") select({ kind: "page" }); setInspectorTab((old) => ({ ...old, [inspectorKey.split(":")[0]!]: next })); },
        tabs: inspectorTabs }}
      dock={{ label: t("Page dock"), tabs: [
        { id: "problems", title: t("Problems"), badge: problemList.length, content: <ProblemList problems={problemList} empty={t("No problems. The page can be published.")} /> },
        { id: "variables", title: t("Variables"), badge: liveValues.length || undefined, content: <DataTable data={liveValues} getRowId={([name]) => name} searchable={false} height="100%" empty={t("Variables show their live values here once the page renders.")} columns={[
          { id: "name", header: t("Variable"), accessorFn: ([name]) => document.variables?.[name]?.title || name, enableSorting: false },
          { id: "type", header: t("Type"), accessorFn: ([name]) => document.variables?.[name]?.type, enableSorting: false },
          { id: "value", header: t("Current value"), accessorFn: ([, value]) => JSON.stringify(value), enableSorting: false },
        ]} /> },
      ] }}>
      {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {!source.entity(page.object) && info && <p className="px-3 pt-2 text-xs text-warning" role="status">{t("Field choices come from a saved object draft. Business data is available after joint activation.")}</p>}
      {importing && <ModuleImportDialog key={clipboardScope} open retained={importPackage?.scope === clipboardScope ? importPackage.pack : undefined} object={page.object} profile={pageUIProfile} onClose={() => setImporting(false)} onApply={(draft, pack) => { setImportPackage({ scope: clipboardScope, pack }); edit(draft, "module-import"); select({ kind: "page" }); setRefused(undefined); }} />}
      {compatibilityOpen && <CompatibilityReview key={clipboardScope} draft={session.draft} busy={busy} onClose={() => setCompatibilityOpen(false)} onLocate={(id) => { select({ kind: "widget", id }); setCompatibilityOpen(false); }} onApply={(review) => { if (lock.current) return; const next = applyProfileUpgrade(session.draft, review); if (!next) return; session.edit(next, "profile-upgrade"); setCompatibilityOpen(false); setRefused(undefined); }} />}
      <StructuredEditor initialMode="source" validate={widgetDeclarationIssues} expectedRevision={baseRevision.current} fieldInput={sourceFieldInput} value={declarationValue({ ...session.draft, sections: sections.map(canonicalModelSection) }, declarationFields)} fields={declarationFields} scope={`build.page:${id}:${source.scope}`} disabled={busy || incompatible} onChange={next => { setSourceApplied(true); session.edit(() => ({ ...emptyDraft(), ...next }) as PageDraft); select({ kind: "page" }); }} onTrial={next => trial("build.page.edit", { type: "build.page", id }, declarationChanges(next, declarationValue(savedPage!, declarationFields)), baseRevision.current)}><fieldset disabled={busy || incompatible} className="flex min-h-0 min-w-0 flex-1 flex-col">
        <CanvasEditor model={canvasModel(document, sections, { ...Object.fromEntries(Object.entries(widgetTitles).map(([id, title]) => [id, title()])), rows: t("Rows"), columns: t("Columns"), tabs: t("Tabs"), flow: t("Flow layout"), toolbar: t("Toolbar"), loop: t("Loop") })} selected={preview ? undefined : nodeID} revision={session.draft} zoom={zoom / 100} disabled={busy || incompatible || preview}
          commands={selectionCommands} icon={canvasSelection ? <WidgetGlyph widget={canvasSelection.widget} /> : <LayoutGlyph kind={document.nodes[nodeID ?? ""]?.kind ?? "rows"} />}
          onSelect={canvasSelect} onDrop={canvasCommitDrop} onMove={(id, delta) => canvasEdit(canvasMove(document, id, delta, sections))}
          resize={(id, axis, pixels, rects) => canvasResize(document, id, axis, pixels, rects)} onResize={canvasCommitSizes}
          onResetSize={(id, axis) => { const sizes = canvasResetSize(document, id, axis); if (sizes) canvasCommitSizes(sizes); }}>
          <div className="shrink-0 border-b border-border px-2 py-2">
            <FlowLayout toolbar label={t("Canvas tools")}>
              <div role="group" aria-label={t("Selection commands")} className="flex shrink-0 items-center gap-1">
                {([["duplicate", "Duplicate selection", Copy], ["copy", "Copy selection", Copy], ["paste", "Paste into layout", Clipboard]] as const).map(([cid, label, Icon]) => <Button key={cid} size="sm" variant="ghost" title={t(label)} aria-label={t(label)} disabled={busy || incompatible || preview || !selectionCommand(cid) || selectionCommand(cid)?.disabled} onClick={() => runSelectionCommand(cid)}><Icon /></Button>)}
              </div>
              <div role="group" aria-label={t("Layout commands")} className="flex shrink-0 items-center gap-1">
                {(["rows", "columns", "tabs"] as const).map((kind) => <Button key={kind} size="sm" variant="ghost" disabled={!nodeID || preview} title={t(kind === "rows" ? "Group in rows" : kind === "columns" ? "Group in columns" : "Group in tabs")} aria-label={t(kind === "rows" ? "Group in rows" : kind === "columns" ? "Group in columns" : "Group in tabs")} onClick={() => { if (!nodeID) return; const result = canvasGroup(document, nodeID, kind, sections); if (result) { canvasEdit(result.document); select({ kind: "container", id: result.id }); } }}>{kind === "rows" ? <Rows3 /> : kind === "columns" ? <Columns2 /> : <Group />}</Button>)}
              </div>
              <div role="group" aria-label={t("Preview controls")} className="ml-auto flex shrink-0 items-center gap-1">
                {([["desktop", "Desktop preview", Monitor], ["tablet", "Tablet preview", Tablet], ["mobile", "Mobile preview", Smartphone]] as const).map(([device, label, Icon]) => <Button key={device} size="sm" variant="ghost" title={t(label)} aria-label={t(label)} aria-pressed={viewport === device} onClick={() => setViewport(device)}><Icon /></Button>)}
                <Select aria-label={t("Canvas zoom")} value={zoom} onChange={(event) => setZoom(Number(event.target.value))} className="w-20">{[50, 75, 100, 125].map((value) => <option key={value} value={value}>{value}%</option>)}</Select>
              </div>
            </FlowLayout>
            {clipboardNotice?.scope === clipboardScope && <p role={clipboardNotice.error ? "alert" : "status"} className={"mt-2 break-words text-xs " + (clipboardNotice.error ? "text-danger" : "text-muted")}>{clipboardNotice.text}</p>}
          </div>
          <div data-canvas-scroll className="min-h-[24rem] flex-1 overflow-auto bg-canvas p-4">
            <div className="mx-auto" style={{ width: viewport === "desktop" ? "100%" : viewport === "tablet" ? 768 : 390, zoom: zoom / 100 }}>
              <ApplicationPage pageRef={{ app: "build", kind: "page", name: page.name }} applicationRef={module ? { app: "build", kind: "app", name: module.project.name } : undefined} route={canvasRoute} preview><ComposedPage modelDraft editingRoot={preview ? undefined : (() => {
                if (selection.kind === "queries") return queryPreviewOwner ? document.overlays?.[queryPreviewOwner]?.root : undefined;
                const node = container ?? (canvasSelection?.id ? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection.id)?.[0] : undefined);
                return Object.values(document.overlays ?? {}).find((overlay) => {
                  const includes = (id: string): boolean => id === node || [...(document.nodes[id]?.children ?? []), ...(document.unusedWidgets ?? []).filter((entry) => entry.parent === id).map((entry) => entry.node)].some(includes);
                  return includes(overlay.root);
                })?.root;
              })()} onVariableValues={setVariableValues} page={asPage({ ...page, title, description, selections }, sections, document)} live={preview} chosen={preview ? -1 : chosen} onChoose={preview ? () => {} : choose}
                notice={nothing && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted"><p>{t("This page is empty.")}</p><p className="mt-1 text-xs">{t("Drag a widget from the library, or pick one to add it here.")}</p><Button className="mt-3" size="sm" onClick={() => { setPreview(false); setLeftTab("widgets"); setLeftOpen(true); }}><Plus />{t("Add widget")}</Button></div>}
                wrapLayout={preview ? undefined : (id, _node, body) => <CanvasRegion key={id} id={id}>{body}</CanvasRegion>} /></ApplicationPage>
            </div>
          </div>
          <div className="flex items-center gap-2 border-t border-border px-3 py-1 text-[11px] text-muted" role="status">{preview ? t("Preview: your records as they are; actions run for real.") : t("Design: your records as they are. Actions do not run while you compose.")}</div>
        </CanvasEditor>
      </fieldset></StructuredEditor>
    </Workbench>
  </>;
}

/** The panel that configures the widget in hand: only what that widget binds. */
type PropertiesProps = {
  section?: Draft; sections:Draft[]; document: HostApi.PageDocument; info?: EntityInfo; object: string; relatedObjects?: string[];
  selections: HostApi.SelectionVariable[];
  catalog: { schema: string; title: string; target: string; draft?: boolean }[];
  onChange: (patch: Partial<Draft>) => void;
  onResourcesChange:(patch:Partial<Draft>,variables:Record<string,HostApi.PageVariable>)=>void;
};

/** Inspector navigation is local UI; every panel edits the same DraftSession. */
/** The inspector tabs of the widget in hand: Properties, Data, Events, Appearance. */
function widgetInspectorTabs(props: PropertiesProps & { section: Draft; events: ReactNode; appearance: ReactNode }): WorkbenchTab[] {
  const { section, sections, document, info, object, onChange, onResourcesChange } = props;
  const leaf = Object.entries(document.nodes).find(([, node]) => node.section === section.id)?.[0];
  const overlay = leaf ? overlayOwner(document, leaf) : undefined;
  const Inspector = widgetBindingInspector(section.widget, section.configVersion ?? 0);
  const contract = widgetContract(section.widget);
  return [
    { id: "properties", title: t("Properties"), content: <div className="grid content-start">
      <InspectorSection title={t("General")}>
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
      </InspectorSection>
      {Inspector && <InspectorSection title={t("Widget settings")}>
        <InspectorFrame id={section.id ?? "selected"} widget={section.widget} version={section.configVersion ?? 0} part="bindings">
          <Inspector section={section} document={document} object={object} info={info} overlay={overlay}
            itemOwner={leaf ? loopOwner(document, leaf) : undefined} sections={sections}
            onResourcesChange={onResourcesChange} onChange={onChange} />
        </InspectorFrame>
      </InspectorSection>}
    </div> },
    { id: "data", title: t("Data"), content: <div className="grid content-start"><Properties {...props} /></div> },
    ...(contract && "events" in contract && contract.events.length ? [{ id: "events", title: t("Events"), badge: document.events?.filter((binding) => binding.source === section.id).length || undefined, content: <div className="grid content-start gap-2 p-2">{props.events}</div> }] : []),
    { id: "appearance", title: t("Appearance"), content: <div className="grid content-start">{props.appearance}</div> },
  ];
}

function Properties({ section, sections, document, info, catalog, object, selections, relatedObjects = [], onChange }: PropertiesProps) {
  const { definitions } = useHost();
  const model = useAuthoringModel();
  const capabilityQuery = useProvidedCapabilities();
  const provided = capabilityQuery.data ?? [];
  const computations = provided.filter(item => item.ref.kind === "compute" && item.capability.kind === "operation" && item.capability.side === "pure").map(item => ({
    binding: { ref: item.ref, sourceVersion: item.version }, name: item.ref.name,
    title: definitions.find(definition => definition.ref.app === item.ref.app && definition.ref.name === item.ref.name && definition.ref.kind === "compute")?.operation?.title ?? item.capability.name,
    input: item.capability.inputs,
  }));
  const functions = provided.filter(item => item.ref.app === "build" && item.capability.kind === "function").flatMap(item => {
    const definition = definitions.find(definition => definition.ref.app === item.ref.app && definition.ref.kind === "function" && definition.ref.name === item.ref.name && definition.version === item.version);
    const version = Number(item.version.match(/\.function-(\d+)$/)?.[1]);
    return definition?.function?.object === object && version > 0 ? [{ name: item.ref.name, title: definition.function.title, version }] : [];
  });
  const queriesOf = (obj: string) => definitions
    .filter(definition => definition.query?.object === obj && provided.some(item => item.capability.kind === "query" && item.capability.side === "pure" && item.ref.app === definition.ref.app && item.ref.name === definition.ref.name && item.version === definition.version))
    .map(definition => ({ key: `${definition.ref.app}.${definition.ref.name}`, title: definition.query?.title ?? definition.ref.name }));
  if (!section) return <p className="p-3 text-xs text-muted">{t("Choose a section to configure it.")}</p>;
  const contract = widgetContract(section.widget);
  const allows = (kind: string) => contract?.bindingKinds.some((binding) => binding === kind) ?? false;
  const fields = info?.fields ?? [];
  const parentType = section.parentSelection ? selections.find((selection) => selection.name === section.parentSelection)?.object.name ?? "" : object;
  const relations = fields.filter((field) => field.type === "reference" && field.ref === parentType && field.inverse).map((field) => field.inverse!);
  const actions = catalog.filter(action => action.target === (section.object || object) && (action.draft || provided.some(item => item.capability.kind === "action" && item.capability.side === "authoritative" && item.capability.name === action.schema)));
  const leaf=Object.entries(document.nodes).find(([,n])=>n.section===section.id)?.[0],overlay=leaf?overlayOwner(document,leaf):undefined;
  const accessible=(v:HostApi.PageVariable)=>variableAccessible(v,undefined,overlay);
  const measures = ["count", ...fields.filter((f) => f.type === "integer" || f.type === "decimal" || f.type === "money").flatMap((f) => [`sum:${f.name}`, `avg:${f.name}`])];
  return (
    <InspectorSection title={t("Data bindings")}>
      {capabilityQuery.isError && <p role="alert" className="text-xs text-danger">{t("Available capabilities could not be loaded.")}</p>}
      {allows("composite")&&<CompositeBindingInspector section={section} sections={sections} document={document} object={object} selections={selections} lookup={model.entity} definitions={definitions} onChange={onChange}/>}
      {allows("object")&&<ModelBindingInspector section={section} sections={sections} document={document} object={object} selections={selections} lookup={type=>type===info?.type?info:model.entity(type)} definitions={definitions} onChange={onChange}/>}
      {!section.model&&relatedObjects.length > 0 && allows("object") && !interfaceQueryForSection({document,sections},section) && (
        <label className="grid gap-1 text-xs">{t("Object")}
          <SemanticObjectSelect label={t("Object")} value={section.object ?? object} drafts={model.drafts.data?.records.filter(draft => !draft.archived && (`build.${draft.name}` === object || relatedObjects.includes(`build.${draft.name}`))).map(draft => ({ name: `build.${draft.name}`, title: draft.title }))} filter={(definition) => definition.ref.name === object || relatedObjects.includes(definition.ref.name)}
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
      {!section.model&&(selections.length > 0 || section.parentSelection) && section.object && relatedObjects.includes(section.object) &&
        !section.collectionVariable && allows("relation") &&
        <label className="grid gap-1 text-xs">{t("Parent selection")}
          <Select value={section.parentSelection ?? ""} onChange={(e) => { const nextType = e.target.value ? selections.find((selection) => selection.name === e.target.value)?.object.name : object; onChange({ parentSelection: e.target.value || undefined, ...(nextType !== parentType ? { relation: undefined, inputs: undefined } : {}) }); }}>
            <option value="">{t("Page's shared selection")}</option>
            {section.parentSelection && !selections.some((v) => v.name === section.parentSelection && fields.some((field) => field.ref === v.object.name)) &&
              <option value={section.parentSelection}>{t("Unavailable selection: {name}", { name: section.parentSelection })}</option>}
            {selections.filter((v) => fields.some((field) => field.type === "reference" && field.ref === v.object.name)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {!section.model&&section.object && relations.length > 0 && !section.collectionVariable && allows("relation") && (
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
      {!section.model&&!section.collectionVariable && allows("query") && queriesOf(section.object || object).length > 0 && (
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
            {section.function && !functions.some(fn => fn.name === section.function!.name && fn.version === section.function!.version) && <option disabled value={`${section.function.name}:${section.function.version}`}>{t("Retained binding")}: {section.function.name} · {section.function.version}</option>}
            {functions.map((f) => <option key={`${f.name}:${f.version}`} value={`${f.name}:${f.version}`}>{f.title} · {t("Version")} {f.version}</option>)}
          </Select></label>
        <p className="text-xs text-muted">{t("The selected record supplies the function input. Its typed answer stays in a separate call record.")}</p>
      </>}
      {section.widget === "compute" && <>
        <label className="grid gap-1 text-xs">{t("Published code function version")}
          <Select value={section.operation ? `${section.operation.ref.app}/${section.operation.ref.name}:${section.operation.sourceVersion}` : ""} onChange={(event) => {
            const selected = computations.find((item) => `${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}` === event.target.value);
            onChange({ operation: selected?.binding, inputs: undefined });
          }}><option value="">{t("Choose a published code function")}</option>{section.operation && !computations.some(item => item.name === section.operation!.ref.name && item.binding.ref.app === section.operation!.ref.app && item.binding.sourceVersion === section.operation!.sourceVersion) && <option disabled value={`${section.operation.ref.app}/${section.operation.ref.name}:${section.operation.sourceVersion}`}>{t("Retained binding")}: {section.operation.ref.name} · {section.operation.sourceVersion}</option>}{computations.map((item) => <option key={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`} value={`${item.binding.ref.app}/${item.name}:${item.binding.sourceVersion}`}>{item.title} · {item.binding.ref.app} · {item.binding.sourceVersion}</option>)}</Select>
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
        parentType={section.relation ? parentType : undefined} entity={model.entity} inputs={section.inputs ?? {}} onChange={(inputs, field, bound) => onChange({ inputs,
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
    </InspectorSection>
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
    <div className="grid content-start">
      <InspectorSection title={t("Page settings")}>
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
      </InspectorSection>
      <InspectorSection title={t("Record selections")}>
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
      </InspectorSection>
    </div>
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
