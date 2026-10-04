import {pageUIManifest} from "@platform/kernel";
import type {AttachedFile} from "./Records";

export function attachmentIdentity(attachment:AttachedFile,scope?:string){return JSON.stringify([scope,attachment.id,attachment.name,attachment.size,attachment.contentType,attachment.hash]);}
export function attachmentBytesError(attachment:AttachedFile,blob:Blob):string|undefined {
 if(!attachment||typeof attachment.id!=="string"||!attachment.id||typeof attachment.name!=="string"||!attachment.name.trim()||typeof attachment.contentType!=="string"||!Number.isSafeInteger(attachment.size)||attachment.size<0||attachment.archived)return "Attachment metadata is unavailable or incompatible.";
 if(!blob||typeof blob.arrayBuffer!=="function"||blob.size!==attachment.size)return "Attachment bytes do not match the confirmed metadata.";
 if(blob.size>pageUIManifest.runtime.collaboration.maxPreviewBytes)return "Attachment bytes exceed the preview budget.";
 return undefined;
}
export function mediaType(value:string){return value.split(";",1)[0]!.trim().toLowerCase();}
const previewErrors=new Set(["Attachment bytes do not match the confirmed hash.","Attachment metadata is unavailable or incompatible.","Attachment bytes do not match the confirmed metadata.","Attachment bytes exceed the preview budget.","Attachment bytes do not match the confirmed media type.","Raster bytes are invalid or do not match the confirmed media type.","Raster dimensions exceed the preview budget.","This browser cannot decode a bounded raster preview.","Decoded raster dimensions do not match its header.","This browser does not support the required PDF rendering APIs.","PDF bytes are invalid or do not match the confirmed media type.","The PDF page count exceeds the preview budget.","PDF page dimensions exceed the preview budget.","This PDF requires resources outside its attachment and cannot be previewed.","The bundled PDF font could not be loaded.","Password-protected PDFs cannot be previewed."]);
export function previewFailure(error:unknown,fallback:string){return error instanceof Error&&previewErrors.has(error.message)?error.message:fallback;}

/** Header dimensions are checked before a decoder can allocate a raster. Decoding still confirms the bytes. */
export function rasterDimensions(bytes:Uint8Array,type:string):{width:number;height:number}|undefined {
 const match=(at:number,text:string)=>bytes.length>=at+text.length&&[...text].every((char,index)=>bytes[at+index]===char.charCodeAt(0));
 const u16=(at:number)=>bytes[at]!<<8|bytes[at+1]!,u32=(at:number)=>(bytes[at]!*0x1000000+(bytes[at+1]!<<16)+(bytes[at+2]!<<8)+bytes[at+3]!),le16=(at:number)=>bytes[at]!|bytes[at+1]!<<8,le24=(at:number)=>bytes[at]!|bytes[at+1]!<<8|bytes[at+2]!<<16;
 if(type==="image/png"&&bytes.length>=33&&[137,80,78,71,13,10,26,10].every((n,index)=>bytes[index]===n)&&u32(8)===13&&match(12,"IHDR"))return {width:u32(16),height:u32(20)};
 if(type==="image/gif"&&bytes.length>=13&&(match(0,"GIF87a")||match(0,"GIF89a")))return {width:le16(6),height:le16(8)};
 if(type==="image/webp"&&bytes.length>=30&&match(0,"RIFF")&&match(8,"WEBP")){
  if(match(12,"VP8X"))return {width:le24(24)+1,height:le24(27)+1};
  if(match(12,"VP8L")&&bytes[20]===0x2f){const packed=le24(21)+bytes[24]!*0x1000000;return {width:(packed&0x3fff)+1,height:((packed>>>14)&0x3fff)+1};}
  if(match(12,"VP8 ")&&bytes[23]===0x9d&&bytes[24]===1&&bytes[25]===0x2a)return {width:le16(26)&0x3fff,height:le16(28)&0x3fff};
 }
 if(type==="image/jpeg"&&bytes.length>=4&&bytes[0]===0xff&&bytes[1]===0xd8){
  let at=2;while(at<bytes.length){if(bytes[at++]!==0xff)return;while(bytes[at]===0xff)at++;const marker=bytes[at++];if(marker===undefined||marker===0xda||marker===0xd9)return;if(marker===0xd8||marker===1||marker>=0xd0&&marker<=0xd7)continue;if(at+2>bytes.length)return;const length=u16(at);if(length<2||at+length>bytes.length)return;if([0xc0,0xc1,0xc2,0xc3,0xc5,0xc6,0xc7,0xc9,0xca,0xcb,0xcd,0xce,0xcf].includes(marker)){if(length<8)return;return {width:u16(at+5),height:u16(at+3)};}at+=length;}
 }
 return undefined;
}
