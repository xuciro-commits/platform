import type {CSSProperties,ReactNode} from "react";
import type {Api} from "@platform/kernel";
import {cn} from "../lib/cn";

export type LayoutSize=Api.PageLayoutSize;

/** One blank region. The document owner validates size; parent gaps stay independent. */
export function Spacer({size}:{size:number}){
 return <div aria-hidden="true" className="min-w-0 shrink-0" style={{height:size}}/>;
}

/** Presentation dimensions only. The document owner validates the budget and
 * parent compatibility; region sizing never changes a child's data or identity. */
export function LayoutRegion({children,size,parent,label,fillHeight,fillWidth}: {children:ReactNode;size?:LayoutSize;parent?:string;label?:string;fillHeight?:boolean;fillWidth?:boolean}) {
 const s=size??{};
 const style:CSSProperties={
  width:s.width===undefined?(fillWidth?"100%":undefined):`min(${s.width}px, 100%)`,
  minWidth:s.minWidth===undefined?0:`min(${s.minWidth}px, 100%)`,maxWidth:s.maxWidth===undefined?"100%":`min(${s.maxWidth}px, 100%)`,
  height:s.height??(fillHeight?"100%":undefined),minHeight:s.minHeight??0,maxHeight:s.maxHeight,
  overflow:s.scroll==="auto"?"auto":undefined,
  flex:parent==="rows"&&s.weight!==undefined?`${s.weight} 1 0%`:parent==="columns"&&s.width===undefined?`${s.weight??1} 1 0%`:"0 0 auto",
 };
 return <div className="platform-layout-region flex min-w-0 flex-col" style={style} role={label?"region":undefined} aria-label={label}>{children}</div>;
}

/** Each Columns group responds to its own container width, including nested
 * groups, overlays and repeated items. Empty/hidden children leave no tracks. */
export function LayoutStack({children,direction,gap=12}: {children:ReactNode;direction:"rows"|"columns";gap?:number}) {
 return <div className="platform-layout-stack min-h-0 min-w-0 flex-1"><div className={cn("flex h-full min-h-0 min-w-0",direction==="columns"?"platform-layout-columns":"flex-col")} style={{gap}}>{children}</div></div>;
}
