import {ApplicationHeaderEditor} from "./ApplicationHeaderEditor";
import { VariablesPanel } from "./page-editor/VariablesPanel";
import { QueriesPanel } from "./page-editor/QueriesPanel";
import { AssetControls } from "./asset-controls";
import { NewActions, pageUIProfile, pageVariableValues, useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { applicationAssetEditors, applicationAssetKey as resourceKey, applicationAssetRef } from "./application-assets";
import { newId } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Card, Checkbox, Input, PageHeader, Panel, RecordList, Select, StatusTag, Textarea, defineStatuses, t, useUnsavedChanges, useWorkspace } from "@platform/ui";
import { ArrowDown, ArrowUp, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import {WorkshopApplicationImport} from './module-import/WorkshopApplicationImport';
import type {ApplicationImportDependency} from './module-import/application-import';

type Draft = Api.Application & { id: string; revision: number; state: string };
const empty: Draft = { id: "", revision: 0, state: "draft", name: "", title: "", description: "", icon: "boxes", pages: [], groups: [], resources: [] };
const hydrate = (record?: Draft): Draft => record ? { ...empty, ...record, pages: record.pages ?? [], groups: record.groups ?? [], resources: record.resources ?? [] } : empty;
const states = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });
const kindTitle = (kind: string) => t(({ object: "Object", flow: "Workflow", function: "AI function", compute: "Code function", query: "Query", action: "Action" } as Record<string, string>)[kind] ?? kind);

export function Applications() {
  const { source, role } = useHost();
  const { open } = useWorkspace();
  return <div className="grid gap-3">
    <PageHeader title={t("Applications")} description={t("Build saved drafts here. Activated applications appear in the business application menu for authorized users.")}
      actions={role("build") === "builder" && <Button variant="primary" onClick={() => open({ view: "application", params: { id: "new" } })}>{t("Create application")}</Button>} />
    <RecordList source={source} type="build.app" fields={["title", "name", "state"]} onOpen={(record) => open({ view: "application", params: { id: record.id } })} />
  </div>;
}

/** Application membership writes the original build.app; release and execution
 * stay with the existing owners. Pages alone determine operator navigation. */
type AssetRecord = { id: string; name: string; title: string; state: string; version?: number; archived?: boolean };

export function ApplicationEditor({ id }: { id: string }) {
  const { client, definitions, decide, role,me } = useHost();
  const { open, close } = useWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.app/${encodeURIComponent(id)}`, undefined, id !== "new");
  const processes = useRecordInventory<{ id: string; name: string; title: string; state: string; version?: number; published?: string; archived?: boolean }>("build.process");
  // The application browses its own assets; opening one keeps this application in context (ADR-0047 §6.2 M2).
  const objects = useRecordInventory<AssetRecord>("build.object");
  const pageRecords = useRecordInventory<AssetRecord>("build.page");
  const queries = useRecordInventory<AssetRecord>("build.query");
  const functions = useRecordInventory<AssetRecord>("build.function");
  const computes = useRecordInventory<AssetRecord>("build.code");
  const links = useRecordInventory<AssetRecord>("build.linktype");
  const properties = useRecordInventory<AssetRecord>("build.propertytype");
  const mounted = useRef(true);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, [client, id]);
  const [draft, setDraft] = useState<Draft>(empty);
  const [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [resourceScope, setResourceScope] = useState<"selected" | "available">("selected");
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
  const inventories = { object: objects, flow: processes, query: queries, function: functions, compute: computes, "link-type": links, "property-type": properties };
  // Saved drafts and installed definitions share semantic identities, but keep
  // their state explicit. Membership does not publish either of them.
  const pagesByName = new Map(definitions.filter(d => d.ref.app === "build" && d.source === "tenant" && d.page).map(d => [d.ref.name, { name: d.ref.name, title: d.page!.title, saved: false }]));
  for (const page of pageRecords.data?.records ?? []) if (!page.archived) pagesByName.set(page.name, { name: page.name, title: page.title, saved: true });
  const pages = [...pagesByName.values()];
  const resourcesByRef = new Map(definitions.filter(d => applicationAssetEditors.some(editor => editor.kind === d.ref.kind) && (d.ref.app !== "build" || d.source === "tenant"))
    .map(d => [resourceKey(d.ref), { ref: d.ref, title: d.entity?.title ?? d.query?.title ?? d.function?.title ?? d.operation?.title ?? d.ref.name, state: "installed" }]));
  for (const editor of applicationAssetEditors) for (const record of inventories[editor.kind].data?.records ?? []) if (!record.archived) {
    const ref = applicationAssetRef(editor.kind, record.name);
    resourcesByRef.set(resourceKey(ref), { ref, title: record.title || record.name, state: "saved" });
  }
  const resources = [...resourcesByRef.values()];
  const selected = draft.resources ?? [];
  const toggleResource = (ref: Api.AssetRef, checked: boolean) => change({ resources: checked ? [...selected, ref] : selected.filter((item) => resourceKey(item) !== resourceKey(ref)) });
  const togglePage = (name: string, checked: boolean) => change({ pages: checked ? [...draft.pages, name] : draft.pages.filter((item) => item !== name),
    groups: (draft.groups ?? []).map((group) => ({ ...group, pages: group.pages.filter((item) => item !== name || checked) })).filter((group) => group.pages.length) });
  const movePage = (at: number, offset: number) => { const next = [...draft.pages]; [next[at], next[at + offset]] = [next[at + offset]!, next[at]!]; change({ pages: next }); };
  // Open the original editor for a listed resource; the application travels with the route.
  const openResource = (ref: Api.AssetRef) => {
    const editor = applicationAssetEditors.find(item => item.kind === ref.kind);
    const record = editor && inventories[editor.kind].data?.records.find(item => !item.archived && resourceKey(applicationAssetRef(editor.kind, item.name)) === resourceKey(ref));
    // A resource owned outside this tenant opens its read-only reference instead.
    open(editor && record ? { view: editor.view, params: { id: record.id, application: draft.id } }
      : ref.kind === "object" ? { view: "model", params: { object: ref.name, application: draft.id } }
      : { view: "definition", params: { ...ref, surface: "studio", application: draft.id } });
  };
  // The editor is the same one the resource library opens; only the return context differs.
  const inventoryFor = (kind: string) => inventories[kind as keyof typeof inventories];
  const openPage = (name: string) => {
    const record = pageRecords.data?.records.find((item) => item.name === name);
    open(record ? { view: "compose", params: { id: record.id, application: draft.id } }
      : { view: "page-preview", params: { app: "build", kind: "page", name, surface: "studio", application: draft.id } });
  };
  // Creating within an application explicitly adds the saved resource to its
  // membership. Both writes use original actions and optimistic revisions.
  const addCreated = async (target: { type: string; id: string }) => {
    setBusy(true); setError("");
    try {
      const created = await client.get<{ record: AssetRecord }>(`/v1/records/${target.type}/${encodeURIComponent(target.id)}`);
      if (!mounted.current) return;
      const editor = applicationAssetEditors.find(item => item.type === target.type);
      const patch = target.type === "build.page" ? { pages: [...new Set([...draft.pages, created.record.name])] }
        : editor ? { resources: [...selected.filter(ref => resourceKey(ref) !== resourceKey(applicationAssetRef(editor.kind, created.record.name))), applicationAssetRef(editor.kind, created.record.name)] } : undefined;
      if (!patch) return;
      const ok = await decide("build.app.edit", { type: "build.app", id: draft.id }, patch, { expectedRevision: draft.revision, quiet: true, onRefused: setError });
      if (!mounted.current) return;
      if (!ok) { setError(t("The resource was saved but could not be added to this application. Reload the application and select it from the saved resources.")); return; }
      await query.refetch();
      if (!mounted.current) return;
      open({ view: target.type === "build.page" ? "compose" : editor!.view, params: { id: target.id, application: draft.id } });
    } catch { setError(t("The resource was saved but could not be added to this application. Reload the application and select it from the saved resources.")); }
    finally { setBusy(false); }
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
  const visible = resources.filter(resource => resourceScope === "available" || selected.some(ref => resourceKey(ref) === resourceKey(resource.ref)))
    .filter((resource) => `${resource.title} ${resource.ref.app} ${resource.ref.name}`.toLowerCase().includes(search.toLowerCase()));
  const grouped = (draft.groups ?? []).flatMap((group) => group.pages);
  const delivered = definitions.find(d => d.ref.app === "build" && d.ref.kind === "app" && d.ref.name === draft.name)?.application;
  const deliveredPage = delivered?.pages.find(name => definitions.some(d => d.ref.app === "build" && d.ref.kind === "page" && d.ref.name === name));
  const membershipUnavailable = pageRecords.isLoading || pageRecords.isError || Object.values(inventories).some(inventory => inventory.isLoading || inventory.isError);
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={draft.title || t("New application")} description={t("Choose saved pages and resources, then deliver the application as one candidate.")}
      actions={<div className="flex flex-wrap gap-2"><Button onClick={() => open({ view: "applications" })}>{t("Applications")}</Button>
        {delivered&&<Button onClick={()=>open({view:"application-runs",params:{app:"build",name:draft.name,application:`build:${draft.name}`}})}>{t("Application runs")}</Button>}
        <AssetControls type="build.app" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{ view: "application", params: { id } }} />
        <Button disabled={busy || !!draft.id && !dirty || !draft.name || !draft.title} onClick={() => void save()}>{t(draft.id ? "Save application" : "Create application")}</Button>
        <Button disabled={busy||dirty||!draft.id||!importApplication} onClick={()=>setImporting(true)}>{t('Import complete Workshop module')}</Button>
        <Button variant="primary" disabled={busy || dirty || !draft.id || !draft.pages.length || processes.isLoading || processes.isError}
          onClick={() => open({ view: "release-review", params: { kind: "app", id: draft.id, application: draft.id, ...(importedDependencies.length ? { drafts: importedDependencies.map((dependency) => `${dependency.kind}:${dependency.id}`).join(",") } : {}) } })}>{t("Review application release")}</Button>
        {deliveredPage && <Button onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: deliveredPage, application: `build:${draft.name}` } })}>{t("Open business application")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    <Panel title={t("Application work") } className="grid gap-3">
      <p className="text-sm">{deliveredPage ? t("An installed version is available in the business application menu. Saved edits remain drafts until activation.") : t("This application is available in the Studio application menu. Add a page, review its dependent drafts, then activate the candidate to make it available for business use.")}</p>
      {!draft.id || dirty ? <p className="text-xs text-muted">{t("Save the application before creating resources within it.")}</p> : <div className="grid gap-3 md:grid-cols-3">
        <div className="grid content-start gap-2"><h2 className="text-sm font-semibold">{t("Pages and experience")}</h2><NewActions type="build.page" disabled={busy} onCreated={target => void addCreated(target)} /></div>
        <div className="grid content-start gap-2"><h2 className="text-sm font-semibold">{t("Data and semantics")}</h2>{["build.object", "build.linktype", "build.propertytype", "build.query"].map(type => <NewActions key={type} type={type} disabled={busy} onCreated={target => void addCreated(target)} />)}</div>
        <div className="grid content-start gap-2"><h2 className="text-sm font-semibold">{t("Logic and AI")}</h2>{["build.process", "build.function", "build.code"].map(type => <NewActions key={type} type={type} disabled={busy} onCreated={target => void addCreated(target)} />)}<Button onClick={() => open({ view: "candidate-test", params: { application: draft.id } })}>{t("Test a candidate")}</Button></div>
      </div>}
      {membershipUnavailable && <p role="status" className="text-xs text-warning">{t("Some resource inventories are unavailable or still loading. The selection may be incomplete; retry before delivery.")}</p>}
    </Panel>
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
          {pages.map((page) => <Checkbox key={page.name} checked={draft.pages.includes(page.name)} onChange={(checked) => togglePage(page.name, checked)}>{page.title} · {page.name} · {t(page.saved ? "Saved draft" : "Installed")}</Checkbox>)}
          {!pages.length && <p className="text-sm text-muted">{t("Create a page draft here, or select an existing page. Publishing it separately is not required.")}</p>}
          {draft.pages.map((name, at) => <div key={name} className="flex items-center gap-2 rounded border border-border p-2 text-xs">
            <span className="min-w-0 flex-1 truncate">{pages.find((page) => page.name === name)?.title ?? name}</span>
            <Button size="sm" aria-label={t("Open {title}", { title: pages.find((page) => page.name === name)?.title ?? name })} onClick={() => openPage(name)}>{t("Open")}</Button>
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
              change({ groups: draft.groups!.map((item, i) => i === at ? { ...item, pages: checked ? [...item.pages, name] : item.pages.filter((page) => page !== name) } : item) })}>{pages.find((page) => page.name === name)?.title ?? name}</Checkbox>)}
          </div>)}
          <Button size="sm" onClick={() => change({ groups: [...(draft.groups ?? []), { title: t("New group"), pages: [] }] })}>{t("Add group")}</Button>
        </fieldset>
      </Card>
      <div className="grid content-start gap-3"><VariablesPanel application document={{formatVersion:2,uiProfile:draft.uiProfile ?? pageUIProfile,root:"root",nodes:{},variables:draft.variables,queries:draft.queries}} sections={[]} values={pageVariableValues(draft.variables ?? {})} onChange={(document) => change({uiProfile:pageUIProfile,variables:document.variables})} />
      <QueriesPanel application document={{formatVersion:2,uiProfile:draft.uiProfile??pageUIProfile,root:"root",nodes:{},variables:draft.variables,queries:draft.queries}} object={draft.queries?Object.values(draft.queries)[0]?.object??{app:"build",kind:"object",name:""}:{app:"build",kind:"object",name:""}} values={{}} onChange={(document)=>change({uiProfile:pageUIProfile,variables:document.variables,queries:document.queries})}/>
      <Panel title={t("Application resources")} className="grid min-w-0 content-start gap-3">
        <StatusTag status={draft.state} registry={states} />
        <p className="text-xs text-muted">{t("Resources keep their original ownership and permissions. They may be shared by multiple applications.")}</p>
        <p className="text-xs text-muted">{t("Membership selects resources; release review explicitly selects saved drafts and resolves their dependencies. Saving does not activate them.")}</p>
        <Button size="sm" onClick={() => setResourceScope(resourceScope === "selected" ? "available" : "selected")}>{t(resourceScope === "selected" ? "Choose existing resources" : "Show application resources")}</Button>
        {resourceScope === "selected" && !selected.length && <p className="text-xs text-muted">{t("No resources selected. Create them in this application or choose existing resources.")}</p>}
        <Input aria-label={t("Search application resources")} placeholder={t("Search objects, workflows and functions")} value={search} onChange={(e) => setSearch(e.target.value)} />
        {processes.isError && <p role="alert" className="text-sm text-danger">{t("The workflow inventory could not be loaded.")}</p>}
        <fieldset className="grid max-h-[32rem] gap-3 overflow-auto" aria-label={t("Available resources")}>
          {visible.map((resource) => <div key={resourceKey(resource.ref)} className="flex items-start gap-2">
            <Checkbox className="min-w-0 flex-1" checked={selected.some((ref) => resourceKey(ref) === resourceKey(resource.ref))} onChange={(checked) => toggleResource(resource.ref, checked)}>
              <span className="grid gap-1 text-xs"><span>{resource.title}</span><span className="break-all text-muted">{kindTitle(resource.ref.kind)} · {resource.ref.app} · {t(resource.state === "saved" ? "Saved draft" : "Installed")}</span></span>
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
