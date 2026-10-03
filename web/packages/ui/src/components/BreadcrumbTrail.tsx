import {pageUIManifest} from "@platform/kernel";
import {Button} from "../primitives/button";
import {t} from "../i18n";

export type BreadcrumbTrailItem={id:string;label:string;detail?:string};
export type BreadcrumbTrailProps={items:readonly BreadcrumbTrailItem[];currentID:string;onActivate?:(item:BreadcrumbTrailItem)=>void;onCurrentActivate?:()=>void;enabled?:boolean;label?:string};

/** Stable original navigation identities; the caller owns navigation and selection clearing. */
export function BreadcrumbTrail({items,currentID,onActivate,onCurrentActivate,enabled=true,label}:BreadcrumbTrailProps) {
 const budget=pageUIManifest.runtime.contextViews.maxLabelBytes,bytes=(value:string)=>new TextEncoder().encode(value).length;
 if(!Array.isArray(items)||items.length===0||new Set(items.map(item=>item?.id)).size!==items.length||!items.some(item=>item?.id===currentID)||items.some(item=>!item||typeof item.id!=="string"||!item.id||typeof item.label!=="string"||bytes(item.label)>budget||item.detail!==undefined&&(typeof item.detail!=="string"||bytes(item.detail)>budget)))return <p role="alert">{t("The original breadcrumb trail is unavailable or incompatible.")}</p>;
 const originalItems:readonly BreadcrumbTrailItem[]=items;
 return <nav aria-label={label??t("Breadcrumbs")} className="min-w-0 max-w-full"><ol className="flex min-w-0 max-w-full flex-wrap items-center gap-1 text-xs">{originalItems.map((item,index)=>{
  const current=item.id===currentID,content=<><span className="block [overflow-wrap:anywhere]">{item.label}</span>{item.detail!==undefined&&<span className="block break-all [overflow-wrap:anywhere] font-mono text-[11px] text-muted">{item.detail}</span>}</>;
  return <li key={item.id} className="flex min-w-0 max-w-full items-center gap-1">{index>0&&<span aria-hidden="true" className="text-muted">/</span>}{current?onCurrentActivate?<Button aria-current="page" size="sm" variant="link" disabled={!enabled} className="min-w-0 max-w-full whitespace-normal text-left text-xs font-semibold" onClick={()=>{if(enabled)onCurrentActivate();}}>{content}</Button>:<span aria-current="page" className="min-w-0 max-w-full font-semibold">{content}</span>:onActivate?<Button size="sm" variant="link" disabled={!enabled} className="min-w-0 max-w-full whitespace-normal text-left text-xs" onClick={()=>{if(enabled)onActivate(item);}}>{content}</Button>:<span className="min-w-0 max-w-full text-muted">{content}</span>}</li>;
 })}</ol></nav>;
}
