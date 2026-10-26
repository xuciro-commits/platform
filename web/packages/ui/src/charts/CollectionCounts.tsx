import type {Api} from "@platform/kernel";
import {validTermCounts,type TermCount} from "../components/TermCounts";
import {t} from "../i18n";

export function signedCounts(terms:TermCount[],steps:Api.PageAnalysisStep[]) {
 if(!validTermCounts(terms)||steps.length!==4||new Set(steps.map(s=>s.value)).size!==4||steps.some((s,i)=>!s.value||typeof s.label!=="string"||s.positive!==(i===0)))return;
 const total=terms.reduce((n,t)=>n+BigInt(t.count),0n),mapped=new Set(steps.map(s=>s.value));let cumulative=0n;
 const rows=steps.map(step=>{const count=BigInt(terms.find(t=>t.value===step.value)?.count??0),delta=step.positive?count:-count;cumulative+=delta;return {...step,count,delta,cumulative,share:total?Number(count)/Number(total):0};});
 return {rows,total,unmapped:terms.filter(t=>!mapped.has(t.value??"")).reduce((n,t)=>n+BigInt(t.count),0n)};
}
/** Original group values/counts; geometry never changes the displayed count. */
export function CollectionCounts({terms,steps,label}:{terms:TermCount[];steps?:Api.PageAnalysisStep[];label:string}) {
 if(!validTermCounts(terms))return <p role="alert">{t("Collection analysis counts are invalid or exceed their budget.")}</p>;
 const name=(v:string|null)=>v===null?t("No value (missing)"):v===""?t("Empty text"):v;
 if(steps){const model=signedCounts(terms,steps);if(!model)return <p role="alert">{t("Signed count steps are unavailable or incompatible.")}</p>;return <section aria-label={label} className="grid min-w-0 gap-2"><p role="status" className="text-xs text-muted">{t("{total} matching records · {unmapped} records outside the mapped steps",{total:model.total.toString(),unmapped:model.unmapped.toString()})}</p><ol className="grid min-w-0 grid-cols-4 gap-2">{model.rows.map(row=><li key={row.value} tabIndex={0} aria-label={`${row.label}: ${row.delta}; Σ ${row.cumulative}`} className="grid min-w-0 content-end gap-1 text-center text-xs"><div className="flex h-32 items-end justify-center"><div aria-hidden="true" style={{height:`${row.share*100}%`,background:`var(${row.positive?"--tone-success":"--tone-danger"})`}} className="w-2/3"/></div><span className="break-words">{row.label}</span><span className="tabular-nums">{row.delta.toString()}</span><span className="tabular-nums text-muted">Σ {row.cumulative.toString()}</span></li>)}</ol><p className="text-xs text-muted">{t("Signed category counts with cumulative values; bar heights show each count's share of the complete set.")}</p></section>;}
 const maximum=Math.max(1,...terms.map(term=>term.count));return <section aria-label={label} className="grid min-w-0 gap-2"><p className="text-xs text-muted">{t("Fixed horizontal count bars over all matching authorized records.")}</p>{!terms.length?<p role="status">{t("No groups in the matching records.")}</p>:<ul className="grid min-w-0 gap-2">{terms.map(term=><li key={JSON.stringify(term.value)} tabIndex={0} aria-label={`${name(term.value)}: ${term.count}`} className="flex min-w-0 items-center gap-2 text-xs"><span className="w-24 shrink-0 break-words">{name(term.value)}</span><span aria-hidden="true" className="h-4 min-w-0 flex-1 bg-row-hover"><span className="block h-full bg-primary" style={{width:`${term.count/maximum*100}%`}}/></span><span className="tabular-nums">{term.count}</span></li>)}</ul>}</section>;
}
