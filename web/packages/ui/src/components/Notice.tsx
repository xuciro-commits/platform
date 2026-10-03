import {Info,TriangleAlert} from "lucide-react";
import {Card} from "../primitives/card";
import type {Tone} from "./StatusTag";

/** Caller-owned text and semantic tone. No data reads, template evaluation or effects. */
export function Notice({title,label,message,tone,role}:{title?:string;label?:string;role?:"note"|"status"|"alert";message:string;tone:Exclude<Tone,"neutral">}){
 const Icon=tone==="info"||tone==="success"?Info:TriangleAlert;
 return <Card role={role??(tone==="info"||tone==="success"?"status":"alert")} aria-label={label??title} className="flex min-w-0 items-start gap-2 p-3 text-sm" style={{borderLeftColor:`var(--tone-${tone})`,borderLeftWidth:3}}><Icon aria-hidden className="mt-0.5 size-4 shrink-0" style={{color:`var(--tone-${tone})`}}/><div className="grid min-w-0 gap-1">{title&&<strong className="break-words text-xs">{title}</strong>}<p className="whitespace-pre-wrap break-words">{message}</p></div></Card>;
}
