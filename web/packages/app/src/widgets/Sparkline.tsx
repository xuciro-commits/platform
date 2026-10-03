import {Panel,RecordSparkline,t,type EntityInfo} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {isDecimal,isNumber} from "../runtime/decimal";
import type {VariableResult} from "../runtime/variables";
import {QueryWindowFrame,type QueryWindow} from "./QueryWindowFrame";
export function SparklineRenderer({value,window,info,fields,title}:{value?:VariableResult;window?:QueryWindow;info?:EntityInfo;fields?:Api.PageSparkline;title:string}) {
 if(!fields)return <Panel role="alert">{t("Sparkline KPI configuration is unavailable.")}</Panel>;
 if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
 if(value?.status!=="value"&&value?.status!=="empty")return <p role="status" className="text-xs text-muted">{t("Sparkline value is unavailable or loading.")}</p>;
 if(value.status==="value"&&!isDecimal(value.value)&&!isNumber(value.value))return <Panel role="alert">{t("Sparkline KPI needs an original decimal or number value.")}</Panel>;
 const valueText=value.status==="value"?isDecimal(value.value)?value.value.value:isNumber(value.value)?String(value.value.value):undefined:undefined,label=fields.label??title;
 if(!fields.field)return <RecordSparkline valueText={valueText} label={label} suffix={fields.suffix}/>;
 return <QueryWindowFrame window={window} description={t("The scalar keeps its original scope; the mini-line shows this ordered record window.")}>{page=><RecordSparkline valueText={valueText} label={label} suffix={fields.suffix} records={page.records} info={info} field={fields.field}/>}</QueryWindowFrame>;
}
