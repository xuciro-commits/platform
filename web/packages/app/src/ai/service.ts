import type {Api} from "@platform/kernel";
import type {Host} from "../index";
import {confirmedDecision} from "../collaboration/decision";

export type AIRequest={id:string;app:string;name:string;version:number;source:string;question?:string;history?:string[]};
export function createAIReader(host:Pick<Host,"client"|"source"|"decide"|"resend"|"can"|"me">,active:()=>boolean){
 const requireActive=()=>{if(!active())throw Error("The original AI view scope has ended.");};
 return {
  async request(attempt:AIRequest){
   requireActive();if(!host.can("build.function-call.start"))throw Error("This AI call is unavailable to this member.");
   const {id,...payload}=attempt,encoded=btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(payload))));
   const unanswered=host.client.authorities.outbox.filter(entry=>entry.submission.tenantId===host.client.connection.tenant&&entry.submission.principalId===host.client.connection.principal&&entry.submission.target?.type==="build.function-call"&&entry.submission.target.id===attempt.id&&["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_SENDING","SUBMISSION_STATE_UNKNOWN"].includes(entry.state));
   if(unanswered.length){if(unanswered.some(entry=>entry.submission.schema?.name!=="build.function-call.start"||entry.submission.payload!==encoded||entry.submission.evidenceFactIds?.length))throw Error("The AI request was not confirmed.");await host.resend();requireActive();return confirmedDecision(host.client.authorities.outbox,host.client.connection.tenant,unanswered[0]!.submission.idempotencyKey??"");}
   let refusal="The AI request was not confirmed.";
   const accepted=await host.decide("build.function-call.start",{type:"build.function-call",id},payload,{quiet:true,onRefused:reason=>refusal=reason});requireActive();if(!accepted)throw Error(refusal);return true;
  },
  async read(attempt:AIRequest):Promise<Api.FunctionRun>{
   requireActive();const view=await host.source.get("build.function-call",attempt.id);requireActive();const call=view.record as Api.FunctionRun;
   if(call.id!==attempt.id||call.app!==attempt.app||call.function!==attempt.name||call.version!==attempt.version||call.contract?.name!==attempt.name||call.source!==`${call.contract.object}/${attempt.source}`||call.member!==host.me.principalId||call.withheld||!["pending","ready","rejected"].includes(call.state))throw Error("The original AI result is unavailable in this context.");
   return call;
  }
 };
}
