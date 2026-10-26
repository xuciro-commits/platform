/// <reference path="./pdf-worker-url.d.ts" />
import workerURL from "pdfjs-dist/build/pdf.worker.mjs?url";
import {pageUIManifest} from "@platform/kernel";
import {pdfStandardFonts} from "./pdf-fonts";

export type PdfDocument={pages:number;render:(page:number,canvas:HTMLCanvasElement,signal:AbortSignal)=>Promise<void>;destroy:()=>Promise<void>};
const abort=()=>new DOMException("PDF preview ended","AbortError");

/** Only the display API and our bundled same-version worker receive original authorized bytes. */
export async function loadPdfDocument(blob:Blob,signal:AbortSignal):Promise<PdfDocument> {
 const limits=pageUIManifest.runtime.collaboration;
 if(typeof Worker!=="function"||typeof (Promise as unknown as {withResolvers?:unknown}).withResolvers!=="function"||typeof (Map.prototype as unknown as {getOrInsertComputed?:unknown}).getOrInsertComputed!=="function"||typeof (Math as unknown as {sumPrecise?:unknown}).sumPrecise!=="function"||typeof (Uint8Array as unknown as {fromBase64?:unknown}).fromBase64!=="function")throw Error("This browser does not support the required PDF rendering APIs.");
 if(blob.size>limits.maxPreviewBytes)throw Error("Attachment bytes exceed the preview budget.");
 const bytes=new Uint8Array(await blob.arrayBuffer());if(signal.aborted)throw abort();
 if(!/^%PDF-[12]\.\d/.test(String.fromCharCode(...bytes.subarray(0,8))))throw Error("PDF bytes are invalid or do not match the confirmed media type.");
 const pdfjs=await import("pdfjs-dist");if(signal.aborted)throw abort();
 const thread=new Worker(workerURL,{type:"module"}),worker=pdfjs.PDFWorker.create({port:thread});let externalResource=false,resourceError=false,destroyed=false,documentReady=false;
 class LocalRendererResources {async fetch({kind,filename}:{kind:string;filename:string}){const url=kind==="standardFontDataUrl"?pdfStandardFonts.get(filename):undefined;if(!url){externalResource=true;throw Error("PDF resources outside the attachment are unavailable.");}try{const response=await fetch(url,{signal});if(!response.ok)throw Error("The bundled PDF font could not be loaded.");return new Uint8Array(await response.arrayBuffer());}catch(error){resourceError=true;throw error;}}}
 const task=pdfjs.getDocument({data:bytes,worker,stopAtErrors:true,useWorkerFetch:false,BinaryDataFactory:LocalRendererResources,useWasm:false,enableXfa:false,disableRange:true,disableStream:true,disableAutoFetch:true,useSystemFonts:false,maxImageSize:limits.maxImagePixels,canvasMaxAreaInBytes:limits.maxPDFPixels*4,isOffscreenCanvasSupported:false,isImageDecoderSupported:false,verbosity:0});
 const destroy=async()=>{if(destroyed)return;destroyed=true;signal.removeEventListener("abort",stop);const disposing=task.destroy();if(!documentReady){void disposing.catch(()=>undefined);worker.destroy();thread.terminate();return;}try{await disposing;}finally{worker.destroy();thread.terminate();}};
 const stop=()=>{void destroy().catch(()=>undefined);};signal.addEventListener("abort",stop,{once:true});
 try {
  let cancelLoading:()=>void=()=>undefined;const ended=new Promise<never>((_,reject)=>{cancelLoading=()=>reject(abort());signal.addEventListener("abort",cancelLoading,{once:true});});
  const document=await Promise.race([task.promise,ended]).finally(()=>signal.removeEventListener("abort",cancelLoading));if(signal.aborted||destroyed)throw abort();documentReady=true;if(!Number.isSafeInteger(document.numPages)||document.numPages<1||document.numPages>limits.maxPDFPages)throw Error("The PDF page count exceeds the preview budget.");
  return {pages:document.numPages,destroy,render:async(page,canvas,renderSignal)=>{
   if(signal.aborted||renderSignal.aborted||destroyed)throw abort();if(!Number.isSafeInteger(page)||page<1||page>document.numPages)throw Error("The PDF page is outside this document.");
   const original=await document.getPage(page);if(signal.aborted||renderSignal.aborted||destroyed)throw abort();
   const ratio=Math.min(2,Math.max(1,globalThis.devicePixelRatio||1)),viewport=original.getViewport({scale:ratio}),width=Math.ceil(viewport.width),height=Math.ceil(viewport.height);
   if(!Number.isSafeInteger(width)||!Number.isSafeInteger(height)||width<=0||height<=0||width*height>limits.maxPDFPixels)throw Error("PDF page dimensions exceed the preview budget.");
   canvas.width=width;canvas.height=height;const rendered=original.render({canvas,viewport,annotationMode:pdfjs.AnnotationMode.DISABLE}),cancel=()=>rendered.cancel();renderSignal.addEventListener("abort",cancel,{once:true});
   try{await rendered.promise;if(signal.aborted||renderSignal.aborted||destroyed)throw abort();if(externalResource)throw Error("This PDF requires resources outside its attachment and cannot be previewed.");if(resourceError)throw Error("The bundled PDF font could not be loaded.");}finally{renderSignal.removeEventListener("abort",cancel);original.cleanup();}
  }};
 }catch(error){await destroy();if(signal.aborted)throw abort();if(error instanceof Error&&error.name==="PasswordException")throw Error("Password-protected PDFs cannot be previewed.");throw error;}
}
