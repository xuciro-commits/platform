import {Progress,Panel,t} from "@platform/ui";
import {isDecimal} from "../runtime/decimal";
import type {VariableResult} from "../runtime/variables";
export function ProgressRenderer({value,total,fixedTotal,title}:{value?:VariableResult;total?:VariableResult;fixedTotal?:string;title:string}) {
 const denominator=total??(fixedTotal?{status:"value" as const,value:{kind:"decimal" as const,value:fixedTotal}}:undefined);
 const error=value?.status==="error"?value.code:denominator?.status==="error"?denominator.code:undefined;
 if(error)return <Panel role="alert">{t(error)}</Panel>;
 if(value?.status!=="value"||denominator?.status!=="value")return <p role="status" className="text-xs text-muted">{t("Progress values are unavailable or loading.")}</p>;
 if(!isDecimal(value.value)||!isDecimal(denominator.value))return <Panel role="alert">{t("Progress needs compatible decimal values.")}</Panel>;
 return <Progress value={value.value.value} total={denominator.value.value} label={title||t("Progress")}/>;
}
