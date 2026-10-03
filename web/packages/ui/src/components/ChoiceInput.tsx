import {useId} from "react";
import {Button} from "../primitives/button";
import {Select} from "../primitives/input";
import {t} from "../i18n";
import {validChoiceInput} from "./choice";
import {StepSelector,TabSelector} from "./IndexedChoices";

/** Choice presentations consume one caller-owned string, without defaulting it. */
export function ChoiceInput({value,options,optionLabels,variant,label,title,disabled,onChange}:{value:string;options:readonly string[];optionLabels?:readonly string[];variant:string;label?:string;title:string;disabled?:boolean;onChange?:(value:string)=>void}){
 const id=useId(),caption=label??"",name=caption.trim()?caption:title;
 if(variant==="multiple"||!validChoiceInput({options,optionLabels,variant,label}))return <p role="alert">{t("Choice configuration is unsupported.")}</p>;
 if(variant==="steps"||variant==="tabs"){const Selector=variant==="steps"?StepSelector:TabSelector;return <Selector options={options.map((value,index)=>({value,label:optionLabels![index]!}))} value={value} label={name} enabled={!disabled} onChange={onChange}/>;}
 disabled=disabled||!onChange;
 const unmatched=value!==""&&!options.includes(value),select=(next:string)=>{if(!disabled&&(options.includes(next)||next===""&&variant==="select"))onChange?.(next);};
 return <div className="grid min-w-0 gap-2">
  {variant==="select"?<label className="grid min-w-0 gap-1 text-xs">{caption&&<span className="break-words">{caption}</span>}<Select aria-label={name} value={value} disabled={disabled} onChange={e=>select(e.target.value)}><option value="">—</option>{unmatched&&<option value={value} disabled>{value}</option>}{options.map(option=><option key={option} value={option}>{option}</option>)}</Select></label>:variant==="radio"?<fieldset role="radiogroup" aria-label={name} disabled={disabled} className="grid min-w-0 gap-1">{caption&&<legend className="break-words text-xs">{caption}</legend>}{options.map(option=><label key={option} className="flex min-w-0 items-center gap-2 text-xs"><input type="radio" name={id} value={option} checked={value===option} onChange={e=>{if(e.target.checked)select(option);}} className="shrink-0 accent-primary"/><span className="min-w-0 break-words">{option}</span></label>)}</fieldset>:<div role="group" aria-label={name} className="grid min-w-0 gap-1">{caption&&<span className="break-words text-xs">{caption}</span>}<div className="flex min-w-0 flex-wrap gap-1">{options.map(option=><Button key={option} size="sm" aria-pressed={value===option} disabled={disabled} variant={value===option?"primary":"default"} className="h-auto min-w-0 whitespace-normal break-words text-left" onClick={()=>select(option)}>{option}</Button>)}</div></div>}
  {options.length===0&&<p role="status" className="text-xs text-muted">{t("No choices available.")}</p>}
  {unmatched&&<p role="status" className="break-words text-xs text-warning">{t("Current choice is outside the configured options: {value}.",{value})}</p>}
 </div>;
}
