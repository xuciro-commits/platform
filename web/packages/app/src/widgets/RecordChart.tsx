import {Panel,RecordChart,t,type EntityInfo,type RecordChartFields} from "@platform/ui";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
import {pageVariableContract} from "../runtime/PageRuntime";

export function RecordChartRenderer({window,info,fields}:{window?:QueryWindow;info?:EntityInfo;fields:RecordChartFields}) {
 if(!info)return <Panel role="alert">{t("Record chart fields or values are unavailable or incompatible.")}</Panel>;
 return <QueryWindowFrame window={window} description={t("This chart shows ordered records in the current query window.")}>{page=><RecordChart records={page.records} info={info} fields={fields} maxPoints={pageVariableContract.recordChart.maxPoints}/>}</QueryWindowFrame>;
}
