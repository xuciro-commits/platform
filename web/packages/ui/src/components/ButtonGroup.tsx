import {ArrowRight,Pencil,Plus,Trash2} from "lucide-react";
import type {Api} from "@platform/kernel";
import {Button,type ButtonProps} from "../primitives/button";
import {FlowLayout} from "../layout/FlowLayout";

/** Stable presentation controls; the caller owns bindings and execution. */
export function ButtonGroup({buttons,label,onActivate,enabled=true,isBound}:{buttons:Api.PageButton[];label:string;onActivate:(id:string)=>void;enabled?:boolean;isBound:(id:string)=>boolean}){
 const icons={arrow:ArrowRight,edit:Pencil,plus:Plus,trash:Trash2};
 return <FlowLayout toolbar label={label}>{buttons.map(b=>{const Icon=icons[b.icon as keyof typeof icons];return <Button key={b.id} variant={(b.variant||"default") as ButtonProps["variant"]} disabled={!enabled||!isBound(b.id)} onClick={()=>onActivate(b.id)}>{Icon&&<Icon aria-hidden/>}{b.title}</Button>;})}</FlowLayout>;
}
