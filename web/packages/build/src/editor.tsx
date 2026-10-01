import { recordPaths } from "./record-paths";
import { AssetControls } from "./asset-controls";
// Application Studio page design (ADR-0046). Document history, UI selection
// and authorized runtime data have separate owners. Preview and operation use
// the same registered widgets; save and activation use the original Go path.
import { ComposedPage, NewActions, SemanticObjectSelect, SemanticPropertySelect, pageDocumentFromSections, pageUIProfile, supportsPageUIProfile, pageVariableDiagnostics, widgetContract, widgetContracts, useHost, useReadQuery, useRecordInventory, type PageVariableValue, type Definition } from "@platform/app";
import {
  Button, Card, EditorWorkbench, Input, MarkdownEditor, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, Toggles, defineStatuses, humanizeKernelError, notify, t, useWorkspace, useUnsavedChanges,
  type EntityInfo,
} from "@platform/ui";
import { Copy, Monitor, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Plus, Redo2, Smartphone, Tablet, Trash2, Undo2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { Api as HostApi } from "@platform/kernel";
import { BindingEditor, WorkflowFormProblems } from "./workflow-binding";
import { loopOwner, synchronizeLoopBindings, addOverlay, removeOverlay, appendWidget, groupWidget, layoutID, moveWidget, relocateWidget, removeWidget, setLayoutKind, ungroup } from "./page-layout";
import { VariablesPanel, NodeBindings } from "./page-editor/VariablesPanel";
import { OverlayProperties, ButtonEventProperties } from "./page-editor/OverlayPanel";
import { LayoutProperties, LayoutTree } from "./page-editor/LayoutTree";
import { useDraftSession } from "./session/DraftSession";

type Api = NonNullable<Definition["page"]>;
type Section = NonNullable<Api["sections"]>[number];
/** The page as its record holds it: what the builder edits and submits. */
type PageRecord = {
  id: string; revision: number; name: string; title: string; description?: string; object: string; state: string;
  list?: string[]; detail?: string[]; actions?: string[];
  selections?: HostApi.SelectionVariable[];
  document?: HostApi.PageDocument;
  sections?: { id?: string; configVersion?: number; widget: string; title?: string; width?: string; object?: string; selection?: string; recordVariable?: string; parentSelection?: string; relation?: string; query?: string; fields?: string[]; actions?: string[]; group?: string; measure?: string; text?: string; function?: { name: string; version: number }; operation?: HostApi.AssetBinding; inputs?: Record<string, HostApi.Binding> }[];
};
type Draft = NonNullable<PageRecord["sections"]>[number];

const pageStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });

const widgets = widgetContracts.map((contract) => contract.componentID);
const widgetTitles: Record<string, () => string> = Object.fromEntries(widgetContracts.map((contract) => [contract.componentID, () => t(contract.title)]));
type PageDraft = { sections: Draft[]; document: HostApi.PageDocument; selections: HostApi.SelectionVariable[]; title: string; description: string };
type StudioSelection = { kind: "page" | "variables" } | { kind: "widget" | "container"; id: string };
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
  sections: sections.map((s) => ({
    id: s.id, configVersion: s.configVersion, widget: s.widget, title: s.title, width: s.width, selection: s.selection, recordVariable: s.recordVariable, parentSelection: s.parentSelection, relation: s.relation, fields: s.fields, group: s.group, measure: s.measure, text: s.text,
    object: s.object ? { app: s.object.split(".")[0] ?? "", kind: "object", name: s.object } : undefined,
    query: s.query ? { app: s.query.split(".")[0] ?? "", kind: "query", name: s.query.split(".").slice(1).join(".") } : undefined,
    function: s.function ? { ref: { app: "build", kind: "function", name: s.function.name }, sourceVersion: `preview.function-${s.function.version}` } : undefined,
    operation: s.operation, inputs: s.inputs,
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
  const session = useDraftSession<PageDraft>(emptyDraft());
  const { sections, document, selections, title, description } = session.draft;
  const { dirty } = session;
  const [selection, select] = useState<StudioSelection>({ kind: "page" });
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
  const overlayProblem = Object.values(document.overlays ?? {}).some((overlay) => !document.nodes[overlay.root]?.children?.length || !overlay.title.trim()) || sections.some((section) => section.widget === "button" && !document.events?.some((event) => event.source === section.id));
  const loopProblem = Object.values(document.nodes).some((node) => node.kind === "loop" && (!node.loop || !document.variables?.[node.loop.collection]));
  const variableProblems = pageVariableDiagnostics(document.variables ?? {});
  const invalid = loopProblem || overlayProblem || variableProblems.length > 0 || Object.values(formProblems).some(Boolean) || !!selectionProblem || incompatible;
  const relatedObjects = useMemo(() => definitions.filter((d) => d.ref.kind === "object" && d.entity && d.ref.name !== page?.object)
    .filter((d) => d.entity!.fields.some((f) => f.type === "reference" && [page?.object, ...selections.map((selection) => selection.object.name)].includes(f.ref))).map((d) => d.ref.name), [definitions, page?.object, selections]);
  const [variableValues, setVariableValues] = useState<Record<string, PageVariableValue>>({});
  if (!page) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const info = source.entity(page.object);
  const change = (index: number, patch: Partial<Draft>) => edit((old) => ({ ...old, sections: old.sections.map((s, at) => at === index ? { ...s, ...patch } : s) }), `widget:${sections[index]?.id}:${Object.keys(patch).join(",")}`);
  const move = (index: number, by: -1 | 1) => { const section = sections[index]; if (section?.id) edit((old) => ({ ...old, document: moveWidget(old.document, section.id!, by) })); };
  const add = (widget: string, destination?: { container: string; after?: string }) => {
    const contract = widgetContract(widget); if (!contract) return;
    const section: Draft = { ...contract.defaults, id: layoutID("section"), configVersion: contract.configVersion, widget, title: t(contract.title) };
    if (contract.fieldPreset === "list") section.fields = info?.fields.slice(0, 4).map((f) => f.name) ?? [];
    if (contract.fieldPreset === "filter") section.fields = info?.fields.filter((f) => filterable.includes(f.type)).slice(0, 2).map((f) => f.name) ?? [];
    if (contract.fieldPreset === "create") section.fields = info?.fields.filter((f) => f.required && !f.readOnly).map((f) => f.name) ?? [];
    edit((old) => ({ ...old, document: appendWidget({ ...old.document, uiProfile: pageUIProfile }, section.id!, destination?.container ?? container ?? old.document.root, destination ? destination.after : chosen >= 0 ? sections[chosen]?.id : undefined), sections: [...old.sections, section] }));
    select({ kind: "widget", id: section.id! }); setRightOpen(true);
  };
  const duplicate = () => {
    const source = sections[chosen]; if (!source?.id) return;
    const copy = { ...structuredClone(source), id: layoutID("section") };
    edit((old) => ({ ...old, sections: [...old.sections, copy], document: { ...appendWidget(old.document, copy.id, old.document.root, source.id), events: [...(old.document.events ?? []), ...(old.document.events ?? []).filter((event) => event.source === source.id).map((event) => ({ ...event, source: copy.id }))] } }));
    select({ kind: "widget", id: copy.id });
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
      if (event.key.toLowerCase() === "d") { event.preventDefault(); duplicate(); }
    }}>
      <PageHeader title={title || page.title} description={t("Compose what people see, save your draft, then review its release candidate.")}
        actions={<><StatusTag status={page.state} registry={pageStates} />{page.state === "published" && <Button onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: page.name } })}>{t("Open published page")}</Button>}</>} />
      <Card role="toolbar" aria-label={t("Page design actions")} className="flex flex-wrap items-center gap-1 px-2 py-1.5">
        <Button variant="ghost" aria-label={t("Toggle widget library")} onClick={() => setLeftOpen(!leftOpen)}>{leftOpen ? <PanelLeftClose /> : <PanelLeftOpen />}</Button>
        <Button variant="ghost" aria-label={t("Undo")} title={t("Undo")} disabled={!session.canUndo || busy} onClick={() => history("undo")}><Undo2 /></Button>
        <Button variant="ghost" aria-label={t("Redo")} title={t("Redo")} disabled={!session.canRedo || busy} onClick={() => history("redo")}><Redo2 /></Button>
        <Button variant="ghost" disabled={chosen < 0 || busy} onClick={duplicate}><Copy />{t("Duplicate widget")}</Button>
        <span className="mx-1 h-4 w-px bg-border" />
        <AssetControls type="build.page" record={page} dirty={dirty} busy={busy} onCancel={discardChanges} route={{ view: "compose", params: { id } }} />
        <span className="ml-auto text-xs text-muted" role="status">{dirty ? t("Unsaved") : t("Saved")}</span>
        <Button onClick={() => void save()} disabled={!dirty || busy || invalid}>{saving ? t("Saving…") : t("Save")}</Button>
        <Button onClick={() => void publish()} disabled={nothing || busy || invalid} title={t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}>{publishing ? t("Installing…") : t("Direct install")}</Button>
        <Button variant="primary" onClick={() => void review()} disabled={nothing || busy || invalid}>{t("Review release")}</Button>
        <Button variant="ghost" aria-label={t("Toggle inspector")} onClick={() => setRightOpen(!rightOpen)}>{rightOpen ? <PanelRightClose /> : <PanelRightOpen />}</Button>
      </Card>
      {refused && <Panel role="alert" className="text-sm text-danger">{t("The host refused it:")} {humanizeKernelError(refused)}</Panel>}
      {variableProblems.length > 0 && <Panel role="alert" className="text-xs text-danger">{variableProblems.map((issue, index) => <p key={index}>{issue.variable}: {t(issue.code)}</p>)}</Panel>}
      {loopProblem && <Panel role="status" className="text-xs text-muted">{t("Choose a query window for each loop before saving.")}</Panel>}
      {overlayProblem && <Panel role="status" className="text-xs text-muted">{t("Add content to each overlay and bind every button before saving.")}</Panel>}
      {selectionProblem && <Panel role="alert" className="text-xs text-danger">{selectionProblem}</Panel>}
      {incompatible && <Panel role="alert" className="text-xs text-danger">{t("This draft needs a newer workspace version. Its saved content has been preserved.")}</Panel>}
      <fieldset disabled={busy || incompatible} className="flex min-w-0 flex-col lg:min-h-0 lg:flex-1">
        <EditorWorkbench leftLabel={t("Widgets and layout")} centerLabel={t("The page")} rightLabel={t("The widget in hand")}
          left={leftOpen && <><Button className="m-3" aria-pressed={selection.kind === "variables"} onClick={() => { select({ kind: "variables" }); setRightOpen(true); }}>{t("Page variables")}</Button><LayoutTree document={document} sections={sections} chosen={chosen} container={container} widgetTitles={widgetTitles} widgets={widgets}
            onChoose={choose} onContainer={(id) => { select({ kind: "container", id }); setRightOpen(true); }} title={title || page.title} onAdd={add} onMove={move}
            onInsert={(widget, container, after) => add(widget, { container, after })}
            onRelocate={(section, target, after) => edit((old) => ({ ...old, document: relocateWidget(old.document, section, target, after) }))}
            onGroup={(kind) => { const section = sections[chosen]; if (!section?.id) return; const result = groupWidget(document, section.id, kind); edit({ document: result.document }); if (result.id) select({ kind: "container", id: result.id }); }}
            onAddOverlay={() => { const result = addOverlay(document, t("Overlay {n}", { n: Object.keys(document.overlays ?? {}).length + 1 })); edit({ document: result.document }); select({ kind: "container", id: result.root }); setRightOpen(true); }}
            onRemove={(index) => { const section = sections[index]; if (!section?.id) return; edit((old) => ({ ...old, document: removeWidget(old.document, section.id!), sections: old.sections.filter((s) => s.id !== section.id) })); select({ kind: "page" }); }} /></>}
          right={rightOpen && (selection.kind === "variables" ? <VariablesPanel document={document} sections={sections} values={variableValues} onChange={(document) => edit({ document })} /> : <div className="grid content-start gap-2">{container && Object.entries(document.overlays ?? {}).filter(([, overlay]) => overlay.root === container).map(([id, overlay]) => <OverlayProperties key={id} overlay={overlay}
            onChange={(patch) => edit({ document: { ...document, overlays: { ...document.overlays, [id]: { ...overlay, ...patch } } } })}
            onRemove={() => { const result = removeOverlay(document, id); edit({ document: result.document, sections: sections.filter((section) => !result.sections.has(section.id!)) }); select({ kind: "page" }); }} />)}{container ? <LayoutProperties document={document} id={container}
            onPatch={patchNode} onChange={(kind) => edit((old) => ({ ...old, document: setLayoutKind(old.document, container, kind) }))}
            onUngroup={() => { edit({ document: ungroup(document, container) }); select({ kind: "page" }); }} /> :
          chosen < 0 ? <Settings value={{ title, description }} object={info?.title ?? page.object}
            selections={selections} objects={definitions.filter((d) => d.ref.kind === "object" && d.entity).map((d) => d.ref).sort((a, b) => Number(b.name === page.object) - Number(a.name === page.object))}
            onSelections={(next, rename) => edit((old) => ({ ...old, selections: next, sections: rename ? old.sections.map((s) => ({ ...s, selection: s.selection === rename.from ? rename.to : s.selection, parentSelection: s.parentSelection === rename.from ? rename.to : s.parentSelection })) : old.sections }))}
            onChange={(patch) => edit(patch, `settings:${Object.keys(patch).join(",")}`)} /> :
          <Properties section={canvasSelection} info={source.entity(canvasSelection?.object || page.object)} catalog={catalog.map((a) => ({ schema: a.schema, title: a.title, target: a.target }))}
            object={page.object} selections={selections} relatedObjects={relatedObjects} onChange={(patch) => change(chosen, patch)} />}
            {chosen >= 0 && sections[chosen]?.id && Object.keys(document.overlays ?? {}).length > 0 && <Card className="grid gap-2 p-3"><label className="grid gap-1 text-xs">{t("Move widget to")}<Select value="" onChange={(event) => { if (event.target.value) edit({ document: relocateWidget(document, sections[chosen]!.id!, event.target.value) }); }}><option value="">{t("Choose a layout root")}</option><option value={document.root}>{t("Main page")}</option>{Object.entries(document.overlays ?? {}).map(([id, overlay]) => <option key={id} value={overlay.root}>{overlay.title}</option>)}</Select></label></Card>}
            {canvasSelection?.widget === "button" && <ButtonEventProperties document={document} section={canvasSelection.id!} owner={nodeID ? loopOwner(document, nodeID) : undefined} onChange={(document) => edit({ document })} />}
            {nodeID && <NodeBindings document={document} id={nodeID} button={canvasSelection?.widget === "button"} onChange={(patch) => patchNode(nodeID, patch)} />}</div>)}>
          <div className="flex flex-wrap items-center gap-1 border-b border-border px-3 py-1.5">
            <span className="mr-auto truncate text-xs font-medium">{title || page.title}</span>
            {([["desktop", "Desktop preview", Monitor], ["tablet", "Tablet preview", Tablet], ["mobile", "Mobile preview", Smartphone]] as const).map(([device, label, Icon]) => <Button key={device} size="sm" variant="ghost" aria-label={t(label)} aria-pressed={viewport === device} onClick={() => setViewport(device)}><Icon /></Button>)}
            <Select aria-label={t("Canvas zoom")} value={zoom} onChange={(event) => setZoom(Number(event.target.value))} className="w-20">{[50, 75, 100, 125].map((value) => <option key={value} value={value}>{value}%</option>)}</Select>
          </div>
          <div className="min-h-[24rem] flex-1 overflow-auto bg-canvas p-4">
            <div className="mx-auto" style={{ width: viewport === "desktop" ? "100%" : viewport === "tablet" ? 768 : 390, zoom: zoom / 100 }}>
              <ComposedPage editingRoot={(() => {
                const node = container ?? Object.entries(document.nodes).find(([, node]) => node.section === canvasSelection?.id)?.[0];
                return Object.values(document.overlays ?? {}).find((overlay) => {
                  const includes = (id: string): boolean => id === node || (document.nodes[id]?.children ?? []).some(includes);
                  return includes(overlay.root);
                })?.root;
              })()} onVariableValues={setVariableValues} page={asPage({ ...page, title, description, selections }, sections, document)} live={false} chosen={chosen} onChoose={choose}
                notice={nothing && <Panel role="status" className="text-xs text-muted">{t("Add at least one widget before installing or reviewing a release.")}</Panel>}
                wrapLayout={(id, node, body) => <div key={id} data-layout-node={id} className={`relative min-w-0 rounded ${dropTarget === id || container === id ? "outline outline-2 outline-primary" : ""}`}
                  onDragOver={(event) => { if (!event.dataTransfer.types.some((type) => type === "application/platform-page-widget" || type === "application/platform-page-section")) return; event.preventDefault(); event.stopPropagation(); setDropTarget(id); }}
                  onDragLeave={() => setDropTarget(undefined)} onDrop={(event) => {
                    const widget = event.dataTransfer.getData("application/platform-page-widget"), section = event.dataTransfer.getData("application/platform-page-section");
                    if (!widget && !section) return; event.preventDefault(); event.stopPropagation(); setDropTarget(undefined);
                    const destination = { container: node.kind === "widget" ? document.root : id, after: node.kind === "widget" ? node.section : undefined };
                    if (widget) add(widget, destination); else edit((old) => ({ ...old, document: relocateWidget(old.document, section, destination.container, destination.after) }));
                  }}>
                  {container === id && <Button size="sm" variant="primary" className="absolute -top-3 left-2 z-10" onClick={() => select({ kind: "container", id })}>{t(node.kind === "tabs" ? "Tabs" : node.kind === "columns" ? "Columns" : node.kind === "flow" ? "Flow layout" : node.kind === "toolbar" ? "Toolbar" : node.kind === "loop" ? "Loop" : "Rows")}</Button>}
                  {body}
                </div>} />
            </div>
          </div>
          <div className="flex items-center gap-2 border-t border-border px-3 py-1.5 text-[11px] text-muted" role="status">{t("Your records, as they are. Actions do not run while you compose.")}</div>
        </EditorWorkbench>
      </fieldset>
    </div>
  </WorkflowFormProblems.Provider>;
}

/** The panel that configures the widget in hand: only what that widget binds. */
function Properties({ section, info, catalog, object, selections, relatedObjects = [], onChange }: {
  section?: Draft; info?: EntityInfo; object: string; relatedObjects?: string[];
  selections: HostApi.SelectionVariable[];
  catalog: { schema: string; title: string; target: string }[];
  onChange: (patch: Partial<Draft>) => void;
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
  const measures = ["count", ...fields.filter((f) => f.type === "integer" || f.type === "decimal" || f.type === "money").flatMap((f) => [`sum:${f.name}`, `avg:${f.name}`])];
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{widgetTitles[section.widget]?.() ?? section.widget}</div>
      {relatedObjects.length > 0 && allows("object") && (
        <label className="grid gap-1 text-xs">{t("Object")}
          <SemanticObjectSelect label={t("Object")} value={section.object ?? object} filter={(definition) => definition.ref.name === object || relatedObjects.includes(definition.ref.name)}
            onChange={(ref) => { if (ref) onChange({ object: ref.name === object ? undefined : ref.name,
              selection: undefined, parentSelection: undefined, relation: undefined, query: undefined, inputs: undefined, fields: [], actions: [] }); }} />
        </label>
      )}
      {section.recordVariable && <p className="text-xs text-muted">{t("This widget reads the current loop record.")}</p>}
      {!section.recordVariable && (selections.length > 0 || section.selection) && contract?.selectionMode !== "none" &&
        <label className="grid gap-1 text-xs">{section.widget === "table" ? t("Writes selection") : t("Reads selection")}
          <Select value={section.selection ?? ""} onChange={(e) => onChange({ selection: e.target.value || undefined })}>
            <option value="">{t("Shared selection for this object")}</option>
            {section.selection && !selections.some((v) => v.name === section.selection && v.object.name === (section.object || object)) &&
              <option value={section.selection}>{t("Unavailable selection: {name}", { name: section.selection })}</option>}
            {selections.filter((v) => v.object.name === (section.object || object)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {(selections.length > 0 || section.parentSelection) && section.object && relatedObjects.includes(section.object) &&
        allows("relation") &&
        <label className="grid gap-1 text-xs">{t("Parent selection")}
          <Select value={section.parentSelection ?? ""} onChange={(e) => { const nextType = e.target.value ? selections.find((selection) => selection.name === e.target.value)?.object.name : object; onChange({ parentSelection: e.target.value || undefined, ...(nextType !== parentType ? { relation: undefined, inputs: undefined } : {}) }); }}>
            <option value="">{t("Page's shared selection")}</option>
            {section.parentSelection && !selections.some((v) => v.name === section.parentSelection && fields.some((field) => field.ref === v.object.name)) &&
              <option value={section.parentSelection}>{t("Unavailable selection: {name}", { name: section.parentSelection })}</option>}
            {selections.filter((v) => fields.some((field) => field.type === "reference" && field.ref === v.object.name)).map((v) => <option key={v.name} value={v.name}>{v.name}</option>)}
          </Select>
        </label>}
      {section.object && relations.length > 0 && allows("relation") && (
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
      {allows("query") && queriesOf(section.object || object).length > 0 && (
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
      {section.widget === "filter" && (
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
      {(section.widget === "table" || section.widget === "detail") && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Fields it shows")}</legend>
          <Toggles options={fields.map((f) => ({ value: f.name, label: f.title }))} value={section.fields ?? []}
            onChange={(value) => onChange({ fields: value })} />
        </fieldset>
      )}
      {section.widget === "actions" && (
        <fieldset className="grid gap-1 text-xs">
          <legend className="mb-1">{t("Actions it offers")}</legend>
          <Toggles options={actions.map((a) => ({ value: a.schema, label: a.title }))} value={section.actions ?? []}
            onChange={(value) => onChange({ actions: value })} empty={t("No action of this object is offered to you.")} />
        </fieldset>
      )}
      {(section.widget === "chart" || section.widget === "metric") && (
        <label className="grid gap-1 text-xs">{t("Measure")}
          <Select value={section.measure ?? "count"} onChange={(e) => onChange({ measure: e.target.value })}>
            {measures.map((m) => <option key={m} value={m}>{m}</option>)}
          </Select>
        </label>
      )}
      {section.widget === "chart" && (
        <label className="grid gap-1 text-xs">{t("Grouped by")}
          <SemanticPropertySelect label={t("Grouped by")} object={{ app: (section.object ?? object).split(".")[0]!, kind: "object", name: section.object ?? object }} value={section.group?.split(":")[0]}
            onChange={(ref) => { const field = fields.find((field) => field.name === ref?.field); onChange({ group: ref ? `${ref.field}${field?.type === "date" || field?.type === "datetime" ? ":month" : ""}` : undefined }); }} />
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
