import { renderingLimits } from "../gen/presentation-limits";
import { BreadcrumbItem, Breadcrumbs } from "@astryxdesign/core/Breadcrumbs";
import { t } from "../i18n";

export type BreadcrumbTrailItem={id:string;label:string;detail?:string};
export type BreadcrumbTrailProps={items:readonly BreadcrumbTrailItem[];currentID:string;onActivate?:(item:BreadcrumbTrailItem)=>void;onCurrentActivate?:()=>void;enabled?:boolean;label?:string};

export function BreadcrumbTrail({items,currentID,onActivate,onCurrentActivate,enabled=true,label}:BreadcrumbTrailProps) {
 const budget=renderingLimits.runtime.contextViews.maxLabelBytes,bytes=(value:string)=>new TextEncoder().encode(value).length;
 if(!Array.isArray(items)||items.length===0||new Set(items.map(item=>item?.id)).size!==items.length||!items.some(item=>item?.id===currentID)||items.some(item=>!item||typeof item.id!=="string"||!item.id||typeof item.label!=="string"||bytes(item.label)>budget||item.detail!==undefined&&(typeof item.detail!=="string"||bytes(item.detail)>budget)))return <p role="alert">{t("The original breadcrumb trail is unavailable or incompatible.")}</p>;
 return <Breadcrumbs label={label??t("Breadcrumbs")} variant="supporting">
  {items.map(item=>{
   const current=item.id===currentID;
   const content=<><span className="block [overflow-wrap:anywhere]">{item.label}</span>{item.detail!==undefined&&<span className="block break-all [overflow-wrap:anywhere] font-mono text-xs text-muted">{item.detail}</span>}</>;
   return <BreadcrumbItem key={item.id} isCurrent={current}
    onClick={!enabled?undefined:current?onCurrentActivate:onActivate?()=>onActivate(item):undefined}>
    {content}
   </BreadcrumbItem>;
  })}
 </Breadcrumbs>;
}
