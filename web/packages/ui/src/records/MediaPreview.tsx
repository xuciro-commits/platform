import {useEffect,useState} from "react";
import {pageUIManifest} from "@platform/kernel";
import {Button} from "../primitives/button";
import {t} from "../i18n";
import type {AttachedFile} from "./Records";
import {attachmentBytesError,attachmentIdentity,mediaType,rasterDimensions,previewFailure} from "./attachment-bytes";

export type AttachmentPreviewProps={attachment:AttachedFile;blob:Blob;label?:string;scope?:string;onDownload?:()=>void|Promise<void>};

/** Authorized original raster bytes. Executable/vector media are kept as metadata and downloads. */
export function MediaPreview({attachment,blob,label,scope,onDownload}:AttachmentPreviewProps) {
 const identity=attachmentIdentity(attachment,scope),type=mediaType(attachment.contentType),metadataError=attachmentBytesError(attachment,blob),supported=["image/png","image/jpeg","image/gif","image/webp"].includes(type);
 const [loaded,setLoaded]=useState<{identity:string;blob:Blob;url?:string;error?:string}>(),current=loaded?.identity===identity&&loaded.blob===blob?loaded:undefined;
 useEffect(()=>{let active=true,url:string|undefined;setLoaded(undefined);if(metadataError||!supported)return;
  void(async()=>{try{if(blob.type&&mediaType(blob.type)!==type)throw Error("Attachment bytes do not match the confirmed media type.");const bytes=new Uint8Array(await blob.arrayBuffer());if(!active)return;const dimensions=rasterDimensions(bytes,type),limit=pageUIManifest.runtime.collaboration.maxImagePixels;if(!dimensions||dimensions.width<=0||dimensions.height<=0)throw Error("Raster bytes are invalid or do not match the confirmed media type.");if(dimensions.width*dimensions.height>limit)throw Error("Raster dimensions exceed the preview budget.");if(typeof createImageBitmap!=="function")throw Error("This browser cannot decode a bounded raster preview.");const decoded=await createImageBitmap(blob);try{if(decoded.width!==dimensions.width||decoded.height!==dimensions.height)throw Error("Decoded raster dimensions do not match its header.");}finally{decoded.close();}if(!active)return;url=URL.createObjectURL(blob);setLoaded({identity,blob,url});}catch(error){if(active)setLoaded({identity,blob,error:previewFailure(error,"Raster bytes could not be decoded.")});}})();
  return()=>{active=false;if(url)URL.revokeObjectURL(url);};
 },[identity,blob,type,metadataError,supported]);
 const error=metadataError??current?.error;
 return <section aria-label={label??t("Media preview")} className="grid min-w-0 gap-2"><h2 className="break-words text-sm font-semibold">{label??t("Media preview")}</h2><p className="break-words text-sm">{attachment.name}</p><p className="break-all text-xs text-muted">{attachment.id} · {attachment.contentType||t("Unknown media type")} · {t("{size} bytes",{size:attachment.size})}</p>{error?<p role="alert" className="break-words text-sm text-[var(--tone-danger)]">{t(error)}</p>:!supported?<p role="status" className="text-sm text-muted">{t("This file type has no inline media preview. Download the original attachment.")}</p>:current?.url?<img src={current.url} alt={attachment.name} className="max-h-96 max-w-full object-contain" onError={()=>setLoaded({identity,blob,error:"Raster bytes could not be decoded."})}/>:<p role="status" className="text-xs text-muted">{t("Decoding authorized media bytes…")}</p>}{onDownload&&<div><Button size="sm" onClick={()=>void onDownload()}>{t("Download original attachment")}</Button></div>}</section>;
}
