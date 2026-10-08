// The Workshop module (ADR-0053 §5.3, D1): a project's pages, navigation,
// header and module-level variables, edited in one Workbench. With a page in
// hand the main view is that page's canvas (PageEditor); without one it is the
// module itself — header and navigation as people will see them.
import { pageUIProfile, pageVariableValues, useHost, useNewRecord, useReadQuery, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { ApplicationHeader, Button, Checkbox, Input, PanelSection, Select, ProblemList, StructureRow, Textarea, Workbench, t, useWorkspace, type WorkbenchProblem } from "@platform/ui";
import { ArrowDown, ArrowUp, FolderKanban, LayoutTemplate, Layers, PanelTop, Plus, Settings2, Trash2, Variable } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useDraftSession } from "../session/DraftSession";
import { DraftStatus, savingState, useAutoSave, WorkbenchMessage } from "../editor/workbench";
import type { Project } from "../projects/project";
import { PageEditor } from "./editor";
import { HeaderEditor } from "./HeaderEditor";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { VariablesPanel } from "./page-editor/VariablesPanel";
import { StudioTemplates } from "./template-ui";

type PageRecord = { id: string; name: string; title: string; state: string; object: string; archived?: boolean };
type ModuleDraft = Pick<Api.Application, "header" | "uiProfile" | "variables" | "queries" | "pages" | "groups" | "title" | "description" | "icon">;

/** What a page editor needs to know about the module it belongs to. */
export type ModuleContext = {
  project: Project; pages: { name: string; title: string; id?: string; state?: string }[]; currentPage?: string;
  openPage: (id: string) => void; openModule: (focus?: ModuleFocus) => void; addPage: () => void; canEdit: boolean;
};
type ModuleFocus = "navigation" | "header" | "variables" | "queries" | "settings";

const hydrate = (project: Project): ModuleDraft => ({ header: project.header, uiProfile: project.uiProfile, variables: project.variables, queries: project.queries, pages: project.pages ?? [], groups: project.groups ?? [], title: project.title, description: project.description, icon: project.icon });

// Omitted fields keep their saved value on edit; null explicitly removes the header.
const moduleChanges = (draft: ModuleDraft) => ({ ...draft, header: draft.header ?? null });

/** Entry for the `module` view: a project's module, optionally with one page in hand. */
export function ModuleWorkbench({ id, page }: { id?: string; page?: string }) {
  const { open } = useWorkspace();
  const pageQuery = useReadQuery<{ record?: PageRecord }>(`/v1/records/build.page/${encodeURIComponent(page ?? "")}`, undefined, !!page && !id);
  const projects = useRecordInventory<Project>("build.app", 1000, !!page && !id);
  // A page opened without its project: find the project that lists it.
  const owner = !id && page && pageQuery.data?.record ? projects.data?.records.find((project) => !project.archived && project.pages?.includes(pageQuery.data!.record!.name)) : undefined;
  const projectId = id ?? owner?.id;
  if (!projectId && !page) return <ModulesList />;
  if (!projectId) {
    if (projects.isLoading || pageQuery.isLoading) return <Workbench storageKey="module" title={t("Module")}><WorkbenchMessage>{t("Loading…")}</WorkbenchMessage></Workbench>;
    // A page that belongs to no project is still editable on its own.
    return <PageEditor id={page!} />;
  }
  return <ProjectModule key={projectId} id={projectId} page={page} openPage={(next) => open({ view: "module", params: { id: projectId, application: projectId, page: next } })} />;
}

function ModulesList() {
  const { open } = useWorkspace();
  const projects = useRecordInventory<Project>("build.app");
  const rows = (projects.data?.records ?? []).filter((project) => !project.archived);
  return <div className="grid min-w-0 gap-4">
    <div><h1 className="text-xl font-semibold tracking-tight">{t("Modules")}</h1><p className="text-sm text-muted">{t("Each project has one module: the pages, navigation and header people use. Open a project to edit its module.")}</p></div>
    <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {rows.map((project) => <li key={project.id}><Button variant="row" type="button" onClick={() => open({ view: "module", params: { id: project.id, application: project.id } })} className="grid w-full gap-1 border border-border bg-surface rounded-md p-4 text-left hover:border-primary">
        <span className="flex items-center gap-2"><LayoutTemplate className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate font-semibold">{project.title || project.name}</span><DraftStatus state={project.state} /></span>
        <span className="text-xs text-muted">{t("{n} pages", { n: project.pages?.length ?? 0 })}</span>
      </Button></li>)}
    </ul>
    {!projects.isLoading && !rows.length && <p className="text-sm text-muted">{t("No projects yet. Create one in Projects.")}</p>}
  </div>;
}

function ProjectModule({ id, page, openPage }: { id: string; page?: string; openPage: (id: string) => void }) {
  const { decide, role, client } = useHost();
  const { open } = useWorkspace();
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id)}`);
  const project = query.data?.record;
  const pageRecords = useRecordInventory<PageRecord>("build.page");
  const session = useDraftSession<ModuleDraft>({ pages: [], groups: [], title: "" }, id);
  const { draft, dirty } = session;
  const loaded = useRef(""), lock = useRef(false);
  const [saving, setSaving] = useState(false), [error, setError] = useState<string>();
  const [focus, setFocus] = useState<ModuleFocus>("navigation");
  const [templates, setTemplates] = useState(false);
  useEffect(() => { if (project && !dirty && !saving && loaded.current !== `${project.id}:${project.revision}`) { session.load(hydrate(project)); loaded.current = `${project.id}:${project.revision}`; } }, [project, dirty, saving, session.load]);
  const canEdit = role("build") === "builder";
  const pages = useMemo(() => (draft.pages ?? []).map((name) => { const record = pageRecords.data?.records.find((item) => item.name === name && !item.archived); return { name, title: record?.title ?? name, id: record?.id, state: record?.state }; }), [draft.pages, pageRecords.data]);
  const save = async () => {
    if (!project || lock.current) return false;
    lock.current = true; setSaving(true); setError(undefined);
    try {
      const ok = await decide("build.app.edit", { type: "build.app", id: project.id }, moduleChanges(draft), { expectedRevision: project.revision, quiet: true, onRefused: setError });
      if (ok) { const result = await query.refetch(); const next = result.data?.record; session.saved(draft, next ? hydrate(next) : undefined); if (next) loaded.current = `${next.id}:${next.revision}`; }
      return ok;
    } catch { setError(t("The module could not be saved.")); return false; } finally { lock.current = false; setSaving(false); }
  };
  useAutoSave({ enabled: canEdit, dirty, busy: saving, save });
  const edit = (patch: Partial<ModuleDraft>) => { if (canEdit) session.edit((old) => ({ ...old, ...patch })); };
  const createPage = useNewRecord("build.page", async (target) => {
    try {
      const { record } = await client.get<{ record: PageRecord }>(`/v1/records/build.page/${encodeURIComponent(target.id)}`);
      const next = { ...draft, pages: [...new Set([...(draft.pages ?? []), record.name])] };
      session.edit(next);
      if (project) await decide("build.app.edit", { type: "build.app", id: project.id }, moduleChanges(next), { expectedRevision: project.revision, quiet: true, onRefused: setError });
      await query.refetch();
      openPage(record.id);
    } catch { setError(t("The page was created but could not be added to this module. Add it from the project's resources.")); }
  });
  const context: ModuleContext | undefined = project ? { project, pages, currentPage: page, canEdit, openPage, addPage: () => createPage.take(),
    openModule: (next) => { if (next) setFocus(next); if (page) open({ view: "module", params: { id: project.id, application: project.id } }); } } : undefined;
  if (!project || !context) return <Workbench storageKey="module" title={t("Module")}><WorkbenchMessage>{query.isError ? t("The project could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  if (page) return <>{<PageEditor key={page} id={page} module={context} />}{createPage.dialog}</>;
  const problems: WorkbenchProblem[] = [
    ...pages.filter((item) => !item.id && pageRecords.data).map((item) => ({ id: `missing:${item.name}`, severity: "warning" as const, text: t("Page {name} is listed but has no saved draft.", { name: item.name }), locate: () => edit({ pages: (draft.pages ?? []).filter((name) => name !== item.name) }) })),
    ...(!pages.length ? [{ id: "empty", severity: "info" as const, text: t("Add at least one page before publishing the module.") }] : []),
    ...(draft.header && !draft.header.title.trim() ? [{ id: "header-title", severity: "warning" as const, text: t("The header has no title."), locate: () => setFocus("header") }] : []),
  ];
  const grouped = (draft.groups ?? []).flatMap((group) => group.pages);
  const ungrouped = pages.filter((item) => !grouped.includes(item.name));
  const movePage = (name: string, offset: number) => { const list = [...(draft.pages ?? [])]; const at = list.indexOf(name); const to = at + offset; if (at < 0 || to < 0 || to >= list.length) return; [list[at], list[to]] = [list[to]!, list[at]!]; edit({ pages: list }); };
  const removePage = (name: string) => edit({ pages: (draft.pages ?? []).filter((item) => item !== name), groups: (draft.groups ?? []).map((group) => ({ ...group, pages: group.pages.filter((item) => item !== name) })).filter((group) => group.pages.length) });
  const document: Api.PageDocument = { formatVersion: 2, uiProfile: draft.uiProfile ?? pageUIProfile, root: "root", nodes: {}, variables: draft.variables, queries: draft.queries };
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
      <Button size="sm" disabled={!canEdit || !createPage.available} onClick={() => createPage.take()}><Plus />{t("New page")}</Button>
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
      <label className="grid gap-1">{t("Icon")}<Select value={draft.icon ?? "boxes"} disabled={!canEdit} onChange={(event) => edit({ icon: event.target.value })}>
        {["boxes", "clipboard", "people", "calendar", "wrench", "map", "chart", "sparkles"].map((icon) => <option key={icon} value={icon}>{t(icon)}</option>)}</Select></label>
      <p className="text-muted">{t("The module's name ({name}) is its identity in releases and links.", { name: project.name })}</p>
    </div>,
  };
  const tabs: { id: ModuleFocus; title: string }[] = [{ id: "navigation", title: t("Navigation") }, { id: "header", title: t("Header") }, { id: "variables", title: t("Variables") }, { id: "queries", title: t("Queries") }, { id: "settings", title: t("Settings") }];
  const previewPage = pages[0]?.name ?? "";
  return <>
    <Workbench storageKey="module" crumbs={[{ label: t("Projects"), onClick: () => open({ view: "projects" }) }, { label: project.title || project.name, onClick: () => open({ view: "project", params: { id: project.id } }) }]} title={t("Module")}
      status={<DraftStatus state={project.state} problems={problems.filter((problem) => problem.severity !== "info").length} />} saving={savingState(dirty, saving, error)}
      history={{ canUndo: session.canUndo, canRedo: session.canRedo, undo: session.undo, redo: session.redo }}
      actions={<>
        {canEdit && <Button size="sm" disabled={!createPage.available} onClick={() => createPage.take()}><Plus />{t("New page")}</Button>}
        {canEdit && <Button size="sm" variant="primary" disabled={!pages.length || dirty} onClick={() => open({ view: "release-review", params: { kind: "app", id: project.id, application: project.id } })}>{t("Publish")}</Button>}
      </>}
      left={{ label: t("Module structure"), content: <ModuleTree context={context} focus={focus} onFocus={setFocus} /> }}
      right={{ label: t("Module inspector"), tabs: tabs.map((tab) => ({ id: tab.id, title: tab.title, content: inspector[tab.id] })), value: focus, onChange: (next) => setFocus(next as ModuleFocus) }}
      dock={{ label: t("Module dock"), tabs: [{ id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} /> }] }}>
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
    </Workbench>
    {createPage.dialog}
    {templates && <div className="fixed inset-0 z-50 grid place-items-center bg-black/40 p-6" onClick={() => setTemplates(false)}>
      <div className="max-h-full w-full max-w-4xl overflow-auto border border-border bg-surface rounded-md p-4" onClick={(event) => event.stopPropagation()}>
        <div className="mb-2 flex items-center justify-between"><h2 className="font-semibold">{t("New page from template")}</h2><Button size="sm" variant="ghost" onClick={() => setTemplates(false)}>{t("Close")}</Button></div>
        <StudioTemplates />
      </div>
    </div>}
  </>;
}

function ModulePreviewBody({ context, ungrouped, groups, onHeader }: { context: ModuleContext; ungrouped: ModuleContext["pages"]; groups: Api.AppGroup[]; onHeader: () => void }) {
  void onHeader;
  const card = (page: ModuleContext["pages"][number]) => <Button variant="row" key={page.name} type="button" disabled={!page.id} onClick={() => page.id && context.openPage(page.id)}
    className="grid gap-1 rounded-md border border-border p-3 text-left hover:border-primary disabled:opacity-60">
    <span className="flex items-center gap-2 text-sm font-medium"><LayoutTemplate className="size-4 text-muted" /><span className="min-w-0 flex-1 truncate">{page.title}</span><DraftStatus state={page.state} /></span>
    <span className="text-[11px] text-muted">{page.id ? t("Open in the canvas") : t("No saved draft")}</span>
  </Button>;
  return <div className="grid gap-4 p-4">
    {!context.pages.length && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">
      <p>{t("This module has no pages yet.")}</p>
      {context.canEdit && <Button className="mt-3" variant="primary" onClick={context.addPage}><Plus />{t("New page")}</Button>}
    </div>}
    {groups.map((group) => <div key={group.title} className="grid gap-2"><h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{group.title}</h3>
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{group.pages.map((name) => context.pages.find((page) => page.name === name)).filter(Boolean).map((page) => card(page!))}</div></div>)}
    {ungrouped.length > 0 && <div className="grid gap-2">{groups.length > 0 && <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Pages")}</h3>}
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">{ungrouped.map(card)}</div></div>}
  </div>;
}

/** The module's structure: header, navigation (groups and pages), module resources. Shared by the module view and the page canvas. */
export function ModuleTree({ context, focus, onFocus, compact = false }: { context: ModuleContext; focus?: ModuleFocus; onFocus?: (focus: ModuleFocus) => void; compact?: boolean }) {
  const { project, pages, currentPage } = context;
  const groups = project.groups ?? [];
  const grouped = groups.flatMap((group) => group.pages);
  const go = (next: ModuleFocus) => { onFocus?.(next); context.openModule(next); };
  const row = (page: ModuleContext["pages"][number], depth: number) => <StructureRow key={page.name} depth={depth} icon={<LayoutTemplate />} label={page.title} selected={!!page.id && page.id === currentPage}
    meta={page.state === "published" ? undefined : page.id ? t("draft") : t("missing")} onClick={() => page.id ? context.openPage(page.id) : go("navigation")} />;
  return <div className="grid min-w-0 content-start">
    <PanelSection title={t("Module")} actions={context.canEdit && <Button size="sm" variant="ghost" aria-label={t("New page")} title={t("New page")} onClick={context.addPage}><Plus /></Button>}>
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
