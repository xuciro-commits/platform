import { sourceFieldInput } from "@platform/app";
import { ContextHelp, PageHeader, StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue, declarationChanges } from "../editor/declaration";
import { CandidateTest } from "../releases/simulate";
import { ReleaseReview } from "../releases/release";
import { ApplicationRuns, ApplicationRuntimePanel, useDependencies, useHost, useNewRecord, useReadQuery, useRecordInventory } from "@platform/app";
import { CompositeRefusalError, type Api } from "@platform/kernel";
import { ActionMenu, Button, Checkbox, Dialog, IconPicker, Input, Notice, Panel, PanelSection, ProblemList, Select, StructureRow, Textarea, Workbench, ViewVisibilityContext, humanizeKernelError, t, useDraftInput, useUnsavedChanges, useViewVisible, useWorkspace, type ContextCommand, type WorkbenchProblem } from "@platform/ui";
import { ArrowDown, ArrowUp, FolderKanban, Plus, Trash2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { DraftStatus, savingState, useAutoSave } from "../editor/workbench";
import { ProjectRoles } from "./project-roles";
import { publishedResourceKind, refKey, resourceKinds, resourceRef, resourceRoute, type ResourceKindInfo, type ResourceRecord } from "./resources";
import { projectResourceChange } from "./membership";
import { BranchChangesPanel } from "../branches/BranchChangesPanel";
import { useBranches } from "../branches/api";
import { useCurrentBranch } from "../branches/current";

export type Project = Api.Application & { id: string; revision: number; state: string; archived?: boolean; changed?: Api.Stamp };

import { kindIcon } from "./resource-icons";
import { useProjectResources, type OwnedResource } from "./resource-inventory";
import { retireLimit, retirementBatch, retirementIntent } from "./retirement";
export type { OwnedResource } from "./resource-inventory";
import { assetRoute, conceptTree, moveConceptAsset, resourceInArea, studioArea, studioAreas, studioResourceGroups, studioRoute } from "./studio-model";
import { StudioTemplates } from "../workshop/template-ui";
import { StudioGuide } from "./studio-guide";
import { installedStudioID, installedStudioRef, studioApplications } from "./studio-applications";
import { InstalledApplicationHome } from "./InstalledApplicationHome";
import { ExtensionSettings } from "./ExtensionSettings";
import { ApplicationLifecycleControls } from "./application-lifecycle";

export function ProjectsList() {
  const { me, definitions , holds} = useHost();
  const { open } = useWorkspace();
  const projects = useRecordInventory<Project>("build.app", 1000, holds("build", "builder"));
  const installedApps = useReadQuery<Api.AppInfo[]>("/v1/apps");
  const buildGrants = (me.profile.grants ?? []).filter((g) => g.app === "build" && g.role === "builder");
  const delegated = buildGrants.length > 0 && buildGrants.every((g) => g.by?.startsWith("project:")) ? buildGrants.map((g) => g.reason || g.by!.slice(8)) : [];
  const [search, setSearch] = useState("");
  const create = useNewRecord("build.app", (target) => open({ view: "project", params: { id: target.id } }));
  const builder = holds("build", "builder");
  const rows = studioApplications(installedApps.data ?? [], definitions, projects.data?.records ?? [])
    .filter(item => `${item.title} ${item.ref.app} ${item.ref.name}`.toLowerCase().includes(search.toLowerCase()));
  return <div className="grid min-w-0 gap-4">
    <PageHeader title={t("Applications")} help={t("Inspect installed applications and extend them through the same drafts and releases. New from template opens the package library: an installed declarative package becomes editable application drafts through its original review and draft generation.")} actions={<div className="flex w-full min-w-0 flex-wrap items-center gap-2 sm:w-auto">
        <Input type="search" aria-label={t("Search projects")} placeholder={t("Search projects")} value={search} onChange={(event) => setSearch(event.target.value)} className="min-w-0 flex-1 basis-56 sm:w-56 sm:flex-none" />
        {builder && create.available && <Button className="shrink-0" variant="primary" onClick={() => create.take()}><Plus />{t("New application")}</Button>}
        {builder && <Button className="shrink-0" variant="ghost" onClick={() => open({ view: "apps" })}>{t("New from template…")}</Button>}
      </div>} />
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
          <p className="text-xs text-muted">{t("{pages} pages · {resources} resources", { pages: (item.project ?? item.definition?.application)?.pages?.length ?? 0, resources: (item.project ?? item.definition?.application)?.resources?.length ?? 0 })}</p>
        </Button>
        {item.project && <div className="mt-2 flex justify-end"><ApplicationLifecycleControls project={item.project} /></div>}
      </li>)}
    </ul>
    {create.dialog}
  </div>;
}

const editorCreatedKinds = new Set(["table", "alert", "function", "agent", "evaluation-set", "sample-set", "test-plan", "connection", "source", "dataset", "pipeline", "writeback", "matching"]);

function NewResourceMenu({ application, onCreated, children, disabled, area }: { application: string; area?: string; onCreated?: (kind: ResourceKindInfo, target: { type: string; id: string }) => void; children?: ReactNode; disabled?: boolean }) {
  const { catalog , holds} = useHost();
  const { open } = useWorkspace();
  const creators = Object.fromEntries(resourceKinds.filter(kind => !editorCreatedKinds.has(kind.kind)).map((kind) => [kind.kind, useNewRecord(kind.type, (target) => onCreated?.(kind, target))])) as Record<string, ReturnType<typeof useNewRecord>>;
  if (!holds("build", "builder")) return null;
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

export function ProjectHome({ id, area, initialSelected, initialSection, initialPlan, initialTemplate }: { id: string; area?: string; initialSelected?: string; initialSection?: string; initialPlan?: string; initialTemplate?: string }) {
  return installedStudioRef(id) ? <InstalledApplicationHome id={id} area={area} selected={initialSelected} />
    : <BuildProjectHome id={id} area={area} initialSelected={initialSelected} initialSection={initialSection} initialPlan={initialPlan} initialTemplate={initialTemplate} />;
}

function BuildProjectHome({ id, area, initialSelected, initialSection, initialPlan, initialTemplate }: { id: string; area?: string; initialSelected?: string; initialSection?: string; initialPlan?: string; initialTemplate?: string }) {
  const activeArea = studioArea(area, initialSelected?.split("/")[1]);
  const { client, decide, definitions, action , holds} = useHost();
  const { open, notify } = useWorkspace();
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id)}`);
  const project = query.data?.record;
  const builder = holds("build", "builder");
  const { id: currentBranch } = useCurrentBranch();
  const branchCount = useBranches(builder).data?.find(item => item.id === currentBranch)?.resources.length ?? 0;

  const [filter, setFilter] = useState("");
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [retireBusy, setRetireBusy] = useState(false);
  const [retireIntent, setRetireIntent] = useState<(ReturnType<typeof retirementIntent<OwnedResource>> & { client: typeof client; attempted?: boolean; refusal?: string })>();
  const pendingRetirement = useRef(retireIntent); pendingRetirement.current = retireIntent;
  const retireLock = useRef(false);
  const rememberRetirement = (intent: typeof retireIntent) => { pendingRetirement.current = intent; setRetireIntent(intent); };
  const retireable = (item: { kind: { type: string }; record?: { id: string } | undefined }) => !!item.record && !!action(`${item.kind.type}.archive`);
  const [selected, setSelected] = useState<string | undefined>(initialSelected);
  type Section = "resources" | "runs" | "roles" | "settings" | "concepts" | "guide" | "templates" | "release";
  const emptyProject = !!project && !project.pages?.length && !project.resources?.length;
  const initialTab = initialSection === "runs" ? "runs" : initialSection === "concepts" ? "concepts" : initialSection === "templates" && activeArea === "interface" ? "templates" : initialSection === "guide" || emptyProject && !area && !initialSelected ? "guide" : activeArea === "release" ? "release" : "resources";
  const [tab, selectTab] = useState<Section>(initialTab);
  const [visited, setVisited] = useState<Section[]>([initialTab]);
  const { inventories, owned, available, shared } = useProjectResources(project, "build", builder);
  const resourcesUnavailable = owned.some(resource => !resource.installed && inventories[resource.kind.kind]?.isError);
  const visibleView = useViewVisible();
  const setTab = (tab: Section) => { selectTab(tab); setVisited(old => old.includes(tab) ? old : [...old, tab]); };
  useEffect(() => {
    setTab(initialTab);
    setSelected(initialSelected);
  }, [activeArea, initialSection, initialSelected, initialPlan, initialTab]);
  const trial = useActionTrial(), declarationHost = useHost();
  const declarationFields = declarationHost.source.entity("build.app")?.fields ?? [];
  const [sourceApplied, setSourceApplied] = useState(false);
  const [settings, setSettings] = useState<Partial<Api.Application> & { title: string; name: string; description: string; icon: string; profile: Api.ApplicationProfile; entry: string; replacements: NonNullable<Api.Application["replacements"]> }>();
  const [saving, setSaving] = useState(false), [error, setError] = useState<string>();
  const [createdResource, setCreatedResource] = useState<{ kind: ResourceKindInfo; target: { type: string; id: string } }>();
  const [pendingPatch, setPendingPatch] = useState<{ changes: Partial<Api.Application>; revision?: number; settings: boolean }>();
  const pendingRef = useRef(pendingPatch); pendingRef.current = pendingPatch;
  const latestProject = useRef(project); latestProject.current = project;
  const [edited, setEdited] = useState(false);
  const lock = useRef(false);
  const baseline = useRef<{ values: NonNullable<typeof settings>; revision: number } | undefined>(undefined);
  const fromRecord = (p: Project) => ({ ...declarationValue(p, declarationFields), title: p.title, name: p.name, description: p.description ?? "", icon: p.icon ?? "boxes", profile: structuredClone(p.profile ?? {}), extends: p.extends, entry: p.entry ?? "", replacements: structuredClone(p.replacements ?? []) });
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
    const out = declarationChanges(settings, current) as Partial<Api.Application>;
    return out;
  }, [project, settings, edited]);
  const dirty = Object.keys(changedSettings).length > 0;
  const { markSaved, confirmDiscard } = useUnsavedChanges(dirty || !!pendingPatch || !!createdResource || !!retireIntent, () => {
    pendingRef.current = undefined; setPendingPatch(undefined); setCreatedResource(undefined);
    rememberRetirement(undefined);
    baseline.current = undefined; setEdited(false); if (project) setSettings(fromRecord(project)); setError(undefined);
  }, saving || retireBusy || !!retireIntent?.attempted && !retireIntent.refusal);
  const patch = async (changes: Partial<Api.Application>, revision = latestProject.current?.revision, settingsSave = false) => {
    if ((!latestProject.current && !pendingRef.current) || lock.current || pendingRetirement.current) return false;
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
    try { const current = await query.refetch(); if (!current.isSuccess) setError(t("The application was saved, but its current record could not be reloaded.")); }
    catch { setError(t("The application was saved, but its current record could not be reloaded.")); }
    finally { lock.current = false; setSaving(false); }
    if (submitted.settings) { baseline.current = undefined; setEdited(false); markSaved(); }
    return true;
  };
  const editSettings = (next: NonNullable<typeof settings>) => {
    if (lock.current || pendingRef.current || !project) return;
    if (!baseline.current) baseline.current = { values: settings ?? fromRecord(project), revision: project.revision };
    setSourceApplied(false); setEdited(true); setSettings(next);
  };
  const saveSettings = async () => {
    if (!dirty || !baseline.current) return !dirty;
    const confirmed = await patch(changedSettings, baseline.current.revision, true);
    if (confirmed) { baseline.current = undefined; setEdited(false); markSaved(); }
    return confirmed;
  };
  useAutoSave({ generation:JSON.stringify(changedSettings), dirty, enabled: !sourceApplied && !pendingPatch && !createdResource, busy: saving, save: saveSettings });
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
  const beginRetire = () => {
    if (lock.current || pendingRetirement.current || dirty || pendingPatch || createdResource) return;
    const items = retirementBatch(owned, picked, item => refKey(item.ref));
    if (items.length > retireLimit) { notify(t("A single retirement accepts at most 64 assets. Select a smaller batch.")); return; }
    if (items.length !== picked.size || items.some(item => !retireable(item))) { notify(t("Some selected assets are no longer available for retirement. Refresh the selection before continuing.")); return; }
    if (!items.length) return;
    rememberRetirement({ ...retirementIntent(crypto.randomUUID(), items), client });
  };
  const retire = async () => {
    const intent = pendingRetirement.current;
    if (!intent || intent.refusal || retireLock.current) return;
    if (intent.client !== client) { notify(t("Return to the original workspace identity before retrying this request.")); return; }
    retireLock.current = true;
    rememberRetirement({ ...intent, attempted: true });
    setRetireBusy(true);
    try {
      const answer = await intent.client.composite(intent.key, intent.edits);
      if (answer.key !== intent.key || answer.applied !== intent.items.length) throw Error(t("The retirement result did not confirm the reviewed batch. Retry its original request."));
      notify(t("{count} assets retired together.", { count: intent.items.length }));
      setPicked(new Set()); rememberRetirement(undefined);
    } catch (problem) {
      if (problem instanceof CompositeRefusalError) rememberRetirement({ ...intent, attempted: true, refusal: problem.message });
      notify(humanizeKernelError(problem instanceof Error ? problem.message : String(problem)));
    } finally { retireLock.current = false; setRetireBusy(false); }
  };
  const remove = async (ref: Api.AssetRef) => {
    const current = latestProject.current;
    if (!current) return;
    const changes = ref.kind === "page"
      ? { pages: (current.pages ?? []).filter(name => name !== ref.name), groups: (current.groups ?? []).map(group => ({ ...group, pages: group.pages.filter(name => name !== ref.name) })).filter(group => group.pages.length), replacements: (current.replacements ?? []).filter(mapping => mapping.page !== ref.name) }
      : { resources: (current.resources ?? []).filter(item => refKey(item) !== refKey(ref)) };
    if (await patch(changes, current.revision)) setSelected(undefined);
  };
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
    {(tab === "concepts" ? conceptTree(project, visible) : studioResourceGroups(project, activeArea, visible)).map(group => <PanelSection key={group.id} title={group.title}>
      {group.items.map(item => <StructureRow key={refKey(item.ref)} icon={kindIcon[item.kind.kind]} label={item.title} selected={selectedKey === refKey(item.ref)} meta={statusOf(item)} onClick={() => setSelected(refKey(item.ref))} />)}
    </PanelSection>)}
    {!visible.length && <p className="p-3 text-xs text-muted">{t("Nothing here yet. Use New, or Add existing.")}</p>}
  </div>;
  const conceptPatch = (concepts: Api.AppConcept[]) => patch({ concepts });
  const locked = saving || dirty || !!pendingPatch || !!createdResource || !!retireIntent;
  const dependencies = dependencyQuery.data;
  const header = installed?.application?.pages.find((name) => definitions.some((d) => d.ref.app === "build" && d.ref.kind === "page" && d.ref.name === name));
  return <>
    <Workbench storageKey="project" crumbs={[{ label: t("Applications"), onClick: () => open({ view: "projects" }) }]} title={project.title || project.name}
      status={<DraftStatus state={project.state} problems={problems.length} />} saving={savingState(dirty, saving, error)}
      actions={<>
        {builder && <ApplicationLifecycleControls project={project} disabled={saving || !!pendingPatch || !!createdResource || !!retireIntent} before={confirmDiscard} />}
        {header && <Button size="sm" onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: header, application: `build:${project.name}` } })}>{t("Open published app")}</Button>}
        {builder && <Button size="sm" disabled={locked} onClick={() => open({ view: "changes", params: { application: project.id } })}>{t("Branches")}{branchCount > 0 && <span className="ml-1 rounded-full bg-primary/15 px-1.5 text-2xs text-primary">{branchCount}</span>}</Button>}
        {builder && (available.length > 0 || shared.length > 0) && <Select aria-label={t("Add existing resource")} value="" className="max-w-48" disabled={locked} onChange={(event) => {
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
        {builder && <NewResourceMenu application={id} area={activeArea} disabled={locked} onCreated={(kind, target) => void addCreated(kind, target)} />}
        {builder && <Button size="sm" variant="primary" disabled={locked || resourcesUnavailable || !!currentBranch || (!project.pages?.length && !project.resources?.length)} title={currentBranch ? t("A branch is published by merging it. Use Save in the branch bar.") : undefined} onClick={() => {
            const route = { view: "release-review" as const, params: { kind: "app", id: project.id, application: project.id, applicationName: project.name } };
            open(route);
          }}>{t("Publish")}</Button>}
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
          {(["resources", ...(activeArea === "release" ? ["release"] as const : []), "concepts", "guide", "roles", "runs", "settings"] as const).map(item => <Button variant="ghost" size="sm" key={item} role="tab" aria-selected={tab === item} onClick={() => setTab(item)}>{t(item === "resources" ? "Overview" : item === "release" ? "Verification and delivery" : item === "concepts" ? "Business concepts" : item === "guide" ? "Guided start" : item === "runs" ? "Runs" : item === "roles" ? "Roles" : "Settings")}</Button>)}
        </div>
        {tab === "resources" && <div className="flex flex-wrap items-center gap-2 px-4 pt-3 text-xs">
          <strong className="mr-auto">{t(studioAreas.find(item => item.id === activeArea)!.description)}</strong>
          {!current && <Input type="search" aria-label={t("Filter resources")} placeholder={t("Filter resources")} className="w-full sm:w-56" value={filter} onChange={event => setFilter(event.target.value)} />}
          <Button size="sm" onClick={() => open(studioRoute(id, "release", selected))}>{t("Verify application")}</Button>
          {picked.size > 0 && <Button size="sm" variant="danger" disabled={locked} onClick={beginRetire}>{t("Retire {count} selected", { count: picked.size })}</Button>}
          {activeArea === "model" && <Button size="sm" onClick={() => open({ view: "object-type", params: { application: id } })}>{t("Open business model")}</Button>}
          {activeArea === "interface" && <><Button size="sm" onClick={() => open({ view: "module", params: { id, application: id } })}>{t("Navigation and header")}</Button><Button size="sm" disabled={locked} onClick={() => setTab("templates")}>{t("New page from template…")}</Button></>}
        </div>}
        {tab === "resources" && <div className="grid gap-5 p-4">
          <div className="grid content-start gap-4">
            {current ? <div className="rounded-md border border-border p-4">
              <div className="flex items-center gap-2">{kindIcon[current.kind.kind]}<h2 className="min-w-0 flex-1 truncate font-semibold">{current.title}</h2><DraftStatus state={current.record?.state} /></div>
              <p className="mt-1 text-xs text-muted">{t(current.kind.label)} · {current.ref.name}</p>
              {!current.record && current.installed && <ContextHelp title={t("Published resource")}>{t("This application references a published asset. Adding or removing the reference does not change its owner, definition or permissions.")}</ContextHelp>}
              {current.missing && <p className="mt-2 text-xs text-danger">{t("{title} is listed, but no readable draft or published definition is available. Check its owner and access, or remove the reference.", { title: current.title })}</p>}
              {current.ref.kind === "object" && current.record && <div className="mt-2 flex flex-wrap gap-1">
                {(["actions", "lifecycle", "constraints", "sets", "data"] as const).map(section => <Button size="sm" variant="ghost" key={section} onClick={() => open({ view: "object-type", params: { id: current.record!.id, application: id, tab: section } })}>{t(section === "actions" ? "Actions and rules" : section === "lifecycle" ? "States" : section === "constraints" ? "Business constraints" : section === "sets" ? "Record sets" : "Record samples")}</Button>)}
              </div>}
              {dependencies && <div className="mt-2 grid gap-2 text-xs">
                {[{ title: "Uses", refs: dependencies.requires }, { title: "Used by", refs: dependencies.usedBy }].map(group => <div key={group.title}><strong>{t(group.title)}</strong><div className="flex flex-wrap gap-1">{group.refs.map(ref => <Button key={refKey(ref)} size="sm" variant="ghost" onClick={() => open(assetRoute(ref, owned, definitions, id))}>{ref.name}</Button>)}</div></div>)}
              </div>}
              {dependencyQuery.isError && <p role="alert" className="text-xs text-danger">{t("Asset dependencies could not be loaded.")}</p>}
              {!current.installed && <ContextHelp title={t("Dependencies")}>{t("Draft dependencies are checked by the release owner. Published links use declared dependencies.")}</ContextHelp>}
              <div className="mt-3 flex flex-wrap gap-2">
                {(current.record || current.installed) && <Button size="sm" variant="primary" onClick={() => open(assetRoute(current.ref, owned, definitions, project.id))}>{t(!current.record && current.ref.kind === "object" ? "Open in object model" : "Open")}</Button>}
                {builder && current.missing && current.kind !== publishedResourceKind && <NewResourceMenu application={id} disabled={locked} onCreated={(kind, target) => void addCreated(kind, target)}>{t("Create it")}</NewResourceMenu>}
                {builder && <Button size="sm" disabled={locked} onClick={() => void remove(current.ref)}>{t("Remove from project")}</Button>}
              </div>
            </div> : <div role="region" aria-label={t("Workspace resources")} className="grid gap-2 divide-y divide-border">
              {studioResourceGroups(project, activeArea, visible).map(group => <Panel variant="plain" key={group.id} className="py-3">
                <h2 className="mb-2 text-sm font-semibold">{t(group.title)}</h2>
                {group.items.map(item => {
                  const key = refKey(item.ref);
                  return <div key={key} className="flex items-center gap-1">
                    {retireable(item) && <Checkbox disabled={locked} ariaLabel={t("Select {title}", { title: item.title })} checked={picked.has(key)}
                      onChange={checked => setPicked(next => { const picked2 = new Set(next); if (checked) picked2.add(key); else picked2.delete(key); return picked2; })} />}
                    <Button variant="row" className="min-w-0 flex-1 gap-3 py-3" onClick={() => item.missing ? setSelected(refKey(item.ref)) : open(assetRoute(item.ref, owned, definitions, id))}>
                      <span className="text-muted [&>svg]:size-4">{kindIcon[item.kind.kind]}</span><span className="min-w-0 flex-1 truncate">{item.title}</span><span className="text-xs text-muted">{statusOf(item)}</span>
                    </Button>
                  </div>;
                })}
              </Panel>)}
              {!visible.length && <p className="py-6 text-sm text-muted">{t("Nothing here yet. Use New, or Add existing.")}</p>}
            </div>}
            <BranchChangesPanel application={id} />
          </div>

        </div>}
        {tab === "concepts" && <div className="grid gap-3 p-4" role="region" aria-label={t("Business concepts")}>
          <div className="flex items-center justify-between gap-2"><h2 className="text-sm font-semibold">{t("Business concepts")}</h2><ContextHelp title={t("Business concepts")}>{t("Name groups in your business vocabulary. Moving an asset changes navigation only.")}</ContextHelp></div>
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
        {visited.includes("templates") && <div hidden={tab !== "templates"} className="p-4"><ViewVisibilityContext.Provider value={visibleView && tab === "templates"}><StudioTemplates initial={initialTemplate} onCreated={target => void addCreated(resourceKinds.find(kind => kind.kind === "page")!, target)} /></ViewVisibilityContext.Provider></div>}
        {visited.includes("release") && <div hidden={tab !== "release"}><ViewVisibilityContext.Provider value={visibleView && tab === "release"}><div className="grid gap-4 p-4" role="region" aria-label={t("Verification and delivery")}>
          <CandidateTest applicationId={id} planId={initialPlan} embedded />
 {installed && <ApplicationRuntimePanel owner="build" name={project.name} />}
          <ReleaseReview initialKind="app" initialID={id} embedded />
        </div></ViewVisibilityContext.Provider></div>}
        {visited.includes("runs") && <div hidden={tab !== "runs"}><ViewVisibilityContext.Provider value={visibleView && tab === "runs"}><ApplicationRuns owner="build" name={project.name} project={id} /></ViewVisibilityContext.Provider></div>}
        {retireIntent && <Dialog open onOpenChange={open => { if (!open && !retireLock.current && (!retireIntent.attempted || retireIntent.refusal)) rememberRetirement(undefined); }} title={t("Retire {count} selected", { count: retireIntent.items.length })}>
          <div className="grid gap-3 p-1 text-sm">
            <p className="text-muted">{t("These assets retire together in one transaction: every step is checked first, and either all retire or none does.")}</p>
            <ul className="grid max-h-56 gap-1 overflow-auto rounded border border-border p-2 text-xs">
              {retireIntent.items.map(item => <li key={refKey(item.ref)} className="flex items-center gap-2"><span className="text-muted [&>svg]:size-3.5">{kindIcon[item.kind.kind]}</span><span className="min-w-0 flex-1 truncate">{item.title}</span><span className="text-muted">{t(item.kind.label)}</span></li>)}
            </ul>
            {retireIntent.refusal ? <p role="alert" className="text-xs text-danger">{humanizeKernelError(retireIntent.refusal)}</p> : !retireBusy && retireIntent.attempted && <p role="alert" className="text-xs text-warning">{t("The previous retirement request was not confirmed. Retry its original key and list before leaving or choosing a different batch.")}</p>}
            <div className="flex justify-end gap-2">
              <Button size="sm" onClick={() => rememberRetirement(undefined)} disabled={retireBusy || !!retireIntent.attempted && !retireIntent.refusal}>{t("Discard and reselect")}</Button>
              <Button size="sm" variant="danger" disabled={retireBusy || !!retireIntent.refusal} onClick={() => void retire()}>{retireBusy ? t("Retiring…") : t("Retire them")}</Button>
            </div>
          </div>
        </Dialog>}
        {tab === "roles" && <ProjectRoles owned={owned} />}
        {tab === "settings" && settings && <div className="grid gap-3"><StructuredEditor expectedRevision={baseline.current?.revision ?? project.revision} fieldInput={sourceFieldInput} value={declarationValue(settings, declarationFields)} fields={declarationFields} scope={`build.app:${id}:${declarationHost.source.scope}`} readOnly={!builder} disabled={saving || !!pendingPatch || !!createdResource} onChange={next => { editSettings({ ...next, title: next.title as string, name: next.name as string, description: next.description as string ?? "", icon: next.icon as string ?? "boxes", profile: next.profile as Api.ApplicationProfile ?? {}, entry: next.entry as string ?? "", replacements: next.replacements as NonNullable<Api.Application["replacements"]> ?? [] }); setSourceApplied(true); }} onTrial={next => trial("build.app.edit", { type: "build.app", id }, declarationChanges(next, baseline.current?.values ?? {}), baseline.current?.revision ?? project.revision)}><fieldset disabled={saving || !!pendingPatch || !!createdResource} className="grid max-w-xl gap-3 p-4">
          <ExtensionSettings value={settings} pages={project.pages ?? []} disabled={!builder} onChange={extension => editSettings({ ...settings, ...extension, entry: extension.entry ?? "", replacements: extension.replacements ?? [] })} />
          <label className="grid gap-1 text-xs">{t("Title")}<Input value={settings.title} disabled={!builder} onChange={(event) => editSettings({ ...settings, title: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Name")}<Input value={settings.name} disabled={!builder} onChange={(event) => editSettings({ ...settings, name: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Icon")}<IconPicker value={settings.icon} disabled={!builder} onChange={(icon) => editSettings({ ...settings, icon })} /></label>
          <label className="grid gap-1 text-xs">{t("Description")}<Textarea rows={3} value={settings.description} disabled={!builder} onChange={(event) => editSettings({ ...settings, description: event.target.value })} /></label>
          <PanelSection title={t("Capacity and recovery declarations")} actions={<ContextHelp title={t("Capacity and recovery declarations")}><p>{t("Declare the expected scale and retention for this application. Recovery targets require evidence from its environment.")}</p><p>{t("Use durations such as 24h, 168h or 30m. These declarations do not delete data or grant access.")}</p></ContextHelp>}>
            {owned.filter(resource => resource.ref.kind === "object").map(resource => <label className="grid gap-1 text-xs" key={refKey(resource.ref)}>
              {t("Expected records: {type}", { type: resource.title })}
              <Input aria-label={t("Expected records: {type}", { type: resource.title })} draftKey={`expected-records:${resource.ref.name}`} type="number" optional step={1} max={Number.MAX_SAFE_INTEGER} min={1} value={settings.profile.expectedRecords?.[resource.ref.name] ?? ""} disabled={!builder} onChange={event => {
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
          </PanelSection>
        </fieldset></StructuredEditor>
        <fieldset disabled={saving || !!pendingPatch || !!createdResource} className="grid max-w-xl gap-3 p-4">
          {error && <p role="alert" className="text-xs text-danger">{error}</p>}
          <p className="text-xs text-muted">{t("Visual changes save automatically. After applying source, save the settings.")}</p>
          <div className="flex gap-2">
            <Button size="sm" disabled={!dirty || saving} onClick={() => void saveSettings()}>{t("Save")}</Button>
            <Button size="sm" variant="ghost" disabled={saving} onClick={() => confirmDiscard(() => { baseline.current = undefined; setEdited(false); if (project) setSettings(fromRecord(project)); void query.refetch(); })}>{t("Reload current project settings")}</Button>
          </div>
        </fieldset></div>}
      </div>
    </Workbench>
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
