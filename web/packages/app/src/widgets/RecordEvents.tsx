import {Panel,RecordEvents,t,type EntityInfo} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
import {pageVariableContract} from "../runtime/PageRuntime";
export function RecordEventsRenderer({window,info,fields}:{window?:QueryWindow;info?:EntityInfo;fields?:Api.PageRecordEvents}) {
 if(!info||!fields)return <Panel role="alert">{t("Event fields or tone mappings are unavailable or incompatible.")}</Panel>;
 return <QueryWindowFrame window={window} description={t("This list shows original business events in the ordered query window.")}>{page=><RecordEvents records={page.records} info={info} fields={fields} maxEvents={pageVariableContract.recordEvents.maxEvents}/>}</QueryWindowFrame>;
}
