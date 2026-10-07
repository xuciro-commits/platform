import type {Host} from "../index";
import {confirmedDecision} from "../collaboration/decision";
type OriginalActionHost=Pick<Host,"client"|"decide"|"resend"|"can">;
const locks=new WeakMap<object,Set<string>>();
const waiting=(state:string)=>["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_SENDING","SUBMISSION_STATE_UNKNOWN"].includes(state);
const signature=(value:unknown):string=>{if(Array.isArray(value))return JSON.stringify(value.map(signature));if(value&&typeof value==="object")return JSON.stringify(Object.entries(value).filter(([,v])=>v!==undefined).sort(([a],[b])=>a.localeCompare(b)).map(([k,v])=>[k,signature(v)]));return JSON.stringify(value)??"undefined";};
/** Original row decisions remain in K5; an unanswered attempt reuses that decision rather than creating a second action. */
export function createRecordActionSubmitter(host:OriginalActionHost,active:()=>boolean) {
 const attempts=new Map<string,{key:string;revision:number;payload:string}>(),tenant=host.client.connection.tenant,principal=host.client.connection.principal;
 const requireActive=()=>{if(!active()||host.client.connection.tenant!==tenant||host.client.connection.principal!==principal)throw Error("The original action window has ended.");};
 return async(schema:string,target:{type:string;id:string},revision:number,payload:Record<string,unknown>)=>{
  requireActive();if(!host.can(schema))throw Error("The original row action is unavailable.");const record=JSON.stringify([tenant,principal,schema,target]),body=signature(payload),entries=()=>host.client.authorities.outbox.filter(e=>e.submission.tenantId===tenant&&e.submission.principalId===principal&&e.submission.schema?.name===schema&&e.submission.target?.type===target.type&&e.submission.target.id===target.id),prior=attempts.get(record);
  const unanswered=entries().findLast(e=>waiting(e.state));
  if(unanswered){if(!prior||prior.key!==unanswered.submission.idempotencyKey||prior.revision!==revision||prior.payload!==body)throw Error("An earlier row decision is unanswered. Resend it before changing this action.");await host.resend();requireActive();const accepted=confirmedDecision(host.client.authorities.outbox,tenant,prior.key);if(accepted){attempts.delete(record);return {accepted:true};}return {accepted:false,error:entries().findLast(e=>e.submission.idempotencyKey===prior.key)?.reason??"The earlier row decision is awaiting confirmation. Retry sends the same decision."};}
  let locked=locks.get(host.client);if(!locked){locked=new Set();locks.set(host.client,locked);}if(locked.has(record))throw Error("This original row action is already being submitted.");locked.add(record);const before=new Set(entries().map(e=>e.submission.idempotencyKey));let error:string|undefined;
  try{const accepted=await host.decide(schema,target,payload,{expectedRevision:revision,quiet:true,onRefused:message=>error=message});if(accepted){attempts.delete(record);return {accepted,error};}return {accepted:false,error};}
  finally{const own=entries().findLast(e=>!before.has(e.submission.idempotencyKey)&&e.submission.expectedRevision===revision);if(own&&waiting(own.state)&&own.submission.idempotencyKey)attempts.set(record,{key:own.submission.idempotencyKey,revision,payload:body});locked.delete(record);}
 };
}
