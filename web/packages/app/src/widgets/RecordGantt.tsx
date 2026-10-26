import {Panel,RecordGantt,t,type EntityInfo} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
import {pageVariableContract} from "../runtime/PageRuntime";
export function RecordGanttRenderer({window,info,fields}:{window?:QueryWindow;info?:EntityInfo;fields?:Api.PageRecordGantt}) {
 if(!fields||!info)return <Panel role="alert">{t("Gantt fields are unavailable or incompatible.")}</Panel>;
 return <QueryWindowFrame window={window} description={t("This Gantt shows the current authorized query window.")}>{page=><RecordGantt records={page.records} info={info} fields={fields} maxRows={pageVariableContract.recordGantt.maxRows}/>}</QueryWindowFrame>;
}
