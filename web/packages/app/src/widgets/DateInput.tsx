import {DateInput,DateTimeInput,Panel,t} from "@platform/ui";
import type {VariableResult} from "../runtime/variables";
export function DateInputRenderer({value,kind,offset,label,title,enabled,onChange}:{kind?:string;offset?:string;value?:VariableResult;label?:string;title:string;enabled?:boolean;onChange?:(value:string)=>void}){
 if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
 if(value?.status!=="value"||typeof value.value!=="string"||!onChange)return <Panel role="alert">{t("Date input is unavailable or incompatible.")}</Panel>;
 if(kind==="datetime")return <DateTimeInput value={value.value} offset={offset??""} label={label} title={title} disabled={enabled===false} onChange={onChange}/>;
 return <DateInput value={value.value} label={label} title={title} disabled={enabled===false} onChange={onChange}/>;
}
