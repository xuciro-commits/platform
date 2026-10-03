import {useEffect,useRef,useState} from "react";
import {Button,MediaPreview,Panel,PdfViewer,RecordComments,RecordUploader,t,type AttachedFile,type EntityRecord,type RecordComment,type RecordSource} from "@platform/ui";
import {pageUIManifest} from "@platform/kernel";
import {newId,useHost} from "../index";
import {createRecordCollaboration} from "../collaboration/service";
import type {RecordReference} from "../runtime/Session";
import type {VariableResult} from "../runtime/variables";
import type {PageSessionStore} from "../runtime/Session";

type Props={kind:string;label:string;record?:EntityRecord;reference?:RecordReference;status?:"empty"|"pending"|"value"|"error";session?:PageSessionStore;slot?:string;bindingEpoch?:number;readSource?:RecordSource;draft?:VariableResult;fileValue?:VariableResult;pageValue?:VariableResult;onDraft?:(value:string)=>void;onFileID?:(value:string)=>void;onPage?:(value:string)=>void;enabled?:boolean;live:boolean};
type MutationLease={identity:string;active:()=>boolean;service:ReturnType<typeof createRecordCollaboration>};
const scalar=(value?:VariableResult)=>value?.status==="value"&&typeof value.value==="string"?value.value:undefined;
const errorText=(failure:unknown)=>failure instanceof Error?failure.message:String(failure);
/** All four views share the captured original record and the owning services. */
export function RecordCollaborationRenderer({kind,label,record,reference,status,session,slot,bindingEpoch,readSource,draft,fileValue,pageValue,onDraft,onFileID,onPage,enabled,live}:Props) {
 const host=useHost(),source=readSource??host.source,limits=pageUIManifest.runtime.collaboration,target=reference?{type:reference.object,id:reference.id}:undefined,identity=JSON.stringify([host.source.scope,slot,bindingEpoch,target?.type,target?.id]),ready=status==="value"&&source.scope===host.source.scope&&!!record&&record.id===target?.id;
 const current=useRef({identity,text:scalar(draft),file:scalar(fileValue),page:scalar(pageValue),mounted:true});current.current={identity,text:scalar(draft),file:scalar(fileValue),page:scalar(pageValue),mounted:current.current.mounted};
 const [comments,setComments]=useState<{identity:string;records:RecordComment[];total:number}>(),[readError,setReadError]=useState<{identity:string;key?:string;message:string}>(),[attachment,setAttachment]=useState<{key:string;file:AttachedFile}>(),[blob,setBlob]=useState<{key:string;value:Blob}>();
 const [selected,setSelected]=useState<{identity:string;file:File}>(),[busy,setBusy]=useState(false),[mutationError,setMutationError]=useState<{identity:string;message:string}>(),[refresh,setRefresh]=useState(0);
 const mutation=useRef<MutationLease|undefined>(undefined);
 useEffect(()=>{current.current.mounted=true;return()=>{current.current.mounted=false;};},[]);
 const capture=()=>{const lease=session&&slot?session.captureRecordLease(slot):undefined;if(!lease||!target)return undefined;const captured=identity;return ()=>current.current.mounted&&current.current.identity===captured&&lease();};
 const mutationLease=()=>{const prior=mutation.current;if(prior?.identity===identity&&prior.active())return prior;const active=capture();if(!active||!target)return;const next={identity,active,service:createRecordCollaboration({...host,source},target,active,newId)};mutation.current=next;return next;};
 useEffect(()=>{setSelected(undefined);setMutationError(undefined);setBusy(false);if(mutation.current?.identity!==identity)mutation.current=undefined;},[identity]);
 useEffect(()=>{
  if(!ready||kind!=="record-comments"||!target)return;let active=true;setComments(undefined);setReadError(undefined);
  void source.list("platform.comment",{domain:[["target","=",`${target.type}/${target.id}`]],sort:["created","id"],limit:limits.maxCommentsWindow}).then(page=>{
   if(!active)return;
   if(!Array.isArray(page.records)||!Number.isSafeInteger(page.total)||page.total<page.records.length||page.records.length>limits.maxCommentsWindow||page.records.some(comment=>comment.archived||comment.target!==`${target.type}/${target.id}`))throw new Error("The comment window is unavailable or incompatible.");
   setComments({identity,records:page.records as RecordComment[],total:page.total});
  }).catch(failure=>{if(active)setReadError({identity,message:errorText(failure)});});return()=>{active=false;};
 },[identity,ready,kind,source.scope,source.revision,refresh]);
 const fileID=scalar(fileValue)??"",fileKey=JSON.stringify([identity,fileID,source.revision]);
 useEffect(()=>{
  if(kind==="record-comments")return;setAttachment(undefined);setBlob(undefined);setReadError(undefined);
  if(!ready||!fileID||!target)return;let active=true;const stop=new AbortController();
  const service=createRecordCollaboration({...host,source},target,()=>active&&current.current.mounted&&current.current.identity===identity,newId);
  void service.attachment(fileID).then(async file=>{
   if(!active)return;setAttachment({key:fileKey,file});
   if(kind==="record-uploader")return;
   const raster=["image/png","image/jpeg","image/gif","image/webp"].includes(file.contentType.toLowerCase());
   if(kind==="media-preview"&&!raster)return;
   if(kind==="pdf-viewer"&&file.contentType.toLowerCase()!=="application/pdf")throw new Error("The selected attachment is not a PDF.");
   const value=await service.bytes(file,limits.maxPreviewBytes,stop.signal);if(active)setBlob({key:fileKey,value});
  }).catch(failure=>{if(active)setReadError({identity,key:fileKey,message:errorText(failure)});});return()=>{active=false;stop.abort();};
 },[fileKey,ready,kind,refresh]);
 const add=async()=>{
  const text=scalar(draft);if(!text?.trim()||busy)return;const lease=mutationLease();if(!lease)return;const {active,service}=lease;setBusy(true);setMutationError(undefined);
  try {if(await service.addComment(text)){if(active()&&current.current.text===text)onDraft?.("");if(active())setRefresh(n=>n+1);}else if(active())setMutationError({identity,message:t("The comment was not confirmed. Your draft is retained.")});}
  catch(failure){if(active())setMutationError({identity,message:errorText(failure)});}finally{if(active())setBusy(false);}
 };
 const upload=async()=>{
  const file=selected?.identity===identity?selected.file:undefined;if(!file||busy)return;const lease=mutationLease();if(!lease)return;const {active,service}=lease;setBusy(true);setMutationError(undefined);
  try {const fileID=await service.upload(file);if(fileID&&active()){onFileID?.(fileID);if(selected?.file===file)setSelected(undefined);setRefresh(n=>n+1);}}
  catch(failure){if(active())setMutationError({identity,message:errorText(failure)});}finally{if(active())setBusy(false);}
 };
 const download=async()=>{const active=capture();if(!target||!active||attachment?.key!==fileKey)return;try{await createRecordCollaboration({...host,source},target,active,newId).download(attachment.file);}catch(failure){if(active())setMutationError({identity,message:errorText(failure)});}};
 if(status==="error")return <Panel role="alert">{t("The original record could not be confirmed.")}</Panel>;
 if(status==="pending")return <p role="status">{t("Confirming record access…")}</p>;
 if(!ready)return <p role="status">{t("Select a confirmed original record for collaboration.")}</p>;
 const failure=readError?.identity===identity&&(!readError.key||readError.key===fileKey)?readError.message:mutationError?.identity===identity?mutationError.message:undefined,writable=live&&enabled!==false;
 if(kind==="record-comments"){
  if(failure&&comments?.identity!==identity)return <Panel role="alert">{t(failure)}</Panel>;
  if(comments?.identity!==identity)return <p role="status">{t("Loading original comments…")}</p>;
  return <RecordComments label={label} list={comments.records} total={comments.total} text={scalar(draft)??""} onText={writable&&host.can("platform.comment.add")?onDraft:undefined} onAdd={writable&&host.can("platform.comment.add")?add:undefined} busy={busy} error={failure?t(failure):undefined}/>;
 }
 const file=attachment?.key===fileKey?attachment.file:undefined;
 if(kind==="record-uploader")return <RecordUploader label={label} file={selected?.identity===identity?selected.file:undefined} currentFile={file} busy={busy} error={failure?t(failure):undefined} enabled={writable&&host.can("files.file.attach")} onFile={writable&&host.can("files.file.attach")?file=>{setSelected({identity,file});setMutationError(undefined);}:undefined} onUpload={upload} onClear={()=>setSelected(undefined)}/>;
 if(failure)return <Panel role="alert">{t(failure)}</Panel>;
 if(!fileID)return <p role="status">{t("No confirmed attachment selected.")}</p>;
 if(!file)return <p role="status">{t("Confirming original attachment access…")}</p>;
 if(kind==="media-preview"&&!["image/png","image/jpeg","image/gif","image/webp"].includes(file.contentType.toLowerCase()))return <Panel><p>{file.name} · {file.contentType} · {file.size} {t("bytes")}</p><p>{t("This attachment type is available for download.")}</p><Button variant="ghost" onClick={()=>void download()}>{t("Download")}</Button></Panel>;
 if(blob?.key!==fileKey)return <p role="status">{t("Loading authorized file bytes…")}</p>;
 return kind==="pdf-viewer"?<PdfViewer attachment={file} blob={blob.value} label={label} scope={identity} page={scalar(pageValue)??""} onPage={writable?onPage:undefined} onDownload={download}/>:<MediaPreview attachment={file} blob={blob.value} label={label} scope={identity} onDownload={download}/>;
}
