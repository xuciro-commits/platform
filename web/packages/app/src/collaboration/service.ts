import type {Host} from "../index";
import type {AttachedFile,EntityRecord} from "@platform/ui";
import {confirmedDecision} from "./decision";
import {validImageRegions} from "@platform/ui/image-regions";
import type {Api} from "@platform/kernel";
export type CollaborationTarget={type:string;id:string};
export type CollaborationHost=Pick<Host,"client"|"source"|"can"|"decide"|"resend"|"me">;
/** Use only this captured original record. Retiring a view cannot retarget a queued decision. */
export function createRecordCollaboration(host:CollaborationHost,target:CollaborationTarget,active:()=>boolean,id:(prefix:string)=>string) {
 const about=`${target.type}/${target.id}`;
 const requireActive=()=>{if(!active())throw new Error("The original record scope has ended.");};
 let commentAttempt:{id:string;text:string}|undefined,fileAttempt:{id:string;file:File;bytes?:Awaited<ReturnType<Host["client"]["upload"]>>}|undefined;
 const waiting=(state:string)=>["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_SENDING","SUBMISSION_STATE_UNKNOWN"].includes(state);
 const entryFor=(schema:string,type:string,id:string)=>host.client.authorities.outbox.findLast(entry=>entry.submission.tenantId===host.client.connection.tenant&&entry.submission.principalId===host.client.connection.principal&&entry.submission.schema?.name===schema&&entry.submission.target?.type===type&&entry.submission.target.id===id);
 const otherWaiting=(schema:string)=>host.client.authorities.outbox.some(entry=>{
  if(!waiting(entry.state)||entry.submission.tenantId!==host.client.connection.tenant||entry.submission.principalId!==host.client.connection.principal||entry.submission.schema?.name!==schema)return false;
  try{return JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(entry.submission.payload??""),c=>c.charCodeAt(0)))).target===about;}catch{return false;}
 });
 const submit=async(schema:string,type:string,id:string,payload:unknown,onRefused?:(reason:string)=>void)=>{
  requireActive();const entry=entryFor(schema,type,id);
  if(entry?.state==="SUBMISSION_STATE_CONFIRMED")return true;
  if(entry&&waiting(entry.state)){
   await host.resend();requireActive();
   if(confirmedDecision(host.client.authorities.outbox,host.client.connection.tenant,entry.submission.idempotencyKey??""))return true;
   const latest=entryFor(schema,type,id);if(latest&&waiting(latest.state))throw new Error("The earlier decision is awaiting confirmation. Retry sends the same decision.");
   onRefused?.(latest?.reason??"The decision was refused.");return false;
  }
  if(otherWaiting(schema))throw new Error("An earlier decision for this record is unanswered. Send unanswered decisions again before creating another.");
  return host.decide(schema,{type,id},payload,{expectedRevision:0,onRefused});
 };
 const confirmAttachment=(record:EntityRecord,fileID:string):AttachedFile=>{
  if(record.id!==fileID||record.archived||record.target!==about||typeof record.name!=="string"||typeof record.contentType!=="string"||!Number.isSafeInteger(record.size)||Number(record.size)<0)throw new Error("The file does not belong to the original record.");
  return record as AttachedFile;
 };
 return {
  async addComment(text:string) {
   requireActive();if(!host.can("platform.comment.add"))throw new Error("Commenting is unavailable for this record.");
   if(commentAttempt&&commentAttempt.text!==text){if(waiting(entryFor("platform.comment.add","platform.comment",commentAttempt.id)?.state??""))throw new Error("An earlier decision for this record is unanswered. Send unanswered decisions again before creating another.");commentAttempt=undefined;}
   commentAttempt??={id:id("CMT"),text};
   const confirmed=await submit("platform.comment.add","platform.comment",commentAttempt.id,{target:about,text});
   if(confirmed)commentAttempt=undefined;
   return confirmed&&active();
  },
  async upload(file:File,signal?:AbortSignal) {
   requireActive();if(!host.can("files.file.attach"))throw new Error("Attaching files is unavailable for this record.");
   if(fileAttempt&&fileAttempt.file!==file){if(waiting(entryFor("files.file.attach","files.file",fileAttempt.id)?.state??""))throw new Error("An earlier decision for this record is unanswered. Send unanswered decisions again before creating another.");fileAttempt=undefined;}
   if(!fileAttempt&&otherWaiting("files.file.attach"))throw new Error("An earlier decision for this record is unanswered. Send unanswered decisions again before creating another.");
   fileAttempt??={id:id("FILE"),file};
   const attempt=fileAttempt,up=attempt.bytes??await host.client.upload(file,file.name,{signal});attempt.bytes=up;requireActive();
   const fileID=attempt.id;let refusal="The attachment was not confirmed.";
   const confirmed=await submit("files.file.attach","files.file",fileID,{hash:up.hash,name:up.name,contentType:up.contentType,size:up.size,target:about},reason=>refusal=reason);
   if(!confirmed)throw new Error(refusal);if(!active())return undefined;
   // The accepted attach decision confirms this ID. Metadata is read by the
   // refreshed view; starting a read here would race the decision's revision.
   fileAttempt=undefined;return fileID;
  },
  async attachment(fileID:string) {
   requireActive();const view=await host.source.get("files.file",fileID);requireActive();return confirmAttachment(view.record,fileID);
  },
  async annotate(fileID:string,revision:number,hash:string,regions:Api.ImageRegion[]) {
   requireActive();if(!host.can("files.file.annotate")||!validImageRegions(regions)||typeof hash!=="string"||!/^[a-f0-9]{64}$/.test(hash)||!Number.isSafeInteger(revision)||revision<1)throw new Error("Saving image regions is unavailable or incompatible.");
   const file=await this.attachment(fileID);requireActive();if(file.hash!==hash||file.by!==host.me.principalId)throw new Error("Only the original attachment author can save its image regions.");
   const payload={hash,regions:structuredClone(regions)},encoded=btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(payload))));
   const unanswered=host.client.authorities.outbox.filter(entry=>entry.submission.tenantId===host.client.connection.tenant&&entry.submission.principalId===host.client.connection.principal&&entry.submission.target?.type==="files.file"&&entry.submission.target.id===fileID&&waiting(entry.state));
   if(unanswered.length){if(unanswered.some(entry=>entry.submission.schema?.name!=="files.file.annotate"||entry.submission.expectedRevision!==revision||entry.submission.payload!==encoded||entry.submission.evidenceFactIds?.length))throw new Error("An earlier image decision is unanswered. Retry its original regions before changing the draft.");await host.resend();requireActive();return confirmedDecision(host.client.authorities.outbox,host.client.connection.tenant,unanswered[0]!.submission.idempotencyKey??"");}
   if(file.revision!==revision)throw new Error("The original attachment changed. Reload or resolve the region conflict before saving.");
   let refusal="Image regions were not saved. Your draft is retained.";
   const confirmed=await host.decide("files.file.annotate",{type:"files.file",id:fileID},payload,{expectedRevision:revision,quiet:true,onRefused:reason=>refusal=reason});requireActive();if(!confirmed)throw new Error(refusal);return true;
  },
  async bytes(file:AttachedFile,maxBytes:number,signal?:AbortSignal) {
   requireActive();confirmAttachment(file,file.id);if(file.size>maxBytes)throw new Error("The file exceeds the preview byte budget.");
   const blob=await host.client.download(file.id,{maxBytes,signal});requireActive();if(blob.size!==file.size)throw new Error("The file bytes do not match the original attachment metadata.");return blob;
  },
  async download(file:Pick<AttachedFile,"id"|"name">) {
   const attached=await this.attachment(file.id),blob=await host.client.download(attached.id);requireActive();
   const url=URL.createObjectURL(blob);Object.assign(document.createElement("a"),{href:url,download:attached.name}).click();setTimeout(()=>URL.revokeObjectURL(url),1000);
  },
 };
}
