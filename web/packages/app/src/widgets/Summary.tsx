import {Panel,SummaryStatistics,t,type EntityInfo} from "@platform/ui";
import type {VariableResult} from "../runtime/variables";
export function SummaryRenderer({value,info,field}:{value?:VariableResult;info?:EntityInfo;field?:string}) {
 if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
 if(value?.status!=="value")return <p role="status" className="text-xs text-muted">{t("Statistics are unavailable or loading.")}</p>;
 const descriptor=info?.fields.find(f=>f.name===field);
 if(!descriptor||!["integer","decimal"].includes(descriptor.type)||typeof value.value!=="object"||value.value.kind!=="statistics")return <Panel role="alert">{t("Statistics fields or result are unavailable or incompatible.")}</Panel>;
 return <SummaryStatistics value={value.value} fieldTitle={descriptor.title}/>;
}
