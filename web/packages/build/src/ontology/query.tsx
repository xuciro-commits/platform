import { StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useState } from "react";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery, useOpenRecord, SemanticObjectSelect, SemanticPropertySelect } from "@platform/app";
import { Button, Input, PageHeader, Panel, Select, Textarea, RecordLookup, InterfaceRecordLookup, t, type InterfaceRecordIdentity, type InterfaceRecordPageData, type RecordPageData, type RecordSource } from "@platform/ui";
import { ObjectSetEditor } from "../editor/ObjectSetEditor";
import { DraftStatus, ResourceControls as AssetControls } from "../editor/workbench";

type Term = [string,string,string|number|boolean];
type Draft = Omit<Api.NamedQuery,"domain"> & { id:string; revision:number; domain:Term[]; published?:string; version?:number };
const empty = ():Draft => ({id:"",revision:0,name:"",title:"",description:"",object:"",interface:"",by:"",domain:[],sort:["id"],limit:50});
const hydrate = (record:Draft):Draft => ({...empty(),...record,domain:record.domain??[],sort:record.sort??["id"]});
const supported = ["text","choice","reference","boolean","integer","decimal","date","datetime"];

export function Queries() {
 const {source, holds}=useHost(),{open}=useApplicationWorkspace();
 if(!holds("build", "builder"))return <PageHeader title={t("Queries")} description={t("Only a builder can edit queries.")}/>;
 return <div className="grid gap-3"><PageHeader title={t("Queries")} help={t("Publish reusable record reads and bind pages to an exact version.")} actions={<Button variant="primary" onClick={()=>open({view:"query",params:{id:"new"}})}>{t("New query")}</Button>}/><ResourceList source={source} type="build.query" fields={["title","name","object","interface","version"]} onOpen={(record)=>open({view:"query",params:{id:record.id}})}/></div>;
}
export function QueryEditor({id}:{id:string}) {
 const {definitions, holds}=useHost(),{open}=useApplicationWorkspace();
 const apps=useReadQuery<Api.AppInfo[]>("/v1/apps").data ?? [];
 const interfaces=apps.flatMap(app=>app.interfaces??[]);
 const editor=useDefinitionDraft<Draft>({
  id,type:"build.query",view:"query",empty:empty,normalize:hydrate,joinApplication:false,
  failure:t("The query could not be saved or loaded. Your draft is still here."),
  payload:draft=>{const {name,title,description,object,interface:interfaceName,by,domain,sort,limit}=draft;return {name,title,description,object:object??"",interface:interfaceName??"",by:by??"",domain,sort,limit};},
 });
 const {draft,query,dirty,busy,error,perform,save,discardChanges,confirmDiscard}=editor;
 const object={app:"build",kind:"object" as const,name:draft.object??""},info=definitions.find((d)=>d.ref.kind==="object"&&d.ref.name===draft.object)?.entity;
 const currentShape=interfaces.find(item=>item.name===draft.interface)??draft.interfaceShape,fields=draft.interface?currentShape?.fields:info?.fields;
 const patch=editor.patch;
 const reload=()=>editor.reload(t("Reload the saved query before editing again."));
 const installed=definitions.find((d)=>d.ref.kind==="query"&&d.ref.app==="build"&&d.ref.name===draft.name);
 const versions=Object.keys(installed?.queryVersions??{});
 if(!holds("build", "builder"))return <PageHeader title={t("Queries")} description={t("Only a builder can edit queries.")}/>;
 if(id!=="new"&&!draft.id)return <PageHeader title={t("Queries")} description={query.isError?t("The query could not be loaded."):t("Loading…")}/>;
 return <div className="grid min-w-0 grid-cols-1 gap-3"><PageHeader crumbs={[{label:t("Queries"),onClick:()=>open({view:"query"})}]} title={draft.title||t("New query")} status={<DraftStatus state={draft.published?"published":"draft"}/>} meta={draft.name?<span className="font-mono">{draft.name}{draft.version?` · v${draft.version}`:""}</span>:undefined} help={t("Publish reusable record reads and bind pages to an exact version.")} actions={<AssetControls type="build.query" record={draft} dirty={dirty} busy={busy} empty={!draft.id} onCancel={discardChanges} onSave={()=>perform(save)} onReload={()=>confirmDiscard(()=>void perform(reload))} route={{view:"query",params:{id}}}/>}/>
 {error&&<Panel role="alert" className="text-danger">{error}</Panel>}
 <StructuredEditor {...editor.structured}><fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
 <Panel title={t("Query settings")} className="grid min-w-0 content-start gap-3">
 <label className="grid min-w-0 gap-1 text-xs">{t("Query name")}<Input disabled={!!draft.published} value={draft.name} onChange={(e)=>patch({name:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query title")}<Input value={draft.title} onChange={(e)=>patch({title:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query description")}<Textarea value={draft.description} onChange={(e)=>patch({description:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query source kind")}<Select value={draft.interface?"interface":"object"} disabled={!!draft.published} onChange={event=>patch({object:"",interface:event.target.value==="interface"?interfaces[0]?.name??"":"",domain:[],sort:["id"],by:""})}><option value="object">{t("Object type")}</option><option value="interface" disabled={!interfaces.length}>{t("Interface")}</option></Select></label>
 {draft.interface?<><label className="grid min-w-0 gap-1 text-xs">{t("Source interface")}<Select value={draft.interface} disabled={!!draft.published} onChange={event=>patch({interface:event.target.value,domain:[],sort:["id"],by:""})}>{interfaces.map(item=><option key={item.name} value={item.name}>{item.title} · {item.name}</option>)}</Select></label><p className="text-xs text-muted">{t("Publication freezes the interface fields and implementation types. Publish a new version to include later implementations.")}</p></>:<><label className="grid min-w-0 gap-1 text-xs">{t("Source object")}<SemanticObjectSelect value={draft.object??""} label={t("Source object")} filter={(d)=>d.ref.app==="build"} disabled={!!draft.published} onChange={(ref)=>{if(ref)patch({object:ref.name,domain:[],sort:["id"],by:""})}}/></label><label className="grid min-w-0 gap-1 text-xs">{t("Parent reference")}<SemanticPropertySelect object={object} value={draft.by??""} label={t("Parent reference")} filter={(f)=>f.type==="reference"||f.sourceType==="record"} onChange={(ref)=>patch({by:ref?.field??""})}/></label></>}
 <label className="grid min-w-0 gap-1 text-xs">{t("Query window limit")}<Input draftKey="query-window-limit" type="number" min={1} max={200} value={draft.limit??50} onChange={(e)=>patch({limit:Number(e.target.value)})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query sort")}<Select value={draft.sort?.[0]??"id"} onChange={(e)=>patch({sort:[e.target.value]})}><option value="id">{t("ID")}</option>{fields?.filter((f)=>supported.includes(f.type)).flatMap((f)=>[f.name,`-${f.name}`].map((name)=><option key={name} value={name}>{f.title} {name.startsWith("-")?"↓":"↑"}</option>))}</Select></label>
 </Panel>
 <Panel title={t("Fixed query conditions")} className="grid min-w-0 content-start gap-3">
 <ObjectSetEditor locked search={false} combine={false} around={false} filter={false} draftKey="query-conditions" interfaces={draft.interfaceShape?[draft.interfaceShape]:undefined} value={draft.interface?{interface:draft.interface,domain:draft.domain??[]}:{object:draft.object??"",domain:draft.domain??[]}} onChange={next=>patch({domain:(next.domain as Term[]|undefined)??[]})}/>
 <p className="text-xs text-muted">{t("Conditions run with the reader's object and field permissions.")}</p>
 </Panel>
 </fieldset></StructuredEditor>
 <Panel role="region" aria-label={t("Published query versions")} title={t("Published query versions")} className="grid gap-2"><p className="text-xs">{t("New drafts and publications do not replace versions already bound by pages.")}</p>{versions.length?versions.map((version)=><p key={version} className="break-all text-xs">{version}</p>):<p className="text-xs text-muted">{t("No published query yet.")}</p>}</Panel>
 {installed?.query && <PublishedQueryLookup key={installed.version} name={draft.name} version={installed.version} declaration={installed.query}/>}
 </div>;
}

function PublishedQueryLookup({name,version,declaration}:{name:string;version:string;declaration:Api.NamedQuery}) {
 const {source,client}=useHost(),openRecord=useOpenRecord();
 const [selected,setSelected]=useState<InterfaceRecordIdentity>();
 const path=(search:string,offset:number,limit?:number)=>`/v1/queries/build/${encodeURIComponent(name)}?${new URLSearchParams({version,search,offset:String(offset),limit:String(limit??25)})}`;
 const scoped:RecordSource={...source,scope:JSON.stringify([source.scope,name,version]),
  interfaceList:(_name,q)=>client.get<InterfaceRecordPageData>(path(q.search??"",q.offset??0,q.limit)),
  watchInterfaceList:(_name,q,changed)=>client.subscribeRead(path(q.search??"",q.offset??0,q.limit),changed),
  list:(_type,q)=>client.get<RecordPageData>(path(q.search??"",q.offset??0,q.limit)),
  watchList:(_type,q,changed)=>client.subscribeRead(path(q.search??"",q.offset??0,q.limit),changed),
 };
 return <Panel title={t("Try published query")} className="grid gap-3">
  <p className="text-xs text-muted">{t("This reads the published version with your current permissions. Unsaved conditions do not change it.")}</p>
  {declaration.interface?<InterfaceRecordLookup source={scoped} name={declaration.interface} value={selected} onChange={setSelected} ariaLabel={t("Query result record")}/>:<RecordLookup source={scoped} type={declaration.object??""} value={selected?.id} onChange={id=>setSelected(id?{type:declaration.object!,id}:undefined)} ariaLabel={t("Query result record")}/>}
  {declaration.interface && <p className="text-xs text-muted">{t("Published implementation types")}: {declaration.implementations?.map(type=>source.entity(type)?.title??type).join(", ") || "—"}</p>}
  <Button disabled={!selected} onClick={()=>selected&&openRecord(selected)}>{t("Open selected record")}</Button>
 </Panel>;
}
