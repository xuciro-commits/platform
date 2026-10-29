import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Card, ContextHelp, Disclosure, PropertyList, Select, t } from "@platform/ui";
import type { Api, ActionDeclaration } from "@platform/kernel";
import { useHost } from "../index";
import { DeclaredActionForm } from "../actions/actions";

export function ProposalReview({call,live,active=()=>true,onChanged}:{call?:Api.FunctionRun;live:boolean;active?:()=>boolean;onChanged?:()=>void}){
 const host=useHost(),lease=useRef(true);
 useEffect(()=>{lease.current=true;return()=>{lease.current=false;};},[]);
 const current=useRef({active,live});current.current={active,live};
 const valid=()=>lease.current&&current.current.live&&current.current.active();
 const [schema,setSchema]=useState(""),[reset,setReset]=useState(0);
 const [busy,setBusy]=useState(false),[error,setError]=useState(""),[requested,setRequested]=useState(false);
 const actions=host.catalog.filter(action=>action.target===call?.contract.object&&action.adoption?.function.name===call.function&&(!action.adoption.function.app||action.adoption.function.app===(call.app||"build"))&&(!action.adoption.function.version||action.adoption.function.version===call.version));
 const action=actions.find(action=>action.schema===schema)??actions[0];
 const source=call?.source.split("/").slice(1).join("/")??"";
 const record=useQuery({queryKey:["proposal-source",host.source.scope,call?.id,call?.source],queryFn:()=>host.source.get(call!.contract.object,source),enabled:!!call?.contract.proposal&&valid()});
 const initial=(selected?:ActionDeclaration)=>{
  let output:Record<string,unknown>={};try{output=JSON.parse(call?.output??"{}");}catch{}
  return Object.fromEntries((selected?.adoption?.mapping??[]).map(mapping=>[mapping.input,output[mapping.output]]));
 };
 useEffect(()=>{setSchema("");setError("");setRequested(false);},[call?.id]);
 if(!call?.contract.proposal)return null;
 const pending=call.state==="ready"&&call.decision==="pending"&&!call.withheld;
 const review=async(decision:"rejected"|"expired")=>{
  if(!pending||!valid()||busy)return;setBusy(true);setError("");
  try{if(await host.decide("build.function-call.review",{type:"build.function-call",id:call.id},{decision},{expectedRevision:call.revision,quiet:true,onRefused:message=>{if(valid())setError(message);}})&&valid()){await host.refresh?.();onChanged?.();}}finally{if(valid())setBusy(false);}
 };
 return <Card className="grid gap-3 p-3" role="region" aria-label={t("Proposal review")}>
 <div className="flex flex-wrap items-center gap-2"><strong>{t("Proposal review")}</strong><span className="text-xs">{t(call.decision||call.state)}</span><ContextHelp title={t("Proposal review")}>{t("Review the suggestion and amend the action inputs. Adoption uses the original permissions, constraints and approval path.")}</ContextHelp></div>
 {(call.expiresAt||call.decidedBy)&&<PropertyList columns={2} items={[
  ...(call.expiresAt?[[t("Proposal expires at"),call.expiresAt] as [string,string]]:[]),
  ...(call.decidedBy?[[t("Decided by"),call.decidedBy] as [string,string],[t("Adoption action"),call.adoptedAction||"—"] as [string,string]]:[]),
 ]}/>}
 {pending&&<fieldset disabled={!live||busy||requested} className="grid gap-2">
 <label className="grid gap-1 text-xs">{t("Adoption action")}<Select value={action?.schema??""} onChange={event=>{setSchema(event.target.value);setReset(n=>n+1);setRequested(false);}}><option value="">{t("Choose an action")}</option>{actions.map(action=><option key={action.schema} value={action.schema}>{action.title}</option>)}</Select></label>
 {action?.adoption&&<><PropertyList columns={2} items={[[t("Retained proposal"),<code className="select-all break-all">{call.id}</code>],[t("Version"),call.version]]}/>
<DeclaredActionForm key={`${call.id}:${action.schema}:${reset}`} declared={action} target={{type:call.contract.object,id:source}} revision={record.data?.record.revision??0} initial={initial(action)} fixed={{[action.adoption.input]:call.id}} enabled={!!record.data?.record&&!record.isError&&valid()} onCancel={()=>setReset(n=>n+1)} onCompleted={()=>{if(valid()){setRequested(true);void host.refresh?.();onChanged?.();}}} submitLabel={t(action.needsApproval?"Request adoption approval":"Adopt proposal")}/></>}
 {!actions.length&&<p className="text-xs">{t("No adoption action is available to this member.")}</p>}
 <div className="flex gap-2"><Button onClick={()=>void review("rejected")}>{t("Reject proposal")}</Button><Button onClick={()=>void review("expired")}>{t("Expire proposal")}</Button></div>
 </fieldset>}
 {pending&&requested&&<p role="status" className="text-sm">{t(action?.needsApproval?"The adoption is waiting in the original approval inbox.":"The adoption action was accepted.")}</p>}
 {!!call.amendments&&<Disclosure summary={t("Human amendments")}><pre className="overflow-auto text-xs">{JSON.stringify(call.amendments,null,2)}</pre></Disclosure>}
 {error&&<p role="alert" className="text-sm text-danger">{error}</p>}
 </Card>
}
