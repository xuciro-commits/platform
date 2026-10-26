import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {AttachedFile} from "./Records";
import {useRasterAttachment} from "./useRasterAttachment";

export type AttachmentPreviewProps={attachment:AttachedFile;blob:Blob;label?:string;scope?:string;onDownload?:()=>void|Promise<void>};

/** Authorized original raster bytes. Executable/vector media are kept as metadata and downloads. */
export function MediaPreview({attachment,blob,label,scope,onDownload}:AttachmentPreviewProps) {
 const {error,supported,url,fail}=useRasterAttachment(attachment,blob,scope);
 return <section aria-label={label??t("Media preview")} className="grid min-w-0 gap-2"><h2 className="break-words text-sm font-semibold">{label??t("Media preview")}</h2><p className="break-words text-sm">{attachment.name}</p><p className="break-all text-xs text-muted">{attachment.id} · {attachment.contentType||t("Unknown media type")} · {t("{size} bytes",{size:attachment.size})}</p>{error?<p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{t(error)}</p>:!supported?<p role="status" className="text-sm text-muted">{t("This file type has no inline media preview. Download the original attachment.")}</p>:url?<img src={url} alt={attachment.name} className="max-h-96 max-w-full object-contain" onError={fail}/>:<p role="status" className="text-xs text-muted">{t("Decoding authorized media bytes…")}</p>}{onDownload&&<div><Button size="sm" onClick={()=>void onDownload()}>{t("Download original attachment")}</Button></div>}</section>;
}
