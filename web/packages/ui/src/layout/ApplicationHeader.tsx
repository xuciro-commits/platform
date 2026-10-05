import type {Api} from "@platform/kernel";
import {useState,type ReactNode} from "react";
import {PanelLeftClose,PanelLeftOpen,Boxes} from "lucide-react";
import {Button} from "../primitives/button";
import {Select} from "../primitives/input";
import {t} from "../i18n";
import {cn} from "../lib/cn";

/** Authorized pages and callbacks are supplied by the application owner. */
export function ApplicationHeader({header,pages,currentPage,onPage,onRefresh,onTheme,children,collapsed:controlledCollapsed,onCollapsedChange}:{header:Api.ApplicationHeader;pages:{name:string;title:string}[];currentPage:string;onPage:(name:string)=>void;onRefresh?:()=>void;onTheme:()=>void;children:ReactNode;collapsed?:boolean;onCollapsedChange?:(collapsed:boolean)=>void}){
 const [localCollapsed,setCollapsed]=useState(header.collapsed??false),collapsed=controlledCollapsed??localCollapsed,vertical=header.variant==="vertical";
 const render=(item:Api.ApplicationHeaderItem,index:number)=>{
  const shown=(item.pages??[]).flatMap(name=>{const page=pages.find(p=>p.name===name);return page?[page]:[];});
  if(item.kind==="logo")return header.logo?<img key={index} src={header.logo} alt="" referrerPolicy="no-referrer" className="size-7 object-contain"/>:<Boxes key={index} aria-hidden className="size-5 shrink-0 text-primary"/>;
  if(item.kind==="title")return <strong key={index} className={cn("truncate text-sm",vertical&&collapsed&&"sr-only")}>{header.title}</strong>;
  if(item.kind==="spacer")return <span key={index} className={vertical?"flex-1":"ml-auto"}/>;
  if(item.kind==="text")return <span key={index} className={cn("text-xs text-muted",vertical&&collapsed&&"sr-only")}>{item.text}</span>;
  if(item.kind==="button")return <Button key={index} size="sm" disabled={item.action==="refresh"&&!onRefresh} onClick={item.action==="refresh"?onRefresh:onTheme} title={item.label}>{vertical&&collapsed?item.label?.slice(0,1):item.label}</Button>;
  if(item.kind==="tabs")return <nav key={index} aria-label={t("Application pages")} className={cn("min-w-0",vertical?"grid gap-1":"flex flex-wrap items-center gap-1")}><Select className="lg:hidden" aria-label={t("Application page")} value={currentPage} onChange={e=>onPage(e.target.value)}>{shown.map(p=><option key={p.name} value={p.name}>{p.title}</option>)}</Select>{shown.map(p=><Button key={p.name} size="sm" variant="ghost" className={cn("hidden lg:inline-flex",p.name===currentPage&&"bg-row-selected text-primary")} aria-current={p.name===currentPage?"page":undefined} title={p.title} onClick={()=>onPage(p.name)}>{vertical&&collapsed?p.title.slice(0,1):p.title}</Button>)}</nav>;
  return null;
 };
 return <div className={cn("min-w-0",vertical?"flex gap-3":"grid gap-3")}><header className={cn("flex gap-2 rounded-md border border-border bg-surface p-3",vertical?"shrink-0 flex-col": "flex-wrap items-center",vertical&&(collapsed?"w-16":"w-52"))}>{header.items.map(render)}{vertical&&<Button size="sm" variant="ghost" aria-label={t(collapsed?"Expand application navigation":"Collapse application navigation")} aria-expanded={!collapsed} onClick={()=>onCollapsedChange?onCollapsedChange(!collapsed):setCollapsed(!collapsed)}>{collapsed?<PanelLeftOpen/>:<PanelLeftClose/>}</Button>}</header><div className="min-w-0 flex-1">{children}</div></div>;
}
