import {Panel,RecordTimeline,t,type EntityRecord,type TimelineFields} from "@platform/ui";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
export type RecordTimelinePorts={window?:QueryWindow;fields?:TimelineFields;selected?:EntityRecord;onSelect:(record?:EntityRecord)=>void;title:string};
export function RecordTimelineRenderer({window,fields,selected,onSelect,title}:RecordTimelinePorts) {
 if(!fields)return <Panel role="alert">{t("Timeline fields are unavailable or incompatible.")}</Panel>;
 return <QueryWindowFrame window={window} description={t("This timeline shows the current query window.")}>{page=><RecordTimeline records={page.records} fields={fields} selected={selected?.id} onSelect={onSelect} label={title}/>}</QueryWindowFrame>;
}
