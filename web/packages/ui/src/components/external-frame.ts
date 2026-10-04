import {pageUIManifest} from "@platform/kernel";

export type ExternalFrameConfig={url:string;origin:string;height?:number};
/** Exact reviewed origin; never repair an ambiguous source URL. */
export function validExternalFrame(value:unknown):value is ExternalFrameConfig {
 if(!value||typeof value!=="object"||Array.isArray(value))return false;
 const c=value as Record<string,unknown>;
 if(Object.keys(c).some(k=>!["url","origin","height"].includes(k))||typeof c.url!=="string"||typeof c.origin!=="string"||c.url.length>2048||!/^[\x21-\x7e]+$/.test(c.url)||c.url.includes("\\")||!/^https:\/\/[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?$/.test(c.origin)||!c.url.startsWith(c.origin+"/"))return false;
 try {const u=new URL(c.url);if(u.origin!==c.origin||u.username||u.password||u.href!==c.url)return false;}catch{return false;}
 return c.height===undefined||typeof c.height==="number"&&Number.isInteger(c.height)&&c.height>=1&&c.height<=pageUIManifest.layout.maxSize;
}
