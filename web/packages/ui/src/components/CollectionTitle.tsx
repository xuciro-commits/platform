import {Boxes} from "lucide-react";
import {t} from "../i18n";

/** The caller owns the authorized complete-set count and its lifecycle. */
export function CollectionTitle({title,value,error}:{title:string;value?:string;error?:string}){
 return <div className="flex min-w-0 flex-wrap items-center gap-2 text-xs" role="group" aria-label={title}><Boxes aria-hidden className="size-4 shrink-0"/><span className="min-w-0 break-words font-semibold">{title}</span>{error?<span role="alert" className="text-danger">{error}</span>:value===undefined?<span role="status" className="text-muted">{t("Loading…")}</span>:<span role="status" aria-label={t("Complete collection count")} className="rounded-md border border-border px-1.5 py-0.5 font-semibold tabular-nums">{value}</span>}</div>;
}
