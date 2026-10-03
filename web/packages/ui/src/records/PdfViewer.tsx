import {useEffect,useRef,useState} from "react";
import {Button} from "../primitives/button";
import {Input} from "../primitives/input";
import {t} from "../i18n";
import type {AttachmentPreviewProps} from "./MediaPreview";
import {attachmentBytesError,attachmentIdentity,mediaType,previewFailure} from "./attachment-bytes";
import type {PdfDocument} from "./pdf-engine";

export type PdfViewerProps=AttachmentPreviewProps&{page:string;onPage?:(page:string)=>void};

/** True PDF pages from authenticated bytes, with an original caller-owned one-based page string. */
export function PdfViewer({attachment,blob,label,scope,onDownload,page,onPage}:PdfViewerProps) {
 const identity=attachmentIdentity(attachment,scope),metadataError=attachmentBytesError(attachment,blob)??(mediaType(attachment.contentType)!=="application/pdf"||blob.type&&mediaType(blob.type)!=="application/pdf"?"PDF bytes are invalid or do not match the confirmed media type.":undefined);
 const [loaded,setLoaded]=useState<{identity:string;blob:Blob;document?:PdfDocument;error?:string}>(),canvas=useRef<HTMLCanvasElement>(null),[painted,setPainted]=useState<{document:PdfDocument;page:string;error?:string}>(),current=loaded?.identity===identity&&loaded.blob===blob?loaded:undefined,document=current?.document;
 const parsed=/^[1-9]\d*$/.test(page)?Number(page):NaN,valid=!!document&&Number.isSafeInteger(parsed)&&parsed<=document.pages;
 useEffect(()=>{const controller=new AbortController();let original:PdfDocument|undefined;setLoaded(undefined);setPainted(undefined);if(metadataError)return;
  void import("./pdf-engine").then(engine=>engine.loadPdfDocument(blob,controller.signal)).then(value=>{original=value;if(controller.signal.aborted){void value.destroy().catch(()=>undefined);return;}setLoaded({identity,blob,document:value});},reason=>{if(!controller.signal.aborted)setLoaded({identity,blob,error:previewFailure(reason,"PDF bytes could not be rendered.")});});
  return()=>{controller.abort();if(original)void original.destroy().catch(()=>undefined);};
 },[identity,blob,metadataError]);
 useEffect(()=>{const controller=new AbortController(),target=canvas.current;setPainted(undefined);if(!document||!valid||!target)return;
  void document.render(parsed,target,controller.signal).then(()=>{if(!controller.signal.aborted)setPainted({document,page});},error=>{if(!controller.signal.aborted)setPainted({document,page,error:previewFailure(error,"PDF bytes could not be rendered.")});});return()=>{controller.abort();};
 },[document,page,valid,parsed]);
 const renderResult=painted&&painted.document===document&&painted.page===page?painted:undefined,loadError=metadataError??current?.error,error=loadError??renderResult?.error,shown=!!document&&valid&&!!renderResult&&!error;
 return <section aria-label={label??t("PDF viewer")} className="grid min-w-0 gap-2"><h2 className="break-words text-sm font-semibold">{label??t("PDF viewer")}</h2><p className="break-words text-sm">{attachment.name}</p><p className="break-all text-xs text-muted">{attachment.id} · {attachment.contentType} · {t("{size} bytes",{size:attachment.size})}</p>{loadError?<p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{t(loadError)}</p>:!document?<p role="status" className="text-xs text-muted">{t("Loading authorized PDF bytes…")}</p>:<><div className="flex min-w-0 flex-wrap items-center gap-2"><Button size="sm" disabled={!onPage||!valid||parsed<=1} onClick={()=>{if(onPage&&valid&&parsed>1)onPage(String(parsed-1));}}>{t("Previous PDF page")}</Button><label className="flex min-w-0 items-center gap-1 text-xs">{t("PDF page")}<Input aria-label={t("PDF page")} className="w-20" inputMode="numeric" value={page} disabled={!onPage} aria-invalid={!valid} onChange={event=>onPage?.(event.target.value)}/></label><span className="text-xs tabular-nums">{t("of {pages} actual pages",{pages:document.pages})}</span><Button size="sm" disabled={!onPage||!valid||parsed>=document.pages} onClick={()=>{if(onPage&&valid&&parsed<document.pages)onPage(String(parsed+1));}}>{t("Next PDF page")}</Button></div>{renderResult?.error?<p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{t(renderResult.error)}</p>:!valid?<p role="status" className="text-sm text-muted">{t("The original page value is empty or outside this PDF. Enter a page from 1 to {pages}.",{pages:document.pages})}</p>:!shown&&<p role="status" className="text-xs text-muted">{t("Rendering PDF page {page}…",{page})}</p>}<div className="min-w-0 overflow-auto rounded-md border border-border bg-row-hover p-2"><canvas key={page} ref={canvas} role="img" aria-label={t("PDF page {page} of {pages}",{page,pages:document.pages})} hidden={!shown} className="h-auto max-w-full bg-white"/></div></>}{onDownload&&<div><Button size="sm" onClick={()=>void onDownload()}>{t("Download original attachment")}</Button></div>}</section>;
}
