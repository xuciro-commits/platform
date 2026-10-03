import {Gauge,Panel,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import {isNumber} from "../runtime/decimal";
import type {VariableResult} from "../runtime/variables";
export function GaugeRenderer({value,fields,title}:{value?:VariableResult;fields?:Api.PageGauge;title:string}) {
 if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
 if(value?.status==="empty")return <p role="status" className="text-xs text-muted">{t("No value for this gauge.")}</p>;
 if(value?.status!=="value")return <p role="status" className="text-xs text-muted">{t("Gauge value is unavailable or loading.")}</p>;
 if(!fields||!isNumber(value.value))return <Panel role="alert">{t("Gauge needs a compatible number value.")}</Panel>;
 return <Gauge value={value.value.value} max={fields.max} warnAt={fields.warnAt} label={fields.label??title} suffix={fields.suffix}/>;
}
