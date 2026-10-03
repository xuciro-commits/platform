import type {Api} from "@platform/kernel";
import type {Host} from "../index";
import {confirmedDecision} from "../collaboration/decision";

export type WorkViewsHost=Pick<Host,"client"|"source"|"can"|"decide"|"resend"|"me">;
const waiting=(state:string)=>["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_SENDING","SUBMISSION_STATE_UNKNOWN"].includes(state);
const validID=(id:unknown):id is string=>typeof id==="string"&&!!id;
type Flight={signature:string;promise:Promise<boolean>};
// Ephemeral promises reserve one member/client target until its send finishes;
// durable unanswered decisions remain solely in the original K5 outbox.
const flights=new WeakMap<object,Map<string,Flight>>();
export function approvalDecisionReason(request:Api.ApprovalRequest,member:string):string|undefined {
 if(request.requester===member)return "The requester cannot approve their own request.";
 const level=request.levels[request.level];if(request.state!=="pending"||!level)return "The original approval request is unavailable or has changed.";
 const delegated=level.delegates&&Object.hasOwn(level.delegates,member)?level.delegates[member]:undefined,who=level.approvers.includes(member)?member:typeof delegated==="string"?delegated:"";
 if(!who)return "You are not a current approver for this request.";
 if((level.approved??[]).includes(who))return "Your approval is already recorded at this level. Other approvers are still awaited.";
}
export function approvalTaskID(task:Api.InboxTask):string|undefined {
 const id=task.ref?.startsWith("work.approval/")?task.ref.slice("work.approval/".length):undefined;
 return validID(id)?id:undefined;
}
/** The original caller inbox owns task membership, ordering and the complete current total. */
export function approvalTasks(items:readonly Api.InboxTask[],member:string):Api.InboxTask[] {
 if(!Array.isArray(items)||items.some(task=>!task||!validID(task.id))||new Set(items.map(task=>task.id)).size!==items.length)throw Error("The original inbox is unavailable or incompatible.");
 const tasks=items.filter(task=>approvalTaskID(task)!==undefined);
 if(tasks.some(task=>task.archived||task.state!=="open"||!Array.isArray(task.candidates)||(task.assignee?task.assignee!==member:!task.candidates.includes(member))))throw Error("The offered approval task is no longer available to this member.");
 if(new Set(tasks.map(approvalTaskID)).size!==tasks.length)throw Error("The original approval inbox has duplicate request identities.");
 return tasks;
}
export function originalApproval(task:Api.InboxTask,value:unknown):Api.ApprovalRequest {
 const request=value as Api.ApprovalRequest;
 if(!request||request.id!==approvalTaskID(task)||request.archived||!Number.isSafeInteger(request.revision)||request.revision<1||request.state!=="pending"||!validID(request.target)||!validID(request.requester)||!validID(request.action)||typeof request.title!=="string"||!Array.isArray(request.levels)||!Number.isSafeInteger(request.level)||request.level<0||request.level>=request.levels.length||request.levels.some(level=>!level||typeof level.title!=="string"||!Array.isArray(level.approvers)||level.approved!==null&&!Array.isArray(level.approved)||level.approvers.some(id=>!validID(id))||(level.approved??[]).some(id=>!validID(id))))throw Error("The original approval request is unavailable or has changed.");
 // Original Go nil Approved slices serialize as null and mean no approvals.
 return request.levels.some(level=>level.approved===null)?{...request,levels:request.levels.map(level=>({...level,approved:level.approved??[]}))}:request;
}
export function originalNotifications(items:readonly Api.Notification[],member:string):Api.Notification[] {
 if(!Array.isArray(items)||new Set(items.map(item=>item?.id)).size!==items.length||items.some(item=>!item||!validID(item.id)||item.member!==member||typeof item.title!=="string"||typeof item.at!=="string"||typeof item.read!=="boolean"))throw Error("The original notifications are unavailable or incompatible.");
 return [...items];
}
/** Reads and decisions retain their original Work/Console owner and caller scope. */
export function createWorkViews(host:WorkViewsHost,active:()=>boolean) {
 const requireActive=()=>{if(!active())throw Error("The original work view scope has ended.");};
 const answer=async(schema:string,type:string,id:string,revision?:number)=>{
  requireActive();if(!host.can(schema))throw Error("This work decision is unavailable to this member.");
  let targets=flights.get(host.client);if(!targets){targets=new Map();flights.set(host.client,targets);}
  const key=JSON.stringify([host.client.connection.tenant,host.client.connection.principal,type,id]),signature=JSON.stringify([schema,revision]),prior=targets.get(key);
  if(prior){if(prior.signature!==signature)throw Error("Another decision for this request is already being confirmed.");return await prior.promise&&active();}
  const flight:Flight={signature,promise:Promise.resolve(false)};targets.set(key,flight);
  flight.promise=Promise.resolve().then(async()=>{
   requireActive();const unanswered=host.client.authorities.outbox.filter(entry=>entry.submission.tenantId===host.client.connection.tenant&&entry.submission.principalId===host.client.connection.principal&&entry.submission.target?.type===type&&entry.submission.target.id===id&&waiting(entry.state));
   if(unanswered.some(entry=>entry.submission.schema?.name!==schema||entry.submission.expectedRevision!==revision||entry.submission.payload!==btoa("{}")||entry.submission.evidenceFactIds?.length))throw Error("An earlier decision for this record is unanswered. Send unanswered decisions again before creating another.");
   const entry=unanswered.at(-1);if(entry){await host.resend();return confirmedDecision(host.client.authorities.outbox,host.client.connection.tenant,entry.submission.idempotencyKey??"");}
   let refusal="The work decision was not confirmed.";
   const accepted=await host.decide(schema,{type,id},{},{expectedRevision:revision,onRefused:reason=>refusal=reason});if(!accepted)throw Error(refusal);return true;
  }).finally(()=>{if(targets!.get(key)===flight)targets!.delete(key);});
  return await flight.promise&&active();
 };
 return {
  async approval(task:Api.InboxTask){requireActive();const id=approvalTaskID(task);if(!id)throw Error("This task is not an original approval request.");const view=await host.source.get("work.approval",id);requireActive();const request=originalApproval(task,view.record),offered=await host.source.get("work.task",task.id);requireActive();const confirmed=approvalTasks([offered.record as Api.InboxTask],host.me.principalId);if(confirmed.length!==1||confirmed[0]!.id!==task.id||confirmed[0]!.ref!==task.ref)throw Error("The offered approval task is no longer available to this member.");return request;},
  async decideApproval(task:Api.InboxTask,request:Api.ApprovalRequest,decision:"approve"|"reject"){
   requireActive();originalApproval(task,request);
   const reason=approvalDecisionReason(request,host.me.principalId);if(reason)throw Error(reason);
   return answer(`work.approval.${decision}`,"work.approval",request.id,request.revision);
  },
  async readNotification(item:Api.Notification){requireActive();originalNotifications([item],host.me.principalId);return item.read?true:answer("platform.notification.read","platform.notification",item.id);},
 };
}
