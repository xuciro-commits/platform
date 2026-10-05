import {useEffect,useRef,useState} from "react";
import type {Api} from "@platform/kernel";
import {useHost,useReadQuery,semanticPropertyTypes} from "@platform/app";
import {Button,Input,PageHeader,Panel,RecordList,Select,Textarea,t,useWorkspace,useUnsavedChanges} from "@platform/ui";
import {AssetControls} from "./asset-controls";
import { useDirectInstall } from "./release-profile";

type Draft = Api.PropertyType & {id:string;revision:number;published?:string;version?:number};
const empty = ():Draft => ({id:"",revision:0,name:"",title:"",description:"",type:"text"});
const hydrate = (record:Draft):Draft => ({...empty(),...record});
const types = ["text","longtext","integer","decimal","date","datetime","boolean"];

export function PropertyTypes() {
 const {source,role}=useHost(),{open}=useWorkspace();
 if(role("build")!=="builder")return <PageHeader title={t("Shared properties")} description={t("Only a builder can edit shared properties.")}/>;
 return <div className="grid gap-3"><PageHeader title={t("Shared properties")} description={t("Publish shared scalar meaning and bind object fields to an exact version.")} actions={<Button onClick={()=>open({view:"property-type",params:{id:"new"}})}>{t("New shared property")}</Button>}/><RecordList source={source} type="build.propertytype" fields={["title","name","type","version"]} onOpen={record=>open({view:"property-type",params:{id:record.id}})}/></div>;
}

export function PropertyTypeEditor({id}:{id:string}) {
 const {decide,role,definitions}=useHost(),{open,close}=useWorkspace();
 const query=useReadQuery<{record?:Draft}>(`/v1/records/build.propertytype/${encodeURIComponent(id)}`);
 const [draft,setDraft]=useState<Draft>(empty),[dirty,setDirty]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const loaded=useRef(""),baseRevision=useRef(0),lock=useRef(false);
 const load=(record:Draft)=>{setDraft(hydrate(record));baseRevision.current=record.revision;loaded.current=`${record.id}:${record.revision}`;};
 const {markSaved,discardChanges,confirmDiscard}=useUnsavedChanges(dirty,()=>{if(query.data?.record)load(query.data.record);else setDraft(empty());setDirty(false);setError("");});
 useEffect(()=>{const record=query.data?.record;if(record&&!dirty&&!busy&&loaded.current!==`${record.id}:${record.revision}`)load(record);},[query.data,dirty,busy]);
 const patch=(change:Partial<Draft>)=>{if(lock.current)return;setDraft(d=>({...d,...change}));setDirty(true);setError("");};
 const perform=async(action:()=>Promise<unknown>)=>{if(lock.current)return;lock.current=true;setBusy(true);setError("");try{await action();}catch{setError(t("The shared property could not be saved or loaded. Your draft is still here."));}finally{lock.current=false;setBusy(false);}};
 const reload=async()=>{const result=await query.refetch();if(result.isSuccess&&result.data?.record){load(result.data.record);markSaved();setDirty(false);}else setError(t("Reload the saved shared property before editing again."));};
 const save=async():Promise<{id:string;revision:number}|undefined>=>{
  const target=draft.id||crypto.randomUUID(),expected=baseRevision.current,{name,title,description,type}=draft;
  if(!await decide(`build.propertytype.${draft.id?"edit":"create"}`,{type:"build.propertytype",id:target},{name,title,description,type},{expectedRevision:draft.id?expected:0,quiet:true,onRefused:setError}))return;
  baseRevision.current=expected+1;loaded.current=`${target}:${expected+1}`;
  setDraft(d=>({...d,id:target,revision:expected+1}));markSaved();setDirty(false);
  if(!draft.id){open({view:"property-type",params:{id:target}});close({view:"property-type",params:{id}});}
  else {const result=await query.refetch();if(result.isSuccess&&result.data?.record?.revision===expected+1)load(result.data.record);}
  return {id:target,revision:expected+1};
 };
 const directInstall = useDirectInstall();
 const publish=async()=>{const saved=dirty?await save():{id:draft.id,revision:baseRevision.current};if(!saved)return;if(await decide("build.propertytype.publish",{type:"build.propertytype",id:saved.id},{},{expectedRevision:saved.revision,quiet:true,onRefused:setError}))await reload();};
 const review=async()=>{const saved=dirty?await save():{id:draft.id,revision:baseRevision.current};if(saved)open({view:"release-review",params:{kind:"property-type",id:saved.id}});};
 const versions=semanticPropertyTypes(definitions).filter(p=>p.binding.ref.app==="build"&&p.binding.ref.name===draft.name);
 if(role("build")!=="builder")return <PageHeader title={t("Shared properties")} description={t("Only a builder can edit shared properties.")}/>;
 if(id!=="new"&&!draft.id)return <PageHeader title={t("Shared properties")} description={query.isError?t("The shared property could not be loaded."):t("Loading…")}/>;
 return <div className="grid min-w-0 grid-cols-1 gap-3"><PageHeader title={draft.title||t("New shared property")} description={t("Publish shared scalar meaning and bind object fields to an exact version.")} actions={<div className="flex min-w-0 flex-wrap gap-2"><Button onClick={()=>open({view:"property-type"})}>{t("Shared properties")}</Button><AssetControls type="build.propertytype" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{view:"property-type",params:{id}}}/><Button disabled={busy||!draft.id} onClick={()=>confirmDiscard(()=>void perform(reload))}>{t("Reload saved shared property")}</Button><Button disabled={busy||!dirty&&!!draft.id} onClick={()=>void perform(save)}>{t("Save shared property")}</Button>{directInstall&&<Button disabled={busy||!draft.id} onClick={()=>void perform(publish)}>{t("Direct install")}</Button>}<Button variant="primary" disabled={busy||!draft.id} onClick={()=>void perform(review)}>{t("Review release")}</Button></div>}/>
 <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
 {!directInstall && <p className="text-xs text-muted">{t("This tenant delivers through a saved release candidate: review the draft and activate it.")}</p>}
 {error&&<Panel role="alert" className="text-danger">{error}</Panel>}
 {dirty&&<Panel role="status">{t("Unsaved changes. Direct install and release review save first.")}</Panel>}
 <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
 <Panel title={t("Shared property settings")} className="grid min-w-0 grid-cols-1 content-start gap-3">
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property name")}<Input disabled={!!draft.published} value={draft.name} onChange={e=>patch({name:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property title")}<Input value={draft.title} onChange={e=>patch({title:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property description")}<Textarea value={draft.description} onChange={e=>patch({description:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property type")}<Select disabled={!!draft.published} value={draft.type} onChange={e=>patch({type:e.target.value})}>{types.map(type=><option key={type} value={type}>{t(type)}</option>)}</Select></label>
 </Panel>
 <Panel role="region" aria-label={t("Published shared property versions")} title={t("Published shared property versions")} className="grid min-w-0 grid-cols-1 content-start gap-2"><p className="text-xs">{t("New publications do not replace object fields already bound to an older version.")}</p><p className="text-xs text-muted">{t("Local field names, required values and access belong to each object.")}</p>{versions.length?versions.map(p=><p key={p.binding.sourceVersion} className="break-all text-xs">{p.property.title} · {t(p.property.type)} · {p.binding.sourceVersion}</p>):<p className="text-xs text-muted">{t("No published shared property yet.")}</p>}</Panel>
 </fieldset></div>;
}
