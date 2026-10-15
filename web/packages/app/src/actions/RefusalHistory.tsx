import type {Api} from "@platform/kernel";
import {Button,Card,Disclosure,Panel,Tag,t} from "@platform/ui";
import {useState} from "react";
import {useHost,useOpenRecord,useReadQuery} from "../index";
import {refusalCategoryName,refusalFactMessage,refusalLevelName} from "./refusal";

/** Read-only projection of the existing accepted-result journal, including refused intents. */
export function RefusalHistory({type,id}:{type:string;id:string}) {
 const [offset,setOffset]=useState(0),{source}=useHost(),open=useOpenRecord();
 const query=useReadQuery<Api.BusinessRefusalPage>(`/v1/business-refusals?type=${encodeURIComponent(type)}&id=${encodeURIComponent(id)}&offset=${offset}&limit=50`);
 if(query.isError)return <Panel role="alert">{t("Refusal and correction history is unavailable.")}</Panel>;
 if(!query.data?.total)return null;
 const {records,total}=query.data;
 const field=(name:string)=>source.entity(type)?.fields.find(item=>item.name===name)?.title??name;
 return <Panel title={t("Refusals and corrections")} className="grid min-w-0 gap-3">
  <p role="status" className="text-xs text-muted">{t("Showing {shown} of {total} recorded outcomes.",{shown:records.length,total})}</p>
  <ol className="grid min-w-0 gap-2">{records.map(entry=><li key={entry.key}><Card className="grid min-w-0 gap-2 p-3 text-xs">
   <div className="flex flex-wrap gap-2"><strong className="break-all">{entry.action}</strong><Tag label={entry.state==="accepted"?entry.correctionOf?t("Correction accepted"):t("Business outcome accepted"):refusalCategoryName(entry.category)} tone={entry.state==="accepted"?"success":entry.category==="business"?"info":"warning"}/>{entry.code&&<span className="break-all font-mono">{entry.code}</span>}</div>
   <p className="break-words text-muted">{entry.member} · <time dateTime={entry.at}>{new Date(entry.at).toLocaleString()}</time></p>
   {!entry.facts?.length&&entry.message&&<p className="whitespace-pre-wrap break-words">{entry.message}</p>}
   <p className="break-all font-mono text-muted">{t("Request")}: {entry.key}</p>
   {entry.correctionOf&&<p className="break-all">{t("Corrects request {key}",{key:entry.correctionOf})}</p>}
   {!!entry.changes?.length&&<ul className="grid gap-1">{entry.changes.map(change=><li key={change.field} className="break-all"><strong>{field(change.field)}</strong>: {JSON.stringify(change.before)??"—"} → {JSON.stringify(change.after)??"—"}</li>)}</ul>}
   {entry.facts?.map((fact,index)=><div key={index} className="grid min-w-0 gap-2 rounded border border-border p-2">
    <div className="flex flex-wrap gap-2"><Tag label={refusalLevelName(fact.level)} tone={fact.level==="flag"?"warning":fact.level==="route"?"info":"neutral"}/><span className="break-all font-mono">{fact.code}</span></div>
    <p className="whitespace-pre-wrap break-words">{refusalFactMessage(fact)}</p>
    <p className="break-words text-muted">{fact.member} · <time dateTime={fact.at}>{new Date(fact.at).toLocaleString()}</time>{fact.step?` · ${fact.step}`:""}</p>
    {fact.route?.action&&<p className="break-all">{t("Correction transaction")}: {fact.route.action}</p>}
    {fact.record&&<Button size="sm" variant="ghost" onClick={()=>open(fact.record!)}>{t("Open refusal record")}</Button>}
    {fact.original!=null&&<Disclosure summary={t("Original business values")}><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(fact.original,null,2)}</pre></Disclosure>}
   </div>)}
   {entry.inputs!=null&&<Disclosure summary={t("Submitted inputs")}><pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all">{JSON.stringify(entry.inputs,null,2)}</pre></Disclosure>}
  </Card></li>)}</ol>
  {total>50&&<div className="flex justify-end gap-2"><Button disabled={offset===0} onClick={()=>setOffset(value=>Math.max(0,value-50))}>{t("Previous")}</Button><Button disabled={offset+records.length>=total} onClick={()=>setOffset(value=>value+50)}>{t("Next page")}</Button></div>}
 </Panel>;
}
