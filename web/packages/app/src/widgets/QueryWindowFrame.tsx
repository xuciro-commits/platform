import {Button,Panel,t,type RecordList,type RecordPageData} from "@platform/ui";
import type {ReactNode} from "react";
export type QueryWindow=NonNullable<Parameters<typeof RecordList>[0]["window"]>;

/** The window owner supplies data, errors and paging. Rendering adds no read. */
export function QueryWindowFrame({window,description,children}:{window?:QueryWindow;description:string;children:(page:RecordPageData)=>ReactNode}) {
 if(!window)return <Panel role="status">{t("Query window is unavailable.")}</Panel>;
 if(window.error)return <Panel role="alert">{window.error}</Panel>;
 if(!window.page)return <Panel role="status">{t("Loading…")}</Panel>;
 const offset=window.query.offset??0,limit=window.query.limit??100,page=window.page;
 return <div className="grid min-w-0 grid-cols-1 gap-2"><p role="status" className="text-xs text-muted">{t("Showing {shown} of {total} matching records",{shown:page.records.length,total:page.total})} · {description}</p>
 {children(page)}
 <div className="flex flex-wrap items-center gap-2"><Button size="sm" disabled={offset===0} onClick={()=>window.onChange({offset:Math.max(0,offset-limit)})}>{t("Previous")}</Button><span className="text-xs text-muted">{page.records.length?offset+1:0}–{offset+page.records.length}</span><Button size="sm" disabled={offset+page.records.length>=page.total||offset+limit>window.maxOffset} onClick={()=>window.onChange({offset:offset+limit})}>{t("Next")}</Button></div></div>;
}
