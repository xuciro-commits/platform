import {useEffect,useRef,useState} from "react";
import {pageUIManifest} from "@platform/kernel";
import {t} from "../i18n";
import {validStaticImage} from "./static-image";
export {validStaticImage,validStaticImageURL,type StaticImageConfig} from "./static-image";

export type StaticImageProps={src?:string;alt:string;caption?:string;height?:number};

/** Browser image loading is independent of authenticated attachment reads. */
function ImageFrame({src,alt,height}:{src:string;alt:string;height:number}) {
 const [state,setState]=useState<"loading"|"loaded"|"error">("loading"),active=useRef(true);
 useEffect(()=>{active.current=true;return()=>{active.current=false;};},[]);
 return <><img src={src} alt={alt} referrerPolicy="no-referrer" className="block w-full object-cover" style={{height}} onLoad={()=>{if(active.current)setState("loaded");}} onError={()=>{if(active.current)setState("error");}}/>{state!=="loaded"&&<p role={state==="error"?"alert":"status"} className="break-words p-2 text-xs text-muted">{t(state==="error"?"The configured image could not be loaded.":"Loading configured image…")}</p>}</>;
}

export function StaticImage({src,alt,caption,height}:StaticImageProps) {
 if(typeof alt!=="string"||!validStaticImage({url:src,caption,height}))return <p role="alert">{t("The static image URL, caption or height is unavailable or incompatible.")}</p>;
 const originalHeight=height??pageUIManifest.runtime.contextViews.defaultImageHeight;
 return <figure className="min-w-0 overflow-hidden rounded-md border border-border bg-surface">{src?<ImageFrame key={src} src={src} alt={alt} height={originalHeight}/>:<div style={{height:originalHeight}} className="flex items-center justify-center overflow-hidden text-xs text-muted"><p role="status" className="p-2">{t("No image URL configured")}</p></div>}{caption&&<figcaption className="break-words px-2 py-1 text-xs text-muted">{caption}</figcaption>}</figure>;
}
