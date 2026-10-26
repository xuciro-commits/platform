import {useEffect,useState} from "react";
import {pageUIManifest} from "@platform/kernel";
import type {AttachedFile} from "./Records";
import {attachmentBytesError,attachmentIdentity,mediaType,rasterDimensions,previewFailure} from "./attachment-bytes";

/** One bounded decoder and Blob lease for media preview and annotation. */
export function useRasterAttachment(attachment:AttachedFile,blob:Blob,scope?:string){
 const identity=attachmentIdentity(attachment,scope),type=mediaType(attachment.contentType),metadataError=attachmentBytesError(attachment,blob),supported=["image/png","image/jpeg","image/gif","image/webp"].includes(type);
 const [loaded,setLoaded]=useState<{identity:string;blob:Blob;url?:string;width?:number;height?:number;error?:string}>(),current=loaded?.identity===identity&&loaded.blob===blob?loaded:undefined;
 useEffect(()=>{let active=true,url:string|undefined;setLoaded(undefined);if(metadataError||!supported)return;
  void(async()=>{try{if(blob.type&&mediaType(blob.type)!==type)throw Error("Attachment bytes do not match the confirmed media type.");const bytes=new Uint8Array(await blob.arrayBuffer());if(!active)return;if(typeof attachment.hash==="string"){const hash=[...new Uint8Array(await crypto.subtle.digest("SHA-256",bytes))].map(n=>n.toString(16).padStart(2,"0")).join("");if(!active)return;if(hash!==attachment.hash)throw Error("Attachment bytes do not match the confirmed hash.");}const dimensions=rasterDimensions(bytes,type),limit=pageUIManifest.runtime.collaboration.maxImagePixels;if(!dimensions||dimensions.width<=0||dimensions.height<=0)throw Error("Raster bytes are invalid or do not match the confirmed media type.");if(dimensions.width*dimensions.height>limit)throw Error("Raster dimensions exceed the preview budget.");if(typeof createImageBitmap!=="function")throw Error("This browser cannot decode a bounded raster preview.");const decoded=await createImageBitmap(blob);try{if(decoded.width!==dimensions.width||decoded.height!==dimensions.height)throw Error("Decoded raster dimensions do not match its header.");}finally{decoded.close();}if(!active)return;url=URL.createObjectURL(blob);setLoaded({identity,blob,url,...dimensions});}catch(error){if(active)setLoaded({identity,blob,error:previewFailure(error,"Raster bytes could not be decoded.")});}})();
  return()=>{active=false;if(url)URL.revokeObjectURL(url);};
 },[identity,blob,type,metadataError,supported]);
 return {identity,supported,url:current?.url,width:current?.width,height:current?.height,error:metadataError??current?.error,fail:()=>setLoaded({identity,blob,error:"Raster bytes could not be decoded."})};
}
