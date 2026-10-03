import {Panel,RangeInput,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import type {VariableResult} from "../runtime/variables";
export function RangeRenderer({lower,upper,fields,title,onChange}:{lower?:VariableResult;upper?:VariableResult;fields?:Api.PageRangeInput;title:string;onChange?:(lower:string,upper:string)=>void}){
 const draft=(value?:VariableResult)=>value?.status==="value"&&typeof value.value==="string"?value.value:value?.status==="error"?value.draft:undefined;
 const a=draft(lower),b=draft(upper);
 if(!fields||a===undefined||b===undefined||!onChange)return <Panel role="alert">{t("Range inputs are unavailable in this scope.")}</Panel>;
 return <RangeInput {...fields} lower={a} upper={b} label={fields.label??title} onChange={onChange}/>;
}
