import { useEffect, useState } from "react";
import { useHost, useReadQuery, SemanticObjectSelect, SemanticPropertySelect } from "@platform/app";
import { Button, Input, Select, PageHeader, Panel, RecordList, Textarea, t, useWorkspace, useUnsavedChanges } from "@platform/ui";
import { AssetControls } from "./asset-controls";
import { useDirectInstall } from "./release-profile";

type Draft = {id:string;revision:number;name:string;title:string;description:string;parent:string;child:string;via:string;forward:string;reverse:string;cardinality:string;deletePolicy:string;published?:string;version?:number};
const empty=():Draft=>({id:"",revision:0,name:"",title:"",description:"",parent:"",child:"",via:"",forward:"children",reverse:"parent",cardinality:"one-to-many",deletePolicy:"owner"});
const hydrate=(record:Draft):Draft=>({...empty(),...record});

export function LinkTypes() {
 const {source,role}=useHost(),{open}=useWorkspace();
 if(role("build")!=="builder")return <PageHeader title={t("Relationships")} description={t("Only a builder can edit relationships.")}/>;
 return <div className="grid gap-3"><PageHeader title={t("Relationships")} description={t("Define a reference-backed relationship and bind pages to an exact version.")} actions={<Button onClick={()=>open({view:"link-type",params:{id:"new"}})}>{t("New relationship")}</Button>}/><RecordList source={source} type="build.linktype" fields={["title","name","parent","child","version"]} onOpen={(record)=>open({view:"link-type",params:{id:record.id}})}/></div>;
}
export function LinkTypeEditor({id,parent,child,via}:{id:string;parent?:string;child?:string;via?:string}) {
 const {decide,role,definitions}=useHost(),{open,close}=useWorkspace();
 const query=useReadQuery<{record?:Draft}>(`/v1/records/build.linktype/${encodeURIComponent(id)}`);
 const [draft,setDraft]=useState<Draft>(()=>({...empty(),parent:parent??"",child:child??"",via:via??""})),[dirty,setDirty]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const {markSaved,discardChanges,confirmDiscard}=useUnsavedChanges(dirty,()=>{setDraft(query.data?.record?hydrate(query.data.record):empty());setDirty(false);setError("");});
 useEffect(()=>{if(query.data?.record&&!dirty)setDraft(hydrate(query.data.record));},[query.data,dirty]);
 const object={app:"build",kind:"object" as const,name:draft.child};
 const patch=(change:Partial<Draft>)=>{setDraft((d)=>({...d,...change}));setDirty(true);setError("");};
 const perform=async(action:()=>Promise<unknown>)=>{setBusy(true);setError("");try{await action();}catch{setError(t("The relationship could not be saved or loaded. Your draft is still here."));}finally{setBusy(false);}};
 const reload=async()=>{const r=await query.refetch();if(r.data?.record&&!r.isError){setDraft(hydrate(r.data.record));markSaved();setDirty(false);}else setError(t("Reload the saved relationship before editing again."));};
 const save=async():Promise<number|undefined>=>{
  const target=draft.id||crypto.randomUUID(),{name,title,description,parent,child,via,forward,reverse,cardinality,deletePolicy}=draft;
  if(!await decide(`build.linktype.${draft.id?"edit":"create"}`,{type:"build.linktype",id:target},{name,title,description,parent,child,via,forward,reverse,cardinality,deletePolicy},{expectedRevision:draft.id?draft.revision:undefined,quiet:true,onRefused:setError}))return;
  const revision=draft.id?draft.revision+1:1;
  if(!draft.id){markSaved();setDirty(false);open({view:"link-type",params:{id:target}});close({view:"link-type",params:{id}});}else await reload();
  return revision;
 };
 const directInstall = useDirectInstall();
 const publish=async()=>{const revision=dirty?await save():draft.revision;if(revision===undefined)return;if(await decide("build.linktype.publish",{type:"build.linktype",id:draft.id},{},{expectedRevision:revision,quiet:true,onRefused:setError}))await reload();};
 const review=async()=>{if(dirty&&await save()===undefined)return;open({view:"release-review",params:{kind:"link-type",id:draft.id}});};
 const installed=definitions.find((d)=>d.ref.kind==="link-type"&&d.ref.app==="build"&&d.ref.name===draft.name);
 const versions=Object.keys(installed?.linkVersions??{});
 if(role("build")!=="builder")return <PageHeader title={t("Relationships")} description={t("Only a builder can edit relationships.")}/>;
 if(id!=="new"&&!draft.id)return <PageHeader title={t("Relationships")} description={query.isError?t("The relationship could not be loaded."):t("Loading…")}/>;
 return <div className="grid min-w-0 grid-cols-1 gap-3"><PageHeader title={draft.title||t("New relationship")} description={t("Define a reference-backed relationship and bind pages to an exact version.")} actions={<div className="flex min-w-0 flex-wrap gap-2"><Button onClick={()=>open({view:"link-type"})}>{t("Relationships")}</Button><AssetControls type="build.linktype" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{view:"link-type",params:{id}}}/><Button disabled={busy||!draft.id} onClick={()=>confirmDiscard(()=>void perform(reload))}>{t("Reload saved relationship")}</Button><Button disabled={busy||!dirty&&!!draft.id} onClick={()=>void perform(save)}>{t("Save relationship")}</Button>{directInstall&&<Button disabled={busy||!draft.id} onClick={()=>void perform(publish)}>{t("Direct install")}</Button>}<Button variant="primary" disabled={busy||!draft.id} onClick={()=>void perform(review)}>{t("Review release")}</Button></div>}/>
 <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
 {!directInstall && <p className="text-xs text-muted">{t("This tenant delivers through a saved release candidate: review the draft and activate it.")}</p>}
 {error&&<Panel role="alert" className="text-danger">{error}</Panel>}
 {dirty&&<Panel role="status">{t("Unsaved changes. Direct install and release review save first.")}</Panel>}
 <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
 <Panel title={t("Relationship settings")} className="grid min-w-0 grid-cols-1 content-start gap-3">
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Relationship name")}<Input disabled={!!draft.published} value={draft.name} onChange={e=>patch({name:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Relationship title")}<Input value={draft.title} onChange={e=>patch({title:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Relationship description")}<Textarea value={draft.description} onChange={e=>patch({description:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Parent object")}<SemanticObjectSelect value={draft.parent} label={t("Parent object")} filter={d=>d.source==="tenant"&&d.ref.app==="build"} disabled={!!draft.published} onChange={ref=>{if(ref)patch({parent:ref.name,via:""})}}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Child object")}<SemanticObjectSelect value={draft.child} label={t("Child object")} filter={d=>d.source==="tenant"&&d.ref.app==="build"} disabled={!!draft.published} onChange={ref=>{if(ref)patch({child:ref.name,via:""})}}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Reference field")}<SemanticPropertySelect object={object} value={draft.via} label={t("Reference field")} disabled={!!draft.published} filter={f=>f.type==="reference"&&f.ref===draft.parent} onChange={ref=>{if(!draft.published)patch({via:ref?.field??""})}}/></label>
 </Panel>
 <Panel title={t("Relationship directions")} className="grid min-w-0 grid-cols-1 content-start gap-3">
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Parent to children name")}<Input value={draft.forward} onChange={e=>patch({forward:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Child to parent name")}<Input value={draft.reverse} onChange={e=>patch({reverse:e.target.value})}/></label>
 <label className="grid gap-1 text-xs">{t("Relationship cardinality")}<Select aria-label={t("Relationship cardinality")} value={draft.cardinality||"one-to-many"} onChange={e=>patch({cardinality:e.target.value})}><option value="one-to-many">{t("One-to-many")}</option><option value="one-to-one">{t("One-to-one (at most one child)")}</option></Select></label>
 {draft.cardinality==="one-to-one"&&<p className="text-xs text-muted">{t("A parent reference may belong to at most one child, including archived children. Empty optional references are not reserved. A published unique relationship cannot be relaxed.")}</p>}
 <label className="grid gap-1 text-xs">{t("Relationship archive policy")}<Select aria-label={t("Relationship archive policy")} value={draft.deletePolicy||"owner"} onChange={e=>patch({deletePolicy:e.target.value})}><option value="owner">{t("Object owner")}</option><option value="restrict-active">{t("Protect active references")}</option></Select></label>
 {draft.deletePolicy==="restrict-active"&&<p className="text-xs text-muted">{t("Active children prevent parent archiving. Archived children keep their references. New or restored active children need an unarchived parent. Published protection cannot be relaxed.")}</p>}
 <p className="text-sm">{t("One child has at most one parent reference. The original field controls required values; the object owner controls archive behavior.")}</p>
 <p className="text-xs text-muted">{t("Instances stay in the original reference field. This profile does not enable cascade deletion.")}</p>
 </Panel></fieldset>
 <Panel role="region" aria-label={t("Published relationship versions")} title={t("Published relationship versions")} className="grid gap-2"><p className="text-xs">{t("New drafts and publications do not replace versions already bound by pages.")}</p>{versions.length?versions.map((version)=><p key={version} className="break-all text-xs">{version}</p>):<p className="text-xs text-muted">{t("No published relationship yet.")}</p>}</Panel>
 </div>;
}
