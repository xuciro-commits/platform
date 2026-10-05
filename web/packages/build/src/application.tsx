import {ApplicationHeaderEditor} from "./ApplicationHeaderEditor";
import { VariablesPanel } from "./page-editor/VariablesPanel";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { AssetControls } from "./asset-controls";
import { pageUIProfile, pageVariableValues, useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { newId } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Card, Checkbox, Input, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, defineStatuses, t, useUnsavedChanges, useWorkspace } from "@platform/ui";
import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import {WorkshopApplicationImport} from './module-import/WorkshopApplicationImport';
import type {ApplicationImportDependency} from './module-import/application-import';

type Draft = Api.Application & { id: string; revision: number; state: string };
const empty: Draft = { id: "", revision: 0, state: "draft", name: "", title: "", description: "", icon: "boxes", pages: [], groups: [], resources: [] };
const hydrate = (record?: Draft): Draft => record ? { ...empty, ...record, pages: record.pages ?? [], groups: record.groups ?? [], resources: record.resources ?? [] } : empty;
const states = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });
const resourceKey = (ref: Api.AssetRef) => `${ref.app}/${ref.kind}/${ref.name}`;
const kindTitle = (kind: string) => t(({ object: "Object", flow: "Workflow", function: "AI function", compute: "Code function", query: "Query", action: "Action" } as Record<string, string>)[kind] ?? kind);

export function Applications() {
  const { source, role } = useHost();
  const { open } = useWorkspace();
  return <div className="grid gap-3">
    <PageHeader title={t("Applications")} description={t("Organize pages and published resources, then review the whole application release.")}
      actions={role("build") === "builder" && <Button variant="primary" onClick={() => open({ view: "application", params: { id: "new" } })}>{t("Create application")}</Button>} />
    <RecordList source={source} type="build.app" fields={["title", "name", "state"]} onOpen={(record) => open({ view: "application", params: { id: record.id } })} />
  </div>;
}

/** Application membership writes the original build.app; release and execution
 * stay with the existing owners. Pages alone determine operator navigation. */
type AssetRecord = { id: string; name: string; title: string; state: string; version?: number; archived?: boolean };

export function ApplicationEditor({ id }: { id: string }) {
  const { definitions, decide, role,me } = useHost();
  const { open, close } = useWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.app/${encodeURIComponent(id)}`, undefined, id !== "new");
  const processes = useRecordInventory<{ id: string; name: string; title: string; state: string; version?: number; published?: string; archived?: boolean }>("build.process");
  // The application browses its own assets; opening one keeps this application in context (ADR-0047 §6.2 M2).
  const objects = useRecordInventory<AssetRecord>("build.object");
  const pageRecords = useRecordInventory<AssetRecord>("build.page");
  const queries = useRecordInventory<AssetRecord>("build.query");
  const functions = useRecordInventory<AssetRecord>("build.function");
  const computes = useRecordInventory<AssetRecord>("build.code");
  const [draft, setDraft] = useState<Draft>(empty);
  const [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [importing,setImporting]=useState(false);
  // The saved object drafts the module import bound pages to: they are inputs
  // to this application's release, not something to publish first (ADR-0048 D6).
  const [importedDependencies,setImportedDependencies]=useState<ApplicationImportDependency[]>([]);
  const importApplication=definitions.find(d=>d.ref.app==='build'&&d.ref.kind==='app'&&d.ref.name===draft.name&&d.application);
  const reset = () => { setDraft(hydrate(query.data?.record)); setDirty(false); setError(""); };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, reset);
  useEffect(() => { if (query.data?.record && !dirty) setDraft(hydrate(query.data.record)); }, [query.data, dirty]);
  useEffect(() => { setImportedDependencies([]); }, [id]);
  const change = (patch: Partial<Draft>) => { setDraft((current) => ({ ...current, ...patch })); setDirty(true); setError(""); };
  const pages = definitions.filter((definition) => definition.ref.app === "build" && definition.page);
  const resources = definitions.filter((definition) => ["object", "query", "function", "compute"].includes(definition.ref.kind) && (definition.ref.app !== "build" || definition.source === "tenant"))
    .map((definition) => ({ ref: definition.ref, title: definition.entity?.title ?? definition.query?.title ?? definition.function?.title ?? definition.operation?.title ?? definition.ref.name, version: definition.version }));
  for (const process of processes.data?.records ?? []) if (process.published && !process.archived)
    resources.push({ ref: { app: "build", kind: "flow", name: `build.${process.name}` }, title: process.title, version: String(process.version ?? "") });
  const selected = draft.resources ?? [];
  const toggleResource = (ref: Api.AssetRef, checked: boolean) => change({ resources: checked ? [...selected, ref] : selected.filter((item) => resourceKey(item) !== resourceKey(ref)) });
  const togglePage = (name: string, checked: boolean) => change({ pages: checked ? [...draft.pages, name] : draft.pages.filter((item) => item !== name),
    groups: (draft.groups ?? []).map((group) => ({ ...group, pages: group.pages.filter((item) => item !== name || checked) })).filter((group) => group.pages.length) });
  const movePage = (at: number, offset: number) => { const next = [...draft.pages]; [next[at], next[at + offset]] = [next[at + offset]!, next[at]!]; change({ pages: next }); };
  // Open the original editor for a listed resource; the application travels with the route.
  const openResource = (ref: Api.AssetRef) => {
    const name = ref.kind === "flow" ? ref.name.replace(/^build\./, "") : ref.name;
    const editor = ref.kind === "object" ? { view: "process", records: objects.data?.records }
      : ref.kind === "query" ? { view: "query", records: queries.data?.records }
      : ref.kind === "function" ? { view: "function", records: functions.data?.records }
      : ref.kind === "compute" ? { view: "code", records: computes.data?.records }
      : ref.kind === "flow" ? { view: "workflow", records: processes.data?.records } : undefined;
    const record = editor?.records?.find((item) => item.name === name);
    // A resource owned outside this tenant opens its read-only reference instead.
    open(editor && record ? { view: editor.view, params: { id: record.id, application: draft.id } } : { view: "definition", params: ref });
  };
  // The editor is the same one the resource library opens; only the return context differs.
  const inventoryFor = (kind: string) => kind === "object" ? objects : kind === "query" ? queries
    : kind === "function" ? functions : kind === "compute" ? computes : kind === "flow" ? processes : undefined;
  const openPage = (name: string) => {
    const record = pageRecords.data?.records.find((item) => item.name === name);
    open(record ? { view: "compose", params: { id: record.id, application: draft.id } } : { view: "pages" });
  };
  const save = async () => {
    setBusy(true); setError("");
    try {
      const target = draft.id || newId("APP");
      const { name, title, description, icon, pages, groups, resources, variables, queries, uiProfile,header } = draft;
      const ok = await decide(`build.app.${draft.id ? "edit" : "create"}`, { type: "build.app", id: target }, { name, title, description, icon, pages, groups, resources, variables, queries, uiProfile,header },
        { expectedRevision: draft.id ? draft.revision : undefined, quiet: true, onRefused: setError });
      if (!ok) return;
      if (!draft.id) { markSaved(); setDirty(false); open({ view: "application", params: { id: target } }); close({ view: "application", params: { id } }); return; }
      const saved = await query.refetch();
      if (!saved.data?.record) { setError(t("Reload the saved application before editing again.")); return; }
      setDraft(hydrate(saved.data.record)); markSaved(); setDirty(false);
    } catch { setError(t("The application could not be saved.")); } finally { setBusy(false); }
  };
  if (role("build") !== "builder") return <PageHeader title={t("Applications")} description={t("Only a builder can edit application assets.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Applications")} description={query.isError ? t("The application could not be loaded.") : t("Loading…")} />;
  const visible = resources.filter((resource) => `${resource.title} ${resource.ref.app} ${resource.ref.name}`.toLowerCase().includes(search.toLowerCase()));
  const grouped = (draft.groups ?? []).flatMap((group) => group.pages);
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={draft.title || t("New application")} description={t("Organize pages and published resources, then review the whole application release.")}
      actions={<div className="flex flex-wrap gap-2"><Button onClick={() => open({ view: "applications" })}>{t("Applications")}</Button>
        <AssetControls type="build.app" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{ view: "application", params: { id } }} />
        <Button disabled={busy || !!draft.id && !dirty || !draft.name || !draft.title} onClick={() => void save()}>{t(draft.id ? "Save application" : "Create application")}</Button>
        <Button disabled={busy||dirty||!draft.id||!importApplication} onClick={()=>setImporting(true)}>{t('Import complete Workshop module')}</Button>
        <Button variant="primary" disabled={busy || dirty || !draft.id || !draft.pages.length || processes.isLoading || processes.isError}
          onClick={() => open({ view: "release-review", params: { kind: "app", id: draft.id, ...(importedDependencies.length ? { drafts: importedDependencies.map((dependency) => `${dependency.kind}:${dependency.id}`).join(",") } : {}) } })}>{t("Review application release")}</Button>
      </div>} />
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    {importApplication&&<WorkshopApplicationImport key={JSON.stringify([draft.id,me])} open={importing} onClose={()=>setImporting(false)} application={{ref:importApplication.ref,sourceVersion:importApplication.version}} onPrepared={({pages,header,dependencies})=>{setImportedDependencies(dependencies);change({pages:[...pages,...draft.pages.filter(name=>!pages.includes(name))],header,uiProfile:header?pageUIProfile:draft.uiProfile});}}/>}
    <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="grid min-w-0 content-start gap-4 p-4">
        <label className="grid gap-1 text-xs">{t("What people call it")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
        <div className="grid gap-3 sm:grid-cols-2"><label className="grid gap-1 text-xs">{t("Name")}<Input value={draft.name} onChange={(e) => change({ name: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Icon")}<Select value={draft.icon ?? "boxes"} onChange={(e) => change({ icon: e.target.value })}>
            {["boxes", "clipboard", "people", "calendar", "wrench", "map", "chart", "sparkles"].map((icon) => <option key={icon} value={icon}>{t(icon)}</option>)}
          </Select></label></div>
        <label className="grid gap-1 text-xs">{t("Description")}<Textarea value={draft.description ?? ""} rows={2} onChange={(e) => change({ description: e.target.value })} /></label>
        <ApplicationHeaderEditor value={draft.header} pages={draft.pages} onChange={header=>change({header,uiProfile:header?pageUIProfile:draft.uiProfile})}/>
        <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-semibold">{t("Pages and navigation")}</legend>
          {pages.map((page) => <Checkbox key={page.ref.name} checked={draft.pages.includes(page.ref.name)} onChange={(checked) => togglePage(page.ref.name, checked)}>{page.page?.title} · {page.ref.name}</Checkbox>)}
          {!pages.length && <p className="text-sm text-muted">{t("Publish a page before adding it to this application.")}</p>}
          {draft.pages.map((name, at) => <div key={name} className="flex items-center gap-2 rounded border border-border p-2 text-xs">
            <span className="min-w-0 flex-1 truncate">{pages.find((page) => page.ref.name === name)?.page?.title ?? name}</span>
            <Button size="sm" aria-label={t("Open {title}", { title: pages.find((page) => page.ref.name === name)?.page?.title ?? name })} onClick={() => openPage(name)}>{t("Open")}</Button>
            <Button size="sm" aria-label={t("Move page up")} disabled={at === 0} onClick={() => movePage(at, -1)}><ArrowUp size={12} /></Button>
            <Button size="sm" aria-label={t("Move page down")} disabled={at === draft.pages.length - 1} onClick={() => movePage(at, 1)}><ArrowDown size={12} /></Button>
            <Button size="sm" aria-label={t("Remove page")} onClick={() => togglePage(name, false)}><Trash2 size={12} /></Button>
          </div>)}
          {!!importedDependencies.length && <p className="text-xs text-muted">{t("Delivered with this application release: {drafts}", { drafts: importedDependencies.map((dependency) => dependency.title || dependency.name).join(", ") })}</p>}
        </fieldset>
        <fieldset className="grid gap-2"><legend className="mb-2 text-sm font-semibold">{t("Navigation groups")}</legend>
          {(draft.groups ?? []).map((group, at) => <div key={at} className="grid gap-2 rounded border border-border p-3">
            <div className="flex gap-2"><Input aria-label={t("Group title")} value={group.title} onChange={(e) => change({ groups: draft.groups!.map((item, i) => i === at ? { ...item, title: e.target.value } : item) })} />
              <Button size="sm" aria-label={t("Remove group")} onClick={() => change({ groups: draft.groups!.filter((_, i) => i !== at) })}><Trash2 size={12} /></Button></div>
            {draft.pages.map((name) => <Checkbox key={name} checked={group.pages.includes(name)} disabled={!group.pages.includes(name) && grouped.includes(name)} onChange={(checked) =>
              change({ groups: draft.groups!.map((item, i) => i === at ? { ...item, pages: checked ? [...item.pages, name] : item.pages.filter((page) => page !== name) } : item) })}>{pages.find((page) => page.ref.name === name)?.page?.title ?? name}</Checkbox>)}
          </div>)}
          <Button size="sm" onClick={() => change({ groups: [...(draft.groups ?? []), { title: t("New group"), pages: [] }] })}>{t("Add group")}</Button>
        </fieldset>
      </Card>
      <div className="grid content-start gap-3"><VariablesPanel application document={{formatVersion:2,uiProfile:draft.uiProfile ?? pageUIProfile,root:"root",nodes:{},variables:draft.variables,queries:draft.queries}} sections={[]} values={pageVariableValues(draft.variables ?? {})} onChange={(document) => change({uiProfile:pageUIProfile,variables:document.variables})} />
      <QueriesPanel application document={{formatVersion:2,uiProfile:draft.uiProfile??pageUIProfile,root:"root",nodes:{},variables:draft.variables,queries:draft.queries}} object={draft.queries?Object.values(draft.queries)[0]?.object??{app:"build",kind:"object",name:""}:{app:"build",kind:"object",name:""}} values={{}} onChange={(document)=>change({uiProfile:pageUIProfile,variables:document.variables,queries:document.queries})}/>
      <Panel title={t("Application resources")} className="grid min-w-0 content-start gap-3">
        <StatusTag status={draft.state} registry={states} />
        <p className="text-xs text-muted">{t("Resources keep their original ownership and permissions. They may be shared by multiple applications.")}</p>
        <p className="text-xs text-muted">{t("The candidate includes the published versions of these resources and their dependencies. Editing a resource draft does not silently publish it.")}</p>
        <Input aria-label={t("Search application resources")} placeholder={t("Search objects, workflows and functions")} value={search} onChange={(e) => setSearch(e.target.value)} />
        {processes.isError && <p role="alert" className="text-sm text-danger">{t("The workflow inventory could not be loaded.")}</p>}
        <fieldset className="grid max-h-[32rem] gap-3 overflow-auto" aria-label={t("Available resources")}>
          {visible.map((resource) => <div key={resourceKey(resource.ref)} className="flex items-start gap-2">
            <Checkbox className="min-w-0 flex-1" checked={selected.some((ref) => resourceKey(ref) === resourceKey(resource.ref))} onChange={(checked) => toggleResource(resource.ref, checked)}>
              <span className="grid gap-1 text-xs"><span>{resource.title}</span><span className="break-all text-muted">{kindTitle(resource.ref.kind)} · {resource.ref.app} · {resource.version}</span></span>
            </Checkbox>
            <Button size="sm" aria-label={t("Open {title}", { title: resource.title })} disabled={resource.ref.app === "build" && inventoryFor(resource.ref.kind)?.isLoading} onClick={() => openResource(resource.ref)}>{t("Open")}</Button>
          </div>)}
          {selected.filter((ref) => !resources.some((resource) => resourceKey(ref) === resourceKey(resource.ref))).map((ref) => <Checkbox key={resourceKey(ref)} checked onChange={() => toggleResource(ref, false)}>
            <span className="break-all text-xs text-danger">{t("Unavailable resource")}: {resourceKey(ref)}</span>
          </Checkbox>)}
        </fieldset>
        <p className="text-xs">{t("{n} explicit resources; dependent assets are resolved in release review.", { n: selected.length })}</p>
      </Panel></div>
    </div>
  </div>;
}
