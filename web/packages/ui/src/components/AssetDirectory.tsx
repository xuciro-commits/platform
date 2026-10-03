import {pageUIManifest,type Api} from "@platform/kernel";
import {File,Folder} from "lucide-react";
import {Button} from "../primitives/button";
import {t} from "../i18n";

export type AssetDirectoryItem={id:string;label:string;asset:Api.AssetBinding;title?:string};
export type AssetDirectoryProps={items:readonly AssetDirectoryItem[];onOpen?:(item:AssetDirectoryItem)=>void;canOpen?:(item:AssetDirectoryItem)=>boolean;enabled?:boolean;label?:string};

/** Literal directory labels accompany actual retained assets; no URI or resource is invented. */
export function AssetDirectory({items,onOpen,canOpen,enabled=true,label}:AssetDirectoryProps) {
 const kinds=["object","link-type","property-type","action","page","app","query","flow","function","compute"];
 if(!Array.isArray(items)||items.length>pageUIManifest.runtime.exploration.maxDirectoryItems||new Set(items.map(item=>item?.id)).size!==items.length||items.some(item=>!item||typeof item.id!=="string"||!item.id||typeof item.label!=="string"||item.title!==undefined&&typeof item.title!=="string"||!item.asset||typeof item.asset.ref?.app!=="string"||!item.asset.ref.app||typeof item.asset.ref.name!=="string"||!item.asset.ref.name||!kinds.includes(item.asset.ref.kind)||typeof item.asset.sourceVersion!=="string"||!item.asset.sourceVersion))return <p role="alert">{t("The original asset directory is unavailable or incompatible.")}</p>;
 const originalItems:readonly AssetDirectoryItem[]=items;
 return <section aria-label={label??t("Asset directory")} className="grid min-w-0 gap-2">{!items.length?<p className="text-xs text-muted">{t("No mapped assets are available.")}</p>:<ul className="grid min-w-0 gap-1">{originalItems.map(item=>{
  const content=<><span aria-hidden="true" className="shrink-0">{item.asset.ref.kind==="app"?<Folder className="size-3"/>:<File className="size-3"/>}</span><span className="min-w-0 flex-1"><span className="block break-words">{item.label}</span>{item.title!==undefined&&<span className="block break-words text-muted">{item.title}</span>}<span className="block break-all font-mono text-[11px] text-muted">{item.asset.ref.app}/{item.asset.ref.kind}/{item.asset.ref.name} · {item.asset.sourceVersion}</span></span></>;
  return <li key={item.id}>{onOpen&&item.asset.ref.kind==="page"&&canOpen?.(item)!==false?<Button variant="row" disabled={!enabled} className="flex h-auto min-w-0 gap-2 whitespace-normal py-1.5 text-left text-xs" onClick={()=>{if(enabled&&canOpen?.(item)!==false)onOpen(item);}}>{content}</Button>:<div className="flex min-w-0 items-center gap-2 border-b border-border py-1.5 text-xs">{content}</div>}</li>;
 })}</ul>}</section>;
}
