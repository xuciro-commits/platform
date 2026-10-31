import {useId} from "react";
import {SegmentedChoice} from "./SegmentedChoice";
import {Select} from "../primitives/input";
import {t} from "../i18n";
import {validChoiceInput} from "./choice";
import {ContentTabs} from "../layout/ContentTabs";
import {CurrentChoice,StepSelector} from "./IndexedChoices";

export function ChoiceInput({value,options,optionLabels,variant,label,title,disabled,onChange}:{value:string;options:readonly string[];optionLabels?:readonly string[];variant:string;label?:string;title:string;disabled?:boolean;onChange?:(value:string)=>void}){
 const id=useId(),caption=label??"",name=caption.trim()?caption:title;
 if(variant==="multiple"||!validChoiceInput({options,optionLabels,variant,label}))return <p role="alert">{t("Choice configuration is unsupported.")}</p>;
 if(variant==="steps")return <StepSelector options={options.map((value,index)=>({value,label:optionLabels![index]!}))} value={value} label={name} enabled={!disabled} onChange={onChange}/>;
 if(variant==="tabs")return <div className="grid min-w-0 gap-2"><ContentTabs strict wrap={false} label={name} value={value} disabled={disabled||!onChange} onChange={next=>onChange?.(next)} items={options.map((option,index)=>({id:option,title:optionLabels![index]||t("Tab {number}",{number:index+1})}))}/><CurrentChoice value={value} known={options.includes(value)}/></div>;
 disabled=disabled||!onChange;
 const unmatched=value!==""&&!options.includes(value),select=(next:string)=>{if(!disabled&&(options.includes(next)||next===""&&variant==="select"))onChange?.(next);};
 return <div className="grid min-w-0 gap-2">
  {variant==="select"?<label className="grid min-w-0 gap-1 text-xs">{caption&&<span className="break-words">{caption}</span>}<Select aria-label={name} value={value} disabled={disabled} onChange={e=>select(e.target.value)}><option value="">—</option>{unmatched&&<option value={value} disabled>{value}</option>}{options.map(option=><option key={option} value={option}>{option}</option>)}</Select></label>:variant==="radio"?<fieldset role="radiogroup" aria-label={name} disabled={disabled} className="grid min-w-0 gap-1">{caption&&<legend className="break-words text-xs">{caption}</legend>}{options.map(option=><label key={option} className="flex min-w-0 items-center gap-2 text-xs"><input type="radio" name={id} value={option} checked={value===option} onChange={e=>{if(e.target.checked)select(option);}} className="shrink-0 accent-primary"/><span className="min-w-0 break-words">{option}</span></label>)}</fieldset>:<div role="group" aria-label={name} className="grid min-w-0 gap-1">{caption&&<span className="break-words text-xs">{caption}</span>}<SegmentedChoice label={name} value={value} options={options.map(option=>({value:option,label:option}))} disabled={disabled} onChange={select}/></div>}
  {options.length===0&&<p role="status" className="text-xs text-muted">{t("No choices available.")}</p>}
  {unmatched&&<p role="status" className="break-words text-xs text-warning">{t("Current choice is outside the configured options: {value}.",{value})}</p>}
 </div>;
}
