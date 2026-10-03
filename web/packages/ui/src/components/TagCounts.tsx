import {Button} from "../primitives/button";
import {t} from "../i18n";
import {validTermCounts,type TermCount} from "./TermCounts";

/** Complete original scalar counts as fixed-size tags with caller-owned optional filter writes. */
export function TagCounts({terms,label,selected=[],enabled=true,onSelect}:{terms:TermCount[];label:string;selected?:readonly string[];enabled?:boolean;onSelect?:(value?:string)=>void}) {
 if(!validTermCounts(terms))return <p role="alert">{t("Term counts are invalid or exceed their budget.")}</p>;
 if(!terms.length)return <p role="status">{t("No terms in the matching records.")}</p>;
 const title=(term:TermCount)=>term.value===null?t("No value (missing)"):term.value===""?t("Empty text"):term.value;
 return <div className="grid min-w-0 gap-2"><ul aria-label={label} className="flex min-w-0 list-none flex-wrap items-center gap-1">{terms.map(term=>{const picked=term.value!==null&&selected.includes(term.value),content=<>{title(term)} <span className="tabular-nums">· {term.count}</span></>;return <li key={JSON.stringify(term.value)} className="min-w-0 max-w-full">{onSelect&&term.value!==null?<Button size="sm" variant={picked?"primary":"default"} disabled={!enabled} aria-pressed={picked} className="h-auto min-w-0 max-w-full whitespace-normal break-words rounded-sm px-2 py-1 text-left text-xs" onClick={()=>{if(enabled)onSelect(term.value!);}}>{content}</Button>:<span className={"inline-flex min-w-0 max-w-full flex-wrap items-center gap-1 break-words rounded-sm border px-2 py-1 text-xs "+(picked?"border-primary bg-row-selected":"border-border bg-surface")}>{content}</span>}</li>;})}</ul>{onSelect&&<div className="flex flex-wrap items-center gap-2"><Button size="sm" variant="ghost" disabled={!enabled} onClick={()=>{if(enabled)onSelect();}}>{t("Clear group filter")}</Button><p className="text-xs text-muted">{t("Missing groups cannot be written to text filters.")}</p></div>}</div>;
}
