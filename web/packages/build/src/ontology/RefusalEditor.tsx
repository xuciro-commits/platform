import type {Api} from "@platform/kernel";
import {useHost} from "@platform/app";
import {Button,Checkbox,Input,Panel,Select,Textarea,t,type EntityInfo} from "@platform/ui";
import {useState} from "react";
import {ExpressionEditor,type ExpressionChoice} from "../editor/ExpressionEditor";
import {nameOf} from "./object-model";

/** Business exits belong to the original transaction, including its step scopes. */
export function RefusalEditor({value,onChange,choices,actions,entities,roles}:{value:Api.BusinessTxn;onChange:(value:Api.BusinessTxn)=>void;choices:ExpressionChoice[];actions:string[];entities:EntityInfo[];roles:string[]}) {
 const {source}=useHost(),[scope,setScope]=useState("");
 const at=value.steps.findIndex(step=>step.name===scope),items=(at>=0?value.steps[at]?.refusals:value.refusals)??[];
 const save=(refusals:Api.Refusal[])=>onChange(at>=0?{...value,steps:value.steps.map((step,index)=>index===at?{...step,refusals}:step)}:{...value,refusals});
 const patch=(index:number,next:Partial<Api.Refusal>)=>save(items.map((item,i)=>i===index?{...item,...next}:item));
 const records=entities.filter(entity=>entity.type.startsWith("build.")&&entity.type!==value.subject&&entity.fields.some(field=>field.type==="reference"&&field.ref===value.subject));
 const record=(type:string):Api.TxnStep=>({name:"refusal",kind:"create",target:{type},changes:[{field:records.find(entity=>entity.type===type)?.fields.find(field=>field.type==="reference"&&field.ref===value.subject)?.name??"",expr:{op:"",kind:"reference",ref:"input.$subject"}}]});
 return <Panel title={t("Business refusal exits")} className="grid gap-3">
  <p className="text-xs text-muted">{t("A business rule may block the request, continue with an audited flag, or route to another declared transaction.")}</p>
  <label className="grid gap-1 text-xs">{t("Check refusals at")}<Select value={at>=0?scope:""} onChange={event=>setScope(event.target.value)}><option value="">{t("Transaction entry")}</option>{value.steps.map(step=><option key={step.name} value={step.name}>{t("Before step: {name}",{name:step.name})}</option>)}</Select></label>
  {items.map((item,index)=><Panel key={index} className="grid gap-3">
   <div className="grid gap-2 sm:grid-cols-2">
    <label className="grid gap-1 text-xs">{t("Stable refusal code")}<Input value={item.code} onChange={event=>patch(index,{code:event.target.value})}/></label>
    <label className="grid gap-1 text-xs">{t("Refusal result")}<Select value={item.level} onChange={event=>{const level=event.target.value;patch(index,{level,route:level==="route"?{type:value.subject,id:"$subject",action:actions[0]??""}:undefined,record:level==="flag"?record(records[0]?.type??""):undefined});}}><option value="block">{t("Block the transaction")}</option><option value="flag">{t("Continue with an audited flag")}</option><option value="route">{t("Route to a correction transaction")}</option></Select></label>
    <label className="grid gap-1 text-xs">{t("Business refusal reason (English)")}<Input value={item.message} onChange={event=>patch(index,{message:event.target.value})}/></label>
    <label className="grid gap-1 text-xs">{t("Business refusal reason (Simplified Chinese)")}<Input value={item.translations?.["zh-CN"]??""} onChange={event=>patch(index,{translations:{"zh-CN":event.target.value}})}/></label>
   </div>
   <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Refuse when this rule is true")}</legend><ExpressionEditor value={item.when} onChange={when=>patch(index,{when})} choices={choices} lookup={source.entity}/></fieldset>
   {item.level==="route"&&<label className="grid gap-1 text-xs">{t("Correction transaction")}<Select value={item.route?.action??""} onChange={event=>patch(index,{route:{type:value.subject,id:"$subject",action:event.target.value}})}><option value="">{t("Choose an action")}</option>{actions.map(action=><option key={action}>{action}</option>)}</Select></label>}
   {item.level!=="block"&&<>
    <label className="grid gap-1 text-xs">{t("Visible refusal record")}<Select value={item.record?.target?.type??""} onChange={event=>patch(index,{record:event.target.value?record(event.target.value):undefined})}><option value="">{item.level==="flag"?t("Choose a related record type"):t("No additional refusal record")}</option>{records.map(entity=><option key={entity.type} value={entity.type}>{entity.title} · {entity.type}</option>)}</Select></label>
    {item.record&&<label className="grid gap-1 text-xs">{t("Refusal record transaction step")}<Textarea parse="json" rows={9} value={JSON.stringify(item.record,null,2)} onChange={event=>patch(index,{record:JSON.parse(event.target.value)})}/></label>}
    <p className="text-xs text-muted">{t("The refusal record must reference the original subject. Actor, reason, time and original values are retained in the audit fact.")}</p>
   </>}
   <Checkbox checked={!!item.escalation} onChange={enabled=>patch(index,{escalation:enabled?{count:3,windowSeconds:3600,reviewerRole:roles[0]??"builder"}:undefined})}>{t("Escalate repeated business refusals")}</Checkbox>
   {item.escalation&&<div className="grid gap-2 sm:grid-cols-3">
    <label className="grid gap-1 text-xs">{t("Distinct refused requests")}<Input type="number" min={2} max={100} value={item.escalation.count} onChange={event=>patch(index,{escalation:{...item.escalation!,count:event.target.valueAsNumber}})}/></label>
    <label className="grid gap-1 text-xs">{t("Refusal window (seconds)")}<Input type="number" min={1} max={604800} value={item.escalation.windowSeconds} onChange={event=>patch(index,{escalation:{...item.escalation!,windowSeconds:event.target.valueAsNumber}})}/></label>
    <label className="grid gap-1 text-xs">{t("Refusal reviewer role")}<Select value={item.escalation.reviewerRole} onChange={event=>patch(index,{escalation:{...item.escalation!,reviewerRole:event.target.value}})}>{roles.map(role=><option key={role}>{role}</option>)}</Select></label>
    <p className="text-xs text-muted sm:col-span-3">{t("Repeated original keys count once. The reviewer receives one normal task and notification for each declared refusal window.")}</p>
   </div>}
   <Button size="sm" onClick={()=>save(items.filter((_,i)=>i!==index))}>{t("Remove refusal exit")}</Button>
  </Panel>)}
  <Button disabled={items.length>=16} onClick={()=>save([...items,{when:{op:"",kind:"boolean",lit:{value:false}},level:"block",code:nameOf("refusal",items.map(item=>item.code)),message:"",translations:{"zh-CN":""}}])}>{t("Add business refusal exit")}</Button>
 </Panel>;
}
