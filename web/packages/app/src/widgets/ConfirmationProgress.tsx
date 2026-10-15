import type {Api} from "@platform/kernel";
import {Button,Card,Disclosure,Input,Select,Tag,t} from "@platform/ui";
import {useState} from "react";
import {useHost,useReadQuery} from "../index";

export function ConfirmationProgress({type,id}:{type:string;id:string}){
 const query=useReadQuery<{records:Api.ConfirmationRun[]}>(`/v1/records/build.confirmation?domain=${encodeURIComponent(JSON.stringify([[type==="build.confirmation"?"id":"subject","=",type==="build.confirmation"?id:`${type}/${id}`]]))}`,5000);
 const {decide}=useHost(),[party,setParty]=useState(""),[outcome,setOutcome]=useState("accepted"),[reason,setReason]=useState(""),[error,setError]=useState("");
 if(query.isError||!query.data?.records.length)return null;
 return <div className="grid gap-2" role="region" aria-label={t("Business confirmation progress")}>{query.data.records.map(run=><Card key={run.id} className="grid gap-2 p-3">
 <div className="flex flex-wrap items-center justify-between gap-2"><strong>{t("Business confirmation progress")}</strong><Tag label={t(run.state)} tone={run.state==="confirmed"||run.state==="compensated"?"success":"warning"}/></div>
 <p className="text-xs">{t("Business deadline")}: {new Date(run.deadline).toLocaleString()}</p>
 {run.partial&&<p role="status" className="text-sm text-warning">{t("This confirmation has a visible partial outcome.")}</p>}
 {run.parties?.map(p=><div key={p.name} className="grid gap-1 rounded border border-border p-2 text-xs"><strong>{p.title}</strong><span>{t(p.state)}{p.at?` · ${new Date(p.at).toLocaleString()}`:""}</span>{p.reason&&<p>{p.reason}</p>}<code className="break-all">{p.intent}</code>{p.compensation&&<p>{t("Compensation")}: {t(p.compensationState??"pending")} · {p.compensation}</p>}{p.differences?.map((d,i)=><p key={i}>{t("Source reconciliation")}: {d.matched?t("Matched"):t("Difference")} · {new Date(d.at).toLocaleString()} · {JSON.stringify(d.observed)} {d.error}</p>)}<Disclosure summary={t("Frozen request evidence")}><pre className="overflow-auto whitespace-pre-wrap">{JSON.stringify({key:p.key,requestVersion:p.requestVersion,sourceVersion:p.sourceVersion,answer:p.answer},null,2)}</pre></Disclosure></div>)}
 {run.completionError&&<p role="alert">{run.completionError}</p>}
 <div className="grid gap-2 sm:grid-cols-3"><Select aria-label={t("Confirmation party")} value={party} onChange={e=>setParty(e.target.value)}><option value="">{t("Choose a party")}</option>{run.parties?.map(p=><option key={p.name} value={p.name}>{p.title}</option>)}</Select><Select aria-label={t("Confirmation decision")} value={outcome} onChange={e=>setOutcome(e.target.value)}>{["accepted","refused","compensate"].map(state=><option key={state} value={state}>{t(state)}</option>)}</Select><Input aria-label={t("Decision evidence")} value={reason} onChange={e=>setReason(e.target.value)}/><Button disabled={!party||!reason.trim()} onClick={async()=>{setError("");if(await decide("build.confirmation.review",{type:"build.confirmation",id:run.id},{party,outcome,reason},{expectedRevision:run.revision,onRefused:setError})){setReason("");await query.refetch();}}}>{t("Record confirmation decision")}</Button></div>
 {error&&<p role="alert">{error}</p>}
 </Card>)}</div>;
}
