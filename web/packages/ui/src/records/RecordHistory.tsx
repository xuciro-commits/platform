import { renderingLimits } from "../gen/presentation-limits";
import { History as HistoryIcon } from "lucide-react";
import type { ReactNode } from "react";
import { t } from "../i18n";
import type { Api } from "@platform/api";
import type {EntityInfo, FieldInfo, Money, RecordChange} from "./Records";

export type RecordHistoryProps={info:EntityInfo;history?:readonly RecordChange[];heading?:boolean;label?:string;limit?:number;total?:number;loadedCount?:number;recordID?:string;recordRevision?:number};
export function RecordHistory({info,history=[],heading=true,label,limit,total,loadedCount=history.length,recordID,recordRevision}:RecordHistoryProps) {
  if(!Array.isArray(history)||limit!==undefined&&(!Number.isSafeInteger(limit)||limit<1||limit>renderingLimits.runtime.workViews.maxHistoryWindow)||!Number.isSafeInteger(loadedCount)||loadedCount<history.length||total!==undefined&&(!Number.isSafeInteger(total)||total<loadedCount)||recordID!==undefined&&(typeof recordID!=="string"||!recordID)||recordRevision!==undefined&&(!Number.isSafeInteger(recordRevision)||recordRevision<0)||history.some((change:RecordChange)=>!change||typeof change.change!=="string"||!change.change||typeof change.schema!=="string"||!change.schema||typeof change.by!=="string"||typeof change.at!=="string"||!change.at||!Array.isArray(change.fields)||change.fields.some((field:Api.FieldChange)=>!field||typeof field.field!=="string"||!field.field)))return <p role="alert">{t("The original record history is unavailable or incompatible.")}</p>;
  const visible:readonly RecordChange[]=limit===undefined?history:history.slice(0,limit),range=limit!==undefined||total!==undefined||loadedCount!==history.length;
  const occurrences=new Map<string,number>();
  return (
    <section aria-label={label??t("History")} className="grid min-w-0 gap-2">
      {heading && <h2 className="flex items-center gap-1 text-sm font-semibold"><HistoryIcon className="size-3.5" />{label??t("History")}</h2>}
      {(recordID!==undefined||recordRevision!==undefined)&&<p className="break-all font-mono text-xs text-muted">{recordID}{recordRevision!==undefined?` · ${t("Revision {revision}",{revision:recordRevision})}`:""}</p>}
      {range&&<p role="status" className="text-xs text-muted">{total===undefined?t("Showing {shown} of {loaded} loaded changes; the complete total is unavailable.",{shown:visible.length,loaded:loadedCount}):t("Showing {shown} of {total} changes.",{shown:visible.length,total})}</p>}
      {visible.length===0&&<p className="text-sm text-muted">{t("No changes yet.")}</p>}
      <ol className="grid min-w-0 gap-2">
        {visible.map(h => {const occurrence=occurrences.get(h.change)??0;occurrences.set(h.change,occurrence+1);return (
          <li key={JSON.stringify([h.change,occurrence])} className="min-w-0 rounded-md border border-border bg-surface p-2 text-xs">
            <div className="flex min-w-0 flex-wrap gap-2"><span className="break-all font-mono">{h.change}</span><span className="break-all font-mono">{h.schema}</span><span className="break-words text-muted">{h.by} · <time dateTime={h.at}>{h.at}</time></span></div>
            {h.fields.length > 0 && (
              <ul className="mt-1 grid gap-0.5">
                {h.fields.map((f) => {
                  const declared = info.fields.find((x) => x.name === f.field);
                  if(!declared&&!(f.field==="archived"&&[f.before,f.after].every(value=>value===undefined||typeof value==="boolean")))return null;
                  return <li key={f.field} className="min-w-0 break-words"><span className="text-muted">{declared?.title ?? t("Archived")}</span>{" "}
                    {declared?.type === "lines" ? <LinesChange field={declared} before={f.before} after={f.after} />
                      : <>{f.before !== undefined && <><s className="text-muted">{shown(f.before)}</s> → </>}{shown(f.after)}</>}</li>;
                })}
              </ul>
            )}
          </li>
        );})}
      </ol>
    </section>
  );
}

const shown = (v: unknown) => (v === undefined || v === null || v === "" ? "—" : typeof v === "object" ? JSON.stringify(v) : String(v));

type Row = Record<string, unknown>;

const cellOf = (f: FieldInfo, v: unknown) => f.type === "money" && v && typeof v === "object"
  ? `${((v as Money).amount / 100).toFixed(2)} ${(v as Money).currency}` : shown(v);

function LinesChange({ field, before, after }: { field: FieldInfo; before: unknown; after: unknown }) {
  const columns = field.fields ?? [];
  const was = Array.isArray(before) ? (before as Row[]) : [], is = Array.isArray(after) ? (after as Row[]) : [];
  const unique = (rows: Row[], name: string) => rows.every((r) => ["string", "number"].includes(typeof r[name]) && r[name] !== "")
    && new Set(rows.map((r) => r[name])).size === rows.length;
  const key = columns.find((c) => c.type !== "money" && unique(was, c.name) && unique(is, c.name))?.name;
  const id = (r: Row, i: number) => String(key ? r[key] : i);
  const text = (r: Row) => columns.filter((c) => r[c.name] !== undefined && r[c.name] !== "" && r[c.name] !== null).map((c) => `${c.title} ${cellOf(c, r[c.name])}`).join(" · ");
  const old = new Map(was.map((r, i) => [id(r, i), r])), now = new Map(is.map((r, i) => [id(r, i), r]));
  const lines: ReactNode[] = [];
  is.forEach((r, i) => {
    const k = id(r, i), prior = old.get(k);
    if (!prior) { lines.push(<li key={`+${k}`} className="text-[var(--tone-success)]">+ {text(r)}</li>); return; }
    const changed = columns.filter((c) => JSON.stringify(prior[c.name]) !== JSON.stringify(r[c.name]));
    if (changed.length) lines.push(<li key={`~${k}`}>{key ? `${r[key]}: ` : `${i + 1}: `}{changed.map((c, j) =>
      <span key={c.name}>{j > 0 && " · "}{c.title} <s className="text-muted">{cellOf(c, prior[c.name])}</s> → {cellOf(c, r[c.name])}</span>)}</li>);
  });
  was.forEach((r, i) => { const k = id(r, i); if (!now.has(k)) lines.push(<li key={`-${k}`} className="text-muted"><s>− {text(r)}</s></li>); });
  return lines.length ? <ul className="ml-3 grid gap-0.5">{lines}</ul> : <span className="text-muted">{t("unchanged")}</span>;
}
