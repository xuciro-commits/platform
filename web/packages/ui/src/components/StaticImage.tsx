import {renderingLimits} from "../gen/presentation-limits";
import {useEffect,useRef,useState} from "react";

import {Card} from "../primitives/card";
import {VStack} from "@astryxdesign/core/Stack";
import {Text} from "@astryxdesign/core/Text";
import {t} from "../i18n";
import {validStaticImage} from "./static-image";
export {validStaticImage,validStaticImageURL,type StaticImageConfig} from "./static-image";

export type StaticImageProps={src?:string;alt:string;caption?:string;height?:number};

function ImageFrame({src,alt,height}:{src:string;alt:string;height:number}) {
 const [state,setState]=useState<"loading"|"loaded"|"error">("loading"),active=useRef(true);
 useEffect(()=>{active.current=true;return()=>{active.current=false;};},[]);
 return <><img src={src} alt={alt} referrerPolicy="no-referrer" className="block w-full object-cover" style={{height}} onLoad={()=>{if(active.current)setState("loaded");}} onError={()=>{if(active.current)setState("error");}}/>{state!=="loaded"&&<Text as="p" role={state==="error"?"alert":"status"} type="supporting">{t(state==="error"?"The configured image could not be loaded.":"Loading configured image…")}</Text>}</>;
}

export function StaticImage({src,alt,caption,height}:StaticImageProps) {
 if(typeof alt!=="string"||!validStaticImage({url:src,caption,height}))return <Text as="p" role="alert">{t("The static image URL, caption or height is unavailable or incompatible.")}</Text>;
 const originalHeight=height??renderingLimits.runtime.contextViews.defaultImageHeight;
 return <Card className="min-w-0 overflow-hidden"><VStack gap={0}>{src?<ImageFrame key={src} src={src} alt={alt} height={originalHeight}/>:<div style={{height:originalHeight}} className="flex items-center justify-center overflow-hidden"><Text as="p" role="status" type="supporting">{t("No image URL configured")}</Text></div>}{caption&&<Text as="div" type="supporting" className="px-2 py-1">{caption}</Text>}</VStack></Card>;
}
