import {pageUIManifest} from "@platform/kernel";
import {Button} from "../primitives/button";
import {Toggles} from "../primitives/controls";
import {t} from "../i18n";
import {validChoiceInput} from "./choice";

/** Change one declared option in the original set; keep unmatched selections. */
export function MultipleChoiceInput({value,options,label,title,disabled,onChange,clearable}:{value:string[];options:readonly string[];label?:string;clearable?:boolean;title:string;disabled?:boolean;onChange:(value:string[])=>void}){
 const limits=pageUIManifest.runtime.choiceInput,caption=label??"",unknown=value.filter(item=>!options.includes(item));
 if(!validChoiceInput({variant:"multiple",options,label,clearable})||value.length>limits.maxSelected||new Set(value).size!==value.length)return <p role="alert">{t("Multiple choice configuration is unsupported.")}</p>;
 return <div role="group" aria-label={caption.trim()?caption:title} className="grid min-w-0 gap-2">{caption&&<span className="break-words text-xs">{caption}</span>}<div className="flex min-w-0 flex-wrap items-center gap-1"><Toggles options={options.map(item=>({value:item,label:item}))} value={value} disabled={disabled} isDisabled={item=>value.length>=limits.maxSelected&&!value.includes(item)} empty={t("No choices available.")} onChange={next=>{if(!disabled&&next.length<=limits.maxSelected)onChange(next);}}/>{clearable&&value.length>0&&<Button className="h-auto px-1.5 py-0.5 text-xs" size="sm" variant="ghost" disabled={disabled} onClick={()=>{if(!disabled)onChange([]);}}>{t("Clear selections")}</Button>}</div>{unknown.length>0&&<p role="status" className="break-words text-xs text-warning">{t("Selections outside the configured options are retained: {values}.",{values:unknown.join(", ")})}</p>}{value.length>=limits.maxSelected&&<p role="status" className="text-xs text-muted">{t("Selection limit reached; remove a selection before adding another.")}</p>}</div>;
}
