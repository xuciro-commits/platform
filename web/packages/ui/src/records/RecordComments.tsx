import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {RecordComment} from "./Records";

export type RecordCommentsProps={list:readonly RecordComment[];total?:number;loadedCount?:number;label?:string;text:string;onText?:(text:string)=>void;onAdd?:()=>void|Promise<void>;busy?:boolean;error?:string;following?:boolean;onFollow?:(on:boolean)=>void|Promise<void>;followBusy?:boolean};

/** Original persisted comments and a caller-owned draft. The caller confirms writes and clears the draft. */
export function RecordComments({list,total,loadedCount=list.length,label,text,onText,onAdd,busy=false,error,following,onFollow,followBusy=false}:RecordCommentsProps) {
 if(!Array.isArray(list)||new Set(list.map(comment=>comment?.id)).size!==list.length||list.some(comment=>!comment||typeof comment.id!=="string"||!comment.id||typeof comment.text!=="string"||typeof comment.by!=="string"||comment.archived)||!Number.isSafeInteger(loadedCount)||loadedCount<list.length||total!==undefined&&(!Number.isSafeInteger(total)||total<loadedCount))return <p role="alert">{t("The comment window is unavailable or incompatible.")}</p>;
 const writable=!!onText&&!!onAdd;
 return <section aria-label={label??t("Comments")} className="grid min-w-0 gap-2">
  <h2 className="flex min-w-0 flex-wrap items-center gap-2 text-sm font-semibold">{label??t("Comments")}{onFollow&&following!==undefined&&<span className="ml-auto flex min-w-0 flex-wrap items-center gap-2 text-xs font-normal text-muted">{following?t("You follow this record: you hear of its changes and comments"):t("You do not follow this record")}<Button size="sm" variant={following?"ghost":undefined} disabled={followBusy} onClick={()=>{if(!followBusy)void onFollow(!following);}}>{following?t("Unfollow"):t("Follow")}</Button></span>}</h2>
  <p role="status" className="text-xs text-muted">{total===undefined?t("{count} loaded comments; the complete total is unavailable.",{count:loadedCount}):t("Showing {shown} of {total} comments.",{shown:list.length,total})}</p>
  {list.length===0&&<p className="text-xs text-muted">{total===0?t("No comments on this record."):t("No comments in this loaded window.")}</p>}
  <ol className="grid min-w-0 gap-2">{list.map(comment=><li key={comment.id} className="min-w-0 rounded-md border border-border bg-surface p-2 text-sm"><div className="flex flex-wrap gap-x-1 break-words text-xs text-muted"><span>{comment.by}</span>{comment.created?.at&&<><span aria-hidden="true">·</span><time dateTime={comment.created.at}>{comment.created.at}</time></>}<span className="break-all font-mono">· {comment.id}</span></div><p className="whitespace-pre-wrap break-words">{comment.text}</p></li>)}</ol>
  {writable&&<form className="grid min-w-0 gap-2" onSubmit={event=>{event.preventDefault();if(!busy&&text.trim())void onAdd!();}}><textarea aria-label={t("Comment")} className="min-h-16 min-w-0 rounded-md border border-border bg-surface p-2 text-sm" placeholder={t("Write a comment; @member tells them")} value={text} onChange={event=>onText!(event.target.value)}/><div><Button type="submit" size="sm" variant="primary" disabled={busy||!text.trim()}>{busy?t("Submitting comment…"):t("Comment")}</Button></div></form>}
  {error&&<p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{error}</p>}
 </section>;
}
