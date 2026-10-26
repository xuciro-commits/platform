import { StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import type {Api} from "@platform/kernel";
import {useHost,semanticPropertyTypes} from "@platform/app";
import {Button,Input,PageHeader,Panel,Select,Textarea,t} from "@platform/ui";
import { ResourceControls as AssetControls } from "../editor/workbench";
import { useDirectInstall } from "../releases/release-profile";

type Draft = Api.PropertyType & {id:string;revision:number;published?:string;version?:number};
const empty = ():Draft => ({id:"",revision:0,name:"",title:"",description:"",type:"text"});
const hydrate = (record:Draft):Draft => ({...empty(),...record});
const types = ["text","longtext","integer","decimal","date","datetime","boolean"];

export function PropertyTypes() {
 const {source,role}=useHost(),{open}=useApplicationWorkspace();
 if(role("build")!=="builder")return <PageHeader title={t("Shared properties")} description={t("Only a builder can edit shared properties.")}/>;
 return <div className="grid gap-3"><PageHeader title={t("Shared properties")} help={t("Publish shared scalar meaning and bind object fields to an exact version.")} actions={<Button onClick={()=>open({view:"property-type",params:{id:"new"}})}>{t("New shared property")}</Button>}/><ResourceList source={source} type="build.propertytype" fields={["title","name","type","version"]} onOpen={record=>open({view:"property-type",params:{id:record.id}})}/></div>;
}

export function PropertyTypeEditor({id}:{id:string}) {
 const {role,definitions}=useHost(),{open}=useApplicationWorkspace();
 const editor=useDefinitionDraft<Draft>({
  id,type:"build.propertytype",kind:"property-type",view:"property-type",empty,normalize:hydrate,joinApplication:false,
  failure:t("The shared property could not be saved or loaded. Your draft is still here."),
  payload:({name,title,description,type})=>({name,title,description,type}),
 });
 const {draft,query,dirty,busy,error,patch,perform,save,review,discardChanges,confirmDiscard}=editor;
 const reload=()=>editor.reload(t("Reload the saved shared property before editing again."));
 const publish=()=>editor.transition("publish");
 const directInstall = useDirectInstall();
 const versions=semanticPropertyTypes(definitions).filter(p=>p.binding.ref.app==="build"&&p.binding.ref.name===draft.name);
 if(role("build")!=="builder")return <PageHeader title={t("Shared properties")} description={t("Only a builder can edit shared properties.")}/>;
 if(id!=="new"&&!draft.id)return <PageHeader title={t("Shared properties")} description={query.isError?t("The shared property could not be loaded."):t("Loading…")}/>;
 return <div className="grid min-w-0 grid-cols-1 gap-3"><PageHeader title={draft.title||t("New shared property")} help={t("Publish shared scalar meaning and bind object fields to an exact version.")} actions={<div className="flex min-w-0 flex-wrap gap-2"><Button onClick={()=>open({view:"property-type"})}>{t("Shared properties")}</Button><AssetControls type="build.propertytype" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} route={{view:"property-type",params:{id}}}/><Button disabled={busy||!draft.id} onClick={()=>confirmDiscard(()=>void perform(reload))}>{t("Reload saved shared property")}</Button><Button disabled={busy||!dirty&&!!draft.id} onClick={()=>void perform(save)}>{t("Save shared property")}</Button>{directInstall&&<Button disabled={busy||!draft.id} onClick={()=>void perform(publish)}>{t("Direct install")}</Button>}<Button variant="primary" disabled={busy||!draft.id} onClick={()=>void perform(review)}>{t("Review release")}</Button></div>}/>
 <p className="text-xs text-muted">{t("Direct install changes the current workspace immediately. It does not save or activate a release candidate.")}</p>
 {!directInstall && <p className="text-xs text-muted">{t("This tenant delivers through a saved release candidate: review the draft and activate it.")}</p>}
 {error&&<Panel role="alert" className="text-danger">{error}</Panel>}
 {dirty&&<Panel role="status">{t("Unsaved changes. Direct install and release review save first.")}</Panel>}
 <StructuredEditor {...editor.structured}><fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
 <Panel title={t("Shared property settings")} className="grid min-w-0 grid-cols-1 content-start gap-3">
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property name")}<Input disabled={!!draft.published} value={draft.name} onChange={e=>patch({name:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property title")}<Input value={draft.title} onChange={e=>patch({title:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property description")}<Textarea value={draft.description} onChange={e=>patch({description:e.target.value})}/></label>
 <label className="grid min-w-0 gap-1 text-xs">{t("Shared property type")}<Select disabled={!!draft.published} value={draft.type} onChange={e=>patch({type:e.target.value})}>{types.map(type=><option key={type} value={type}>{t(type)}</option>)}</Select></label>
 </Panel>
 <Panel role="region" aria-label={t("Published shared property versions")} title={t("Published shared property versions")} help={<><p>{t("New publications do not replace object fields already bound to an older version.")}</p><p>{t("Local field names, required values and access belong to each object.")}</p></>} className="grid min-w-0 grid-cols-1 content-start gap-2">{versions.length?versions.map(p=><p key={p.binding.sourceVersion} className="break-all text-xs">{p.property.title} · {t(p.property.type)} · {p.binding.sourceVersion}</p>):<p className="text-xs text-muted">{t("No published shared property yet.")}</p>}</Panel>
 </fieldset></StructuredEditor></div>;
}
