import {useEffect,useRef,useState} from "react";
import {useQueries} from "@tanstack/react-query";
import {AIResult,Panel,t,type AIResultTurn,type EntityRecord} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {useHost} from "../index";
import {createAIReader,type AIRequest} from "../ai/service";
import type {VariableResult} from "../runtime/variables";

type Props={section:Api.Section;record?:EntityRecord;status?:string;identity:string;active:()=>boolean;live:boolean;enabled?:boolean;question?:VariableResult;onQuestion?:(value:string)=>void};
export function AIWidget(props:Props){const host=useHost();return <AISession key={JSON.stringify([host.source.scope,props.identity,props.section.function,props.section.ai])} {...props}/>;}
function AISession({section,record,status,active,live,enabled=true,question,onQuestion}:Props){
 const host=useHost(),lease=useRef(true),current=useRef({active,live,enabled,status});current.current={active,live,enabled,status};useEffect(()=>{lease.current=true;return()=>{lease.current=false;};},[]);
 const valid=()=>lease.current&&current.current.active(),reader=createAIReader(host,valid),[requests,setRequests]=useState<AIRequest[]>([]),[attempt,setAttempt]=useState<AIRequest>(),[busy,setBusy]=useState(false),[error,setError]=useState<string>();
 const config=section.ai,fn=section.function,version=Number(fn?.sourceVersion.match(/\.function-(\d+)$/)?.[1]??0),text=question?.status==="value"&&typeof question.value==="string"?question.value:"";
 const answers=useQueries({queries:requests.map(request=>({queryKey:["original-ai-call",host.source.scope,request.id],queryFn:()=>reader.read(request),enabled:valid(),refetchInterval:(query:{state:{error:unknown;data?:Api.FunctionRun}})=>!valid()||query.state.error?false:query.state.data?.state==="pending"?1000:false}))});
 if(!config||!fn||!version)return <Panel role="alert">{t("Choose a fixed original AI function.")}</Panel>;
 if(status!=="value"||!record)return <p role={status==="error"?"alert":"status"}>{t(status==="error"?"The AI source record is unavailable.":"Select an original record for AI context.")}</p>;
 const turns:AIResultTurn[]=requests.map((request,i)=>{const answer=answers[i];if(answer?.isError)return {id:request.id,question:request.question,state:"rejected",reason:t("The original AI result is unavailable in this context.")};const call=answer?.data;let fields:AIResultTurn["fields"];if(call?.state==="ready"){try{const output=JSON.parse(call.output??"");fields=call.contract.output.filter(f=>Object.hasOwn(output,f.name)&&(config.kind!=="chatbot"||f.name===config.replyField)).map(f=>({name:f.name,value:String(output[f.name])}));}catch{return {id:request.id,question:request.question,state:"rejected",reason:t("The original AI result is unavailable in this context.")};}}return {id:request.id,question:call?.question??request.question,state:call?.state??"pending",fields,reason:call?.reason};});
 const pending=turns.some(turn=>turn.state==="pending"),run=async(suggestion?:string,retry=false)=>{
  if(!valid()||current.current.status!=="value"||!current.current.live||!current.current.enabled||busy||pending||attempt&&!retry)return;
  const question=retry?attempt?.question??text:suggestion??text;if(config.kind==="chatbot"&&(!question.trim()||new TextEncoder().encode(question).length>4096))return;
  const next=retry?attempt:{id:"CALL-"+crypto.randomUUID(),app:fn.ref.app,name:fn.ref.name,version,source:record.id,...config.kind==="chatbot"?{question,history:requests.filter((_,i)=>answers[i]?.data?.state==="ready").map(r=>r.id)}:{}};
  if(!next)return;if(config.kind==="chatbot"&&(next.history?.length??0)>8){setError(t("Reset the AI view before starting another conversation."));return;}
  setAttempt(next);setBusy(true);setError(undefined);
  try{if(await reader.request(next)&&valid()){setRequests(old=>config.kind==="chatbot"?[...old,next]:[next]);setAttempt(undefined);if(config.kind==="chatbot")onQuestion?.("");}}catch(err){if(valid())setError(err instanceof Error?t(err.message):t("The AI request was not confirmed."));}finally{if(valid())setBusy(false);}
 };
 return <AIResult kind={config.kind} turns={turns} question={text} suggestions={config.suggestions??[]} disabled={!live||!enabled||!valid()||pending||!host.can("build.function-call.start")} busy={busy} error={error} onQuestion={value=>{if(valid())onQuestion?.(value);}} onRun={value=>void run(value)} onReset={()=>{if(valid()){setRequests([]);setAttempt(undefined);setError(undefined);}}} onRetry={attempt?()=>void run(undefined,true):undefined}/>;
}
