import { CandidateTest } from "../releases/simulate";
import { ReleaseReview, releaseDraftsValue } from "../releases/release";
// ADR-0047: one application, five workspaces and builder-named concepts.
// Membership, edits, candidates and runtime keep their original owners.
import { ApplicationRuns, ApplicationRuntimePanel, pageUIProfile, useDependencies, useHost, useNewRecord, useReadQuery, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { ActionMenu, Button, IconPicker, Input, Notice, PanelSection, ProblemList, Select, StructureRow, Textarea, Workbench, ViewVisibilityContext, t, useDraftInput, useUnsavedChanges, useViewVisible, useWorkspace, type ContextCommand, type WorkbenchProblem } from "@platform/ui";
import { ArrowDown, ArrowUp, FolderKanban, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { WorkshopApplicationImport } from "../workshop/module-import/WorkshopApplicationImport";
import type { ApplicationImportDependency } from "../workshop/module-import/application-import";
import { DraftStatus, savingState, useAutoSave } from "../editor/workbench";
import { ProjectRoles } from "./project-roles";
import { publishedResourceKind, refKey, resourceKinds, resourceRef, resourceRoute, type ResourceKindInfo, type ResourceRecord } from "./resources";
import { projectResourceChange } from "./membership";
import { draftChangeKind, useSavedDraftChanges } from "../releases/changes";

export type Project = Api.Application & { id: string; revision: number; state: string; archived?: boolean; changed?: Api.Stamp };

import { kindIcon } from "./resource-icons";
import { useProjectResources, type OwnedResource } from "./resource-inventory";
export type { OwnedResource } from "./resource-inventory";
import { assetRoute, conceptTree, moveConceptAsset, resourceInArea, studioArea, studioAreas, studioRoute } from "./studio-model";
import { StudioTemplates } from "../workshop/template-ui";
import { StudioGuide } from "./studio-guide";
import { installedStudioID, installedStudioRef, studioApplications } from "./studio-applications";
import { InstalledApplicationHome } from "./InstalledApplicationHome";
import { ExtensionSettings } from "./ExtensionSettings";

/** Every project the builder may open, with what each holds. */
export function ProjectsList() {
  const { role, me, definitions } = useHost();
  const { open } = useWorkspace();
  const projects = useRecordInventory<Project>("build.app");
  const installedApps = useReadQuery<Api.AppInfo[]>("/v1/apps");
  // A builder role held only through projects (#141): the Studio opens, edits stay within the assets they name.
  const buildGrants = (me.profile.grants ?? []).filter((g) => g.app === "build" && g.role === "builder");
  const delegated = buildGrants.length > 0 && buildGrants.every((g) => g.by?.startsWith("project:")) ? buildGrants.map((g) => g.reason || g.by!.slice(8)) : [];
  const [search, setSearch] = useState("");
  const create = useNewRecord("build.app", (target) => open({ view: "project", params: { id: target.id } }));
  const builder = role("build") === "builder";
  const rows = studioApplications(installedApps.data ?? [], definitions, projects.data?.records ?? [])
    .filter(item => `${item.title} ${item.ref.app} ${item.ref.name}`.toLowerCase().includes(search.toLowerCase()));
  return <div className="grid min-w-0 gap-4">
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div><h1 className="text-xl font-semibold tracking-tight">{t("Applications")}</h1><p className="text-sm text-muted">{t("Inspect installed applications and extend them through the same drafts and releases.")}</p></div>
      <div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:w-auto">
        <Input type="search" aria-label={t("Search projects")} placeholder={t("Search projects")} value={search} onChange={(event) => setSearch(event.target.value)} className="min-w-0 flex-1 basis-56 sm:w-56 sm:flex-none" />
        {builder && create.available && <Button className="shrink-0" variant="primary" onClick={() => create.take()}><Plus />{t("New application")}</Button>}
      </div>
    </div>
    {delegated.length > 0 && <Notice tone="info" role="note" message={t("You build within {projects}: only the assets they name accept your edits; publishing stays with the builders.", { projects: delegated.join(", ") })} />}
    {(projects.isLoading || installedApps.isLoading) && <p className="text-sm text-muted">{t("Loading…")}</p>}
    {(projects.isError || installedApps.isError) && <Notice tone="warning" role="alert" message={t("Applications could not be loaded.")} />}
    {!projects.isError && !installedApps.isError && !projects.isLoading && !installedApps.isLoading && !rows.length && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">{search ? t("No project matches.") : t("No projects yet. Create one to start building.")}</div>}
    <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {rows.map((item) => <li key={refKey(item.ref)}>
        <Button variant="row" type="button" onClick={() => open({ view: "project", params: { id: item.id } })} className="grid w-full gap-2 border border-border bg-surface rounded-md p-4 text-left hover:border-primary focus-visible:outline-ring">
          <div className="flex items-center gap-2"><FolderKanban className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate font-semibold">{item.title}</span><DraftStatus state={item.project?.state} /></div>
          {item.description && <p className="line-clamp-2 text-xs text-muted">{item.description}</p>}
          <p className="text-xs text-muted">{item.project ? t(item.project.extends ? "Application extension" : "Build application") : t(item.definition?.source === "code" || item.ref.app !== "build" ? "Native code" : "Published application")} · {item.ref.app}</p>
          <p className="text-[11px] text-muted">{t("{pages} pages · {resources} resources", { pages: (item.project ?? item.definition?.application)?.pages?.length ?? 0, resources: (item.project ?? item.definition?.application)?.resources?.length ?? 0 })}</p>
        </Button>
      </li>)}
    </ul>
    {create.dialog}
  </div>;
}

const editorCreatedKinds = new Set(["table", "alert", "function", "agent", "evaluation-set", "sample-set", "test-plan", "connection", "source", "dataset", "pipeline", "writeback", "matching"]);

/** Dedicated editors own their creation; only remaining forms keep a dialog session. */
function NewResourceMenu({ application, onCreated, children, disabled, area }: { application: string; area?: string; onCreated?: (kind: ResourceKindInfo, target: { type: string; id: string }) => void; children?: ReactNode; disabled?: boolean }) {
  const { role, catalog } = useHost();
  const { open } = useWorkspace();
  const creators = Object.fromEntries(resourceKinds.filter(kind => !editorCreatedKinds.has(kind.kind)).map((kind) => [kind.kind, useNewRecord(kind.type, (target) => onCreated?.(kind, target))])) as Record<string, ReturnType<typeof useNewRecord>>;
  if (role("build") !== "builder") return null;
  const sections = [...studioAreas.filter(section => section.id === area), ...studioAreas.filter(section => section.id !== area)];
  const offered = new Set<string>();
  const commands: ContextCommand[] = sections.flatMap((section, index) => resourceKinds.filter(kind => (section.kinds as readonly string[]).includes(kind.ref ?? kind.kind) && !offered.has(kind.kind) && catalog.some(action => action.target === kind.type && action.new))
    .map((kind, at) => {
      offered.add(kind.kind);
      return { id: kind.kind, label: t(kind.label), icon: kindIcon[kind.kind], separatorBefore: index > 0 && at === 0, disabled, run: () => editorCreatedKinds.has(kind.kind) ? open(resourceRoute(kind, "new", application)) : creators[kind.kind]!.take(undefined, kind.kind === "automation" ? { kind: "automation" } : undefined) };
    }));
  return <>
    <ActionMenu label={t("New resource")} variant="primary" icon={<Plus />} commands={commands}>{children ?? t("New")}</ActionMenu>
    {Object.values(creators).map((creator, index) => <span key={index}>{creator.dialog}</span>)}
  </>;
}

/** The project home: resource tree, what changed, settings. */
export function ProjectHome({ id, area, initialSelected, initialSection, initialPlan }: { id: string; area?: string; initialSelected?: string; initialSection?: string; initialPlan?: string }) {
  return installedStudioRef(id) ? <InstalledApplicationHome id={id} area={area} selected={initialSelected} />
    : <BuildProjectHome id={id} area={area} initialSelected={initialSelected} initialSection={initialSection} initialPlan={initialPlan} />;
}

function BuildProjectHome({ id, area, initialSelected, initialSection, initialPlan }: { id: string; area?: string; initialSelected?: string; initialSection?: string; initialPlan?: string }) {
  const activeArea = studioArea(area);
  const { client, decide, role, definitions, me } = useHost();
  const { open } = useWorkspace();
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id)}`);
  const project = query.data?.record;
  const builder = role("build") === "builder";
  const savedChanges = useSavedDraftChanges(id, undefined, builder);
  const { inventories, owned, available, shared, loading } = useProjectResources(project);
  const resourcesUnavailable = owned.some(resource => !resource.installed && inventories[resource.kind.kind]?.isError);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<string | undefined>(initialSelected);
  type Section = "resources" | "runs" | "roles" | "settings" | "concepts" | "guide" | "templates" | "release";
  const emptyProject = !!project && !project.pages?.length && !project.resources?.length;
  const initialTab = initialSection === "runs" ? "runs" : initialSection === "concepts" ? "concepts" : initialSection === "templates" && activeArea === "interface" ? "templates" : initialSection === "guide" || emptyProject && !area && !initialSelected ? "guide" : activeArea === "verify" ? "release" : "resources";
  const [tab, selectTab] = useState<Section>(initialTab);
  const [visited, setVisited] = useState<Section[]>([initialTab]);
  const visibleView = useViewVisible();
  const setTab = (tab: Section) => { selectTab(tab); setVisited(old => old.includes(tab) ? old : [...old, tab]); };
  useEffect(() => {
    setTab(initialTab);
    if (initialSelected !== undefined) setSelected(initialSelected);
  }, [activeArea, initialSection, initialSelected, initialPlan, initialTab]);
  const [settings, setSettings] = useState<{ title: string; name: string; description: string; icon: string; profile: Api.ApplicationProfile; extends?: Api.AssetRef; entry: string; replacements: NonNullable<Api.Application["replacements"]> }>();
  const [saving, setSaving] = useState(false), [error, setError] = useState<string>();
  const [importing, setImporting] = useState(false);
  const [importedDependencies, setImportedDependencies] = useState<ApplicationImportDependency[]>([]);
  const [createdResource, setCreatedResource] = useState<{ kind: ResourceKindInfo; target: { type: string; id: string } }>();
  const [pendingPatch, setPendingPatch] = useState<{ changes: Partial<Api.Application>; revision?: number; settings: boolean }>();
  const pendingRef = useRef(pendingPatch); pendingRef.current = pendingPatch;
  const latestProject = useRef(project); latestProject.current = project;
  const importRequest = useRef<{ changes: Partial<Api.Application>; revision: number; dependencies: ApplicationImportDependency[] } | undefined>(undefined);
  // Settings follow the record until this editor types into them (`edited`);
  // only typed changes are saved, and only the fields that differ. Comparing
  // a stale local copy against a record that another editor moved on would
  // otherwise "save" the old values back and the two would overwrite each
  // other once a second.
  const [edited, setEdited] = useState(false);
  const lock = useRef(false);
  const baseline = useRef<{ values: NonNullable<typeof settings>; revision: number } | undefined>(undefined);
  const fromRecord = (p: Project) => ({ title: p.title, name: p.name, description: p.description ?? "", icon: p.icon ?? "boxes", profile: structuredClone(p.profile ?? {}), extends: p.extends, entry: p.entry ?? "", replacements: structuredClone(p.replacements ?? []) });
  useEffect(() => {
    if (project && !edited) {
      const values = fromRecord(project);
      baseline.current = { values, revision: project.revision };
      setSettings(values);
    }
  }, [project, edited]);
  const changedSettings = useMemo(() => {
    if (!settings || !edited) return {} as Partial<Api.Application>;
    const current = baseline.current?.values;
    if (!current) return {} as Partial<Api.Application>;
    const out: Partial<Api.Application> = {};
    for (const key of ["title", "name", "description", "icon"] as const) if (settings[key] !== current[key]) out[key] = settings[key];
    if (JSON.stringify(settings.profile) !== JSON.stringify(current.profile)) out.profile = settings.profile;
    if (JSON.stringify(settings.extends) !== JSON.stringify(current.extends)) Object.assign(out, { extends: settings.extends ?? null });
    if (settings.entry !== current.entry) out.entry = settings.entry;
    if (JSON.stringify(settings.replacements) !== JSON.stringify(current.replacements)) out.replacements = settings.replacements;
    return out;
  }, [project, settings, edited]);
  const dirty = Object.keys(changedSettings).length > 0;
  const { markSaved, confirmDiscard } = useUnsavedChanges(dirty || !!pendingPatch || !!createdResource, () => {
    pendingRef.current = undefined; setPendingPatch(undefined); setCreatedResource(undefined);
    baseline.current = undefined; setEdited(false); if (project) setSettings(fromRecord(project)); setError(undefined);
  }, saving);
  const patch = async (changes: Partial<Api.Application>, revision = latestProject.current?.revision, settingsSave = false) => {
    if ((!latestProject.current && !pendingRef.current) || lock.current) return false;
    if (dirty && !settingsSave) { setError(t("Save or discard project settings before changing resource membership.")); return false; }
    const original = pendingRef.current;
    if (original && (JSON.stringify(original.changes) !== JSON.stringify(changes) || original.revision !== revision)) {
      setError(t("An earlier application update is unconfirmed. Retry its original payload before changing membership.")); return false;
    }
    const submitted = original ?? { changes: structuredClone(changes), revision, settings: settingsSave };
    lock.current = true; setSaving(true); setError(undefined);
    let confirmed = false;
    try {
      confirmed = await decide("build.app.edit", { type: "build.app", id }, submitted.changes, {
        expectedRevision: submitted.revision, quiet: true, onRefused: setError,
        onOutcome: entry => {
          const pending = entry.state === "SUBMISSION_STATE_PENDING" || entry.state === "SUBMISSION_STATE_UNKNOWN";
          pendingRef.current = pending ? submitted : undefined; setPendingPatch(pending ? submitted : undefined);
        },
      });
    } catch {
      pendingRef.current = submitted; setPendingPatch(submitted); setError(t("The application update is unconfirmed. Retry the original request."));
    }
    if (!confirmed) { lock.current = false; setSaving(false); return false; }
    pendingRef.current = undefined; setPendingPatch(undefined);
    // The write stands even if its projection cannot refresh. Never offer a
    // second write just because the subsequent read failed.
    try { const current = await query.refetch(); if (!current.isSuccess) setError(t("The application was saved, but its current record could not be reloaded.")); }
    catch { setError(t("The application was saved, but its current record could not be reloaded.")); }
    finally { lock.current = false; setSaving(false); }
    if (submitted.settings) { baseline.current = undefined; setEdited(false); markSaved(); }
    return true;
  };
  const editSettings = (next: NonNullable<typeof settings>) => {
    if (lock.current || pendingRef.current || !project) return;
    // Diff against the snapshot actually displayed, not a newer read that
    // arrived before this event. The owner's revision gate decides conflicts.
    if (!baseline.current) baseline.current = { values: settings ?? fromRecord(project), revision: project.revision };
    setEdited(true); setSettings(next);
  };
  const saveSettings = async () => {
    if (!dirty || !baseline.current) return !dirty;
    const confirmed = await patch(changedSettings, baseline.current.revision, true);
    if (confirmed) { baseline.current = undefined; setEdited(false); markSaved(); }
    return confirmed;
  };
  useAutoSave({ generation:JSON.stringify(changedSettings), dirty, enabled: !pendingPatch && !createdResource && !importing, busy: saving, save: saveSettings });
  const changesForResource = (current: Project, kind: ResourceKindInfo, record: ResourceRecord) => projectResourceChange(current, resourceRef(kind, record.name));
  const addCreated = async (kind: ResourceKindInfo, target: { type: string; id: string }) => {
    setCreatedResource({ kind, target });
    if (target.type !== kind.type || lock.current || pendingRef.current) return;
    try {
      const { record } = await client.get<{ record: ResourceRecord }>(`/v1/records/${encodeURIComponent(target.type)}/${encodeURIComponent(target.id)}`);
      const current = latestProject.current;
      if (!record || record.id !== target.id || record.archived || !current) {
        setError(t("The resource exists, but its project membership could not be read. Retry the same resource.")); return;
      }
      if (!await patch(changesForResource(current, kind, record), current.revision)) return;
      setCreatedResource(undefined); open(resourceRoute(kind, target.id, id), { ifCurrent: { view: "project", params: { id } } });
    } catch { setError(t("The resource exists, but its project membership could not be read. Retry the same resource.")); }
  };
  const retryPatch = async () => {
    const original = pendingRef.current;
    if (!original || !await patch(original.changes, original.revision, original.settings)) return;
    if (createdResource) { const resource = createdResource; setCreatedResource(undefined); open(resourceRoute(resource.kind, resource.target.id, id), { ifCurrent: { view: "project", params: { id } } }); }
  };
  const remove = async (ref: Api.AssetRef) => {
    const current = latestProject.current;
    if (!current) return;
    const changes = ref.kind === "page"
      ? { pages: (current.pages ?? []).filter(name => name !== ref.name), groups: (current.groups ?? []).map(group => ({ ...group, pages: group.pages.filter(name => name !== ref.name) })).filter(group => group.pages.length), replacements: (current.replacements ?? []).filter(mapping => mapping.page !== ref.name) }
      : { resources: (current.resources ?? []).filter(item => refKey(item) !== refKey(ref)) };
    if (await patch(changes, current.revision)) setSelected(undefined);
  };
  const unpublished = savedChanges.data?.changes ?? [];
  const releaseSelection = [...new Map([...unpublished.map(change => ({ kind: draftChangeKind(change), id: change.id })), ...importedDependencies].map(choice => [JSON.stringify([choice.kind, choice.id]), choice])).values()];
  // Prepare Verify once from the same saved changes as Publish. Later reads
  // must not replace an in-progress or unconfirmed release selection.
  const verificationSelection = useRef<typeof releaseSelection | undefined>(undefined);
  if (!verificationSelection.current && visited.includes("release") && savedChanges.isSuccess && !savedChanges.isFetching) verificationSelection.current = releaseSelection;
  const problems: WorkbenchProblem[] = owned.filter((item) => item.missing).map((item) => ({ id: refKey(item.ref), severity: "warning", text: t("{title} is listed, but no readable draft or published definition is available. Check its owner and access, or remove the reference.", { title: item.title }), subject: t(item.kind.label), locate: () => { setSelected(refKey(item.ref)); setTab("resources"); } }));
  const addExisting = (kind: ResourceKindInfo, record: ResourceRecord) => latestProject.current
    ? patch(changesForResource(latestProject.current, kind, record), latestProject.current.revision) : Promise.resolve(false);
  const installed = definitions.find((d) => d.ref.app === "build" && d.ref.kind === "app" && d.ref.name === project?.name);
  const visible = owned.filter(item => (tab === "concepts" || resourceInArea(item, activeArea)) && `${item.title} ${item.ref.name} ${t(item.kind.plural)}`.toLowerCase().includes(filter.toLowerCase()));
  const current = selected ? owned.find(item => refKey(item.ref) === selected || item.record?.id === selected || item.ref.name === selected) : undefined;
  const selectedKey = current ? refKey(current.ref) : selected;
  const dependencyQuery = useDependencies(current?.ref, undefined, !!current?.installed);
  if (!project) return <WorkbenchFrame title={t("Project")}>{query.isError ? t("The project could not be loaded.") : t("Loading…")}</WorkbenchFrame>;
  const statusOf = (item: OwnedResource) => item.record ? (item.record.state === "published" ? undefined : t("draft")) : item.installed ? t("published") : item.missing ? t("missing") : undefined;
  const tree = <div className="grid min-w-0 content-start">
    <div className="p-2"><Input type="search" aria-label={t("Filter resources")} placeholder={t("Filter resources")} value={filter} onChange={event => setFilter(event.target.value)} /></div>
    {conceptTree(project, visible).map(group => <PanelSection key={group.id} title={group.title}>
      {group.items.map(item => <StructureRow key={refKey(item.ref)} icon={kindIcon[item.kind.kind]} label={item.title} selected={selectedKey === refKey(item.ref)} meta={statusOf(item)} onClick={() => setSelected(refKey(item.ref))} />)}
    </PanelSection>)}
    {!visible.length && <p className="p-3 text-xs text-muted">{t("Nothing here yet. Use New, or Add existing.")}</p>}
  </div>;
  const conceptPatch = (concepts: Api.AppConcept[]) => patch({ concepts });
  const locked = saving || dirty || !!pendingPatch || !!createdResource;
  const dependencies = dependencyQuery.data;
  const header = installed?.application?.pages.find((name) => definitions.some((d) => d.ref.app === "build" && d.ref.kind === "page" && d.ref.name === name));
  return <>
    <Workbench storageKey="project" crumbs={[{ label: t("Applications"), onClick: () => open({ view: "projects" }) }]} title={project.title || project.name}
      status={<DraftStatus state={project.state} problems={problems.length} />} saving={savingState(dirty, saving, error)}
      actions={<>
        {header && <Button size="sm" onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: header, application: `build:${project.name}` } })}>{t("Open published app")}</Button>}
        {builder && installed?.application && <Button size="sm" disabled={saving || dirty || !!pendingPatch || !!createdResource} onClick={() => { importRequest.current = undefined; setImporting(true); }}>{t("Import Workshop module")}</Button>}
        {builder && <Button size="sm" disabled={saving || dirty || !!pendingPatch || !!createdResource} onClick={() => open({ view: "changes", params: { application: project.id } })}>{t("Changes")}{unpublished.length > 0 && <span className="ml-1 rounded-full bg-primary/15 px-1.5 text-[10px] text-primary">{unpublished.length}</span>}</Button>}
        {builder && (available.length > 0 || shared.length > 0) && <Select aria-label={t("Add existing resource")} value="" className="max-w-48" disabled={saving || dirty || !!pendingPatch || !!createdResource} onChange={(event) => {
          let choice: { kind?: string; id?: string; ref?: Api.AssetRef };
          try { choice = JSON.parse(event.target.value); } catch { return; }
          if (choice.kind === "shared" && choice.ref) {
            const found = shared.find(item => refKey(item.ref) === refKey(choice.ref!));
            const current = latestProject.current;
            if (found && current) void patch({ resources: [...(current.resources ?? []).filter(ref => refKey(ref) !== refKey(found.ref)), found.ref] }, current.revision);
            return;
          }
          const found = available.find(item => item.kind.kind === choice.kind && item.record.id === choice.id);
          if (found) void addExisting(found.kind, found.record); }}>
          <option value="">{t("Add existing…")}</option>
          {shared.length > 0 && <optgroup label={t("Published resources (all owners)")}>{shared.map((item) => <option key={refKey(item.ref)} value={JSON.stringify({ kind: "shared", ref: item.ref })}>{item.title} · {item.app} · {item.ref.kind}</option>)}</optgroup>}
          {resourceKinds.map((kind) => { const rows = available.filter((item) => item.kind === kind); return rows.length ? <optgroup key={kind.kind} label={t(kind.plural)}>{rows.map((item) => <option key={item.record.id} value={JSON.stringify({ kind: kind.kind, id: item.record.id })}>{item.record.title || item.record.name}</option>)}</optgroup> : null; })}
        </Select>}
        {builder && <NewResourceMenu application={id} area={activeArea} disabled={saving || dirty || !!pendingPatch || !!createdResource} onCreated={(kind, target) => void addCreated(kind, target)} />}
        {builder && <Button size="sm" variant="primary" disabled={saving || dirty || resourcesUnavailable || savedChanges.isPending || savedChanges.isError || !!pendingPatch || !!createdResource || (!project.pages?.length && !project.resources?.length)} onClick={() => open({ view: "release-review", params: { kind: "app", id: project.id, application: project.id, drafts: releaseDraftsValue(releaseSelection) } })}>{t("Publish")}</Button>}
      </>}
      dock={{ label: t("Project dock"), tabs: [{ id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} /> }] }}>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto">
        {resourcesUnavailable && <Notice tone="warning" role="alert" message={t("Some project assets are unreadable. Membership and release selection must not be inferred from an empty inventory.")} />}
        {project.extends && <div className="flex flex-wrap items-center gap-2 border-b border-border px-4 py-2 text-xs"><span>{t("Application extension")} · {project.extends.app}</span><Button size="sm" variant="ghost" onClick={() => open(studioRoute(installedStudioID(project.extends!), activeArea))}>{t("Open original application")}</Button></div>}
        {error && tab !== "settings" && <p role="alert" className="p-3 text-sm text-danger">{error}</p>}
        {pendingPatch && <div className="grid gap-2 p-3"><Notice tone="warning" role="alert" message={t("An application update may already have been accepted. Retry its original payload; leaving does not undo accepted changes.")} />
          <div><Button disabled={saving} onClick={() => void retryPatch()}>{t("Retry original application update")}</Button></div></div>}
        {createdResource && !pendingPatch && <div className="grid gap-2 p-3"><Notice tone="warning" role="alert" message={t("The resource was created but is not confirmed in this project. Retry adding the same record; do not create another.")} />
          <div className="flex gap-2"><Button disabled={saving || dirty} onClick={() => void addCreated(createdResource.kind, createdResource.target)}>{t("Retry adding created resource")}</Button>
            <Button variant="ghost" disabled={saving} onClick={() => confirmDiscard(() => setCreatedResource(undefined))}>{t("Keep resource without project membership")}</Button></div></div>}
        <div role="tablist" aria-label={t("Project sections")} className="flex flex-wrap gap-1 border-b border-border px-3 py-1">
          {(["resources", ...(activeArea === "verify" ? ["release"] as const : []), "concepts", "guide", "roles", "runs", "settings"] as const).map(item => <Button variant="ghost" size="sm" key={item} role="tab" aria-selected={tab === item} onClick={() => setTab(item)}>{t(item === "resources" ? "Overview" : item === "release" ? "Verification and delivery" : item === "concepts" ? "Business concepts" : item === "guide" ? "Guided start" : item === "runs" ? "Runs" : item === "roles" ? "Roles" : "Settings")}</Button>)}
        </div>
        {tab === "resources" && <div className="flex flex-wrap items-center gap-2 px-4 pt-3 text-xs">
          <strong>{t(studioAreas.find(item => item.id === activeArea)!.description)}</strong>
          {activeArea === "model" && <Button size="sm" onClick={() => open({ view: "object-type", params: { application: id } })}>{t("Open business model")}</Button>}
          {activeArea === "interface" && <><Button size="sm" onClick={() => open({ view: "module", params: { id, application: id } })}>{t("Navigation and header")}</Button><Button size="sm" disabled={locked} onClick={() => setTab("templates")}>{t("New page from template…")}</Button></>}
        </div>}
        {tab === "resources" && <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
          <div className="grid content-start gap-4">
            {current ? <div className="rounded-md border border-border p-4">
              <div className="flex items-center gap-2">{kindIcon[current.kind.kind]}<h2 className="min-w-0 flex-1 truncate font-semibold">{current.title}</h2><DraftStatus state={current.record?.state} /></div>
              <p className="mt-1 text-xs text-muted">{t(current.kind.label)} · {current.ref.name}</p>
              {!current.record && current.installed && <p className="mt-2 text-xs text-muted">{t("This application references a published asset. Adding or removing the reference does not change its owner, definition or permissions.")}</p>}
              {current.missing && <p className="mt-2 text-xs text-danger">{t("{title} is listed, but no readable draft or published definition is available. Check its owner and access, or remove the reference.", { title: current.title })}</p>}
              {current.ref.kind === "object" && current.record && <div className="mt-2 flex flex-wrap gap-1">
                {(["actions", "lifecycle", "constraints", "sets", "data"] as const).map(section => <Button size="sm" variant="ghost" key={section} onClick={() => open({ view: "object-type", params: { id: current.record!.id, application: id, tab: section } })}>{t(section === "actions" ? "Actions and rules" : section === "lifecycle" ? "States" : section === "constraints" ? "Business constraints" : section === "sets" ? "Record sets" : "Record samples")}</Button>)}
              </div>}
              {dependencies && <div className="mt-2 grid gap-2 text-xs">
                {[{ title: "Uses", refs: dependencies.requires }, { title: "Used by", refs: dependencies.usedBy }].map(group => <div key={group.title}><strong>{t(group.title)}</strong><div className="flex flex-wrap gap-1">{group.refs.map(ref => <Button key={refKey(ref)} size="sm" variant="ghost" onClick={() => open(assetRoute(ref, owned, definitions, id))}>{ref.name}</Button>)}</div></div>)}
              </div>}
              {dependencyQuery.isError && <p role="alert" className="text-xs text-danger">{t("Asset dependencies could not be loaded.")}</p>}
              {!current.installed && <p className="text-xs text-muted">{t("Draft dependencies are checked by the release owner. Published links use declared dependencies.")}</p>}
              <div className="mt-3 flex flex-wrap gap-2">
                {(current.record || current.installed) && <Button size="sm" variant="primary" onClick={() => open(assetRoute(current.ref, owned, definitions, project.id))}>{t(!current.record && current.ref.kind === "object" ? "Open in object model" : "Open")}</Button>}
                {builder && current.missing && current.kind !== publishedResourceKind && <NewResourceMenu application={id} disabled={saving || dirty || !!pendingPatch || !!createdResource} onCreated={(kind, target) => void addCreated(kind, target)}>{t("Create it")}</NewResourceMenu>}
                {builder && <Button size="sm" disabled={saving || dirty || !!pendingPatch || !!createdResource} onClick={() => void remove(current.ref)}>{t("Remove from project")}</Button>}
              </div>
            </div> : <div className="rounded-md border border-dashed border-border p-6 text-sm text-muted">
              {project.description || t("Choose a resource on the left, create one with New, or bring one in with Add existing.")}
            </div>}
            <div className="grid gap-2">
              <h2 className="text-sm font-semibold">{t("Unpublished changes")} <span className="text-muted">({unpublished.length})</span></h2>
              {savedChanges.isError && <p role="alert" className="text-xs text-danger">{t("Saved changes could not be loaded.")}</p>}
              {!savedChanges.isError && !unpublished.length && <p className="text-xs text-muted">{loading || savedChanges.isPending ? t("Loading…") : t("No saved changes. Unsaved editor input is not included.")}</p>}
              <ul className="divide-y divide-border rounded-md border border-border">
                {unpublished.slice(0, 12).map((item) => <li key={`${item.type}/${item.id}`}>
                  <Button variant="row" type="button" className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-row-hover" onClick={() => open({ view: "changes", params: { application: project.id, kind: draftChangeKind(item), id: item.id } })}>
                    <span className="text-muted [&>svg]:size-4">{kindIcon[resourceKinds.find(kind => kind.type === item.type)?.kind ?? "definition"]}</span><span className="min-w-0 flex-1 truncate">{item.title}</span><span className="text-xs text-muted">{item.fields.map(field => field.name).join(", ")}</span><DraftStatus state="draft" />
                  </Button>
                </li>)}
              </ul>
            </div>
          </div>
          <aside className="grid content-start gap-3 text-sm">
            <div className="rounded-md border border-border p-3">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Contents")}</h3>
              <dl className="mt-2 grid grid-cols-2 gap-1 text-xs">
                {[...resourceKinds, publishedResourceKind].map((kind) => { const n = owned.filter((item) => item.kind === kind).length; return n ? <div key={kind.kind} className="contents"><dt className="text-muted">{t(kind.plural)}</dt><dd className="text-right">{n}</dd></div> : null; })}
              </dl>
            </div>
            <div role="region" aria-label={t("Application progress")} className="rounded-md border border-border p-3 text-xs">
              <h3 className="font-semibold">{t("Application progress")}</h3>
              <p className="mt-2">{t("{models} models · {pages} pages · {drafts} saved changes", { models: owned.filter(item => item.ref.kind === "object").length, pages: owned.filter(item => item.ref.kind === "page").length, drafts: unpublished.length })}</p>
              <p>{t("{count} missing assets", { count: problems.length })}</p>
              <p className="mt-2 text-muted">{t("The release owner checks model completeness, rules and page bindings before delivery.")}</p>
              <Button size="sm" className="mt-2" onClick={() => open(studioRoute(id, "verify", selected))}>{t("Verify application")}</Button>
            </div>
            <div className="rounded-md border border-border p-3 text-xs text-muted">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Release")}</h3>
              <p className="mt-2">{installed ? t("A published version is in the Applications portal. Saved edits remain drafts until the next release.") : t("Not published yet. Publish reviews every draft in this project and activates them together.")}</p>
            </div>
          </aside>
        </div>}
        {tab === "concepts" && <div className="grid gap-3 p-4" role="region" aria-label={t("Business concepts")}>
          <p className="text-xs text-muted">{t("Name groups in your business vocabulary. Moving an asset changes navigation only.")}</p>
          {(project.concepts ?? []).map((group, at) => <div key={group.id} className="grid gap-2 rounded-md border border-border p-3">
            <div className="flex flex-wrap gap-2"><ConceptTitle group={group} disabled={!builder || locked} onRename={title => conceptPatch(project.concepts!.map(item => item.id === group.id ? { ...item, title } : item))} />
              <Button size="sm" variant="ghost" aria-label={t("Move concept up")} disabled={!builder || locked || at === 0} onClick={() => { const groups = [...project.concepts!]; [groups[at - 1], groups[at]] = [groups[at]!, groups[at - 1]!]; conceptPatch(groups); }}><ArrowUp /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Move concept down")} disabled={!builder || locked || at === project.concepts!.length - 1} onClick={() => { const groups = [...project.concepts!]; [groups[at], groups[at + 1]] = [groups[at + 1]!, groups[at]!]; conceptPatch(groups); }}><ArrowDown /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Remove concept")} disabled={!builder || locked} onClick={() => conceptPatch(project.concepts!.filter(item => item.id !== group.id))}><Trash2 /></Button>
            </div>
            <p className="text-xs text-muted">{group.assets.map(ref => owned.find(item => refKey(item.ref) === refKey(ref))?.title).filter(Boolean).join(" · ") || t("No assets in this concept.")}</p>
          </div>)}
          {builder && <Button size="sm" disabled={locked || (project.concepts?.length ?? 0) >= 100} onClick={() => conceptPatch([...(project.concepts ?? []), { id: crypto.randomUUID(), title: t("New business concept"), assets: [] }])}><Plus />{t("Add business concept")}</Button>}
          <div className="rounded-md border border-border">{tree}</div>
          {current && <label className="grid gap-1 text-xs">{t("Move {asset} to concept", { asset: current.title })}<Select aria-label={t("Business concept for selected asset")} value={project.concepts?.find(group => group.assets.some(ref => refKey(ref) === selectedKey))?.id ?? ""} disabled={!builder || locked} onChange={event => conceptPatch(moveConceptAsset(project.concepts ?? [], current.ref, event.target.value))}>
            <option value="">{t("Default business namespace")}</option>{project.concepts?.map(group => <option key={group.id} value={group.id}>{group.title}</option>)}
          </Select></label>}
        </div>}
        {visited.includes("guide") && <div hidden={tab !== "guide"}><ViewVisibilityContext.Provider value={visibleView && tab === "guide"}><StudioGuide project={project} owned={owned} disabled={saving || !!pendingPatch || !!createdResource} intent={settings?.description ?? project.description ?? ""} onIntent={description => settings && editSettings({ ...settings, description })} onCreated={(kind, target) => void addCreated(kind, target)} /></ViewVisibilityContext.Provider></div>}
        {visited.includes("templates") && <div hidden={tab !== "templates"} className="p-4"><ViewVisibilityContext.Provider value={visibleView && tab === "templates"}><StudioTemplates onCreated={target => void addCreated(resourceKinds.find(kind => kind.kind === "page")!, target)} /></ViewVisibilityContext.Provider></div>}
        {visited.includes("release") && <div hidden={tab !== "release"}><ViewVisibilityContext.Provider value={visibleView && tab === "release"}><div className="grid gap-4 p-4" role="region" aria-label={t("Verification and delivery")}>
          <CandidateTest applicationId={id} planId={initialPlan} embedded />
 {installed && <ApplicationRuntimePanel owner="build" name={project.name} />}
          {verificationSelection.current ? <ReleaseReview initialKind="app" initialID={id} initialDrafts={verificationSelection.current} embedded /> : <p role={savedChanges.isError ? "alert" : "status"}>{t(savedChanges.isError ? "Saved changes could not be loaded." : "Loading…")}</p>}
        </div></ViewVisibilityContext.Provider></div>}
        {visited.includes("runs") && <div hidden={tab !== "runs"}><ViewVisibilityContext.Provider value={visibleView && tab === "runs"}><ApplicationRuns owner="build" name={project.name} project={id} /></ViewVisibilityContext.Provider></div>}
        {tab === "roles" && <ProjectRoles owned={owned} />}
        {tab === "settings" && settings && <fieldset disabled={saving || !!pendingPatch || !!createdResource} className="grid max-w-xl gap-3 p-4">
          <ExtensionSettings value={settings} pages={project.pages ?? []} disabled={!builder} onChange={extension => editSettings({ ...settings, ...extension, entry: extension.entry ?? "", replacements: extension.replacements ?? [] })} />
          <label className="grid gap-1 text-xs">{t("Title")}<Input value={settings.title} disabled={!builder} onChange={(event) => editSettings({ ...settings, title: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Name")}<Input value={settings.name} disabled={!builder} onChange={(event) => editSettings({ ...settings, name: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Icon")}<IconPicker value={settings.icon} disabled={!builder} onChange={(icon) => editSettings({ ...settings, icon })} /></label>
          <label className="grid gap-1 text-xs">{t("Description")}<Textarea rows={3} value={settings.description} disabled={!builder} onChange={(event) => editSettings({ ...settings, description: event.target.value })} /></label>
          <PanelSection title={t("Capacity and recovery declarations")}>
            <p className="text-xs text-muted">{t("Declare the expected scale and retention for this application. Recovery targets require evidence from its environment.")}</p>
            {owned.filter(resource => resource.ref.kind === "object").map(resource => <label className="grid gap-1 text-xs" key={refKey(resource.ref)}>
              {t("Expected records: {type}", { type: resource.title })}
              <Input type="number" optional step={1} max={Number.MAX_SAFE_INTEGER} min={1} value={settings.profile.expectedRecords?.[resource.ref.name] ?? ""} disabled={!builder} onChange={event => {
                const expectedRecords = { ...settings.profile.expectedRecords };
                if (event.target.value) expectedRecords[resource.ref.name] = event.target.valueAsNumber; else delete expectedRecords[resource.ref.name];
                editSettings({ ...settings, profile: { ...settings.profile, expectedRecords } });
              }} />
            </label>)}
            <label className="grid gap-1 text-xs">{t("Expected runs")}<Input type="number" optional step={1} max={Number.MAX_SAFE_INTEGER} min={0} value={settings.profile.expectedRuns ?? ""} disabled={!builder} onChange={event => editSettings({ ...settings, profile: { ...settings.profile, expectedRuns: event.target.value ? event.target.valueAsNumber : undefined } })} /></label>
            {(["runs", "logs", "effects", "ai", "compute"] as const).map(kind => <label className="grid gap-1 text-xs" key={kind}>
              {t("Retention: {kind}", { kind: t(kind) })}
              <Input value={settings.profile.retention?.[kind] ?? ""} placeholder="168h" disabled={!builder} onChange={event => {
                const retention = { ...settings.profile.retention };
                if (event.target.value) retention[kind] = event.target.value; else delete retention[kind];
                editSettings({ ...settings, profile: { ...settings.profile, retention } });
              }} />
            </label>)}
            <label className="grid gap-1 text-xs">{t("Maximum acceptable data loss period")}<Input value={settings.profile.rpo ?? ""} placeholder="1h" disabled={!builder} onChange={event => editSettings({ ...settings, profile: { ...settings.profile, rpo: event.target.value } })} /></label>
            <label className="grid gap-1 text-xs">{t("Target recovery time")}<Input value={settings.profile.rto ?? ""} placeholder="4h" disabled={!builder} onChange={event => editSettings({ ...settings, profile: { ...settings.profile, rto: event.target.value } })} /></label>
            <p className="text-xs text-muted">{t("Use durations such as 24h, 168h or 30m. These declarations do not delete data or grant access.")}</p>
          </PanelSection>
          {error && <p role="alert" className="text-xs text-danger">{error}</p>}
          <p className="text-xs text-muted">{t("Changes save automatically.")}</p>
          <div className="flex gap-2">
            <Button size="sm" disabled={!dirty || saving} onClick={() => void saveSettings()}>{t("Save")}</Button>
            <Button size="sm" variant="ghost" disabled={saving} onClick={() => confirmDiscard(() => { baseline.current = undefined; setEdited(false); if (project) setSettings(fromRecord(project)); void query.refetch(); })}>{t("Reload current project settings")}</Button>
          </div>
        </fieldset>}
      </div>
    </Workbench>
    {installed?.application && <WorkshopApplicationImport key={JSON.stringify([project.id, me])} open={importing} onClose={() => setImporting(false)} application={{ ref: installed.ref, sourceVersion: installed.version }}
      onReviewMembership={async () => {
        if (pendingRef.current || lock.current) { setError(t("An earlier application update is unconfirmed. Retry its original payload before changing membership.")); return false; }
        if (dirty) { setError(t("Save or discard project settings before changing resource membership.")); return false; }
        const fresh = await query.refetch();
        if (!fresh.isSuccess || !fresh.data?.record) return false;
        latestProject.current = fresh.data.record;
        // Only this explicit operator choice can adopt a new revision after
        // refusal. Background updates cannot replace the original request.
        importRequest.current = undefined;
        return true;
      }}
      onPrepared={async ({ pages, header, dependencies }) => {
        if (!importRequest.current) {
          const current = latestProject.current;
          if (!current) return false;
          importRequest.current = {
            changes: { pages: [...pages, ...(current.pages ?? []).filter(name => !pages.includes(name))], ...(header ? { header, uiProfile: pageUIProfile } : {}) },
            revision: current.revision, dependencies: structuredClone(dependencies),
          };
        }
        const submitted = importRequest.current;
        const confirmed = await patch(submitted.changes, submitted.revision);
        if (confirmed) { setImportedDependencies(submitted.dependencies); importRequest.current = undefined; }
        return confirmed;
      }} />}
  </>;
}

function ConceptTitle({ group, disabled, onRename }: { group: Api.AppConcept; disabled: boolean; onRename: (title: string) => Promise<boolean> }) {
  const input = useDraftInput(`concept:${group.id}`, group.title);
  const rename = async () => {
    const title = input.text.trim();
    if (!title || input.problem) return;
    if (title === group.title || await onRename(title)) input.clear();
  };
  return <label className="grid min-w-0 flex-1 gap-1"><Input aria-label={t("Business concept title")} value={input.text} aria-invalid={!!input.problem} disabled={disabled} onChange={event => input.write(event.target.value, event.target.value.trim() ? "" : t("A business concept needs a title."))} onBlur={() => void rename()} />{input.problem && <span role="alert" className="text-xs text-danger">{input.problem}</span>}</label>;
}

function WorkbenchFrame({ title, children }: { title: string; children: ReactNode }) {
  return <Workbench storageKey="project" title={title}><div className="flex flex-1 items-center justify-center text-sm text-muted">{children}</div></Workbench>;
}
