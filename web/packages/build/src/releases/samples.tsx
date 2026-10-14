import { useEffect, useState } from "react";
import { useHost, useRecordInventory } from "@platform/app";
import { Button, Card, Input, Select, Textarea, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import type { Project } from "../projects/project";
import { useApplicationScope } from "../projects/application-scope";

export type SampleSetRecord = { id:string;revision:number;name:string;title:string;samples:Api.SampleRecords[];published?:string;definitionVersion?:number };

/** Sample edits are explicit Build decisions. Trial changes never call Save. */
export function ApplicationSamples({application,selected,samples,onChange}:{application:Project;selected:string;samples:string;onChange:(name:string,samples:string)=>void}) {
 const {decide}=useHost();
 const inventory=useRecordInventory<SampleSetRecord>("build.sample-set");
 const record=inventory.data?.records.find(record=>record.name===selected);
 const [name,setName]=useState(record?.name??""),[title,setTitle]=useState(record?.title??"");
 const [createId,setCreateId]=useState(()=>crypto.randomUUID());
 const [busy,setBusy]=useState(false),[error,setError]=useState(""),[saved,setSaved]=useState(false);
 useEffect(()=>{setName(record?.name??"");setTitle(record?.title??"");},[record?.id,record?.revision]);
 const save=async()=>{
  setError("");setSaved(false);let rows:Api.SampleRecords[];
  try{rows=JSON.parse(samples);if(!Array.isArray(rows))throw new Error();}catch{setError(t("Use a JSON array of synthetic model records."));return;}
  setBusy(true);
  try{
   const id=record?.id??createId;
   if(!await decide(`build.sample-set.${record?"edit":"create"}`,{type:"build.sample-set",id},{name,title,samples:rows},{expectedRevision:record?.revision,quiet:true,onRefused:setError}))return;
   await inventory.refetch();
   onChange(name,JSON.stringify(rows,null,2));
   const ref:Api.AssetRef={app:"build",kind:"sample-set",name};
   if(!application.resources?.some(resource=>resource.app===ref.app&&resource.kind===ref.kind&&resource.name===ref.name)){
    if(!await decide("build.app.edit",{type:"build.app",id:application.id},{resources:[...(application.resources??[]),ref]},{expectedRevision:application.revision,quiet:true,onRefused:setError}))return;
   }
   onChange(name,JSON.stringify(rows,null,2));setSaved(true);
  }catch{setError(t("The application sample could not be saved."));}finally{setBusy(false);}
 };
 return <Card className="grid gap-2 p-3" role="region" aria-label={t("Application samples")}>
 <strong className="text-sm">{t("Application samples")}</strong>
 <p className="text-xs text-muted">{t("Prepare synthetic records and references. Trial edits remain temporary until you explicitly save the sample.")}</p>
 <fieldset disabled={busy} className="grid gap-2">
 <label className="grid gap-1 text-xs">{t("Sample set")}<Select value={selected} onChange={event=>{const next=inventory.data?.records.find(record=>record.name===event.target.value);setName(next?.name??"");setTitle(next?.title??"");setCreateId(crypto.randomUUID());onChange(next?.name??"",JSON.stringify(next?.samples??[],null,2));setSaved(false);}}><option value="">{t("New sample set")}</option>{inventory.data?.records.map(record=><option key={record.id} value={record.name}>{record.title}{application.resources?.some(ref=>ref.kind==="sample-set"&&ref.name===record.name)?"":" · "+t("Add to application")}</option>)}</Select></label>
 <div className="grid gap-2 sm:grid-cols-2"><label className="grid gap-1 text-xs">{t("Sample set name")}<Input value={name} disabled={!!record} onChange={event=>{setName(event.target.value);onChange(selected,samples);setSaved(false);}}/></label><label className="grid gap-1 text-xs">{t("Sample set title")}<Input value={title} onChange={event=>{setTitle(event.target.value);onChange(selected,samples);setSaved(false);}}/></label></div>
 <label className="grid gap-1 text-xs">{t("Synthetic sample records (JSON)")}<Textarea parse="json" aria-label={t("Synthetic sample records (JSON)")} rows={8} className="font-mono" value={samples} onChange={event=>{onChange(selected,event.target.value);setSaved(false);}}/></label>
 <Button className="justify-self-start" disabled={!name.trim()||!title.trim()} onClick={save}>{t("Save application sample")}</Button>
 </fieldset>
 {record?.definitionVersion?<p className="text-xs">{t("Published sample version")}: {record.definitionVersion}</p>:null}
 {saved&&<p role="status" className="text-xs">{t("Application sample saved. Release review retains its version.")}</p>}
 {(error||inventory.isError)&&<p role="alert" className="text-sm text-danger">{error||t("The application sample could not be loaded.")}</p>}
 </Card>
}

export function ApplicationSampleAsset({id}:{id?:string}){
 const application=useApplicationScope();
 const projects=useRecordInventory<Project>("build.app");
 const inventory=useRecordInventory<SampleSetRecord>("build.sample-set");
 const project=projects.data?.records.find(record=>record.id===application);
 const [selected,setSelected]=useState("");const [samples,setSamples]=useState("[]");
 useEffect(()=>{const row=inventory.data?.records.find(record=>record.id===id);if(row){setSelected(row.name);setSamples(JSON.stringify(row.samples,null,2));}},[id,inventory.data]);
 return project?<ApplicationSamples application={project} selected={selected} samples={samples} onChange={(name,rows)=>{setSelected(name);setSamples(rows);}}/>:<p className="p-3 text-sm">{t("Open sample sets from an application.")}</p>;
}
