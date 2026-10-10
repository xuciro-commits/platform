import {useDraftInput} from "../fields/draft";
import {Button} from "../primitives/button";
import {Input} from "../primitives/input";
import {t} from "../i18n";
import {validCivilDate} from "./date";

export function DateInput({value:source,label,title,disabled,onChange}:{value:string;label?:string;title:string;disabled?:boolean;onChange:(value:string)=>void}){
 const caption=label??"",name=caption.trim()?caption:title;
 const input=useDraftInput(`DateInput:${title}`,source),value=input.text;
 const change=(next:string)=>{const valid=next===""||validCivilDate(next);if(input.write(next,valid?"":t("Invalid date value. Use a real YYYY-MM-DD date; the original draft is retained."))&&valid)onChange(next);};
 const invalid=value!==""&&!validCivilDate(value);
 return <div className="grid min-w-0 gap-2"><label className="grid min-w-0 gap-1 text-xs">{caption&&<span className="break-words">{caption}</span>}<Input data-draft-key={input.path} aria-label={name} type="date" min="0001-01-01" max="9999-12-31" value={invalid?"":value} disabled={disabled} aria-invalid={invalid} onChange={e=>{if(!disabled)change(e.target.value);}}/></label>{invalid&&<><label className="grid min-w-0 gap-1 text-xs">{t("Invalid date draft")}<Input aria-label={t("{label} date draft",{label:name})} value={value} disabled={disabled} aria-invalid onChange={e=>{if(!disabled)change(e.target.value);}}/></label><p role="alert" className="break-words text-xs text-danger">{t("Invalid date value. Use a real YYYY-MM-DD date; the original draft is retained.")}</p></>}<Button size="sm" variant="ghost" disabled={disabled||value===""} className="justify-self-start" onClick={()=>change("")}>{t("Clear date")}</Button></div>;
}
