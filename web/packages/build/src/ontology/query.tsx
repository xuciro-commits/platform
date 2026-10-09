import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useEffect, useState } from "react";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery, useOpenRecord, SemanticObjectSelect, SemanticPropertySelect } from "@platform/app";
import { Button, Card, Checkbox, Input, PageHeader, Panel, Select, Textarea, RecordLookup, InterfaceRecordLookup, t, useUnsavedChanges, type InterfaceRecordIdentity, type InterfaceRecordPageData, type RecordPageData, type RecordSource } from "@platform/ui";
import { ResourceControls as AssetControls } from "../editor/workbench";
import { useDirectInstall } from "../releases/release-profile";

type Term = [string,string,string|number|boolean];
type Draft = Omit<Api.NamedQuery,"domain"> & { id:string; revision:number; domain:Term[]; published?:string; version?:number };
const empty = ():Draft => ({id:"",revision:0,name:"",title:"",description:"",object:"",interface:"",by:"",domain:[],sort:["id"],limit:50});
const hydrate = (record:Draft):Draft => ({...empty(),...record,domain:record.domain??[],sort:record.sort??["id"]});
const supported = ["text","choice","reference","boolean","integer","decimal","date","datetime"];

export function Queries() {
 const {source,role}=useHost(),{open}=useApplicationWorkspace();
 if(role("build")!=="builder")return <PageHeader title={t("Queries")} description={t("Only a builder can edit queries.")}/>;
 return <div className="grid gap-3"><PageHeader title={t("Queries")} description={t("Publish reusable record reads and bind pages to an exact version.")} actions={<Button onClick={()=>open({view:"query",params:{id:"new"}})}>{t("New query")}</Button>}/><ResourceList source={source} type="build.query" fields={["title","name","object","interface","version"]} onOpen={(record)=>open({view:"query",params:{id:record.id}})}/></div>;
}
export function QueryEditor({id}:{id:string}) {
 const {decide,role,definitions}=useHost(),{open,close}=useApplicationWorkspace();
 const query=useReadQuery<{record?:Draft}>(`/v1/records/build.query/${encodeURIComponent(id)}`);
 const apps=useReadQuery<Api.AppInfo[]>("/v1/apps").data ?? [];
 const interfaces=apps.flatMap(app=>app.interfaces??[]);
 const [draft,setDraft]=useState<Draft>(empty),[dirty,setDirty]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const {markSaved,discardChanges,confirmDiscard}=useUnsavedChanges(dirty,()=>{setDraft(query.data?.record?hydrate(query.data.record):empty());setDirty(false);setError("");});
 useEffect(()=>{if(query.data?.record&&!dirty)setDraft(hydrate(query.data.record));},[query.data,dirty]);
 const object={app:"build",kind:"object" as const,name:draft.object??""},info=definitions.find((d)=>d.ref.kind==="object"&&d.ref.name===draft.object)?.entity;
 const currentShape=interfaces.find(item=>item.name===draft.interface)??draft.interfaceShape,fields=draft.interface?currentShape?.fields:info?.fields;
 const patch=(change:Partial<Draft>)=>{setDraft((d)=>({...d,...change}));setDirty(true);setError("");};
 const term=(index:number,value:Term)=>patch({domain:draft.domain.map((c,i)=>i===index?value:c)});
 const perform=async(action:()=>Promise<unknown>)=>{setBusy(true);setError("");try{await action();}catch{setError(t("The query could not be saved or loaded. Your draft is still here."));}finally{setBusy(false);}};
 const reload=async()=>{const r=await query.refetch();if(r.data?.record&&!r.isError){setDraft(hydrate(r.data.record));markSaved();setDirty(false);}else setError(t("Reload the saved query before editing again."));};
 const save=async():Promise<number|undefined>=>{
  const target=draft.id||crypto.randomUUID(),{name,title,description,object,interface:interfaceName,by,domain,sort,limit}=draft;
  if(!await decide(`build.query.${draft.id?"edit":"create"}`,{type:"build.query",id:target},{name,title,description,object:object??"",interface:interfaceName??"",by:by??"",domain,sort,limit},{expectedRevision:draft.id?draft.revision:undefined,quiet:true,onRefused:setError}))return;
  const revision=draft.id?draft.revision+1:1;
  if(!draft.id){markSaved();setDirty(false);open({view:"query",params:{id:target}});close({view:"query",params:{id}});}else await reload();
  return revision;
 };
 const directInstall = useDirectInstall();
 const publish=async()=>{const revision=dirty?await save():draft.revision;if(revision===undefined)return;if(await decide("build.query.publish",{type:"build.query",id:draft.id},{},{expectedRevision:revision,quiet:true,onRefused:setError}))await reload();};
 const review=async()=>{if(dirty&&await save()===undefined)return;open({view:"release-review",params:{kind:"query",id:draft.id}});};
 const installed=definitions.find((d)=>d.ref.kind==="query"&&d.ref.app==="build"&&d.ref.name===draft.name);
 const versions=Object.keys(installed?.queryVersions??{});
 if(role("build")!=="builder")return <PageHeader title={t("Queries")} description={t("Only a builder can edit queries.")}/>;
 if(id!=="new"&&!draft.id)return <PageHeader title={t("Queries")} description={query.isError?t("The query could not be loaded."):t("Loading…")}/>;
 return <div className="grid min-w-0 grid-cols-1 gap-3"><PageHeader title={draft.title||t("New query")} description={t("Publish reusable record reads and bind pages to an exact version.")} actions={<div className="flex flex-wrap gap-2"><Button onClick={()=>open({view:"query"})}>{t("Queries")}</Button><AssetControls type="build.query" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{view:"query",params:{id}}}/><Button disabled={busy||!draft.id} onClick={()=>confirmDiscard(()=>void perform(reload))}>{t("Reload saved query")}</Button><Button disabled={busy||!dirty&&!!draft.id} onClick={()=>void perform(save)}>{t("Save query")}</Button>{directInstall&&<Button disabled={busy||!draft.id} onClick={()=>void perform(publish)}>{t("Direct install")}</Button>}<Button variant="primary" disabled={busy||!draft.id} onClick={()=>void perform(review)}>{t("Review release")}</Button></div>}/>
 <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
 {!directInstall && <p className="text-xs text-muted">{t("This tenant delivers through a saved release candidate: review the draft and activate it.")}</p>}
 {error&&<Panel role="alert" className="text-danger">{error}</Panel>}
 {dirty&&<Panel role="status">{t("Unsaved changes. Direct install and release review save first.")}</Panel>}
 <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
 <Panel title={t("Query settings")} className="grid min-w-0 content-start gap-3">
 <label className="grid min-w-0 gap-1 text-xs">{t("Query name")}<Input disabled={!!draft.published} value={draft.name} onChange={(e)=>patch({name:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query title")}<Input value={draft.title} onChange={(e)=>patch({title:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query description")}<Textarea value={draft.description} onChange={(e)=>patch({description:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query source kind")}<Select value={draft.interface?"interface":"object"} disabled={!!draft.published} onChange={event=>patch({object:"",interface:event.target.value==="interface"?interfaces[0]?.name??"":"",domain:[],sort:["id"],by:""})}><option value="object">{t("Object type")}</option><option value="interface" disabled={!interfaces.length}>{t("Interface")}</option></Select></label>
 {draft.interface?<><label className="grid min-w-0 gap-1 text-xs">{t("Source interface")}<Select value={draft.interface} disabled={!!draft.published} onChange={event=>patch({interface:event.target.value,domain:[],sort:["id"],by:""})}>{interfaces.map(item=><option key={item.name} value={item.name}>{item.title} · {item.name}</option>)}</Select></label><p className="text-xs text-muted">{t("Publication freezes the interface fields and implementation types. Publish a new version to include later implementations.")}</p></>:<><label className="grid min-w-0 gap-1 text-xs">{t("Source object")}<SemanticObjectSelect value={draft.object??""} label={t("Source object")} filter={(d)=>d.ref.app==="build"&&d.source==="tenant"} disabled={!!draft.published} onChange={(ref)=>{if(ref)patch({object:ref.name,domain:[],sort:["id"],by:""})}}/></label><label className="grid min-w-0 gap-1 text-xs">{t("Parent reference")}<SemanticPropertySelect object={object} value={draft.by??""} label={t("Parent reference")} filter={(f)=>f.type==="reference"} onChange={(ref)=>patch({by:ref?.field??""})}/></label></>}
 <label className="grid min-w-0 gap-1 text-xs">{t("Query window limit")}<Input type="number" min={1} max={200} value={draft.limit??50} onChange={(e)=>patch({limit:Number(e.target.value)})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Query sort")}<Select value={draft.sort?.[0]??"id"} onChange={(e)=>patch({sort:[e.target.value]})}><option value="id">{t("ID")}</option>{fields?.filter((f)=>supported.includes(f.type)).flatMap((f)=>[f.name,`-${f.name}`].map((name)=><option key={name} value={name}>{f.title} {name.startsWith("-")?"↓":"↑"}</option>))}</Select></label>
 </Panel>
 <Panel title={t("Fixed query conditions")} className="grid min-w-0 content-start gap-3">
 {draft.domain.map((c,i)=>{const field=fields?.find((f)=>f.name===c[0]);const selectField=(name:string)=>{const f=fields?.find(f=>f.name===name);term(i,[name,"=",f?.type==="boolean"?false:["integer","decimal"].includes(f?.type??"")?0:""]);};return <Card key={i} className="grid gap-2 p-3" role="group" aria-label={t("Condition {n}",{n:i+1})}>
 {draft.interface?<Select aria-label={t("Query condition field")} value={c[0]} onChange={event=>selectField(event.target.value)}><option value="">{t("Choose a field")}</option>{fields?.filter(f=>supported.includes(f.type)).map(f=><option key={f.name} value={f.name}>{f.title}</option>)}</Select>:<SemanticPropertySelect object={object} value={c[0]} label={t("Query condition field")} filter={(f)=>supported.includes(f.type)} onChange={ref=>{if(ref)selectField(ref.field);}}/>}
 <Select aria-label={t("Query condition operator")} value={c[1]} onChange={(e)=>term(i,[c[0],e.target.value,c[2]])}>{(field&&["boolean","reference"].includes(field.type)?["=","!="]:["=","!=","<","<=",">",">=",...(["text","choice"].includes(field?.type??"")?["like"]:[])]).map((op)=><option key={op}>{op}</option>)}</Select>
 {typeof c[2]==="boolean"?<Checkbox checked={c[2]} onChange={(checked)=>term(i,[c[0],c[1],checked])}>{t("Query literal value")}</Checkbox>:<Input aria-label={t("Query literal value")} type={typeof c[2]==="number"?"number":"text"} value={String(c[2])} onChange={(e)=>term(i,[c[0],c[1],typeof c[2]==="number"?Number(e.target.value):e.target.value])}/>}
 <Button onClick={()=>patch({domain:draft.domain.filter((_,at)=>at!==i)})}>{t("Remove query condition")}</Button>
 </Card>})}
 <Button disabled={!fields?.length||draft.domain.length>=16} onClick={()=>patch({domain:[...draft.domain,["","=",""]]})}>{t("Add query condition")}</Button>
 <p className="text-xs text-muted">{t("Conditions run with the reader's object and field permissions.")}</p>
 </Panel>
 </fieldset>
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
