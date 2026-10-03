import {useState,type KeyboardEvent} from "react";
import {pageUIManifest} from "@platform/kernel";
import {Button} from "../primitives/button";
import {t} from "../i18n";

type ChoiceOption={value:string;label:string};
type SelectorProps={options:readonly ChoiceOption[];value:string;label:string;enabled?:boolean;onChange?:(value:string)=>void};
function validOptions(options:readonly ChoiceOption[]) {
 const limits=pageUIManifest.runtime.choiceInput,bytes=(value:string)=>new TextEncoder().encode(value).length;
 return Array.isArray(options)&&options.length>0&&options.length<=limits.maxOptions&&new Set(options.map(option=>option?.value)).size===options.length&&options.every(option=>option&&typeof option.value==="string"&&option.value!==""&&bytes(option.value)<=limits.maxOptionBytes&&typeof option.label==="string"&&bytes(option.label)<=limits.maxOptionBytes);
}
function moveFocus(event:KeyboardEvent<HTMLElement>) {
 if(!["ArrowLeft","ArrowRight","Home","End"].includes(event.key)||!(event.target instanceof HTMLButtonElement))return;
 const buttons=Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)")),at=buttons.indexOf(event.target);
 if(at<0||!buttons.length)return;
 event.preventDefault();buttons[event.key==="Home"?0:event.key==="End"?buttons.length-1:(at+(event.key==="ArrowRight"?1:-1)+buttons.length)%buttons.length]?.focus();
}
function CurrentChoice({value,known}:{value:string;known:boolean}) {
 return !known?<p role="status" className="break-words text-xs text-muted">{value===""?t("No current choice is selected."):t("Current choice is outside the configured options: {value}.",{value})}</p>:null;
}

/** Caller-owned ordered choice state. The circles describe position, not business workflow progress. */
export function StepSelector({options,value,label,enabled=true,onChange}:SelectorProps) {
 if(!validOptions(options))return <p role="alert">{t("Choice configuration is unsupported.")}</p>;
 const current=options.findIndex(option=>option.value===value),writable=enabled&&!!onChange;
 return <div className="grid min-w-0 gap-2"><ol aria-label={label} className="flex min-w-0 list-none items-start overflow-x-auto" onKeyDown={moveFocus}>{options.map((option,index)=><li key={option.value} className="flex min-w-32 flex-1 items-start"><Button aria-label={option.label||t("Step {number}",{number:index+1})} aria-current={index===current?"step":undefined} aria-pressed={index===current} disabled={!writable} variant="ghost" className="h-auto w-24 max-w-24 shrink-0 flex-col whitespace-normal px-2 py-1 text-xs" onClick={()=>{if(writable)onChange!(option.value);}}><span aria-hidden="true" className={"flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-bold "+(current>=0&&index<=current?"bg-primary text-primary-foreground":"bg-row-hover text-muted")}>{index+1}</span><span className={"min-w-0 max-w-full break-words "+(index===current?"font-bold":"font-medium")}>{option.label}</span></Button>{index<options.length-1&&<span aria-hidden="true" className={"mt-4 h-0.5 min-w-4 flex-1 "+(current>=0&&index<current?"bg-primary":"bg-border")}/>}</li>)}</ol><CurrentChoice value={value} known={current>=0}/></div>;
}

/** Tabs select one original string identity. They do not invent panel content or navigate pages. */
export function TabSelector({options,value,label,enabled=true,onChange}:SelectorProps) {
 const [focused,setFocused]=useState<string>();
 if(!validOptions(options))return <p role="alert">{t("Choice configuration is unsupported.")}</p>;
 const current=options.findIndex(option=>option.value===value),writable=enabled&&!!onChange,focusable=options.some(option=>option.value===focused)?focused:options[current>=0?current:0]!.value;
 return <div className="grid min-w-0 gap-2"><div role="tablist" aria-label={label} aria-orientation="horizontal" className="flex min-w-0 overflow-x-auto border-b border-border" onKeyDown={moveFocus}>{options.map((option,index)=><Button key={option.value} role="tab" aria-label={option.label||t("Tab {number}",{number:index+1})} aria-selected={value===option.value} tabIndex={writable&&focusable===option.value?0:-1} disabled={!writable} variant="ghost" className={"h-auto max-w-64 shrink-0 whitespace-normal rounded-none border-0 border-b-2 px-3 py-2 text-xs "+(value===option.value?"border-primary font-semibold":"border-transparent text-muted")} onFocus={()=>setFocused(option.value)} onClick={()=>{if(writable)onChange!(option.value);}}>{option.label}</Button>)}</div><CurrentChoice value={value} known={current>=0}/></div>;
}
