import {retainedWindowView,type RetainedWindowView} from './query-window-view';
import {Button,Panel,t,type RecordList,type RecordPageData} from "@platform/ui";
import {useRef,type ReactNode} from "react";
export type QueryWindow=NonNullable<Parameters<typeof RecordList>[0]["window"]>;

/** The window owner supplies data, errors and paging. Rendering adds no read.
 * A retained view stays hidden and inert while the same authorized window refreshes. */
export function QueryWindowFrame({window,description,children,paging=true,viewIdentity}:{window?:QueryWindow;description:string;paging?:boolean;viewIdentity?:string;children:(page:RecordPageData,ready:boolean)=>ReactNode}) {
 const retained=useRef<RetainedWindowView|undefined>(undefined);
 retained.current=retainedWindowView(retained.current,viewIdentity,window);
 const page=!window||window.error?undefined:window.page??retained.current?.page,ready=!!window?.page&&!window.error;
 const status=!window?<Panel role="status">{t("Query window is unavailable.")}</Panel>:window.error?<Panel role="alert">{window.error}</Panel>:!ready?<Panel role="status">{t("Loading…")}</Panel>:undefined;
 if(!page)return status;
 const offset=window?.query.offset??0,limit=window?.query.limit??100;
 return <div className="grid min-w-0 grid-cols-1 gap-2">{status}
 <p hidden={!ready} role="status" className="text-xs text-muted">{t("Showing {shown} of {total} matching records",{shown:page.records.length,total:page.total})} · {description}</p>
 {viewIdentity===undefined?children(page,ready):<div hidden={!ready}>{children(page,ready)}</div>}
 {paging&&ready&&window&&<div className="flex flex-wrap items-center gap-2"><Button size="sm" disabled={offset===0} onClick={()=>window.onChange({offset:Math.max(0,offset-limit)})}>{t("Previous")}</Button><span className="text-xs text-muted">{page.records.length?offset+1:0}–{offset+page.records.length}</span><Button size="sm" disabled={offset+page.records.length>=page.total||offset+limit>window.maxOffset} onClick={()=>window.onChange({offset:offset+limit})}>{t("Next")}</Button></div>}</div>;
}
