import { StructuredEditor } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { useState } from "react";
import { useHost, SemanticObjectSelect, SemanticPropertySelect } from "@platform/app";
import { Button, Checkbox, Input, Select, PageHeader, Panel, Textarea, t } from "@platform/ui";
import { ResourceControls as AssetControls } from "../editor/workbench";
import { useDirectInstall } from "../releases/release-profile";
import { useAuthoringModel } from "./authoring-model";

type Draft = {id:string;revision:number;name:string;title:string;description:string;parent:string;child:string;via:string;forward:string;reverse:string;cardinality:string;deletePolicy:string;storage?:string;roles?:string[];required?:boolean;published?:string;version?:number};
const empty=():Draft=>({id:"",revision:0,name:"",title:"",description:"",parent:"",child:"",via:"",forward:"children",reverse:"parent",cardinality:"one-to-many",deletePolicy:"owner",storage:"edge",roles:["builder","user"]});
const hydrate=(record:Draft):Draft=>({...empty(),...record,storage:record.storage||"reference"});

export function LinkTypes() {
 const {source,role}=useHost(),{open}=useApplicationWorkspace();
 if(role("build")!=="builder")return <PageHeader title={t("Relationships")} description={t("Only a builder can edit relationships.")}/>;
 return <div className="grid gap-3"><PageHeader title={t("Relationships")} help={t("Declare relationships in their object owner and bind pages to an exact version.")} actions={<Button onClick={()=>open({view:"link-type",params:{id:"new"}})}>{t("New relationship")}</Button>}/><ResourceList source={source} type="build.linktype" fields={["title","name","parent","child","version"]} onOpen={(record)=>open({view:"link-type",params:{id:record.id}})}/></div>;
}
export function LinkTypeEditor({id,parent,child,via}:{id:string;parent?:string;child?:string;via?:string}) {
 const {decide,role,definitions,client}=useHost(),{open}=useApplicationWorkspace();
 const authoring=useAuthoringModel();
 const objects=(authoring.drafts.data?.records??[]).filter(draft=>!draft.archived).map(draft=>({name:`build.${draft.name}`,title:draft.title}));
 const [migration,setMigration]=useState<Api.RelationMigrationPlan>();
 const editor=useDefinitionDraft<Draft>({
  id,type:"build.linktype",kind:"link-type",view:"link-type",empty:() => ({...empty(),parent:parent??"",child:child??"",via:via??""}),normalize:hydrate,joinApplication:false,
  failure:t("The relationship could not be saved or loaded. Your draft is still here."),
  payload:draft=>{const {name,title,description,parent,child,via,forward,reverse,cardinality,deletePolicy,storage,roles,required}=draft;return {name,title,description,parent,child,via,forward,reverse,cardinality,deletePolicy,storage,roles,required};},
 });
 const {draft,query,dirty,busy,error,setError,inputs,perform,save,review,discardChanges,confirmDiscard}=editor;
 const object={app:"build",kind:"object" as const,name:draft.child};
 const patch=(change:Partial<Draft>)=>{editor.patch(change);setMigration(undefined);};
 const reload=()=>editor.reload(t("Reload the saved relationship before editing again."));
 const publish=()=>editor.transition("publish");
 const directInstall = useDirectInstall();
 const installed=definitions.find((d)=>d.ref.kind==="link-type"&&d.ref.app==="build"&&d.ref.name===draft.name);
 const versions=Object.keys(installed?.linkVersions??{});
 if(role("build")!=="builder")return <PageHeader title={t("Relationships")} description={t("Only a builder can edit relationships.")}/>;
 if(id!=="new"&&!draft.id)return <PageHeader title={t("Relationships")} description={query.isError?t("The relationship could not be loaded."):t("Loading…")}/>;
 return <div className="grid min-w-0 grid-cols-1 gap-3"><PageHeader title={draft.title||t("New relationship")} help={t("Declare relationships in their object owner and bind pages to an exact version.")} actions={<div className="flex min-w-0 flex-wrap gap-2"><Button onClick={()=>open({view:"link-type"})}>{t("Relationships")}</Button><AssetControls type="build.linktype" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{view:"link-type",params:{id}}}/><Button disabled={busy||!draft.id} onClick={()=>confirmDiscard(()=>void perform(reload))}>{t("Reload saved relationship")}</Button><Button disabled={busy||inputs?.invalid||!dirty&&!!draft.id} onClick={()=>void perform(save)}>{t("Save relationship")}</Button>{directInstall&&<Button disabled={busy||inputs?.invalid||!draft.id} onClick={()=>void perform(publish)}>{t("Direct install")}</Button>}<Button variant="primary" disabled={busy||inputs?.invalid||!draft.id} onClick={()=>void perform(review)}>{t("Review release")}</Button></div>}/>
 <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
 {!directInstall && <p className="text-xs text-muted">{t("This tenant delivers through a saved release candidate: review the draft and activate it.")}</p>}
 {error&&<Panel role="alert" className="text-danger">{error}</Panel>}
 {dirty&&<Panel role="status">{t("Unsaved changes. Direct install and release review save first.")}</Panel>}
 <StructuredEditor {...editor.structured}><fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
 <Panel title={t("Relationship settings")} className="grid min-w-0 grid-cols-1 content-start gap-3">
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Relationship name")}<Input disabled={!!draft.published} value={draft.name} onChange={e=>patch({name:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Relationship title")}<Input value={draft.title} onChange={e=>patch({title:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Relationship description")}<Textarea value={draft.description} onChange={e=>patch({description:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Parent object")}<SemanticObjectSelect value={draft.parent} label={t("Parent object")} drafts={objects} filter={d=>d.source==="tenant"&&d.ref.app==="build"} disabled={!!draft.published} onChange={ref=>{if(ref)patch({parent:ref.name,via:""})}}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Child object")}<SemanticObjectSelect value={draft.child} label={t("Child object")} drafts={objects} filter={d=>d.source==="tenant"&&d.ref.app==="build"} disabled={!!draft.published} onChange={ref=>{if(ref)patch({child:ref.name,via:""})}}/></label>
 <label className="grid gap-1 text-xs">{t("Relationship storage")}<Select value={draft.storage||"reference"} disabled={!!draft.published} onChange={event=>patch({storage:event.target.value,via:"",cardinality:"one-to-many"})}><option value="edge">{t("Owned relationship records")}</option><option value="reference">{t("Original reference field")}</option></Select></label>
 {draft.storage==="edge"&&<label className="flex items-center gap-2 text-xs"><Checkbox checked={!!draft.required} disabled={!!draft.published&&JSON.parse(draft.published).required===true} onChange={checked=>patch({required:checked})}/>{t("Parent relationship required")}</label>}
 {draft.storage==="edge"&&<label className="grid gap-1 text-xs">{t("Relationship writer roles")}<Input value={draft.roles?.join(", ")??""} onChange={event=>patch({roles:event.target.value.split(",").map(role=>role.trim()).filter(Boolean)})}/></label>}
 {draft.storage!=="edge"&&<label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Reference field")}<SemanticPropertySelect object={object} value={draft.via} fields={authoring.entity(object.name)?.fields} label={t("Reference field")} disabled={!!draft.published} filter={f=>f.type==="reference"&&f.ref===draft.parent} onChange={ref=>{if(!draft.published)patch({via:ref?.field??""})}}/></label>}
 </Panel>
 <Panel title={t("Relationship directions")} className="grid min-w-0 grid-cols-1 content-start gap-3">
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Parent to children name")}<Input value={draft.forward} onChange={e=>patch({forward:e.target.value})}/></label>
 <label className="grid min-w-0 grid-cols-1 gap-1 text-xs">{t("Child to parent name")}<Input value={draft.reverse} onChange={e=>patch({reverse:e.target.value})}/></label>
 <label className="grid gap-1 text-xs">{t("Relationship cardinality")}<Select aria-label={t("Relationship cardinality")} value={draft.cardinality||"one-to-many"} onChange={e=>patch({cardinality:e.target.value})}><option value="one-to-many">{t("One-to-many")}</option><option value="one-to-one">{t("One-to-one (at most one child)")}</option>{draft.storage==="edge"&&<option value="many-to-many">{t("Many-to-many")}</option>}</Select></label>
 {draft.cardinality==="one-to-one"&&<p className="text-xs text-muted">{t("A parent reference may belong to at most one child, including archived children. Empty optional references are not reserved. A published unique relationship cannot be relaxed.")}</p>}
 <label className="grid gap-1 text-xs">{t("Relationship archive policy")}<Select aria-label={t("Relationship archive policy")} value={draft.deletePolicy||"owner"} onChange={e=>patch({deletePolicy:e.target.value})}><option value="owner">{t("Object owner")}</option><option value="restrict-active">{t("Protect active references")}</option></Select></label>
 {draft.deletePolicy==="restrict-active"&&<p className="text-xs text-muted">{t("Active children prevent parent archiving. Archived children keep their references. New or restored active children need an unarchived parent. Published protection cannot be relaxed.")}</p>}
 {draft.storage!=="edge"&&<p className="text-sm">{t("One child has at most one parent reference. The original field controls required values; the object owner controls archive behavior.")}</p>}
 <p className="text-xs text-muted">{t(draft.storage==="edge"?"Relationship instances commit in the object owner with the records they connect.":"Instances stay in the original reference field. This profile does not enable cascade deletion.")}</p>
 </Panel></fieldset></StructuredEditor>
 {draft.storage==="edge"&&installed&&<Panel title={t("Historical relationship migration")} className="grid gap-2">
   <p className="text-xs text-muted">{t("Preview historical links, then move them in one accepted decision. Original history remains readable.")}</p>
   <Button disabled={busy||dirty} onClick={()=>void perform(async()=>setMigration(await client.get<Api.RelationMigrationPlan>(`/v1/relation-migrations/preview\n${JSON.stringify({ref:installed.ref,sourceVersion:installed.version})}`,true)))}>{t("Preview relationship migration")}</Button>
   {migration&&<><p>{migration.migrated?t("Relationships already migrated"):t("{n} visible relationships",{n:migration.links.length})}</p>{migration.partial&&<p role="alert">{t("Migration contains hidden relationships and cannot proceed.")}</p>}
   <Button disabled={busy||dirty||migration.partial||migration.migrated} onClick={()=>void perform(async()=>{if(await decide("build.linktype.migrate",{type:"build.linktype",id:draft.id},{},{expectedRevision:draft.revision,onRefused:setError})){setMigration(undefined);await reload();}})}>{t("Migrate relationships")}</Button></>}
 </Panel>}
 <Panel role="region" aria-label={t("Published relationship versions")} title={t("Published relationship versions")} className="grid gap-2"><p className="text-xs">{t("New drafts and publications do not replace versions already bound by pages.")}</p>{versions.length?versions.map((version)=><p key={version} className="break-all text-xs">{version}</p>):<p className="text-xs text-muted">{t("No published relationship yet.")}</p>}</Panel>
 </div>;
}
