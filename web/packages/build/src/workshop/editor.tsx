import { useWorkingState } from "../session/WorkingState";
import { decodePage, encodePage, pageAuthoringIssues } from "@platform/app/page-contract";
import { pageEditingProfile } from "./page-editor/compatibility";
import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue, declarationChanges } from "../editor/declaration";
import { pageDraftDiagnostics } from "./page-editor/diagnostics";
import { materializeModelSection, canonicalModelSection, foldModelPorts, modelEntity } from "@platform/app/model";
import { InputDraftProvider } from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useAuthoringModel } from "../ontology/authoring-model";
import { WidgetGlyph, LayoutGlyph } from "./page-editor/WidgetGlyph";
import { canvasModel, canvasDrop, canvasMove, canvasResize, canvasResetSize, canvasGroup, canvasEqualize, canvasUngroup, canvasRemove } from "./page-editor/canvas-layout";
import { addWidgetSlot } from "./page-editor/widget-slots";
import { pageLayoutDiagnostics } from "@platform/app";
import { ApplicationPage, ComposedPage, pageDocumentFromSections, pageVariableContract, widgetContract, widgetContracts, useHost, type PageVariableValue, type Definition } from "@platform/app";
import { Button, CanvasEditor, CanvasRegion, DataTable, FlowLayout, InspectorSection, ProblemList, Workbench, type WorkbenchCrumb, type WorkbenchProblem, type WorkbenchTab, Panel, SegmentedChoice, Select, humanizeKernelError, notify, t, useUnsavedChanges, type CanvasCommand, type CanvasDrop, type CanvasPayload, type EntityInfo } from "@platform/ui";
import { Copy, Clipboard, Columns2, ExternalLink, Rows3, Group, Ungroup, Archive, ChevronUp, Settings2, Equal, RotateCcw, Monitor, PenLine, Play, Plus, Smartphone, Tablet, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ModuleTree, type ModuleContext } from "./ModuleWorkbench";
import type { Route } from "@platform/ui";
import { WidgetLibrary } from "./page-editor/WidgetLibrary";
import { DraftStatus, ResourceControls, WorkbenchMessage } from "../editor/workbench";
import type { Api as HostApi } from "@platform/kernel";
import { overlayOwner, loopOwner, synchronizeLoopBindings, addOverlay, removeOverlay, appendWidget, layoutID, stashWidget, restoreWidget, setLayoutKind } from "./page-layout";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { VariablesPanel, NodeBindings } from "./page-editor/VariablesPanel";
import { InterfacePanel } from "./page-editor/InterfacePanel";
import { widgetInspector } from "./page-editor/widgets/registry";
import { InspectorFrame } from "./page-editor/widgets/InspectorFrame";
import { CompatibilityReview } from "./page-editor/CompatibilityReview";
import { applyProfileUpgrade } from "./page-editor/compatibility";
import { OverlayProperties } from "./page-editor/OverlayPanel";
import { LayoutTree } from "./page-editor/LayoutTree";
import { LayoutProperties, LayoutSizing } from "./page-editor/LayoutProperties";
import { useDraftSession } from "../session/DraftSession";
import { useDraftRecord } from "../branches/draft-record";
import { copyLayout, copyLayoutIssue, pasteLayout, type LayoutClipboard, type ClipboardIssue } from "./page-editor/clipboard";
import { previewSection, authoringSection, type AuthoringSection, type PageDraft } from "./page-editor/draft";
import type { PageUI } from "@platform/app";
import {Settings, widgetInspectorTabs} from "./PageProperties";

type Api = NonNullable<Definition["page"]>;

type PageRecord = Partial<Omit<HostApi.Page,"object"|"sections"|"document"|"actions">>&{
  id: string; revision: number; name: string; title: string; description?: string; object: string; state: string;
  list?: string[]; detail?: string[]; actions?: string[];
  selections?: HostApi.SelectionVariable[];
  document?: PageUI.PageDocument;
  sections?: AuthoringSection[];
};
export type Draft = NonNullable<PageRecord["sections"]>[number];

const widgets = widgetContracts.map((contract) => contract.componentID);
const widgetTitles: Record<string, () => string> = Object.fromEntries(widgetContracts.map((contract) => [contract.componentID, () => t(contract.title)]));

type StudioSelection = { kind: "page" | "interface" } | { kind: "widget" | "container"; id: string };
const emptyDraft = (): PageDraft => ({ sections: [], document: pageDocumentFromSections<Draft>([]).document, selections: [], title: "", description: "" });
const loadDraft = (record: PageRecord,lookup:(type:string)=>EntityInfo|undefined=()=>undefined, fields: HostApi.FieldInfo[] = []): PageDraft => {
  if(record.format===3||record.format===4){
    const page=decodePage({...record,object:{app:record.object.split(".")[0],kind:"object",name:record.object}} as HostApi.Page);
    const lifted=pageDocumentFromSections((page.sections??[]).map(authoringSection));
    return {...declarationValue(record,fields),actions:page.actions.map(action=>action.name),sections:lifted.sections.map(section=>materializeModelSection(section,lookup,true)),document:page.document??lifted.document,selections:page.selections??[],title:record.title,description:record.description??""};
  }
  const shorthand: Draft[] = (record.list ?? []).length ? [
    { widget: "table", title: t("Table"), width: "half", fields: record.list },
    { widget: "detail", title: t("Detail"), width: "half", fields: record.detail },
    ...((record.actions ?? []).length ? [{ widget: "actions", title: t("Actions"), actions: record.actions }] : []),
  ] : [];
  const lifted = pageDocumentFromSections(record.sections?.length ? record.sections : shorthand);
  return { ...declarationValue(record, fields), sections: (record.document ? record.sections ?? [] : lifted.sections).map(s=>materializeModelSection(s,lookup,true)), document: record.document ?? lifted.document,
    selections: record.selections ?? [], title: record.title, description: record.description ?? "" };
};
export const filterable = ["choice", "boolean", "reference"];

const asPage = (record: PageRecord, sections: Draft[], document?: PageUI.PageDocument): Api => ({
  name: record.name, title: record.title, description: record.description, layout: "composed",
  object: { app: record.object.split(".")[0] ?? "", kind: "object", name: record.object },
  listFields: [], detailFields: [], actions: [],
  selections: record.selections,
  document,
  sections: sections.map(previewSection),
});

export function PageEditor({ id, module }: { id: string; module?: ModuleContext }) {
  const { decide, source, catalog, definitions } = useHost();
  const { open } = useApplicationWorkspace();
  const { query, scope, branch, mainRevision } = useDraftRecord<PageRecord>("build.page", id);
  const model = useAuthoringModel(), authoringEntity = model.entity;
  const savedPage = query.data?.record;
  const declarationFields = source.entity("build.page")?.fields ?? [], trial = useActionTrial();
  const [sourceApplied, setSourceApplied] = useState(false);
  const session = useDraftSession<PageDraft,LayoutClipboard<Draft>>(emptyDraft(),JSON.stringify([id,source.scope,scope]));
  const page = savedPage ? { ...savedPage, ...session.draft } : undefined;
  const [clipboardNotice,setClipboardNotice]=useState<{scope:string;error?:boolean;text:string}>();
  const clipboardScope=JSON.stringify([id,source.scope]);
  useEffect(()=>{setClipboardNotice(undefined);setCompatibilityOpen(false);},[clipboardScope]);
  const { sections, document, selections, title, description } = session.draft;
  const { dirty } = session;
  const [selection, select] = useState<StudioSelection>({ kind: "page" });
  const [compatibilityOpen,setCompatibilityOpen]=useState(false);
  const [leftTab, setLeftTab] = useState("layers"), [preview, setPreview] = useState(false);
  const [leftOpen, setLeftOpen] = useState<boolean>();
  const [inspectorTab, setInspectorTab] = useState<Record<string, string>>({});
  const [viewport, setViewport] = useState<"desktop" | "tablet" | "mobile">("desktop"), [zoom, setZoom] = useState(100);
  const [refused, setRefused] = useState<string>();
  const [saving, setSaving] = useState(false);
  const lock = useRef(false), loaded = useRef(""), baseRevision = useRef(0);
  const busy = saving;
  const working=useWorkingState();
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    void working?.clear();
    if (page) { session.load(loadDraft(savedPage!,authoringEntity,declarationFields)); baseRevision.current = page.revision; loaded.current = `${page.id}:${page.revision}`; }
    select({ kind: "page" }); setRefused(undefined);
  }, busy);
  const loadedScope = useRef(scope);
  useEffect(() => {
    if (loadedScope.current === scope) return;
    loadedScope.current = scope; loaded.current = "";
  }, [scope]);
  useEffect(() => {
    if (!page || dirty || busy || loaded.current === `${page.id}:${page.revision}`) return;
    session.load(loadDraft(savedPage!,authoringEntity,declarationFields)); baseRevision.current = page.revision; loaded.current = `${page.id}:${page.revision}`;
  }, [page, dirty, busy, session.load]);
  useEffect(() => {
    if (selection.kind === "widget" && !sections.some((section) => section.id === selection.id) ||
      selection.kind === "container" && !document.nodes[selection.id]) select({ kind: "page" });
  }, [selection, sections, document]);
  const chosen = selection.kind === "widget" ? sections.findIndex((section) => section.id === selection.id) : selection.kind === "container" ? -2 : -1;
  const container = selection.kind === "container" ? selection.id : undefined;
  const choose = (index: number) => { select(index < 0 || !sections[index]?.id ? { kind: "page" } : { kind: "widget", id: sections[index]!.id! });  };
  const edit: typeof session.edit = (update, key) => { if (preview) return; setSourceApplied(false); session.edit((old) => {
    const next = typeof update === "function" ? update(old) : { ...old, ...update };
    const document={...next.document,uiProfile:old.document.uiProfile};
    const sections=next.sections.map(section=>{
      if(section.composite)return materializeModelSection(canonicalModelSection(section),authoringEntity,true);
      if(!section.model)return section;
      const previous=old.sections.find(s=>s.id===section.id);
      const canonical=JSON.stringify(previous?.model)!==JSON.stringify(section.model)?canonicalModelSection(section):foldModelPorts(section,authoringEntity);
      return materializeModelSection(canonical,authoringEntity,true);
    });
    return { ...next, ...synchronizeLoopBindings({...document,uiProfile:pageEditingProfile({...next,document,sections})}, sections) };
  }, key); };
  const history = (direction: "undo" | "redo") => { if (lock.current || preview) return; session[direction](); setRefused(undefined); };
  const diagnostics = pageDraftDiagnostics(session.draft, { object: page?.object ?? "", entity: authoringEntity, catalog, definitions });
  const problemList: WorkbenchProblem[] = [
    ...diagnostics.map(issue => ({ id: issue.id, text: t(issue.code, issue.values), subject: issue.subject, locate: () => {
      const target = issue.target;
      if (target.kind === "compatibility") setCompatibilityOpen(true);
      else if (target.kind === "node") canvasSelect(target.id);
      else if (target.kind === "widget") { const at = sections.findIndex(section => section.id === target.id); if (at >= 0) choose(at); }
      else if (target.kind === "page") select({ kind: "page" });
      else { select({ kind: "page" }); setLeftTab("data"); setLeftOpen(true); }
    } })),
    ...Object.entries(session.inputProblems).filter(([, problem]) => problem).map(([id, problem]) => ({ id: `form:${id}`, text: problem, subject: id })),
  ];
  const invalid = problemList.length > 0;
  const incompatible = diagnostics.some(issue => issue.target.kind === "compatibility");
  const relatedObjects = model.entities.filter(entity => entity.type !== page?.object)
    .filter(entity => entity.fields.some(field => field.type === "reference" && [page?.object, ...selections.map(selection => selection.object.name)].includes(field.ref))).map(entity => entity.type);
  const [queryPreviewOwner,setQueryPreviewOwner]=useState<string|undefined>(undefined);
  const [variableValues, setVariableValues] = useState<Record<string, PageVariableValue>>({});
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
      const encoded=encodePage(asPage({...savedPage!,...submitted},submitted.sections.map(canonicalModelSection),{...submitted.document,uiProfile:pageEditingProfile(submitted)}),definitions);
      const payload={...encoded,object:savedPage!.object};
      const ok = await decide("build.page.edit", { type: "build.page", id }, declarationChanges(payload, declarationValue(savedPage!, declarationFields)), { expectedRevision, onRefused: setRefused });
      if (ok) {
        const result = await query.refetch();
        const confirmed = result.isSuccess && (branch ? !!result.data?.record : result.data?.record?.revision === expectedRevision + 1) ? result.data!.record : undefined;
        baseRevision.current = branch ? confirmed?.revision ?? expectedRevision : expectedRevision + 1; loaded.current = `${id}:${baseRevision.current}`;
        session.saved(submitted, confirmed ? loadDraft(confirmed,authoringEntity,declarationFields) : undefined); markSaved();
      }
      return ok;
    } catch { setRefused(t("The page could not be saved. Your draft is still here.")); return false; }
    finally { lock.current = false; setSaving(false); }
  };
  const preparedPage = savedPage && !invalid ? { ...encodePage(asPage({ ...savedPage, ...session.draft }, session.draft.sections.map(canonicalModelSection), { ...session.draft.document, uiProfile: pageEditingProfile(session.draft) }), definitions), object: savedPage.object } : undefined;
  working?.prepare(preparedPage ? { schema: "build.page.edit", target: { type: "build.page", id }, payload: declarationChanges(preparedPage, declarationValue(savedPage!, declarationFields)), expectedRevision: baseRevision.current } : undefined, invalid ? [t("Fix the problems to save")] : []);
  const nothing = sections.length === 0;
  const canvasSelection = sections[chosen];
  const nodeID = container ?? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection?.id && node.kind === "widget")?.[0];
  const patchNode = (id: string, patch: Partial<PageUI.PageLayoutNode>) => edit((old) => ({ ...old, document: { ...old.document, nodes: { ...old.document.nodes, [id]: { ...old.document.nodes[id]!, ...patch } } } }), `node:${id}:${Object.keys(patch).join(",")}`);
  const canvasSelect=(id:string)=>{const node=document.nodes[id];if(!node)return;if(node.kind==='widget'){const index=sections.findIndex(s=>s.id===node.section);if(index>=0)choose(index);}else{select({kind:'container',id});}};
  const canvasEdit=(next:PageUI.PageDocument|undefined)=>{if(next){for(const key of Object.keys(session.inputs.values)){const node=key.match(/^\/layout:([^/]+)\//)?.[1];if(node&&!next.nodes[node])session.inputs.set(key);}edit({document:next});}};
  const canvasCommitDrop=(payload:CanvasPayload,target:CanvasDrop)=>{if(lock.current)return;if(payload.kind==='new')add(payload.type,undefined,target);else{const next=canvasDrop(document,payload.id,target,sections);if(next){canvasEdit(next);canvasSelect(payload.id);}else notify.error(t("This move would change a protected scope or exceed the layout limits."));}};
  const canvasCommitSizes=(sizes:Record<string,PageUI.PageLayoutSize>)=>{if(lock.current)return;const next=structuredClone(document);for(const [id,size]of Object.entries(sizes))if(next.nodes[id])next.nodes[id]!.size=Object.keys(size).length?size:undefined;if(!pageLayoutDiagnostics(next,sections).length)canvasEdit(next);};
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
  const pageTabs: WorkbenchTab[] = [
    { id: "page", title: t("Page"), content: <Settings value={{ title, description }} object={info?.title ?? page.object} device={document.device ?? ""} onDevice={(device) => edit({ document: { ...document, device: device || undefined } }, "settings:device")}
      selections={selections} objects={definitions.filter((d) => d.ref.kind === "object" && d.entity).map((d) => d.ref).sort((a, b) => Number(b.name === page.object) - Number(a.name === page.object))}
      onSelections={(next, rename) => edit((old) => ({ ...old, selections: next, sections: rename ? old.sections.map((s) => ({ ...s, selection: s.selection === rename.from ? rename.to : s.selection, parentSelection: s.parentSelection === rename.from ? rename.to : s.parentSelection })) : old.sections }))}
      onChange={(patch) => edit(patch, `settings:${Object.keys(patch).join(",")}`)} /> },
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
    catalog: model.actions, pageActions: page.actions ?? [], onChange: (patch) => change(chosen, patch),
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
  const inspectorValue = selection.kind === "interface" ? "interface" : inspectorTab[inspectorKey.split(":")[0]!];
  const liveValues = Object.entries(variableValues);
  const crumbs: WorkbenchCrumb[] = module ? [
    { label: t("Files"), onClick: () => open({ view: "projects" }) },
    { label: module.project.title || module.project.name, onClick: () => open({ view: "project", params: { id: module.project.id } }) },
    { label: t("Module"), onClick: () => module.openModule() },
  ] : [{ label: t("Pages"), onClick: () => open({ view: "module" }) }];
  return <>
    <Workbench storageKey="page" crumbs={crumbs} title={title || page.title}
      status={<DraftStatus state={page.state} problems={problemList.length} />} busy={saving}
      history={{ canUndo: session.canUndo && !busy && !preview, canRedo: session.canRedo && !busy && !preview, undo: () => history("undo"), redo: () => history("redo") }}
      actions={<>
        <SegmentedChoice compact label={t("Editor mode")} value={preview ? "preview" : "edit"} options={[{ value: "edit", label: t("Edit"), icon: <PenLine /> }, { value: "preview", label: t("Preview"), icon: <Play /> }]}
          onChange={(mode) => { setPreview(mode === "preview"); setCompatibilityOpen(false); select({ kind: "page" }); }} />
        <ResourceControls autosave={!sourceApplied && !branch} type="build.page" record={page} dirty={dirty} busy={busy} invalid={invalid} empty={nothing} extra={[
          { id: "compatibility", label: t("Review page compatibility…"), disabled: busy || preview, run: () => setCompatibilityOpen(true) },
          ...(page.state === "published" ? [{ id: "open", label: t("Open published page"), icon: <ExternalLink />, run: () => open({ view: "page", params: { app: "build", kind: "page", name: page.name } }) }] : []),
        ]} onSave={() => save()} onCancel={discardChanges} route={canvasRoute} />
      </>}
      onKeyDown={(event) => {
        const command = event.metaKey || event.ctrlKey;
        const typing = (event.target as HTMLElement).closest("input,textarea,select,[contenteditable=true]");
        if (preview) return;
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
      left={preview ? undefined : { label: t("Page structure"), open: leftOpen, onOpenChange: setLeftOpen, value: leftTab, onChange: setLeftTab, tabs: [
        { id: "layers", title: t("Layers"), content: <div className="grid content-start">
          {module && <ModuleTree context={module} compact />}
          <LayoutTree document={document} sections={sections} chosen={chosen} container={container} title={title || page.title}
            widgetTitles={widgetTitles} onChoose={choose} onContainer={(id) => canvasSelect(id)}
            onGroup={(kind) => { if (!nodeID) return; const result = canvasGroup(document, nodeID, kind, sections); if (result) { canvasEdit(result.document); select({ kind: "container", id: result.id }); } }}
            onAddOverlay={() => { const result = addOverlay(document, t("Overlay {n}", { n: Object.keys(document.overlays ?? {}).length + 1 })); canvasEdit(result.document); select({ kind: "container", id: result.root }); }}
            commandsForNode={canvasCommands} />
        </div> },
        { id: "widgets", title: t("Widgets"), content: <WidgetLibrary widgets={widgets} widgetTitles={widgetTitles} onAdd={(widget) => add(widget)} /> },
        { id: "data", title: t("Variables"), badge: (Object.keys(document.variables ?? {}).length + Object.keys(document.queries ?? {}).length) || undefined, content: <div className="grid content-start">
          <InspectorSection title={t("Variables")}><VariablesPanel object={objectRef} entity={authoringEntity} onUpgrade={()=>setCompatibilityOpen(true)} document={document} sections={sections} values={variableValues} onChange={(document) => edit({ document })} /></InspectorSection>
          <InspectorSection title={t("Queries")}><QueriesPanel sections={sections} onPreviewOwner={setQueryPreviewOwner} onUpgrade={()=>setCompatibilityOpen(true)} document={document} object={objectRef} entity={authoringEntity} values={variableValues} onChange={(document) => edit({ document })} /></InspectorSection>
        </div> },
      ] }}
      right={preview ? undefined : { label: t("Inspector"), heading: <span className="flex min-w-0 items-center gap-1.5">{canvasSelection ? <WidgetGlyph widget={canvasSelection.widget} /> : container ? <LayoutGlyph kind={document.nodes[container]?.kind ?? "rows"} /> : <Settings2 className="size-3.5 shrink-0" />}<span className="truncate" title={canvasSelection ? canvasSelection.title || widgetTitles[canvasSelection.widget]?.() : container ? t("Layout") : t("Page")}>{canvasSelection ? canvasSelection.title || widgetTitles[canvasSelection.widget]?.() : container ? t("Layout") : t("Page")}</span></span>, scope:inspectorKey,selection:nodeID,locate:()=>select(selection), value: inspectorValue, onChange: (next) => { if (next === "interface" && !canvasSelection && !container) select({ kind: "interface" }); else if (next === "page") select({ kind: "page" }); setInspectorTab((old) => ({ ...old, [inspectorKey.split(":")[0]!]: next })); },
        tabs: inspectorTabs }}
      dock={{ label: t("Page dock"), tabs: [
        { id: "problems", title: t("Problems"), badge: problemList.length, content: <ProblemList problems={problemList} empty={t("No problems. The page can be published.")} /> },
        { id: "variables", title: t("Live values"), badge: liveValues.length || undefined, content: <DataTable data={liveValues} getRowId={([name]) => name} searchable={false} height="100%" empty={t("Variables show their live values here once the page renders.")} columns={[
          { id: "name", header: t("Variable"), accessorFn: ([name]) => document.variables?.[name]?.title || name, enableSorting: false },
          { id: "type", header: t("Type"), accessorFn: ([name]) => document.variables?.[name]?.type, enableSorting: false },
          { id: "value", header: t("Current value"), accessorFn: ([, value]) => JSON.stringify(value), enableSorting: false },
        ]} /> },
      ] }}>
      {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {!source.entity(page.object) && info && <p className="px-3 pt-2 text-xs text-warning" role="status">{t("Field choices come from a saved object draft. Business data is available after joint activation.")}</p>}
      {compatibilityOpen && <CompatibilityReview key={clipboardScope} draft={session.draft} busy={busy} onClose={() => setCompatibilityOpen(false)} onLocate={(id) => { select({ kind: "widget", id }); setCompatibilityOpen(false); }} onApply={(review) => { if (lock.current || preview) return; const next = applyProfileUpgrade(session.draft, review); if (!next) return; session.edit(next, "profile-upgrade"); setCompatibilityOpen(false); setRefused(undefined); }} />}
      <StructuredEditor validate={pageAuthoringIssues} expectedRevision={branch ? mainRevision : baseRevision.current} fieldInput={sourceFieldInput} value={incompatible&&(savedPage?.format===3||savedPage?.format===4)?declarationValue(savedPage,declarationFields):declarationValue({...encodePage(asPage({...savedPage!,...session.draft},sections.map(canonicalModelSection),document),definitions),object:savedPage!.object},declarationFields)} fields={declarationFields} scope={`build.page:${id}:${source.scope}`} disabled={busy || incompatible || preview} onChange={next => { if (preview) return; setSourceApplied(true); session.edit(() => loadDraft({...savedPage!,...next}as PageRecord,authoringEntity,declarationFields)); select({ kind: "page" }); }} onTrial={next => trial("build.page.edit", { type: "build.page", id }, declarationChanges(next, declarationValue(savedPage!, declarationFields)), branch ? mainRevision : baseRevision.current)}><fieldset disabled={busy || incompatible} className="flex min-h-0 min-w-0 flex-1 flex-col">
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
                if (leftTab === "data" && queryPreviewOwner) return document.overlays?.[queryPreviewOwner]?.root;
                const node = container ?? (canvasSelection?.id ? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection.id)?.[0] : undefined);
                return Object.values(document.overlays ?? {}).find((overlay) => {
                  const includes = (id: string): boolean => id === node || [...(document.nodes[id]?.children ?? []), ...(document.unusedWidgets ?? []).filter((entry) => entry.parent === id).map((entry) => entry.node)].some(includes);
                  return includes(overlay.root);
                })?.root;
              })()} onVariableValues={setVariableValues} page={asPage({ ...page, title, description, selections }, sections, document)} live={false} chosen={preview ? -1 : chosen} onChoose={preview ? () => {} : choose}
                notice={nothing && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted"><p>{t("This page is empty.")}</p><p className="mt-1 text-xs">{t("Drag a widget from the library, or pick one to add it here.")}</p><Button className="mt-3" size="sm" onClick={() => { setPreview(false); setLeftTab("widgets"); setLeftOpen(true); }}><Plus />{t("Add widget")}</Button></div>}
                wrapLayout={preview ? undefined : (id, _node, body) => <CanvasRegion key={id} id={id}>{body}</CanvasRegion>} /></ApplicationPage>
            </div>
          </div>
          <div className="flex items-center gap-2 border-t border-border px-3 py-1 text-xs text-muted" role="status">{preview ? t("Preview: your records as they are; actions do not run here. Use a rehearsal or the published application for real runs.") : t("Design: your records as they are. Actions do not run while you compose.")}</div>
        </CanvasEditor>
      </fieldset></StructuredEditor>
    </Workbench>
  </>;
}
