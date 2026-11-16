import {renderingLimits} from "../gen/presentation-limits";
import {type Api} from "@platform/api";
import {File,Folder} from "lucide-react";
import {List, ListItem} from "@astryxdesign/core/List";
import {Text} from "@astryxdesign/core/Text";
import {t} from "../i18n";

export type AssetDirectoryItem={id:string;label:string;asset:Api.AssetBinding;title?:string};
export type AssetDirectoryProps={items:readonly AssetDirectoryItem[];onOpen?:(item:AssetDirectoryItem)=>void;canOpen?:(item:AssetDirectoryItem)=>boolean;enabled?:boolean;label?:string};

export function AssetDirectory({items,onOpen,canOpen,enabled=true,label}:AssetDirectoryProps) {
 const kinds=["object","link-type","property-type","value-type","action","page","app","query","flow","function","compute"];
 if(!Array.isArray(items)||items.length>renderingLimits.runtime.exploration.maxDirectoryItems||new Set(items.map(item=>item?.id)).size!==items.length||items.some(item=>!item||typeof item.id!=="string"||!item.id||typeof item.label!=="string"||item.title!==undefined&&typeof item.title!=="string"||!item.asset||typeof item.asset.ref?.app!=="string"||!item.asset.ref.app||typeof item.asset.ref.name!=="string"||!item.asset.ref.name||!kinds.includes(item.asset.ref.kind)||typeof item.asset.sourceVersion!=="string"||!item.asset.sourceVersion))return <p role="alert">{t("The original asset directory is unavailable or incompatible.")}</p>;
 const originalItems:readonly AssetDirectoryItem[]=items;
 return <section aria-label={label??t("Asset directory")} className="grid min-w-0 gap-2">{!items.length?<Text type="supporting">{t("No mapped assets are available.")}</Text>:<List aria-label={label??t("Asset directory")} density="compact" hasDividers>
  {originalItems.map(item=>{
   const open=onOpen&&item.asset.ref.kind==="page"&&canOpen?.(item)!==false;
   return <ListItem key={item.id} itemKey={item.id} isDisabled={!enabled} onClick={open?()=>{if(enabled&&canOpen?.(item)!==false)onOpen?.(item);}:undefined}
    startContent={item.asset.ref.kind==="app"?<Folder className="size-3"/>:<File className="size-3"/>}
    label={item.label}
    description={<span className="grid min-w-0 gap-0.5"><span className="break-words">{item.title}</span><span className="break-all font-mono text-xs text-muted">{item.asset.ref.app}/{item.asset.ref.kind}/{item.asset.ref.name} · {item.asset.sourceVersion}</span></span>} />;
  })}
 </List>}</section>;
}
