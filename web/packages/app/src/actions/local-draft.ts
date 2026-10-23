import type {ActionDeclaration,LocalFormDraft} from "@platform/kernel";
import type {Host} from "../index";
import {actionInputSignature} from "./refusal";

export function actionDraftContext(host:Host,action:ActionDeclaration){
 const application=host.client.prepared?.current();if(!application)return;
 const definition=host.definitions.find(item=>item.ref.app===application.binding.ref.app&&item.ref.kind==="app"&&item.ref.name===application.binding.ref.name);
 if(definition?.version!==application.binding.sourceVersion)return;
 const ownsTarget=definition.application?.resources?.some(ref=>ref.kind==="object"&&ref.name===action.target)||host.definitions.some(item=>item.page?.object.name===action.target&&definition.application?.pages.includes(item.ref.name));
 if(!ownsTarget)return;
 return {application:application.id,binding:application.binding};
}
export function actionDraftKey(context:NonNullable<ReturnType<typeof actionDraftContext>>,action:ActionDeclaration,target:{type:string;id:string},fixed:Record<string,unknown>={}){
 return JSON.stringify([context.application,context.binding.ref.app,context.binding.ref.name,context.binding.sourceVersion,action.schema,action.schemaVersion?.version??null,action.schemaVersion?.family??1,target.type,target.id,actionInputSignature(fixed)]);
}
export function completedLocalDraft(host:Host,draft:LocalFormDraft){
 return host.client.authorities.outbox.some(entry=>entry.state==="SUBMISSION_STATE_CONFIRMED"&&entry.submission.schema?.name===draft.schema&&entry.submission.target?.type===draft.target.type&&entry.submission.target.id===draft.target.id&&(entry.submission.expectedRevision??0)===(draft.expectedRevision??0));
}
export function actionDrafts(host:Host,action:ActionDeclaration,targetType:string,targetID?:string){
 const context=actionDraftContext(host,action);if(!context)return [];
 return (host.client.prepared?.forms()??[]).filter(draft=>draft.application===context.application&&draft.binding.sourceVersion===context.binding.sourceVersion&&draft.binding.ref.app===context.binding.ref.app&&draft.binding.ref.name===context.binding.ref.name&&draft.schema===action.schema&&draft.schemaVersion?.version===action.schemaVersion?.version&&draft.schemaVersion?.family===action.schemaVersion?.family&&draft.target.type===targetType&&(!targetID||draft.target.id===targetID)&&!completedLocalDraft(host,draft));
}
