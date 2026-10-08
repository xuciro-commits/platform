import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceControls as AssetControls } from "../editor/workbench";
import { NewActions, newId, pageDocumentFromSections, semanticModelView, assetBindingKey, useHost, useRecordInventory,
  type Definition, type PropertyRef, type SemanticRelation } from "@platform/app";
import { Button, Card, Checkbox, DataTable, EditorWorkbench, Form, GroupedList, Input, NodeCanvas, PageHeader, Panel, PropertyList, RecordList, Select, Tag,
  canvasNodeWidth, layout, t, type CanvasEdge, type CanvasNode, type ColumnDef, type NodeCatalog } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { Boxes, Database, GitBranch, Layers, Link2 } from "lucide-react";
import { useInterfaces } from "./shape";
import { projectGroup, useProjectIndex } from "../projects/membership";
import { useEffect, useMemo, useState } from "react";

type DraftProperty = { name: string; title: string; type: string; property?:Api.AssetBinding; choices?: string; required?: boolean; ref?: string; inverse?: string; read?: string[]; write?: string[] };
type ObjectDraft = { id: string; revision: number; archived?: boolean; name: string; title: string; state: string; fields: DraftProperty[]; actions?: { name: string; title: string }[]; access?: { role: string; read: string }[]; implements?: string[]; extends?: string };
type Resource = { ref: Api.AssetRef; title: string; source: string; installed?: Definition; draft?: ObjectDraft; fields: (Api.FieldInfo | DraftProperty)[] };
type Selection = { kind: "property"; ref: PropertyRef } | { kind: "relation"; relation: SemanticRelation; inbound: boolean } | { kind: "action"; definition: Definition };
type PageSeed = { object: Api.AssetRef; field?: string; relation?: SemanticRelation };
const objectCatalog: NodeCatalog = [{ id: "object", title: t("Object"), category: "model", inputs: [{ id: "in", label: "", type: "reference" }], outputs: [{ id: "out", label: "", type: "reference" }] }];

/** The original member-scoped definitions are the ontology. Builder drafts are
 * linked authoring resources; browsing never mutates or republishes them.
 */
export function ModelWorkbench({ initialObject, initialTab }: { initialObject?: string; initialTab?: string }) {
  const { role } = useHost();
  return role("build") === "builder" ? <ModelInventory initialObject={initialObject} initialTab={initialTab} />
    : <PageHeader title={t("Objects")} description={t("Only a builder can edit application assets.")} />;
}

function ModelInventory({ initialObject, initialTab }: { initialObject?: string; initialTab?: string }) {
  const host = useHost();
  const { open } = useApplicationWorkspace();
  const inventory = useRecordInventory<ObjectDraft>("build.object");
  const linkInventory=useRecordInventory<{id:string;name:string}>("build.linktype");
  const propertyInventory=useRecordInventory<{id:string;name:string}>("build.propertytype");
  const model = useMemo(() => semanticModelView(host.definitions), [host.definitions]);
  const [search, setSearch] = useState(""), [origin, setOrigin] = useState("all");
  const [current, setCurrent] = useState(initialObject ?? ""), [view, setView] = useState<"catalog" | "graph" | "detail" | "shared" | "interfaces">(initialObject ? "detail" : "catalog");
  const interfaces = useInterfaces();
  const [shared,setShared]=useState("");
  const [tab, setTab] = useState(initialTab ?? "overview"), [selection, select] = useState<Selection>();
  const [seed, setSeed] = useState<PageSeed>();
  const resources = useMemo(() => {
    const all = new Map<string, Resource>(model.objects.map((installed) => [installed.ref.name, { ref: installed.ref, title: installed.entity!.title, source: installed.source, installed, fields: installed.entity!.fields }]));
    for (const draft of inventory.data?.records ?? []) {
      if (draft.archived) continue;
      const name = `build.${draft.name}`, installed = all.get(name)?.installed;
      all.set(name, { ref: { app: "build", kind: "object", name }, title: installed?.entity?.title ?? draft.title, source: "tenant", installed, draft, fields: installed?.entity?.fields ?? draft.fields });
    }
    return [...all.values()].sort((a, b) => a.title.localeCompare(b.title));
  }, [model, inventory.data]);
  const resource = resources.find((item) => item.ref.name === current);
  const visible = useMemo(() => resources.filter((item) => (origin === "all" || item.source === origin) && `${item.title} ${item.ref.name}`.toLocaleLowerCase().includes(search.toLocaleLowerCase())), [resources, origin, search]);
  const projects = useProjectIndex();
  const appTitle = (id: string) => host.me.apps.find((a) => a.id === id)?.title ?? id;
  const choose = (item: Resource) => { setCurrent(item.ref.name); setView("detail"); select(undefined); setTab("overview"); };
  useEffect(() => { setSeed(undefined); }, [current]);
  const edit = (item: Resource, parameters: Record<string, string> = {}) => {
    if (item.draft && host.can("build.object.edit")) open({ view: "object-type", params: { id: item.draft.id, ...parameters } });
  };
  const columns: ColumnDef<Resource>[] = [
    { accessorKey: "title", header: t("Object"), meta: { width: 210, pin: "left" } },
    { id: "identity", accessorFn: (item) => item.ref.name, header: t("Identity"), meta: { width: 230 } },
    { id: "source", header: t("Source"), accessorFn: (item) => item.source, cell: ({ row }) => <Tag label={t(row.original.source === "code" ? "Native code" : "Tenant definition")} /> },
    { id: "state", header: t("Status"), accessorFn: (item) => item.installed ? "published" : "draft", cell: ({ row }) => t(row.original.installed ? "Published" : "Draft") },
  ];
  const graph = useMemo(() => {
    // The graph is a bounded view. Search and origin filters are reflected in
    // both the node set and edges; hidden objects never get placeholder nodes.
    const shown = visible.slice(0, 80), ids = new Set(shown.map((item) => item.ref.name));
    const edges: CanvasEdge[] = model.relations.filter((relation) => ids.has(relation.ref.object.name) && ids.has(relation.target.name)).map((relation) => ({
      id: relation.ref.kind==="link-type"?`${relation.ref.binding.ref.app}/${relation.ref.binding.ref.name}`:`${relation.ref.object.name}/${relation.ref.field}`, source: relation.ref.object.name, target: relation.target.name, sourcePort: "out", targetPort: "in", label: relation.title,
    }));
    const positions = layout(shown.map((item) => ({ id: item.ref.name, label: item.title })), edges.map((edge) => ({ from: edge.source, to: edge.target })), "right",
      { width: canvasNodeWidth, height: 74, gapX: 80, gapY: 32 });
    // A relation graph is a map, not an editor: every object starts collapsed and the toolbar expands them all.
    const nodes: CanvasNode[] = shown.map((item) => ({ id: item.ref.name, kind: "object", label: item.title, detail: item.ref.name, collapsed: true, position: positions.get(item.ref.name)! }));
    return { nodes, edges };
  }, [visible, model]);
  const related = resource ? model.relations.filter((relation) => relation.ref.object.name === current || relation.target.name === current) : [];
  const actions = host.definitions.filter((definition) => definition.ref.kind === "action" && definition.action?.target === current);
  const used = model.usages.filter((usage) => usage.resource.name === current && usage.resource.kind === "object");
  const property = selection?.kind === "property" ? resource?.fields.find((field) => field.name === selection.ref.field) : undefined;
  const sharedProperties=model.propertyTypes.filter(p=>(origin==="all"||p.definition.source===origin)&&`${p.property.title} ${p.binding.ref.name}`.toLocaleLowerCase().includes(search.toLocaleLowerCase()));
  const sharedProperty=model.propertyTypes.find(p=>assetBindingKey(p.binding)===shared);
  const propertySource=property?.property?model.propertyTypes.find(p=>assetBindingKey(p.binding)===assetBindingKey(property.property!)):undefined;
  const editShared=(name:string)=>{const draft=propertyInventory.data?.records.find(p=>p.name===name);if(draft)open({view:"property-type",params:{id:draft.id}});};
  return <div className="flex flex-col gap-2 lg:h-[calc(100dvh-8rem)] lg:min-h-0">
    <PageHeader title={t("Objects")} description={t("Explore the business model, inspect relationships, and bind real resources to pages.")}
      actions={<div className="flex flex-wrap gap-2"><AssetControls type="build.object" /><NewActions type="build.object"/><Button onClick={()=>open({view:"link-type",params:{id:"new",parent:current}})}>{t("New relationship")}</Button><Button onClick={()=>open({view:"property-type",params:{id:"new"}})}>{t("New shared property")}</Button></div>} />
    <Card role="toolbar" aria-label={t("Model navigation")} className="flex flex-wrap items-center gap-2 px-3 py-2">
      <Button variant={view === "catalog" ? "primary" : "ghost"} onClick={() => setView("catalog")}><Boxes />{t("Model catalog")}</Button>
      <Button variant={view === "graph" ? "primary" : "ghost"} onClick={() => setView("graph")}><GitBranch />{t("Relationship graph")}</Button>
      <Button variant={view === "shared" ? "primary" : "ghost"} onClick={() => setView("shared")}><Boxes />{t("Shared property catalog")}</Button>
      <Button variant={view === "interfaces" ? "primary" : "ghost"} onClick={() => setView("interfaces")}><Layers />{t("Interfaces")}</Button>
      <Input aria-label={t("Search model resources")} placeholder={t("Search model resources")} value={search} onChange={(event) => setSearch(event.target.value)} className="ml-auto h-8 w-56 text-xs" />
      <label className="flex min-w-0 items-center gap-2 text-xs">{t("Source")}<Select value={origin} onChange={(event) => setOrigin(event.target.value)}>
        <option value="all">{t("All sources")}</option><option value="code">{t("Native code")}</option><option value="tenant">{t("Tenant definitions")}</option>
      </Select></label>
    </Card>
    {inventory.isError && <Panel role="alert">{t("Object drafts could not be loaded. Installed definitions are still available.")}</Panel>}
    <EditorWorkbench leftLabel={t("Model resources")} centerLabel={t("Model workspace")} rightLabel={t("Semantic inspector")}
      left={<div className="grid content-start gap-3 p-3">
        <GroupedList<Resource> items={resources.filter((item) => origin === "all" || item.source === origin)} id={(item) => item.ref.name} selected={current} onSelect={choose}
          text={(item) => `${item.title} ${item.ref.name}`} dense
          groupings={[
            { id: "project", label: t("Project"), of: (item) => item.source === "code" ? { id: `app:${item.installed?.ref.app ?? item.ref.app}`, label: appTitle(item.installed?.ref.app ?? item.ref.app), hint: t("shipped"), order: 5 } : projectGroup(projects, item.ref) },
            { id: "owner", label: t("Owner"), of: (item) => ({ id: item.installed?.ref.app ?? item.ref.app, label: appTitle(item.installed?.ref.app ?? item.ref.app) }) },
            { id: "status", label: t("Status"), of: (item) => item.installed ? { id: "published", label: t("Published") } : { id: "draft", label: t("Draft"), order: 1 } },
          ]}
          row={(item) => <><Database className="size-3 shrink-0 text-muted" /><span className="min-w-0 flex-1 truncate">{item.title}</span>
            {!item.installed && <span className="shrink-0 text-[10px] text-muted">{t("Draft")}</span>}</>}
          empty={t("No matching objects.")} />
      </div>}
      right={<div className="grid content-start gap-3 p-3">
        <h3 className="text-xs font-semibold text-muted">{t("Semantic inspector")}</h3>
        {view==="shared" ? sharedProperty ? <><h2 className="text-sm font-semibold">{sharedProperty.property.title}</h2><PropertyList items={[[t("Identity"),sharedProperty.binding.ref.name],[t("Type"),t(sharedProperty.property.type)],[t("Version"),sharedProperty.binding.sourceVersion],[t("Source"),t(sharedProperty.definition.source==="code"?"Native code":"Tenant definition")]]}/><p className="text-xs">{sharedProperty.property.description}</p>{sharedProperty.definition.source==="tenant"&&<Button onClick={()=>editShared(sharedProperty.binding.ref.name)}>{t("Edit shared property")}</Button>}<p className="text-xs text-muted">{t("Local field names, required values and access belong to each object.")}</p>{resources.flatMap(item=>item.fields.filter(f=>f.property&&assetBindingKey(f.property)===shared).map(f=><Button key={`${item.ref.name}/${f.name}`} variant="row" onClick={()=>{choose(item);setTab("properties");select({kind:"property",ref:{object:item.ref,field:f.name}});}}>{item.title} · {f.name}</Button>))}</>:<p className="text-xs text-muted">{t("Choose a shared property version to inspect its consumers.")}</p> : resource ? <><h2 className="text-sm font-semibold">{property?.title ?? resource.title}</h2><p className="break-all font-mono text-xs text-muted">{current}{property ? `.${property.name}` : ""}</p>
          <Tag label={t(resource.source === "code" ? "Native code" : "Tenant definition")} />
          {view === "graph" && <Button onClick={() => choose(resource)}>{t("Open object details")}</Button>}
          {selection?.kind === "property" && property && <>
            <PropertyList items={[[t("Type"), t(property.type)], [t("Required"), property.required ? t("Yes") : t("No")]]} />
            {property.property&&<><PropertyList items={[[t("Shared property"),property.property.ref.name],[t("Version"),property.property.sourceVersion]]}/>{propertySource?<><p className="text-xs">{propertySource.property.description}</p>{propertySource.definition.source==="tenant"&&<Button onClick={()=>editShared(propertySource.binding.ref.name)}>{t("Edit shared property")}</Button>}</>:<p role="alert" className="text-xs text-danger">{t("The bound shared property version is unavailable.")}</p>}</>}
            <Button disabled={!resource.installed} onClick={() => setSeed({ object: resource.ref, field: property.name })}>{t("Use property in page")}</Button>
            {resource.draft && <Button disabled={!host.can("build.object.edit")} onClick={() => edit(resource, { field: property.name })}>{t("Edit property")}</Button>}
          </>}
          {selection?.kind === "relation" && <>
            <p className="text-xs">{selection.relation.title} · {selection.relation.ref.field}</p>
            {selection.relation.linkType?<><PropertyList items={[[t("Relationship version"),selection.relation.definition?.version??""],[t("Cardinality"),t(selection.relation.linkType.cardinality==="one-to-one"?"One-to-one (at most one child)":"One-to-many")],[t("Archive policy"),t(selection.relation.linkType.deletePolicy==="restrict-active"?"Protect active references":"Object owner")]]}/>{selection.relation.definition?.source==="tenant"&&<Button onClick={()=>{const row=linkInventory.data?.records.find(r=>r.name===selection.relation.definition?.ref.name);if(row)open({view:"link-type",params:{id:row.id}});}}>{t("Edit relationship")}</Button>}</>:<><p className="text-xs text-muted">{t("This relationship is backed by a reference field. Cardinality and delete rules are not inferred.")}</p>{selection.relation.owner.source==="tenant"&&selection.relation.targetDefinition.source==="tenant"&&<Button onClick={()=>open({view:"link-type",params:{id:"new",parent:selection.relation.target.name,child:selection.relation.ref.object.name,via:selection.relation.ref.field}})}>{t("Define relationship asset")}</Button>}</>}
            <Button onClick={() => choose(resources.find((item) => item.ref.name === (selection.inbound ? selection.relation.ref.object.name : selection.relation.target.name))!)}>{t("Open related object")}</Button>
            <Button disabled={!selection.inbound || (!selection.relation.inverse&&!selection.relation.linkType) || selection.relation.field.type !== "reference"} onClick={() => setSeed({ object: resource.ref, relation: selection.relation })}>{t("Use related records in page")}</Button>
            {(!selection.inbound || (!selection.relation.inverse&&!selection.relation.linkType) || selection.relation.field.type !== "reference") && <p className="text-xs text-muted">{t("Related-page binding needs an incoming scalar reference with a declared inverse name.")}</p>}
          </>}
          {selection?.kind === "action" && selection.definition.action && <>
            <h3 className="text-sm font-medium">{selection.definition.action.title}</h3><p className="text-xs text-muted">{selection.definition.action.description}</p>
            <PropertyList items={selection.definition.action.payload.map((field) => [field.name, `${t(field.type)}${field.required ? " *" : ""}`])} />
            {resource.draft && <Button onClick={() => edit(resource, { action: selection.definition.ref.name.replace(`${current}.`, "") })}>{t("Edit action")}</Button>}
          </>}
          {resource.draft && <Button onClick={() => edit(resource)}>{t("Edit object")}</Button>}
          {resource.source === "code" && <p className="text-xs text-muted">{t("Native definitions are maintained by their code owner. This workbench provides read-only inspection.")}</p>}
          {!resource.installed && <p className="text-xs text-muted">{t("Publish this object before binding it to a page.")}</p>}
          {resource.draft && <Button onClick={() => open({ view: "release-review", params: { kind: "object", id: resource.draft!.id } })}>{t("Review release")}</Button>}
        </> : <p className="text-xs text-muted">{t("Choose an object or relationship to inspect it.")}</p>}
      </div>}>
      <div className="min-h-[26rem] flex-1 overflow-auto p-3">
        {view==="shared"&&<DataTable data={sharedProperties} getRowId={p=>assetBindingKey(p.binding)} searchable={false} height="100%" onRowClick={p=>setShared(assetBindingKey(p.binding))} empty={t("No published shared property yet.")} columns={[{id:"title",header:t("Shared property"),accessorFn:p=>p.property.title},{id:"name",header:t("Identity"),accessorFn:p=>p.binding.ref.name},{id:"type",header:t("Type"),accessorFn:p=>t(p.property.type)},{id:"version",header:t("Version"),accessorFn:p=>p.binding.sourceVersion}]}/>}
        {view === "interfaces" && <div className="grid gap-3"><p className="text-xs text-muted">{t("An interface is a shape several objects share; a page or query written against it works for all of them. Apps declare interfaces; objects implement them.")}</p>
          {interfaces.map((shape) => { const implementers = resources.filter((item) => item.installed?.entity?.implements?.includes(shape.name)); return <div key={shape.name} className="grid gap-1 rounded-md border border-border p-3">
            <div className="flex items-center gap-2"><Layers className="size-4 text-primary" /><span className="text-sm font-semibold">{shape.title}</span><span className="font-mono text-xs text-muted">{shape.name}</span><Tag label={shape.app} /></div>
            {shape.description && <p className="text-xs text-muted">{shape.description}</p>}
            <p className="text-xs">{shape.fields.map((f) => `${f.name}: ${t(f.type)}`).join(" · ")}</p>
            <div className="flex flex-wrap gap-1">{implementers.map((item) => <Button key={item.ref.name} size="sm" onClick={() => choose(item)}>{item.title}</Button>)}{!implementers.length && <span className="text-xs text-muted">{t("No installed object implements it yet.")}</span>}</div>
          </div>; })}{!interfaces.length && <p className="text-xs text-muted">{t("No app declares an interface yet.")}</p>}</div>}
        {view === "catalog" && <DataTable data={visible} columns={columns} getRowId={(item) => item.ref.name} height="100%" loading={inventory.isLoading} searchable={false}
          onRowClick={(item) => item.draft && !item.installed ? edit(item) : choose(item)} empty={t("No matching objects.")} />}
        {view === "graph" && <div className="flex h-full min-h-[25rem] flex-col"><p className="mb-2 text-xs text-muted" role="status">{t("Showing {shown} of {total} objects", { shown: graph.nodes.length, total: visible.length })}</p>
          <NodeCanvas catalog={objectCatalog} nodes={graph.nodes} edges={graph.edges} selected={current} height="100%" label={t("Relationship graph")}
            onSelect={(name) => { setCurrent(name); select(undefined); }} onOpen={(name) => { const item = resources.find((item) => item.ref.name === name); if (item) choose(item); }} /></div>}
        {view === "detail" && resource && <div className="grid gap-3">
          <div className="flex items-center gap-3"><Database className="size-6 text-primary" /><div className="min-w-0 flex-1"><h2 className="text-base font-semibold">{resource.title}</h2><p className="font-mono text-xs text-muted">{resource.ref.name}{resource.installed?.entity?.implements?.length ? <> · {t("implements")} {resource.installed.entity.implements.join(", ")}</> : null}{resource.draft?.extends ? <> · {t("extends")} {resource.draft.extends}</> : null}</p></div><Tag label={t(resource.installed ? "Published" : "Draft")} /></div>
          <div role="tablist" aria-label={t("Object resource views")} className="flex flex-wrap gap-1 border-b border-border pb-2">
            {[["overview", "Overview"], ["properties", "Properties"], ["links", "Relationships"], ["actions", "Actions"], ["data", "Data"], ["usage", "Usage"], ["access", "Access"]].map(([key, label]) => <Button key={key} size="sm" variant={tab === key ? "primary" : "ghost"} role="tab" aria-selected={tab === key} onClick={() => { setTab(key!); select(undefined); }}>{t(label!)}</Button>)}
          </div>
          {tab === "overview" && <><PropertyList items={[[t("Identity"), resource.ref.name], [t("Owner"), resource.ref.app], [t("Source"), t(resource.source === "code" ? "Native code" : "Tenant definition")], [t("Version"), resource.installed?.version ?? t("Draft")], [t("Title property"), resource.installed?.entity?.display ?? "—"]]} />
            <p className="text-sm text-muted">{resource.installed?.entity?.description ?? t("Define fields, relationships, actions and access.")}</p></>}
          {tab === "properties" && <DataTable data={resource.fields} getRowId={(field) => field.name} searchable={false} height={380} onRowClick={(field) => select({ kind: "property", ref: { object: resource.ref, field: field.name } })}
            columns={[{ accessorKey: "title", header: t("Property") }, { accessorKey: "name", header: t("Name") }, { accessorKey: "type", header: t("Type"), cell: ({ row }) => t(row.original.type) }, { id: "shared", header: t("Shared property version"), accessorFn:f=>f.property?`${f.property.ref.name}@${f.property.sourceVersion}`:"—" }, { id: "reference", header: t("Reference object"), accessorFn: (field) => field.ref ?? "—" }]} />}
          {tab === "links" && <div className="grid gap-2">{related.map((relation) => {
            const inbound = relation.target.name === current;
            return <Button key={relation.ref.kind==="link-type"?`${relation.ref.binding.ref.app}/${relation.ref.binding.ref.name}`:`${relation.ref.object.name}/${relation.ref.field}`} variant="row" onClick={() => select({ kind: "relation", relation, inbound })}>
              <Link2 /><span className="min-w-0 flex-1 text-left"><span className="block text-xs font-medium">{inbound ? relation.inverse ?? relation.title : relation.title}</span><span className="block truncate font-mono text-[10px] text-muted">{relation.ref.object.name}.{relation.ref.field} → {relation.target.name}</span></span><Tag label={t(inbound ? "Incoming reference" : "Outgoing reference")} />
            </Button>;
          })}{!related.length && <p className="text-xs text-muted">{t("No visible reference relationships.")}</p>}</div>}
          {tab === "actions" && <div className="grid gap-2">{actions.map((definition) => <Button key={definition.ref.name} variant="row" onClick={() => select({ kind: "action", definition })}>{definition.action!.title}<span className="ml-auto font-mono text-[10px] text-muted">{definition.ref.name}</span></Button>)}
            {!actions.length && <p className="text-xs text-muted">{t("No action of this object is offered to you.")}</p>}{resource.draft && <div className="flex flex-wrap gap-2">{(resource.draft.actions ?? []).map((a) => <Button key={a.name} size="sm" onClick={() => host.can("build.object.edit") && open({ view: "action-type", params: { id: resource.draft!.id, action: a.name } })}>{a.title || a.name}</Button>)}<Button size="sm" variant="primary" onClick={() => edit(resource, { tab: "lifecycle" })}>{t("Edit the lifecycle")}</Button></div>}</div>}
          {tab === "data" && (resource.installed ? <RecordList source={host.source} type={resource.ref.name} fields={resource.fields.filter((field) => field.type !== "lines" && !("aside" in field && field.aside)).slice(0, 5).map((field) => field.name)} height={420} /> : <Panel>{t("Publish this object before reading its business records.")}</Panel>)}
          {tab === "usage" && <div className="grid gap-2"><p className="text-xs text-muted">{t("Usage follows declared dependencies visible to the current member.")}</p>{used.map(({ owner }) => <Button key={`${owner.ref.app}/${owner.ref.kind}/${owner.ref.name}`} variant="row" onClick={() => owner.page ? open({ view: "page", params: owner.ref }) : open({ view: "definition", params: owner.ref })}>
            {owner.page?.title ?? owner.action?.title ?? owner.query?.title ?? owner.ref.name}<Tag label={owner.ref.kind} /></Button>)}{!used.length && <p className="text-xs text-muted">{t("No visible declared usage.")}</p>}</div>}
          {tab === "access" && <><p className="text-xs text-muted">{t("Object, field and action access is enforced by the original owner.")}</p><PropertyList items={resource.fields.filter((field) => field.read?.length || field.write?.length).map((field) => [field.title, `${t("Read")}: ${field.read?.join(", ") ?? "—"} · ${t("Write")}: ${field.write?.join(", ") ?? "—"}`])} />
            {resource.draft && <><PropertyList items={(resource.draft.access ?? []).map((access) => [access.role, access.read])} /><Button onClick={() => edit(resource, { access: "true" })}>{t("Edit access")}</Button></>}</>}
        </div>}
        {seed && <PageFromModel key={`${seed.object.name}/${seed.field ?? seed.relation?.ref.field ?? ""}`} seed={seed} onClose={() => setSeed(undefined)} />}
      </div>
    </EditorWorkbench>
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
  const create = async () => {
    if (!valid || busy) return; setBusy(true); setError("");
    try {
      const sections: Api.Section[] = relation && related ? [
        { widget: "table", title: entity!.plural, fields, selection: "parent" },
        { widget: "table", title: related.plural, object: relation.ref.object, fields: related.fields.filter((field) => !field.aside && field.type !== "lines").slice(0, 4).map((field) => field.name), ...(relation.ref.kind==="link-type"?{collectionVariable:"relatedWindow"}:{relation:relation.inverse,parentSelection:"parent"}), selection:"related" },
        { widget: "detail", title: t("Detail"), object: relation.ref.object, fields: related.fields.filter((field) => !field.aside && field.type !== "lines").slice(0, 4).map((field) => field.name), selection: "related" },
      ] : [{ widget: "table", title: entity!.plural, fields }, { widget: "detail", title: t("Detail"), fields }];
      const lifted = pageDocumentFromSections(sections);
      if(relation?.ref.kind==="link-type") {
        lifted.document.variables={...lifted.document.variables,parentRecord:{scope:"page",type:"record",mode:"resource",source:{kind:"record",section:lifted.sections[0]!.id}},relatedWindow:{scope:"page",type:"object-set",mode:"resource",source:{kind:"plan",query:"related"}}};
        lifted.document.queries={related:{title:relation.linkType?.title,object:relation.ref.object,query:relation.ref.binding,direction:"forward",for:{variable:"parentRecord"},sort:["id"],limit:50}};
      }
      const id = newId("PAGE");
      const payload = { name, title: title.trim(), object: seed.object.name, document: lifted.document,
        sections: lifted.sections.map((section) => ({ ...section, object: section.object?.name })),
        selections: relation ? [{ name: "parent", object: seed.object }, { name: "related", object: relation.ref.object }] : [] };
      if (await decide("build.page.create", { type: "build.page", id }, payload, { expectedRevision: 0, onRefused: setError })) open({ view: "module", params: { page: id } });
    } catch { setError(t("The page draft could not be created. Your choices are still here.")); }
    finally { setBusy(false); }
  };
  return <Panel role="region" aria-label={t("Create page from model")} title={t("Create page from model")} className="mt-4">
    <Form className="grid gap-3" onSubmit={() => void create()}>
      <p className="text-xs text-muted">{t("This creates a controlled page draft. Review its bindings before releasing it.")}</p>
      <fieldset disabled={busy} className="grid gap-3">
        <label className="grid gap-1 text-xs">{t("Page name")}<Input value={name} onChange={(event) => setName(event.target.value)} required /></label>
        <label className="grid gap-1 text-xs">{t("Page title")}<Input value={title} onChange={(event) => setTitle(event.target.value)} required /></label>
        <fieldset className="grid gap-1"><legend className="mb-2 text-xs">{t("Visible fields")}</legend>{entity?.fields.filter((field) => !field.aside && field.type !== "lines").map((field) => <Checkbox key={field.name} checked={fields.includes(field.name)} onChange={(on) => setFields(on ? [...fields, field.name] : fields.filter((name) => name !== field.name))}>{field.title}</Checkbox>)}</fieldset>
      </fieldset>
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      <div className="flex gap-2"><Button type="submit" variant="primary" disabled={!valid || busy}>{busy ? t("Creating draft…") : t("Create page draft")}</Button><Button disabled={busy} onClick={onClose}>{t("Cancel")}</Button></div>
    </Form>
  </Panel>;
}
