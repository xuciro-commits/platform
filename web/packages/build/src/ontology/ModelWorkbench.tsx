import type {PageUI} from "@platform/app";
import { useCurrentBranch } from "../branches/current";
import { useDraftInventory } from "../branches/records";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { useStudioApplication } from "../projects/use-studio-application";
import { SystemDefinitionsToggle, useSystemDefinitionsVisibility } from "../editor/SystemDefinitions";
import { NewActions, assetKey, useDependencies, newId, pageDocumentFromSections, semanticModelView, useHost, type Definition, type SemanticRelation } from "@platform/app";
import { ContextHelp, Button, Checkbox, DataTable, EmptyState, Form, Input, PageHeader, Panel, ObjectIcon, PropertyList, RecordList, Tag,
  t, type ColumnDef, ContentTabs } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { Layers, Link2 } from "lucide-react";
import { useInterfaces } from "./shape";
import { projectGroup, useProjectIndex } from "../projects/membership";
import { useMemo, useState } from "react";

type DraftProperty = { name: string; title: string; type: string; property?:Api.AssetBinding; choices?: string; required?: boolean; ref?: string; inverse?: string; read?: string[]; write?: string[] };
type ObjectDraft = { id: string; revision: number; archived?: boolean; name: string; title: string; icon?: string; color?: string; state: string; fields: DraftProperty[]; actions?: { name: string; title: string }[]; access?: { role: string; read: string }[]; implements?: string[]; extends?: string };
type Resource = { ref: Api.AssetRef; title: string; source: string; installed?: Definition; draft?: ObjectDraft; fields: (Api.FieldInfo | DraftProperty)[] };
type PageSeed = { object: Api.AssetRef; field?: string; relation?: SemanticRelation };

function useObjectResources() {
  const host = useHost();
  const application = useApplicationScope();
  const { application: project, installed: installedApplication } = useStudioApplication(application);
  const held = project?.resources?.filter(ref => ref.kind === "object").map(ref => ref.name);
  const inventory = useDraftInventory<ObjectDraft>("build.object", 1000, !installedApplication);
  const model = useMemo(() => semanticModelView(host.definitions), [host.definitions]);
  const resources = useMemo(() => {
    const all = new Map<string, Resource>(model.objects.map((installed) => [installed.ref.name, { ref: installed.ref, title: installed.entity!.title, source: installed.source, installed, fields: installed.entity!.fields }]));
    for (const draft of installedApplication ? [] : inventory.data?.records ?? []) {
      if (draft.archived) continue;
      const name = `build.${draft.name}`, installed = all.get(name)?.installed;
      all.set(name, { ref: { app: "build", kind: "object", name }, title: installed?.entity?.title ?? draft.title, source: "tenant", installed, draft, fields: installed?.entity?.fields ?? draft.fields });
    }
    return [...all.values()].filter(item => !application || held?.includes(item.ref.name)).sort((a, b) => a.title.localeCompare(b.title));
  }, [model, inventory.data, installedApplication, application, project]);
  return { resources, model, inventory, application };
}

const iconOf = (item: Resource) => ({ icon: item.installed?.entity?.icon ?? item.draft?.icon, color: item.installed?.entity?.color ?? item.draft?.color });

export function ModelWorkbench({ initialObject, initialTab }: { initialObject?: string; initialTab?: string }) {
  const { holds } = useHost();
  if (!holds("build", "builder")) return <PageHeader title={t("Object types")} description={t("Only a builder can edit application assets.")} />;
  return initialObject ? <ObjectTypeDetail key={initialObject} name={initialObject} initialTab={initialTab} /> : <ObjectTypes />;
}

function ObjectTypes() {
  const host = useHost();
  const { id: branch } = useCurrentBranch();
  const { open } = useApplicationWorkspace();
  const { resources, model, inventory, application } = useObjectResources();
  const [search, setSearch] = useState("");
  const [showSystem, setShowSystem] = useSystemDefinitionsVisibility();
  const includeSystem = showSystem || !!application;
  const projects = useProjectIndex();
  const appTitle = (id: string) => host.me.apps.find((a) => a.id === id)?.title ?? id;
  const visible = useMemo(() => resources.filter(item => (includeSystem || item.source !== "code") && `${item.title} ${item.ref.name}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [resources, includeSystem, search]);
  const linkCount = (item: Resource) => model.relations.filter(relation => relation.ref.object.name === item.ref.name || relation.target.name === item.ref.name).length;
  const actionCount = (item: Resource) => Math.max(host.definitions.filter(definition => definition.ref.kind === "action" && definition.action?.target === item.ref.name).length, item.draft?.actions?.length ?? 0);
  const openResource = (item: Resource) => item.draft && host.can("build.object.edit")
    ? open({ view: "object-type", params: { id: item.draft.id } })
    : open({ view: "object-type", params: { object: item.ref.name } });
  const count = (value: number) => <span className="tabular-nums text-muted">{value}</span>;
  const columns: ColumnDef<Resource>[] = [
    { id: "title", accessorKey: "title", header: t("Object type"), meta: { width: 300, pin: "left" }, cell: ({ row }) => <span className="flex min-w-0 items-center gap-2">
      <ObjectIcon {...iconOf(row.original)} className="size-6 rounded-sm" />
      <span className="grid min-w-0"><span className="truncate font-medium">{row.original.title}</span><span className="truncate font-mono text-2xs text-muted">{row.original.ref.name}</span></span>
    </span> },
    { id: "state", header: t("Status"), meta: { width: 110 }, accessorFn: (item) => item.installed ? "published" : "draft", cell: ({ row }) => <Tag label={t(row.original.installed ? (row.original.draft && row.original.draft.state !== "published" ? "Changed" : "Published") : "Draft")} tone={row.original.installed ? (row.original.draft && row.original.draft.state !== "published" ? "warning" : "success") : "neutral"} /> },
    { id: "properties", header: t("Properties"), meta: { width: 100 }, accessorFn: (item) => item.fields.length, cell: ({ row }) => count(row.original.fields.length) },
    { id: "links", header: t("Links"), meta: { width: 80 }, accessorFn: linkCount, cell: ({ row }) => count(linkCount(row.original)) },
    { id: "actions", header: t("Actions"), meta: { width: 90 }, accessorFn: actionCount, cell: ({ row }) => count(actionCount(row.original)) },
    { id: "project", header: t("Project"), meta: { width: 200 }, accessorFn: (item) => item.source === "code" ? appTitle(item.installed?.ref.app ?? item.ref.app) : projectGroup(projects, item.ref).label },
    { id: "source", header: t("Defined by"), meta: { width: 140 }, accessorFn: (item) => item.source, cell: ({ row }) => <span className="text-muted">{t(row.original.source === "code" ? "Native code" : "Tenant definition")}</span> },
  ];
  return <div className="flex min-h-full flex-col gap-3">
    <PageHeader title={t("Object types")} meta={t("{count} object types", { count: visible.length })} help={t("Every object type in the tenant ontology. Open one to edit its properties, links, actions and access.")}
      actions={<>
        <Input type="search" aria-label={t("Search object types")} placeholder={t("Search object types")} value={search} onChange={(event) => setSearch(event.target.value)} className="w-56" />
        {!application && <SystemDefinitionsToggle checked={showSystem} onChange={setShowSystem} />}
        {application ? <Button variant="primary" onClick={() => open({ view: "project", params: { id: application } })}>{t("Add object types")}</Button>
          : <NewActions type="build.object" onCreated={branch ? target => open({ view: "object-type", params: { id: target.id } }) : undefined} />}
      </>} />
    {inventory.isError && <Panel role="alert">{t("Object drafts could not be loaded. Installed definitions are still available.")}</Panel>}
    <div className="min-h-[24rem] flex-1">
      <DataTable data={visible} columns={columns} getRowId={(item) => item.ref.name} height="100%" loading={inventory.isLoading} searchable={false}
        onRowClick={openResource} empty={search ? t("No matching objects.") : t("No object types yet.")} />
    </div>
  </div>;
}

function ObjectTypeDetail({ name, initialTab }: { name: string; initialTab?: string }) {
  const host = useHost();
  const { open } = useApplicationWorkspace();
  const { resources, model, inventory } = useObjectResources();
  const resource = resources.find(item => item.ref.name === name);
  const [tab, setTab] = useState(initialTab ?? "properties");
  const [seed, setSeed] = useState<PageSeed>();
  const dependencies = useDependencies(resource?.ref, undefined, !!resource?.installed);
  if (!resource) return <PageHeader title={t("Object type")} crumbs={[{ label: t("Object types"), onClick: () => open({ view: "object-type" }) }]} description={inventory.isLoading ? t("Loading…") : t("This object type is not visible to you.")} />;
  const related = model.relations.filter((relation) => relation.ref.object.name === name || relation.target.name === name);
  const actions = host.definitions.filter((definition) => definition.ref.kind === "action" && definition.action?.target === name);
  const used = (dependencies.data?.usedBy ?? []).flatMap(ref => { const owner = host.definitions.find(definition => assetKey(definition.ref) === assetKey(ref)); return owner ? [owner] : []; });
  const edit = (parameters: Record<string, string> = {}) => { if (resource.draft && host.can("build.object.edit")) open({ view: "object-type", params: { id: resource.draft.id, ...parameters } }); };
  const entity = resource.installed?.entity;
  return <div className="grid min-w-0 gap-3">
    <PageHeader crumbs={[{ label: t("Object types"), onClick: () => open({ view: "object-type" }) }]} icon={<ObjectIcon {...iconOf(resource)} />} title={resource.title}
      meta={<span className="font-mono">{resource.ref.name}</span>}
      status={<Tag label={t(resource.source === "code" ? "Native code" : "Tenant definition")} />}
      actions={<>
        {resource.installed && <Button onClick={() => setSeed({ object: resource.ref })}>{t("Create page from model")}</Button>}
        {resource.draft && <Button variant="primary" disabled={!host.can("build.object.edit")} onClick={() => edit()}>{t("Edit object type")}</Button>}
      </>} />
    {resource.source === "code" && <p className="text-xs text-muted">{t("Native definitions are maintained by their code owner. This workbench provides read-only inspection.")}</p>}
    {entity?.description && <p className="max-w-3xl text-sm text-muted">{entity.description}</p>}
    <PropertyList items={[[t("Owner"), resource.ref.app], [t("Version"), resource.installed?.version ?? t("Draft")], [t("Title property"), entity?.display ?? "—"], [t("Implements"), entity?.implements?.join(", ") || "—"]]} />
    <ContentTabs label={t("Object type sections")} value={tab} onChange={setTab} items={[
      { id: "properties", title: t("Properties"), badge: resource.fields.length, content: <DataTable data={resource.fields} getRowId={(field) => field.name} searchable={false} height={420}
        columns={[{ accessorKey: "title", header: t("Property") }, { accessorKey: "name", header: t("API name"), cell: ({ row }) => <span className="font-mono text-xs">{row.original.name}</span> }, { accessorKey: "type", header: t("Type"), cell: ({ row }) => t(row.original.type) }, { id: "shared", header: t("Shared property"), accessorFn: f => f.property ? `${f.property.ref.name}@${f.property.sourceVersion}` : "—" }, { id: "reference", header: t("Reference object"), accessorFn: (field) => field.ref ?? "—" }, { id: "actions", header: "", meta: { width: 140 }, cell: ({ row }) => resource.installed ? <Button size="sm" variant="ghost" onClick={() => setSeed({ object: resource.ref, field: row.original.name })}>{t("Use in page")}</Button> : null }]} /> },
      { id: "links", title: t("Links"), badge: related.length, content: <div className="grid gap-1">{related.map((relation) => {
        const inbound = relation.target.name === name;
        const usable = inbound && (!!relation.inverse || !!relation.linkType) && relation.field.type === "reference";
        return <div key={relation.ref.kind === "link-type" ? `${relation.ref.binding.ref.app}/${relation.ref.binding.ref.name}` : `${relation.ref.object.name}/${relation.ref.field}`} className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-2">
          <Link2 className="size-3.5 text-muted" /><span className="min-w-0 flex-1"><span className="block text-sm font-medium">{inbound ? relation.inverse ?? relation.title : relation.title}</span><span className="block truncate font-mono text-2xs text-muted">{relation.ref.object.name}.{relation.ref.field} → {relation.target.name}</span></span>
          <Tag label={t(inbound ? "Incoming reference" : "Outgoing reference")} />
          {usable && <Button size="sm" variant="ghost" onClick={() => setSeed({ object: resource.ref, relation })}>{t("Use in page")}</Button>}
          <Button size="sm" variant="ghost" onClick={() => open({ view: "object-type", params: { object: inbound ? relation.ref.object.name : relation.target.name } })}>{t("Open related object")}</Button>
        </div>;
      })}{!related.length && <EmptyState title={t("No visible reference relationships.")} />}</div> },
      { id: "actions", title: t("Actions"), badge: actions.length, content: <div className="grid gap-1">{actions.map((definition) => <div key={definition.ref.name} className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-2">
          <span className="min-w-0 flex-1"><span className="block text-sm font-medium">{definition.action!.title}</span><span className="block truncate text-xs text-muted">{definition.action!.payload.map(field => `${field.name}${field.required ? "*" : ""}`).join(" · ")}</span></span>
          <span className="font-mono text-2xs text-muted">{definition.ref.name}</span>
          {resource.draft && <Button size="sm" variant="ghost" onClick={() => edit({ action: definition.ref.name.replace(`${name}.`, "") })}>{t("Edit action")}</Button>}
        </div>)}{!actions.length && <EmptyState title={t("No action of this object is offered to you.")} />}</div> },
      { id: "data", title: t("Data"), content: resource.installed ? <RecordList source={host.source} type={resource.ref.name} fields={resource.fields.filter((field) => field.type !== "lines" && !("aside" in field && field.aside)).slice(0, 6).map((field) => field.name)} height={420} /> : <EmptyState title={t("Publish this object before reading its business records.")} /> },
      { id: "usage", title: t("Usage"), badge: used.length, content: <div className="grid gap-1">{dependencies.isError && <p role="alert" className="text-xs text-danger">{t("Asset dependencies could not be loaded.")}</p>}
        {used.map(owner => <Button key={assetKey(owner.ref)} variant="row" onClick={() => owner.page ? open({ view: "page", params: { app: owner.ref.app, kind: "page", name: owner.ref.name } }) : open({ view: "definition", params: { ...owner.ref, surface: "studio" } })}>
          <span className="min-w-0 flex-1 truncate">{owner.page?.title ?? owner.action?.title ?? owner.query?.title ?? owner.ref.name}</span><Tag label={owner.ref.kind} /></Button>)}
        {!used.length && <EmptyState title={t("No visible declared usage.")} />}</div> },
      { id: "access", title: t("Access"), content: <div className="grid gap-2"><PropertyList items={resource.fields.filter((field) => field.read?.length || field.write?.length).map((field) => [field.title, `${t("Read")}: ${field.read?.join(", ") ?? "—"} · ${t("Write")}: ${field.write?.join(", ") ?? "—"}`])} />
        {resource.draft && <><PropertyList items={(resource.draft.access ?? []).map((access) => [access.role, access.read])} /><div><Button onClick={() => edit({ access: "true" })}>{t("Edit access")}</Button></div></>}</div> },
    ]} />
    {seed && <PageFromModel key={`${seed.object.name}/${seed.field ?? seed.relation?.ref.field ?? ""}`} seed={seed} onClose={() => setSeed(undefined)} />}
  </div>;
}

export function Interfaces() {
  const host = useHost();
  const { open } = useApplicationWorkspace();
  const { resources } = useObjectResources();
  const [showSystem, setShowSystem] = useSystemDefinitionsVisibility();
  const interfaces = useInterfaces().filter(shape => showSystem || host.definitions.find(definition => definition.ref.app === shape.app && definition.ref.kind === "interface" && definition.ref.name === shape.name)?.source !== "code");
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={t("Interfaces")} meta={t("{count} interfaces", { count: interfaces.length })} help={t("An interface is a shape several objects share; a page or query written against it works for all of them. Apps declare interfaces; objects implement them.")}
      actions={<SystemDefinitionsToggle checked={showSystem} onChange={setShowSystem} />} />
    <div className="grid gap-2">
      {interfaces.map((shape) => { const implementers = resources.filter((item) => item.installed?.entity?.implements?.includes(shape.name)); return <div key={shape.name} className="grid gap-1.5 rounded-md border border-border bg-surface p-3">
        <div className="flex items-center gap-2"><Layers className="size-4 text-primary" /><span className="text-sm font-semibold">{shape.title}</span><span className="font-mono text-xs text-muted">{shape.name}</span><Tag label={shape.app} /></div>
        {shape.description && <p className="text-xs text-muted">{shape.description}</p>}
        <p className="text-xs">{shape.fields.map((f) => `${f.name}: ${t(f.type)}`).join(" · ")}</p>
        <div className="flex flex-wrap items-center gap-1"><span className="text-xs text-muted">{t("Implemented by")}</span>{implementers.map((item) => <Button key={item.ref.name} size="sm" variant="ghost" onClick={() => open({ view: "object-type", params: { object: item.ref.name } })}>{item.title}</Button>)}{!implementers.length && <span className="text-xs text-muted">{t("No installed object implements it yet.")}</span>}</div>
      </div>; })}
      {!interfaces.length && <EmptyState title={t("No app declares an interface yet.")} />}
    </div>
  </div>;
}

function PageFromModel({ seed, onClose }: { seed: PageSeed; onClose: () => void }) {
  const { definitions, decide, can } = useHost();
  const { open } = useApplicationWorkspace();
  const [name, setName] = useState(""), [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const entity = definitions.find((definition) => definition.ref.kind === "object" && definition.ref.name === seed.object.name)?.entity;
  const relation = seed.relation;
  const related = relation?.owner.entity;
  const initialFields = seed.field ? [seed.field] : entity?.fields.filter((field) => !field.aside && field.type !== "lines").slice(0, 4).map((field) => field.name) ?? [];
  const [fields, setFields] = useState(initialFields);
  const valid = !!entity && fields.length > 0 && /^[a-z][a-z0-9]*$/.test(name) && !!title.trim() && can("build.page.create");
  const [pageID]=useState(()=>newId("PAGE"));
  const create = async () => {
    if (!valid || busy) return; setBusy(true); setError("");
    try {
      const sections: PageUI.Section[] = relation && related ? [
        { widget: "table", title: entity!.plural, fields, selection: "parent" },
        { widget: "table", title: related.plural, object: relation.ref.object, fields: related.fields.filter((field) => !field.aside && field.type !== "lines").slice(0, 4).map((field) => field.name), ...(relation.ref.kind==="link-type"?{collectionVariable:"relatedWindow"}:{relation:relation.inverse,parentSelection:"parent"}), selection:"related" },
        { widget: "detail", title: t("Detail"), object: relation.ref.object, fields: related.fields.filter((field) => !field.aside && field.type !== "lines").slice(0, 4).map((field) => field.name), selection: "related" },
      ] : [{ widget: "table", title: entity!.plural, fields }, { widget: "detail", title: t("Detail"), fields }];
      const lifted = pageDocumentFromSections(sections);
      if(relation?.ref.kind==="link-type") {
        lifted.document.variables={...lifted.document.variables,parentRecord:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:lifted.sections[0]!.id}},relatedWindow:{scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"related"}}};
        lifted.document.queries={related:{title:relation.linkType?.title,object:relation.ref.object,query:relation.ref.binding,direction:"forward",for:{variable:"parentRecord"},sort:["id"],limit:50}};
      }
      const id = pageID;
      const payload = { name, title: title.trim(), object: seed.object.name, document: lifted.document,
        sections: lifted.sections.map((section) => ({ ...section, object: section.object?.name })),
        selections: relation ? [{ name: "parent", object: seed.object }, { name: "related", object: relation.ref.object }] : [] };
      if (await decide("build.page.create", { type: "build.page", id }, payload, { expectedRevision: 0, onRefused: setError })) open({ view: "module", params: { page: id } });
    } catch { setError(t("The page draft could not be created. Your choices are still here.")); }
    finally { setBusy(false); }
  };
  return <Panel role="region" aria-label={t("Create page from model")} title={t("Create page from model")}>
    <Form className="grid gap-3" onSubmit={() => create()}>
      <ContextHelp title={t("Create page draft")}><p className="text-xs text-muted">{t("This creates a controlled page draft. Review its bindings before releasing it.")}</p></ContextHelp>
      <fieldset disabled={busy} className="grid gap-3">
        <label className="grid gap-1 text-xs">{t("Page name")}<Input value={name} onChange={(event) => setName(event.target.value)} required /></label>
        <label className="grid gap-1 text-xs">{t("Page title")}<Input value={title} onChange={(event) => setTitle(event.target.value)} required /></label>
        <fieldset className="grid gap-1"><legend className="mb-2 text-xs">{t("Visible fields")}</legend>{entity?.fields.filter((field) => !field.aside && field.type !== "lines").map((field) => <Checkbox key={field.name} checked={fields.includes(field.name)} onChange={(on) => setFields(on ? [...fields, field.name] : fields.filter((name) => name !== field.name))}>{field.title}</Checkbox>)}</fieldset>
      </fieldset>
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      <div className="flex gap-2"><Button type="submit" variant="primary" loading={busy} disabled={!valid}>{t("Create page draft")}</Button><Button disabled={busy} onClick={onClose}>{t("Cancel")}</Button></div>
    </Form>
  </Panel>;
}
