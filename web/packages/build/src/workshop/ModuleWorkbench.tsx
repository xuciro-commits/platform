import type {PageUI} from "@platform/app";
import { validApplicationHeader, sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue, declarationChanges } from "../editor/declaration";
import { pageUIProfile, pageVariableValues, useHost, useNewRecord } from "@platform/app";
import type { Api } from "@platform/kernel";
import {ApplicationHeader, InputDraftProvider, Button, Dialog, Checkbox, IconPicker, Input, PanelSection, ProblemList, StructureRow, Textarea, Workbench, t, useUnsavedChanges, useWorkspace, type WorkbenchProblem} from "@platform/ui";
import { ArrowDown, ArrowUp, FolderKanban, LayoutTemplate, Layers, PanelTop, Plus, Settings2, Trash2, Variable } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useDraftSession } from "../session/DraftSession";
import { useDraftRecord } from "../branches/draft-record";
import { readDraftIdentity } from "../branches/identity";
import { useDraftInventory } from "../branches/records";
import { DraftStatus, ResourceControls, savingState, useAutoSave, WorkbenchMessage } from "../editor/workbench";
import type { Project } from "../projects/project";
import { PageEditor } from "./editor";
import { HeaderEditor } from "./HeaderEditor";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { VariablesPanel } from "./page-editor/VariablesPanel";
import { StudioTemplates } from "./template-ui";
import { SystemDefinitionReferences } from "../editor/SystemDefinitions";

type PageRecord = { id: string; revision: number; name: string; title: string; state: string; object: string; archived?: boolean };
type ModuleDraft = Partial<Api.Application> & Pick<Api.Application, "title">;

export type ModuleContext = {
  project: Project; pages: { name: string; title: string; id?: string; state?: string }[]; currentPage?: string;
  openPage: (id: string) => void; openModule: (focus?: ModuleFocus) => void; addPage: () => void; canEdit: boolean; busy?: boolean;
};
type ModuleFocus = "navigation" | "header" | "variables" | "queries" | "settings";

const hydrate = (project: Project, fields: Api.FieldInfo[] = []): ModuleDraft => ({ ...declarationValue(project, fields), pages: project.pages ?? [], groups: project.groups ?? [], title: project.title });

const moduleChanges = (draft: ModuleDraft) => ({ ...draft, header: draft.header ?? null, variables: draft.variables ?? null, queries: draft.queries ?? null });

export function ModuleWorkbench({ id, page, focus }: { id?: string; page?: string; focus?: string }) {
  const { open } = useWorkspace();
  const { query: pageQuery } = useDraftRecord<PageRecord>("build.page", page ?? "", !!page && !id);
  const projects = useDraftInventory<Project>("build.app", 1000, !!page && !id);
  const owner = !id && page && pageQuery.data?.record ? projects.data?.records.find((project) => !project.archived && project.pages?.includes(pageQuery.data!.record!.name)) : undefined;
  const projectId = id ?? owner?.id;
  if (!projectId && !page) return <ModulesList />;
  if (!projectId) {
    if (projects.isError || pageQuery.isError) return <Workbench storageKey="module" title={t("Module")}><WorkbenchMessage>{t("Module ownership could not be read. Open the page from its project after reloading.")}</WorkbenchMessage></Workbench>;
    if (projects.isLoading || pageQuery.isLoading) return <Workbench storageKey="module" title={t("Module")}><WorkbenchMessage>{t("Loading…")}</WorkbenchMessage></Workbench>;
    return <PageEditor id={page!} />;
  }
  return <ProjectModule key={projectId} id={projectId} page={page} initialFocus={focus} openPage={(next) => open({ view: "module", params: { id: projectId, application: projectId, page: next } })} />;
}

function ModulesList() {
  const { open } = useWorkspace();
  const projects = useDraftInventory<Project>("build.app");
  const rows = (projects.data?.records ?? []).filter((project) => !project.archived);
  return <div className="grid min-w-0 gap-4">
    <div><h1 className="text-xl font-semibold tracking-tight">{t("Modules")}</h1><p className="text-sm text-muted">{t("Each project has one module: the pages, navigation and header people use. Open a project to edit its module.")}</p></div>
    <SystemDefinitionReferences type="build.page" />
    <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {rows.map((project) => <li key={project.id}><Button variant="row" type="button" onClick={() => open({ view: "module", params: { id: project.id, application: project.id } })} className="grid w-full gap-1 border border-border bg-surface rounded-md p-4 text-left hover:border-primary">
        <span className="flex items-center gap-2"><LayoutTemplate className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate font-semibold">{project.title || project.name}</span><DraftStatus state={project.state} /></span>
        <span className="text-xs text-muted">{t("{n} pages", { n: project.pages?.length ?? 0 })}</span>
      </Button></li>)}
    </ul>
    {projects.isError && <p role="alert" className="text-sm text-danger">{t("Modules are unavailable. This does not mean no applications exist.")}</p>}
    {!projects.isError && !projects.isLoading && !rows.length && <p className="text-sm text-muted">{t("No projects yet. Create one in Projects.")}</p>}
  </div>;
}

function ProjectModule({ id, page, openPage, initialFocus }: { id: string; page?: string; openPage: (id: string) => void; initialFocus?: string }) {
  const { decide, client, source , holds} = useHost();
  const declarationFields = source.entity("build.app")?.fields ?? [], trial = useActionTrial();
  const [sourceApplied, setSourceApplied] = useState(false);
  const { open } = useWorkspace();
  const { query, scope, branch, mainRevision } = useDraftRecord<Project>("build.app", id);
  const project = query.data?.record;
  const pageRecords = useDraftInventory<PageRecord>("build.page");
  const session = useDraftSession<ModuleDraft>({ pages: [], groups: [], title: "" }, `${id}:${scope}`);
  const { draft, dirty } = session;
  const loaded = useRef(""),baseRevision=useRef(0), lock = useRef(false), loadedScope = useRef(scope);
  const [saving, setSaving] = useState(false), [error, setError] = useState<string>();
  const [createdPage, setCreatedPage] = useState<{ type: string; id: string }>();
  const latest = useRef(draft); latest.current = draft;
  const [focus, setFocus] = useState<ModuleFocus>(isModuleFocus(initialFocus) ? initialFocus : "navigation");
  const [templates, setTemplates] = useState(false), [templateBusy, setTemplateBusy] = useState(false);
  const templateClose = useRef<(() => void) | undefined>(undefined);
  useEffect(() => {
    if (loadedScope.current === scope) return;
    loadedScope.current = scope; loaded.current = "";
  }, [scope]);
  useEffect(() => { if (project && !dirty && !saving && loaded.current !== `${project.id}:${project.revision}`) { session.load(hydrate(project,declarationFields));baseRevision.current=project.revision; loaded.current = `${project.id}:${project.revision}`; } }, [project, dirty, saving, session.load]);
  const canEdit = holds("build", "builder");
  const pages = useMemo(() => (draft.pages ?? []).map((name) => { const record = pageRecords.data?.records.find((item) => item.name === name && !item.archived); return { name, title: record?.title ?? name, id: record?.id, state: record?.state }; }), [draft.pages, pageRecords.data]);
  const headerInvalid = !validApplicationHeader(draft.header,draft.pages??[]);
  const invalid = session.inputs.invalid || headerInvalid;
  const { markSaved, confirmDiscard } = useUnsavedChanges(dirty || !!createdPage, () => {
    if (project) { session.load(hydrate(project,declarationFields)); baseRevision.current = project.revision; loaded.current = `${project.id}:${project.revision}`; }
    setCreatedPage(undefined); setError(undefined);
  }, saving || templateBusy);
  const persist = async (submitted: ModuleDraft) => {
    const expected = baseRevision.current;
    if (!await decide("build.app.edit", { type: "build.app", id }, declarationChanges(moduleChanges(submitted), declarationValue(project!, declarationFields)), { expectedRevision: expected, quiet: true, onRefused: setError })) return false;
    baseRevision.current = expected + 1; loaded.current = `${id}:${expected + 1}`;
    session.saved(submitted); markSaved();
    try {
      const refreshed = await query.refetch();
      const next = refreshed.isSuccess && (branch ? !!refreshed.data?.record : refreshed.data?.record?.revision === expected + 1) ? refreshed.data!.record : undefined;
      if (next) { if (branch) { baseRevision.current = next.revision; loaded.current = `${id}:${next.revision}`; } session.saved(submitted, hydrate(next,declarationFields)); }
      else setError(t("The module was saved, but its current record could not be reloaded."));
    } catch { setError(t("The module was saved, but its current record could not be reloaded.")); }
    return true;
  };
  const save = async () => {
    if (!project || !canEdit || lock.current || invalid || createdPage) return false;
    lock.current = true; setSaving(true); setError(undefined);
    try { return await persist(structuredClone(latest.current)); }
    catch { setError(t("The module could not be saved.")); return false; }
    finally { lock.current = false; setSaving(false); }
  };
  useAutoSave({ generation: JSON.stringify(draft), invalid, enabled: !sourceApplied && canEdit && !createdPage && !templates && !branch, dirty, busy: saving || templateBusy, save });
  const edit = (patch: Partial<ModuleDraft>) => { if (canEdit && !lock.current && !createdPage) { setSourceApplied(false); session.edit(old => ({ ...old, ...patch })); } };
  const attachCreatedPage = async (target: { type: string; id: string }) => {
    setCreatedPage(target);
    if (!project || !canEdit || lock.current || invalid) return;
    lock.current = true; setSaving(true); setError(undefined);
    try {
      const record = await readDraftIdentity(client, "build.page", target.id);
      if (!record || record.id !== target.id || record.archived) { setError(t("The created page is unavailable. Its record was not replaced or recreated.")); return; }
      const submitted = { ...structuredClone(latest.current), pages: [...new Set([...(latest.current.pages ?? []), record.name])] };
      session.edit(submitted);
      if (!await persist(submitted)) return;
      setCreatedPage(undefined); openPage(record.id);
    } catch { setError(t("The page was created but could not be added to this module. Retry adding the same page.")); }
    finally { lock.current = false; setSaving(false); }
  };
  const createPage = useNewRecord("build.page", target => { void attachCreatedPage(target); });
  const addPage = () => { if (!lock.current && !createdPage && !invalid) createPage.take(); };
  const reload = () => confirmDiscard(() => {
    if (project) { session.load(hydrate(project,declarationFields)); baseRevision.current = project.revision; loaded.current = `${project.id}:${project.revision}`; }
    setError(undefined); void query.refetch();
  });
  const pendingPage = createdPage && <div role="alert" className="grid gap-2 border border-warning/30 p-3 text-sm">
    <p>{t("A page was created but its module membership is unconfirmed. Retry the same page; creating another page would duplicate the work.")}</p>
    <code className="break-all text-xs">{createdPage.id}</code>
    {error && <p className="text-danger">{error}</p>}
    <div className="flex gap-2"><Button disabled={saving || invalid} onClick={() => void attachCreatedPage(createdPage)}>{t("Retry adding created page")}</Button>
      <Button variant="ghost" disabled={saving} onClick={() => confirmDiscard(() => setCreatedPage(undefined))}>{t("Keep page without module membership")}</Button></div>
  </div>;
  const context: ModuleContext | undefined = project ? { project, pages, currentPage: page, canEdit, busy: saving || !!createdPage, openPage, addPage,
    openModule: (next) => { if (next) setFocus(next); if (page) open({ view: "module", params: { id: project.id, application: project.id } }); } } : undefined;
  if (!project || !context) return <Workbench storageKey="module" title={t("Module")}><WorkbenchMessage>{query.isError ? t("The project could not be loaded.") : t("Loading…")}{pendingPage}</WorkbenchMessage></Workbench>;
  if (page) return <>{pendingPage}<InputDraftProvider isolated scope={`page:${page}`}><PageEditor key={page} id={page} module={context} /></InputDraftProvider>{createPage.dialog}</>;
  const problems: WorkbenchProblem[] = [
    ...(headerInvalid ? [{id:"header",text:t("The application header configuration is invalid."),locate:()=>setFocus("header")}] : []),
    ...pages.filter((item) => !item.id && pageRecords.data).map((item) => ({ id: `missing:${item.name}`, severity: "warning" as const, text: t("Page {name} is listed but has no saved draft.", { name: item.name }), locate: () => setFocus("navigation") })),
    ...(!pages.length ? [{ id: "empty", severity: "info" as const, text: t("Add at least one page before publishing the module.") }] : []),
    ...(draft.header && !draft.header.title.trim() ? [{ id: "header-title", severity: "warning" as const, text: t("The header has no title."), locate: () => setFocus("header") }] : []),
    ...groupProblems(draft.groups ?? [], draft.pages ?? [], () => setFocus("navigation")),
  ];
  const grouped = (draft.groups ?? []).flatMap((group) => group.pages);
  const ungrouped = pages.filter((item) => !grouped.includes(item.name));
  const movePage = (name: string, offset: number) => { const list = [...(draft.pages ?? [])]; const at = list.indexOf(name); const to = at + offset; if (at < 0 || to < 0 || to >= list.length) return; [list[at], list[to]] = [list[to]!, list[at]!]; edit({ pages: list }); };
  const removePage = (name: string) => edit({ pages: (draft.pages ?? []).filter((item) => item !== name), groups: (draft.groups ?? []).map((group) => ({ ...group, pages: group.pages.filter((item) => item !== name) })).filter((group) => group.pages.length), replacements: draft.replacements?.filter(item => item.page !== name) });
  const document: PageUI.PageDocument = { formatVersion: 2, uiProfile: draft.uiProfile ?? pageUIProfile, root: "root", nodes: {}, variables: draft.variables, queries: draft.queries };
  const inspector: Record<ModuleFocus, ReactNode> = {
    navigation: <div className="grid gap-3 p-3 text-xs">
      <p className="text-muted">{t("Pages appear in the module's navigation in this order. Groups collect pages under a heading.")}</p>
      <div className="grid gap-1">
        {pages.map((item, at) => <div key={item.name} className="flex items-center gap-1 rounded border border-border px-2 py-1">
          <span className="min-w-0 flex-1 truncate">{item.title}</span>
          <Button size="sm" variant="ghost" aria-label={t("Move page up")} disabled={at === 0 || !canEdit} onClick={() => movePage(item.name, -1)}><ArrowUp /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Move page down")} disabled={at === pages.length - 1 || !canEdit} onClick={() => movePage(item.name, 1)}><ArrowDown /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Remove page")} disabled={!canEdit} onClick={() => removePage(item.name)}><Trash2 /></Button>
        </div>)}
      </div>
      <Button size="sm" disabled={!canEdit || saving || !!createdPage || invalid || !createPage.available} onClick={addPage}><Plus />{t("New page")}</Button>
      <Button size="sm" variant="ghost" disabled={!canEdit} onClick={() => setTemplates(true)}>{t("New page from template…")}</Button>
      <fieldset className="grid gap-2 border-t border-border pt-3"><legend className="font-semibold">{t("Groups")}</legend>
        {(draft.groups ?? []).map((group, at) => <div key={at} className="grid gap-1 rounded border border-border p-2">
          <div className="flex gap-1"><Input aria-label={t("Group title")} value={group.title} disabled={!canEdit} onChange={(event) => edit({ groups: draft.groups!.map((item, i) => i === at ? { ...item, title: event.target.value } : item) })} />
            <Button size="sm" variant="ghost" aria-label={t("Remove group")} disabled={!canEdit} onClick={() => edit({ groups: draft.groups!.filter((_, i) => i !== at) })}><Trash2 /></Button></div>
          {pages.map((item) => <Checkbox key={item.name} checked={group.pages.includes(item.name)} disabled={!canEdit || !group.pages.includes(item.name) && grouped.includes(item.name)}
            onChange={(checked) => edit({ groups: draft.groups!.map((g, i) => i === at ? { ...g, pages: checked ? [...g.pages, item.name] : g.pages.filter((name) => name !== item.name) } : g) })}>{item.title}</Checkbox>)}
        </div>)}
        <Button size="sm" disabled={!canEdit} onClick={() => edit({ groups: [...(draft.groups ?? []), { title: t("New group"), pages: [] }] })}><Plus />{t("Add group")}</Button>
      </fieldset>
    </div>,
    header: <div className="grid gap-3 p-3"><HeaderEditor value={draft.header} pages={draft.pages ?? []} onChange={(header) => edit({ header, uiProfile: header ? pageUIProfile : draft.uiProfile })} /></div>,
    variables: <VariablesPanel application document={document} sections={[]} values={pageVariableValues(draft.variables ?? {})} onChange={(next) => edit({ uiProfile: pageUIProfile, variables: next.variables })} />,
    queries: <QueriesPanel application document={document} object={draft.queries ? Object.values(draft.queries)[0]?.object ?? { app: "build", kind: "object", name: "" } : { app: "build", kind: "object", name: "" }} values={{}} onChange={(next) => edit({ uiProfile: pageUIProfile, variables: next.variables, queries: next.queries })} />,
    settings: <div className="grid gap-3 p-3 text-xs">
      <label className="grid gap-1">{t("Title")}<Input value={draft.title ?? ""} disabled={!canEdit} onChange={(event) => edit({ title: event.target.value })} /></label>
      <label className="grid gap-1">{t("Description")}<Textarea rows={3} value={draft.description ?? ""} disabled={!canEdit} onChange={(event) => edit({ description: event.target.value })} /></label>
      <label className="grid gap-1">{t("Icon")}<IconPicker value={draft.icon} disabled={!canEdit} onChange={(icon) => edit({ icon })} /></label>
      <p className="text-muted">{t("The module's name ({name}) is its identity in releases and links.", { name: project.name })}</p>
    </div>,
  };
  const tabs: { id: ModuleFocus; title: string }[] = [{ id: "navigation", title: t("Navigation") }, { id: "header", title: t("Header") }, { id: "variables", title: t("Variables") }, { id: "queries", title: t("Queries") }, { id: "settings", title: t("Settings") }];
  const previewPage = pages[0]?.name ?? "";
  return <>
    <Workbench storageKey="module" crumbs={[{ label: t("Files"), onClick: () => open({ view: "projects" }) }, { label: project.title || project.name, onClick: () => open({ view: "project", params: { id: project.id } }) }]} title={t("Module")}
      status={<DraftStatus state={project.state} problems={problems.filter((problem) => problem.severity !== "info").length} />} saving={savingState(dirty, saving, error)}
      history={{ canUndo: !saving && !createdPage && session.canUndo, canRedo: !saving && !createdPage && session.canRedo, undo: session.undo, redo: session.redo }}
      actions={<ResourceControls showStatus={false} autosave={false} type="build.app" record={canEdit ? project : undefined} dirty={dirty} busy={saving || !!createdPage} invalid={invalid}
        operations={canEdit ? [{ id: "new-page", label: t("New page"), icon: <Plus />, disabled: !createPage.available, run: addPage }] : []}
        onSave={canEdit ? () => void save() : undefined} onReload={reload} route={{ view: "module", params: { id: project.id } }} />}
      left={{ label: t("Module structure"), content: <ModuleTree context={context} focus={focus} onFocus={setFocus} /> }}
      right={{ label: t("Module inspector"),scope:focus, tabs: tabs.map((tab) => ({ id: tab.id, title: tab.title, content: <fieldset disabled={!canEdit || saving || !!createdPage} className="contents">{inspector[tab.id]}</fieldset> })), value: focus, onChange: (next) => setFocus(next as ModuleFocus) }}
      dock={{ label: t("Module dock"), tabs: [{ id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} /> }] }}>
      <StructuredEditor expectedRevision={branch ? mainRevision : baseRevision.current} fieldInput={sourceFieldInput} value={declarationValue(draft, declarationFields)} fields={declarationFields} scope={`build.app:${id}:${source.scope}`} readOnly={!canEdit} disabled={saving || !!createdPage} onChange={next => { setSourceApplied(true); session.edit(() => ({ ...next, title: next.title as string, pages: next.pages as string[] ?? [], groups: next.groups as Api.AppGroup[] ?? [] })); }} onTrial={next => trial("build.app.edit", { type: "build.app", id }, declarationChanges(next, declarationValue(project, declarationFields)), branch ? mainRevision : baseRevision.current)}>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto bg-canvas p-4">
        <div className="mx-auto w-full max-w-5xl overflow-hidden border border-border bg-surface rounded-md shadow-sm">
          {draft.header ? <ApplicationHeader header={draft.header} pages={pages} currentPage={previewPage} onPage={() => setFocus("navigation")} onTheme={() => {}}>
            <ModulePreviewBody context={context} ungrouped={ungrouped} groups={draft.groups ?? []} onHeader={() => setFocus("header")} />
          </ApplicationHeader> : <div>
            <Button variant="row" type="button" onClick={() => setFocus("header")} className="flex w-full items-center justify-center gap-2 border-b border-dashed border-border px-3 py-2 text-xs text-muted hover:bg-row-hover"><PanelTop className="size-3.5" />{t("No header. Click to add one.")}</Button>
            <ModulePreviewBody context={context} ungrouped={ungrouped} groups={draft.groups ?? []} onHeader={() => setFocus("header")} />
          </div>}
        </div>
        {error && <p role="alert" className="mx-auto mt-3 text-xs text-danger">{error}</p>}
      </div>
      </StructuredEditor>
    </Workbench>
    {pendingPage}
    {createPage.dialog}
    <Dialog open={templates} wide title={t("New page from template")} onOpenChange={next => { if (!saving && !templateBusy) { if (next) setTemplates(true); else templateClose.current?.(); } }}>
      <StudioTemplates closeGuard={templateClose} onClose={() => setTemplates(false)} onBusy={setTemplateBusy} onCreated={target => { setTemplates(false); void attachCreatedPage(target); }} />
    </Dialog>
  </>;
}

function ModulePreviewBody({ context, ungrouped, groups, onHeader }: { context: ModuleContext; ungrouped: ModuleContext["pages"]; groups: Api.AppGroup[]; onHeader: () => void }) {
  void onHeader;
  const card = (page: ModuleContext["pages"][number]) => <Button variant="row" key={page.name} type="button" disabled={!page.id} onClick={() => page.id && context.openPage(page.id)}
    className="grid gap-1 rounded-md border border-border p-3 text-left hover:border-primary disabled:opacity-60">
    <span className="flex items-center gap-2 text-sm font-medium"><LayoutTemplate className="size-4 text-muted" /><span className="min-w-0 flex-1 truncate">{page.title}</span><DraftStatus state={page.state} /></span>
    <span className="text-xs text-muted">{page.id ? t("Open in the canvas") : t("No saved draft")}</span>
  </Button>;
  return <div className="grid gap-4 p-4">
    {!context.pages.length && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">
      <p>{t("This module has no pages yet.")}</p>
      {context.canEdit && <Button className="mt-3" variant="primary" disabled={context.busy} onClick={context.addPage}><Plus />{t("New page")}</Button>}
    </div>}
    {groups.map((group) => <div key={group.title} className="grid gap-2"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{group.title}</h3>
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{group.pages.map((name) => context.pages.find((page) => page.name === name)).filter(Boolean).map((page) => card(page!))}</div></div>)}
    {ungrouped.length > 0 && <div className="grid gap-2">{groups.length > 0 && <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Pages")}</h3>}
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{ungrouped.map(card)}</div></div>}
  </div>;
}

export function ModuleTree({ context, focus, onFocus, compact = false }: { context: ModuleContext; focus?: ModuleFocus; onFocus?: (focus: ModuleFocus) => void; compact?: boolean }) {
  const { project, pages, currentPage } = context;
  const groups = project.groups ?? [];
  const grouped = groups.flatMap((group) => group.pages);
  const go = (next: ModuleFocus) => { onFocus?.(next); context.openModule(next); };
  const row = (page: ModuleContext["pages"][number], depth: number) => <StructureRow key={page.name} depth={depth} icon={<LayoutTemplate />} label={page.title} selected={!!page.id && page.id === currentPage}
    meta={page.state === "published" ? undefined : page.id ? t("draft") : t("missing")} onClick={() => page.id ? context.openPage(page.id) : go("navigation")} />;
  return <div className="grid min-w-0 content-start">
    <PanelSection title={t("Module")} actions={context.canEdit && <Button size="sm" variant="ghost" aria-label={t("New page")} title={t("New page")} disabled={context.busy} onClick={context.addPage}><Plus /></Button>}>
      <StructureRow icon={<FolderKanban />} label={project.title || project.name} selected={!currentPage && focus === "settings"} onClick={() => go("settings")} />
      <StructureRow depth={1} icon={<PanelTop />} label={t("Header")} meta={project.header ? undefined : t("off")} selected={!currentPage && focus === "header"} onClick={() => go("header")} />
      <StructureRow depth={1} icon={<Layers />} label={t("Navigation")} meta={pages.length} selected={!currentPage && focus === "navigation"} onClick={() => go("navigation")} />
      {groups.map((group) => <div key={group.title} className="grid">
        <StructureRow depth={2} label={group.title} className="text-muted" onClick={() => go("navigation")} />
        {group.pages.map((name) => pages.find((page) => page.name === name)).filter(Boolean).map((page) => row(page!, 3))}
      </div>)}
      {pages.filter((page) => !grouped.includes(page.name)).map((page) => row(page, 2))}
      {!compact && <StructureRow depth={1} icon={<Variable />} label={t("Variables and queries")} meta={Object.keys(project.variables ?? {}).length || undefined} selected={!currentPage && (focus === "variables" || focus === "queries")} onClick={() => go("variables")} />}
      {!compact && <StructureRow depth={1} icon={<Settings2 />} label={t("Settings")} selected={!currentPage && focus === "settings"} onClick={() => go("settings")} />}
    </PanelSection>
  </div>;
}

const moduleFocuses = ["navigation", "header", "variables", "queries", "settings"] as const;
const isModuleFocus = (value?: string): value is ModuleFocus => !!value && (moduleFocuses as readonly string[]).includes(value);

function groupProblems(groups: Api.AppGroup[], pages: string[], locate: () => void): WorkbenchProblem[] {
  const problems: WorkbenchProblem[] = [];
  const under = new Map<string, string>();
  groups.forEach((group, at) => {
    const title = group.title.trim() || t("group {n}", { n: String(at + 1) });
    if (!group.title.trim()) problems.push({ id: `group-title:${at}`, severity: "warning", text: t("A group has no title."), locate });
    if (!group.pages.length) problems.push({ id: `group-empty:${at}`, severity: "warning", text: t("The group “{group}” holds no page. Put a page under it or remove the group.", { group: title }), locate });
    for (const page of group.pages) {
      if (!pages.includes(page)) problems.push({ id: `group-page:${at}:${page}`, severity: "warning", text: t("The group “{group}” lists the page “{page}”, which this module does not hold.", { group: title, page }), locate });
      if (under.has(page)) problems.push({ id: `group-twice:${page}`, severity: "warning", text: t("The page “{page}” is under two groups: “{a}” and “{b}”.", { page, a: under.get(page)!, b: title }), locate });
      if (!under.has(page)) under.set(page, title);
    }
  });
  return problems;
}
