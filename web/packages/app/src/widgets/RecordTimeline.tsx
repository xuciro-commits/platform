import {Button,Panel,RecordTimeline,t,type EntityRecord,type RecordList,type TimelineFields} from "@platform/ui";

type Window=NonNullable<Parameters<typeof RecordList>[0]["window"]>;
export type RecordTimelinePorts={window?:Window;fields?:TimelineFields;selected?:EntityRecord;onSelect:(record?:EntityRecord)=>void;title:string};
export function RecordTimelineRenderer({window,fields,selected,onSelect,title}:RecordTimelinePorts) {
 if(!fields)return <Panel role="alert">{t("Timeline fields are unavailable or incompatible.")}</Panel>;
 if(!window)return <Panel role="status">{t("Query window is unavailable.")}</Panel>;
 if(window.error)return <Panel role="alert">{window.error}</Panel>;
 if(!window.page)return <Panel role="status">{t("Loading…")}</Panel>;
 const offset=window.query.offset??0,limit=window.query.limit??100,page=window.page;
 return <div className="grid min-w-0 grid-cols-1 gap-2"><p role="status" className="text-xs text-muted">{t("Showing {shown} of {total} matching records",{shown:page.records.length,total:page.total})} · {t("This timeline shows the current query window.")}</p>
 <RecordTimeline records={page.records} fields={fields} selected={selected?.id} onSelect={onSelect} label={title}/>
 <div className="flex flex-wrap items-center gap-2"><Button size="sm" disabled={offset===0} onClick={()=>window.onChange({offset:Math.max(0,offset-limit)})}>{t("Previous")}</Button><span className="text-xs text-muted">{page.records.length?offset+1:0}–{offset+page.records.length}</span><Button size="sm" disabled={offset+page.records.length>=page.total||offset+limit>window.maxOffset} onClick={()=>window.onChange({offset:offset+limit})}>{t("Next")}</Button></div></div>;
}
