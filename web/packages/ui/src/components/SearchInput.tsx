import {Search} from "lucide-react";
import type {InputHTMLAttributes} from "react";
import {cn} from "../lib/cn";
import {Input} from "../primitives/input";
import {t} from "../i18n";
/** Caller-owned text and readable object scope; this control performs no reads. */
export function SearchInput({value,onChange,scope,...props}:Omit<InputHTMLAttributes<HTMLInputElement>,"type"|"value"|"onChange"|"placeholder">&{value:string;onChange:(value:string)=>void;scope:string[]}) {
 if(!scope.length)return <p role="status">{t("Search scope is unavailable.")}</p>;
 return <div className="grid min-w-0 gap-1"><div className="relative min-w-0"><Search aria-hidden="true" focusable="false" size={13} className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-muted"/><Input {...props} type="search" className={cn("pl-7",props.className)} placeholder={t("Search records…")} value={value} onChange={e=>{if(!props.disabled)onChange(e.target.value);}}/></div><p className="break-words text-xs text-muted">{t("Search in: {objects}",{objects:scope.join(", ")})}</p></div>;
}
