import {pageUIManifest} from "@platform/kernel";
import {t} from "../i18n";
export type TermCount={value:string|null;count:number};
/** Original complete counts, stable row identity and caller-owned read scope. */
export function TermCounts({terms,label}:{terms:TermCount[];label:string}){
 if(terms.length>pageUIManifest.runtime.terms.maxGroups||new Set(terms.map(term=>JSON.stringify(term.value))).size!==terms.length||terms.some(term=>term.value!==null&&typeof term.value!=="string"||!Number.isSafeInteger(term.count)||term.count<1))return <p role="alert">{t("Term counts are invalid or exceed their budget.")}</p>;
 if(!terms.length)return <p role="status">{t("No terms in the matching records.")}</p>;
 const maximum=Math.max(...terms.map(term=>term.count));
 return <ul aria-label={label} className="flex min-w-0 list-none flex-wrap items-end gap-2">{terms.map(term=><li key={JSON.stringify(term.value)} className="min-w-0 max-w-full break-words rounded-sm border border-border bg-surface px-2 py-1 text-xs" style={{fontSize:10+8*term.count/maximum}}><span>{term.value===null?t("No value"):term.value===""?t("Empty text"):term.value}</span><span className="tabular-nums"> · {term.count}</span></li>)}</ul>;
}
