// Projects (ADR-0053 §5.1, ADR-0055 §2): the container of everything a builder
// makes. A project is the host's Application (`build.app`); its tree lists what
// it owns by the host's asset kinds, `+ New` creates a resource inside it, `Add
// existing` brings one in, and its pages, navigation and header are its interface.
import { pageUIProfile, useHost, useNewRecord, useReadQuery, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { ActionMenu, Button, Input, PanelSection, Select, ProblemList, StructureRow, Textarea, Workbench, notify, t, useWorkspace, type ContextCommand, type WorkbenchProblem } from "@platform/ui";
import { Boxes, Braces, FolderKanban, Layers, LayoutTemplate, Link2, Plus, Search, Sparkles, Tags, Workflow } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { WorkshopApplicationImport } from "../workshop/module-import/WorkshopApplicationImport";
import type { ApplicationImportDependency } from "../workshop/module-import/application-import";
import { DraftStatus, savingState, useAutoSave } from "../shared/workbench";
import { ProjectRuns } from "./project-runs";
import { ProjectRoles } from "./project-roles";
import { kindOfType, refKey, resourceGroups, resourceKinds, resourceRef, resourceRoute, type ResourceKindInfo, type ResourceRecord } from "./resources";

export type Project = Api.Application & { id: string; revision: number; state: string; archived?: boolean; changed?: Api.Stamp };

export const kindIcon: Record<string, ReactNode> = {
  object: <Boxes />, "link-type": <Link2 />, "property-type": <Tags />, query: <Search />, page: <LayoutTemplate />, flow: <Workflow />, function: <Sparkles />, compute: <Braces />,
};

/** Every project the builder may open, with what each holds. */
export function ProjectsList() {
  const { role, me } = useHost();
  const { open } = useWorkspace();
  const projects = useRecordInventory<Project>("build.app");
  // A builder role held only through projects (#141): the Studio opens, edits stay within the assets they name.
  const buildGrants = (me.profile.grants ?? []).filter((g) => g.app === "build");
  const delegated = buildGrants.length > 0 && buildGrants.every((g) => g.by?.startsWith("project:")) ? buildGrants.map((g) => g.reason || g.by!.slice(8)) : [];
  const [search, setSearch] = useState("");
  const create = useNewRecord("build.app", (target) => open({ view: "project", params: { id: target.id } }));
  const builder = role("build") === "builder";
  const rows = (projects.data?.records ?? []).filter((project) => !project.archived && `${project.title} ${project.name}`.toLowerCase().includes(search.toLowerCase()));
  return <div className="grid min-w-0 gap-4">
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div><h1 className="text-xl font-semibold tracking-tight">{t("Projects")}</h1><p className="text-sm text-muted">{t("Each project owns its object types, pages, automations and functions, and is published as one release.")}</p></div>
      <div className="flex items-center gap-2">
        <Input type="search" aria-label={t("Search projects")} placeholder={t("Search projects")} value={search} onChange={(event) => setSearch(event.target.value)} className="w-56" />
        {builder && create.available && <Button variant="primary" onClick={() => create.take()}><Plus />{t("New project")}</Button>}
      </div>
    </div>
    {delegated.length > 0 && <p className="rounded-md border border-border bg-surface px-3 py-2 text-sm text-muted">{t("You build within {projects}: only the assets they name accept your edits; publishing stays with the builders.", { projects: delegated.join(", ") })}</p>}
    {projects.isLoading && <p className="text-sm text-muted">{t("Loading…")}</p>}
    {!projects.isLoading && !rows.length && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">{search ? t("No project matches.") : t("No projects yet. Create one to start building.")}</div>}
    <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {rows.map((project) => <li key={project.id}>
        <Button variant="row" type="button" onClick={() => open({ view: "project", params: { id: project.id } })} className="grid w-full gap-2 border border-border bg-surface rounded-md p-4 text-left hover:border-primary focus-visible:outline-ring">
          <div className="flex items-center gap-2"><FolderKanban className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate font-semibold">{project.title || project.name}</span><DraftStatus state={project.state} /></div>
          {project.description && <p className="line-clamp-2 text-xs text-muted">{project.description}</p>}
          <p className="text-[11px] text-muted">{t("{pages} pages · {resources} resources", { pages: project.pages?.length ?? 0, resources: project.resources?.length ?? 0 })}{project.changed?.at && ` · ${new Date(project.changed.at).toLocaleDateString()}`}</p>
        </Button>
      </li>)}
    </ul>
    {create.dialog}
  </div>;
}

export type OwnedResource = { kind: ResourceKindInfo; record?: ResourceRecord; ref: Api.AssetRef; title: string; installed: boolean; missing: boolean };

/** Inventories of every resource type, keyed by kind, resolved to the records a project names — and to
 * what the host has installed, so a published resource without a tenant draft is never called missing. */
function useProjectResources(project?: Project) {
  const { definitions } = useHost();
  const inventories = Object.fromEntries(resourceKinds.map((kind) => [kind.kind, useRecordInventory<ResourceRecord>(kind.type)])) as Record<string, ReturnType<typeof useRecordInventory<ResourceRecord>>>;
  const owned = useMemo(() => {
    const items: OwnedResource[] = [];
    if (!project) return items;
    const installedKeys = new Set(definitions.map((d) => refKey(d.ref)));
    const resolve = (kind: ResourceKindInfo, ref: Api.AssetRef, record: ResourceRecord | undefined, loaded: boolean): OwnedResource => {
      const installed = installedKeys.has(refKey(ref));
      const definition = installed ? definitions.find((d) => refKey(d.ref) === refKey(ref)) : undefined;
      const title = record?.title ?? definition?.page?.title ?? definition?.entity?.title ?? definition?.query?.title ?? definition?.function?.title ?? ref.name;
      return { kind, record, ref, title, installed, missing: !record && !installed && loaded };
    };
    const pages = inventories.page?.data?.records ?? [];
    for (const name of project.pages ?? []) {
      items.push(resolve(kindOfType("build.page")!, { app: "build", kind: "page", name }, pages.find((page) => page.name === name && !page.archived), !!inventories.page?.data));
    }
    for (const ref of project.resources ?? []) {
      const kind = resourceKinds.find((item) => (item.ref ?? item.kind) === ref.kind);
      if (!kind) continue;
      const record = (inventories[kind.kind]?.data?.records ?? []).find((item) => !item.archived && refKey(resourceRef(kind, item.name)) === refKey(ref));
      items.push(resolve(kind, ref, record, !!inventories[kind.kind]?.data));
    }
    return items;
  }, [project, definitions, ...resourceKinds.map((kind) => inventories[kind.kind]?.data)]);
  /** Records of every kind that exist but are not in the project yet: what "Add existing" offers. */
  const available = useMemo(() => {
    const held = new Set(owned.map((item) => refKey(item.ref)));
    return resourceKinds.flatMap((kind) => (inventories[kind.kind]?.data?.records ?? []).filter((record) => !record.archived && !held.has(refKey(kind.kind === "page" ? { app: "build", kind: "page", name: record.name } : resourceRef(kind, record.name)))).map((record) => ({ kind, record })));
  }, [owned, ...resourceKinds.map((kind) => inventories[kind.kind]?.data)]);
  /** Object types other apps installed (the shared master data of `core`, HR's employees…): a project
   * references them instead of defining its own (ADR-0058 A1). */
  const shared = useMemo(() => {
    const held = new Set(owned.map((item) => refKey(item.ref)));
    return definitions.filter((d) => d.ref.kind === "object" && d.ref.app !== "build" && d.entity && !held.has(refKey(d.ref)))
      .map((d) => ({ ref: d.ref, title: d.entity!.title || d.ref.name, app: d.ref.app }))
      .sort((a, b) => (a.app === "core" ? 0 : 1) - (b.app === "core" ? 0 : 1) || a.title.localeCompare(b.title));
  }, [owned, definitions]);
  const loading = Object.values(inventories).some((inventory) => inventory.isLoading);
  return { inventories, owned, available, shared, loading };
}

/** `+ New ▾`: create a resource and add it to the project in one step. */
function NewResourceMenu({ onCreated, children, disabled }: { onCreated?: (kind: ResourceKindInfo, target: { type: string; id: string }) => void; children?: ReactNode; disabled?: boolean }) {
  const { role } = useHost();
  const creators = Object.fromEntries(resourceKinds.map((kind) => [kind.kind, useNewRecord(kind.type, (target) => onCreated?.(kind, target))])) as Record<string, ReturnType<typeof useNewRecord>>;
  if (role("build") !== "builder") return null;
  const commands: ContextCommand[] = resourceGroups.flatMap((group, index) => resourceKinds.filter((kind) => kind.group === group.id && creators[kind.kind]?.available)
    .map((kind, at) => ({ id: kind.kind, label: t(kind.label), icon: kindIcon[kind.kind], separatorBefore: index > 0 && at === 0, disabled, run: () => creators[kind.kind]!.take() })));
  return <>
    <ActionMenu label={t("New resource")} variant="primary" icon={<Plus />} commands={commands}>{children ?? t("New")}</ActionMenu>
    {Object.values(creators).map((creator, index) => <span key={index}>{creator.dialog}</span>)}
  </>;
}

/** The project home: resource tree, what changed, settings. */
export function ProjectHome({ id }: { id: string }) {
  const { client, decide, role, definitions, me } = useHost();
  const { open } = useWorkspace();
  const query = useReadQuery<{ record?: Project }>(`/v1/records/build.app/${encodeURIComponent(id)}`);
  const project = query.data?.record;
  const { owned, available, shared, loading } = useProjectResources(project);
  const [filter, setFilter] = useState("");
  const [selected, setSelected] = useState<string>();
  const [tab, setTab] = useState<"resources" | "runs" | "roles" | "settings">("resources");
  const [settings, setSettings] = useState<{ title: string; name: string; description: string; icon: string }>();
  const [saving, setSaving] = useState(false), [error, setError] = useState<string>();
  const [importing, setImporting] = useState(false);
  const [importedDependencies, setImportedDependencies] = useState<ApplicationImportDependency[]>([]);
  useEffect(() => { if (project && !settings) setSettings({ title: project.title, name: project.name, description: project.description ?? "", icon: project.icon ?? "boxes" }); }, [project, settings]);
  const dirty = !!project && !!settings && (settings.title !== project.title || settings.name !== project.name || settings.description !== (project.description ?? "") || settings.icon !== (project.icon ?? "boxes"));
  const patch = async (changes: Partial<Api.Application>) => {
    if (!project) return false;
    setSaving(true); setError(undefined);
    try {
      const ok = await decide("build.app.edit", { type: "build.app", id: project.id }, changes, { expectedRevision: project.revision, quiet: true, onRefused: setError });
      if (ok) await query.refetch();
      return ok;
    } catch { setError(t("The project could not be saved.")); return false; } finally { setSaving(false); }
  };
  useAutoSave({ dirty, busy: saving, save: () => patch({ title: settings!.title, name: settings!.name, description: settings!.description, icon: settings!.icon }) });
  const addCreated = async (kind: ResourceKindInfo, target: { type: string; id: string }) => {
    if (!project) return;
    const record = await fetchRecord(target);
    if (!record) { notify.error(t("The resource was created but could not be added to this project. Select it from the project's resources.")); return; }
    const changes = kind.kind === "page" ? { pages: [...new Set([...(project.pages ?? []), record.name])] }
      : { resources: [...(project.resources ?? []).filter((ref) => refKey(ref) !== refKey(resourceRef(kind, record.name))), resourceRef(kind, record.name)] };
    if (await patch(changes)) open(resourceRoute(kind, target.id, project.id));
  };
  async function fetchRecord(target: { type: string; id: string }) {
    try { const { record } = await client.get<{ record: ResourceRecord }>(`/v1/records/${target.type}/${encodeURIComponent(target.id)}`); return record; } catch { return undefined; }
  }
  const remove = (ref: Api.AssetRef) => project && void patch(ref.kind === "page" ? { pages: (project.pages ?? []).filter((name) => name !== ref.name), groups: (project.groups ?? []).map((group) => ({ ...group, pages: group.pages.filter((name) => name !== ref.name) })).filter((group) => group.pages.length) }
    : { resources: (project.resources ?? []).filter((item) => refKey(item) !== refKey(ref)) });
  const builder = role("build") === "builder";
  const unpublished = owned.filter((item) => item.record && item.record.state !== "published");
  const problems: WorkbenchProblem[] = owned.filter((item) => item.missing).map((item) => ({ id: refKey(item.ref), severity: "warning", text: t("{title} is listed but neither a draft nor a published definition exists. Remove it or create it.", { title: item.title }), subject: t(item.kind.label), locate: () => { setSelected(refKey(item.ref)); setTab("resources"); } }));
  const addExisting = (kind: ResourceKindInfo, record: ResourceRecord) => patch(kind.kind === "page" ? { pages: [...new Set([...(project!.pages ?? []), record.name])] }
    : { resources: [...(project!.resources ?? []), resourceRef(kind, record.name)] });
  const installed = definitions.find((d) => d.ref.app === "build" && d.ref.kind === "app" && d.ref.name === project?.name);
  const visible = owned.filter((item) => `${item.title} ${item.ref.name} ${t(item.kind.plural)}`.toLowerCase().includes(filter.toLowerCase()));
  const current = owned.find((item) => refKey(item.ref) === selected);
  if (!project) return <WorkbenchFrame title={t("Project")}>{query.isError ? t("The project could not be loaded.") : t("Loading…")}</WorkbenchFrame>;
  const statusOf = (item: OwnedResource) => item.record ? (item.record.state === "published" ? undefined : t("draft")) : item.installed ? t("published") : item.missing ? t("missing") : undefined;
  const tree = <div className="grid min-w-0 content-start">
    <div className="p-2"><Input type="search" aria-label={t("Filter resources")} placeholder={t("Filter resources")} value={filter} onChange={(event) => setFilter(event.target.value)} /></div>
    {resourceGroups.map((group) => {
      const kinds = resourceKinds.filter((kind) => kind.group === group.id);
      const items = visible.filter((item) => kinds.includes(item.kind));
      if (!items.length && filter && group.id !== "interface") return null;
      return <PanelSection key={group.id} title={t(group.label)}>
        {group.id === "interface" && <StructureRow icon={<Layers />} label={t("Navigation and header")} meta={project.groups?.length || undefined} onClick={() => open({ view: "module", params: { id: project.id, application: project.id } })} />}
        {kinds.map((kind) => {
          const rows = items.filter((item) => item.kind === kind);
          if (!rows.length) return null;
          return <div key={kind.kind} className="grid">
            <StructureRow icon={kindIcon[kind.kind]} label={t(kind.plural)} meta={rows.length} className="font-medium" />
            {rows.map((item) => <StructureRow key={refKey(item.ref)} depth={1} label={item.title} selected={selected === refKey(item.ref)} meta={statusOf(item)}
              onClick={() => { setSelected(refKey(item.ref)); setTab("resources"); if (item.record) open(resourceRoute(item.kind, item.record.id, project.id)); else if (item.installed && item.kind.kind === "object") open({ view: "object-type", params: { object: item.ref.name, application: project.id } }); }} />)}
          </div>;
        })}
        {!items.length && group.id !== "interface" && <p className="px-2 text-[11px] text-muted">{t("Nothing here yet. Use New, or Add existing.")}</p>}
      </PanelSection>;
    })}
  </div>;
  const header = installed?.application?.pages.find((name) => definitions.some((d) => d.ref.app === "build" && d.ref.kind === "page" && d.ref.name === name));
  return <>
    <Workbench storageKey="project" crumbs={[{ label: t("Projects"), onClick: () => open({ view: "projects" }) }]} title={project.title || project.name}
      status={<DraftStatus state={project.state} problems={problems.length} />} saving={savingState(dirty, saving, error)}
      actions={<>
        {header && <Button size="sm" onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: header, application: `build:${project.name}` } })}>{t("Open published app")}</Button>}
        {builder && installed?.application && <Button size="sm" onClick={() => setImporting(true)}>{t("Import Workshop module")}</Button>}
        {builder && <Button size="sm" onClick={() => open({ view: "changes", params: { application: project.id } })}>{t("Changes")}{unpublished.length > 0 && <span className="ml-1 rounded-full bg-primary/15 px-1.5 text-[10px] text-primary">{unpublished.length}</span>}</Button>}
        {builder && (available.length > 0 || shared.length > 0) && <Select aria-label={t("Add existing resource")} value="" className="max-w-48" onChange={(event) => {
          const [kind, id] = event.target.value.split(":");
          if (kind === "shared") { const found = shared.find((item) => refKey(item.ref) === id); if (found) void patch({ resources: [...(project!.resources ?? []), found.ref] }); return; }
          const found = available.find((item) => item.kind.kind === kind && item.record.id === id); if (found) void addExisting(found.kind, found.record); }}>
          <option value="">{t("Add existing…")}</option>
          {shared.length > 0 && <optgroup label={t("Shared object types (platform and other apps)")}>{shared.map((item) => <option key={refKey(item.ref)} value={`shared:${refKey(item.ref)}`}>{item.title} · {item.app}</option>)}</optgroup>}
          {resourceKinds.map((kind) => { const rows = available.filter((item) => item.kind === kind); return rows.length ? <optgroup key={kind.kind} label={t(kind.plural)}>{rows.map((item) => <option key={item.record.id} value={`${kind.kind}:${item.record.id}`}>{item.record.title || item.record.name}</option>)}</optgroup> : null; })}
        </Select>}
        {builder && <NewResourceMenu onCreated={(kind, target) => void addCreated(kind, target)} />}
        {builder && <Button size="sm" variant="primary" disabled={!project.pages?.length} onClick={() => open({ view: "release-review", params: { kind: "app", id: project.id, application: project.id, ...(importedDependencies.length ? { drafts: importedDependencies.map((dependency) => `${dependency.kind}:${dependency.id}`).join(",") } : {}) } })}>{t("Publish")}</Button>}
      </>}
      left={{ label: t("Project resources"), content: tree }}
      dock={{ label: t("Project dock"), tabs: [{ id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} /> }] }}>
      <div className="flex min-h-0 flex-1 flex-col overflow-auto">
        <div role="tablist" aria-label={t("Project sections")} className="flex gap-1 border-b border-border px-3 pt-2">
          {(["resources", "runs", "roles", "settings"] as const).map((item) => <Button variant="row" key={item} type="button" role="tab" aria-selected={tab === item} onClick={() => setTab(item)}
            className={"rounded-t border-b-2 px-3 py-1.5 text-sm " + (tab === item ? "border-primary font-semibold" : "border-transparent text-muted hover:text-foreground")}>{t(item === "resources" ? "Overview" : item === "runs" ? "Runs" : item === "roles" ? "Roles" : "Settings")}</Button>)}
        </div>
        {tab === "resources" && <div className="grid gap-4 p-4 lg:grid-cols-[minmax(0,1fr)_20rem]">
          <div className="grid content-start gap-4">
            {current ? <div className="rounded-md border border-border p-4">
              <div className="flex items-center gap-2">{kindIcon[current.kind.kind]}<h2 className="min-w-0 flex-1 truncate font-semibold">{current.title}</h2><DraftStatus state={current.record?.state} /></div>
              <p className="mt-1 text-xs text-muted">{t(current.kind.label)} · {current.ref.name}</p>
              {!current.record && current.installed && <p className="mt-2 text-xs text-muted">{t("Published and in use, but this tenant holds no draft of it: it was defined in code or by another tenant. It is part of the release as it is.")}</p>}
              {current.missing && <p className="mt-2 text-xs text-danger">{t("Listed in the project, but nothing by this name exists. Remove it, or create it with New (same name) and it is picked up.")}</p>}
              <div className="mt-3 flex flex-wrap gap-2">
                {current.record && <Button size="sm" variant="primary" onClick={() => open(resourceRoute(current.kind, current.record!.id, project.id))}>{t("Open")}</Button>}
                {!current.record && current.installed && current.kind.kind === "object" && <Button size="sm" variant="primary" onClick={() => open({ view: "object-type", params: { object: current.ref.name, application: project.id } })}>{t("Open in object model")}</Button>}
                {builder && current.missing && <NewResourceMenu onCreated={(kind, target) => void addCreated(kind, target)}>{t("Create it")}</NewResourceMenu>}
                {builder && <Button size="sm" onClick={() => { remove(current.ref); setSelected(undefined); }}>{t("Remove from project")}</Button>}
              </div>
            </div> : <div className="rounded-md border border-dashed border-border p-6 text-sm text-muted">
              {project.description || t("Choose a resource on the left, create one with New, or bring one in with Add existing.")}
            </div>}
            <div className="grid gap-2">
              <h2 className="text-sm font-semibold">{t("Unpublished changes")} <span className="text-muted">({unpublished.length})</span></h2>
              {!unpublished.length && <p className="text-xs text-muted">{loading ? t("Loading…") : t("Everything in this project is published.")}</p>}
              <ul className="divide-y divide-border rounded-md border border-border">
                {unpublished.slice(0, 12).map((item) => <li key={refKey(item.ref)}>
                  <Button variant="row" type="button" className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-row-hover" onClick={() => item.record && open(resourceRoute(item.kind, item.record.id, project.id))}>
                    <span className="text-muted [&>svg]:size-4">{kindIcon[item.kind.kind]}</span><span className="min-w-0 flex-1 truncate">{item.title}</span><span className="text-xs text-muted">{t(item.kind.label)}</span><DraftStatus state={item.record?.state} />
                  </Button>
                </li>)}
              </ul>
            </div>
          </div>
          <aside className="grid content-start gap-3 text-sm">
            <div className="rounded-md border border-border p-3">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Contents")}</h3>
              <dl className="mt-2 grid grid-cols-2 gap-1 text-xs">
                {resourceKinds.map((kind) => { const n = owned.filter((item) => item.kind === kind).length; return n ? <div key={kind.kind} className="contents"><dt className="text-muted">{t(kind.plural)}</dt><dd className="text-right">{n}</dd></div> : null; })}
              </dl>
            </div>
            <div className="rounded-md border border-border p-3 text-xs text-muted">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Release")}</h3>
              <p className="mt-2">{installed ? t("A published version is in the Applications portal. Saved edits remain drafts until the next release.") : t("Not published yet. Publish reviews every draft in this project and activates them together.")}</p>
            </div>
          </aside>
        </div>}
        {tab === "runs" && <ProjectRuns owned={owned} />}
        {tab === "roles" && <ProjectRoles owned={owned} />}
        {tab === "settings" && settings && <div className="grid max-w-xl gap-3 p-4">
          <label className="grid gap-1 text-xs">{t("Title")}<Input value={settings.title} disabled={!builder} onChange={(event) => setSettings({ ...settings, title: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Name")}<Input value={settings.name} disabled={!builder} onChange={(event) => setSettings({ ...settings, name: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Icon")}<Select value={settings.icon} disabled={!builder} onChange={(event) => setSettings({ ...settings, icon: event.target.value })}>
            {["boxes", "clipboard", "people", "calendar", "wrench", "map", "chart", "sparkles"].map((icon) => <option key={icon} value={icon}>{t(icon)}</option>)}</Select></label>
          <label className="grid gap-1 text-xs">{t("Description")}<Textarea rows={3} value={settings.description} disabled={!builder} onChange={(event) => setSettings({ ...settings, description: event.target.value })} /></label>
          {error && <p role="alert" className="text-xs text-danger">{error}</p>}
          <p className="text-xs text-muted">{t("Changes save automatically.")}</p>
        </div>}
      </div>
    </Workbench>
    {installed?.application && <WorkshopApplicationImport key={JSON.stringify([project.id, me])} open={importing} onClose={() => setImporting(false)} application={{ ref: installed.ref, sourceVersion: installed.version }}
      onPrepared={({ pages, header, dependencies }) => { setImportedDependencies(dependencies); void patch({ pages: [...pages, ...(project.pages ?? []).filter((name) => !pages.includes(name))], header, uiProfile: header ? pageUIProfile : project.uiProfile }); }} />}
  </>;
}

function WorkbenchFrame({ title, children }: { title: string; children: ReactNode }) {
  return <Workbench storageKey="project" title={title}><div className="flex flex-1 items-center justify-center text-sm text-muted">{children}</div></Workbench>;
}
