import {pageUIManifest} from "@platform/kernel";

export type StaticImageConfig={url?:string;caption?:string;height?:number};
const forbidden=/[\p{White_Space}\p{Cc}\\]/u;
const bytes=(value:string)=>new TextEncoder().encode(value).length;

/** A configured public URL, before browser URL repair. Empty URLs belong to the explicit placeholder. */
export function validStaticImageURL(value:unknown):value is string {
 if(typeof value!=="string"||!value||bytes(value)>pageUIManifest.runtime.contextViews.maxURLBytes||forbidden.test(value))return false;
 let decoded:string;try{decoded=decodeURIComponent(value);}catch{return false;}
 if(forbidden.test(decoded))return false;
 if(value.startsWith("/"))return !value.startsWith("//")&&!decoded.startsWith("//");
 const authority=/^https?:\/\/([^/?#]+)(?:[/?#]|$)/i.exec(value)?.[1];
 if(!authority||authority.includes("@")||authority.endsWith(":"))return false;
 try{const url=new URL(value);return (url.protocol==="http:"||url.protocol==="https:")&&!!url.hostname&&!url.username&&!url.password;}catch{return false;}
}

/** Shared by the presenter and source compiler; it never loads or rewrites a URL. */
export function validStaticImage(value:unknown):value is StaticImageConfig {
 if(!value||typeof value!=="object"||Array.isArray(value))return false;
 const config=value as Record<string,unknown>,limits=pageUIManifest.runtime.contextViews;
 return Object.keys(config).every(key=>["url","caption","height"].includes(key))
  &&(config.url===undefined||config.url===""||validStaticImageURL(config.url))
  &&(config.caption===undefined||typeof config.caption==="string"&&bytes(config.caption)<=limits.maxCaptionBytes)
  &&(config.height===undefined||typeof config.height==="number"&&Number.isFinite(config.height)&&config.height>=0&&config.height<=limits.maxImageHeight);
}
