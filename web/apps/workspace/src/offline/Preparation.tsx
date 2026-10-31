import {useState} from "react";
import {useHost} from "@platform/app";
import {Button,Dialog,Disclosure,InspectorSection,Panel,Textarea,t} from "@platform/ui";
import {prepareShell} from "./shell";

export function OfflinePreparation({application,release,open,onOpenChange}:{application?:string;release?:string;open:boolean;onOpenChange:(open:boolean)=>void}){
 const host=useHost(),client=host.client;
 const [busy,setBusy]=useState(false),[error,setError]=useState(""),[saved,setSaved]=useState(""),[,setRevision]=useState(0);
 const definition=host.definitions.find(item=>item.ref.kind==="app"&&item.application&&`${item.ref.app}:${item.application.name}`===application&&item.source!=="code");
 let applications:ReturnType<NonNullable<typeof client.prepared>["applications"]>=[],drafts:ReturnType<NonNullable<typeof client.prepared>["forms"]>=[],storageError="";
 try{if(open){applications=client.prepared?.applications()??[];drafts=client.prepared?.forms()??[];}}catch(problem){storageError=String(problem instanceof Error?problem.message:problem);}
 const refresh=()=>setRevision(value=>value+1);
 const prepare=async()=>{
  if(!definition?.application||!release||busy)return;
  setBusy(true);setError("");setSaved("");
  try{
   const paths=new Set<string>(),objects=new Set<string>(),references=new Set<string>();
   for(const page of host.definitions.filter(item=>item.ref.kind==="page"&&item.ref.app===definition.ref.app&&definition.application!.pages.includes(item.ref.name))){
    if(page.page?.object.name)objects.add(page.page.object.name);
    if(page.contentVersion)paths.add(`/v1/pages/${encodeURIComponent(page.ref.app)}/${encodeURIComponent(page.ref.name)}/${encodeURIComponent(page.contentVersion)}`);
   }
   for(const type of objects){paths.add(client.recordsPath(type,{sort:["-changed"],limit:100}));for(const field of host.source.entity(type)?.fields??[])if(field.type==="reference"&&field.ref&&field.ref!=="enterprise.element")references.add(field.ref);}
   for(const type of references)paths.add(client.recordsPath(type,{limit:25}));
   await prepareShell();
   const prepared=await client.prepareApplication({id:application!,title:definition.application.title,binding:{ref:definition.ref,sourceVersion:definition.version},release,route:location.hash},[...paths]);
   setSaved(t("Prepared at {time}.",{time:new Date(prepared.at).toLocaleString()}));refresh();
  }catch(problem){setError(t(problem instanceof Error?problem.message:String(problem)));}finally{setBusy(false);}
 };
 return <Dialog open={open} onOpenChange={value=>!busy&&onOpenChange(value)} title={t("Offline applications")}>
  <div className="grid gap-4">
   <p className="text-sm">{t("Prepare this published application, its first record window and the tasks currently open. Other records and tasks require a connection.")}</p>
   <p className="text-sm text-muted">{t("Saved content is a dated copy. Queued requests wait for the host's current permissions and business decision.")}</p>
   {definition&&<Panel title={definition.application!.title}><p className="text-xs text-muted">{t("Application version {version}",{version:definition.version})}</p><Button className="mt-3" variant="primary" loading={busy} disabled={!release||navigator.onLine===false||!!storageError} onClick={()=>void prepare()}>{t("Prepare for offline")}</Button></Panel>}
   {!definition&&<p className="text-sm text-muted">{t("Open a published business application to prepare it.")}</p>}
   {(error||storageError)&&<p role="alert" className="text-sm text-danger">{error||t(storageError)}</p>}
   {saved&&<p role="status" className="text-sm">{saved}</p>}
   <InspectorSection title={t("Prepared applications")}>
    {!applications.length&&!storageError&&<p className="text-sm text-muted">{t("No application is prepared for this identity.")}</p>}
    {applications.map(app=><Panel key={app.id} title={app.title}><p className="text-xs text-muted">{t("Version {version} · saved {time}",{version:app.binding.sourceVersion,time:new Date(app.at).toLocaleString()})}</p><div className="mt-2 flex flex-wrap gap-2"><Button disabled={busy} onClick={()=>void(async()=>{client.prepared?.activate(app.id);await client.refreshDeclarations();await host.refresh?.();location.hash=app.route;onOpenChange(false);})().catch(problem=>setError(t(problem instanceof Error?problem.message:String(problem))))}>{t("Open prepared application")}</Button><Button disabled={busy} onClick={()=>{try{client.prepared?.remove(app.id);refresh();}catch(problem){setError(t(String(problem instanceof Error?problem.message:problem)));}}}>{t("Remove preparation")}</Button></div></Panel>)}
   </InspectorSection>
   <InspectorSection title={t("Local form drafts")}>
    <p className="text-xs text-muted">{t("Open the same create or record action to continue a saved draft. Earlier definitions retain their original inputs here.")}</p>
    {drafts.map(draft=><Disclosure key={draft.key} summary={`${host.catalog.find(action=>action.schema===draft.schema)?.title??draft.schema} · ${draft.target.id}`}><div className="grid gap-2"><p className="text-xs text-muted">{t("Version {version} · saved {time}",{version:draft.schemaVersion?.version??draft.binding.sourceVersion,time:new Date(draft.at).toLocaleString()})}</p><Textarea aria-label={t("Saved draft inputs")} readOnly rows={6} value={JSON.stringify(draft.values,null,2)}/><Button disabled={busy} onClick={()=>{try{client.prepared?.removeForm(draft.key);refresh();}catch(problem){setError(t(String(problem instanceof Error?problem.message:problem)));}}}>{t("Discard local draft")}</Button></div></Disclosure>)}
   </InspectorSection>
  </div>
 </Dialog>;
}
