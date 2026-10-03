import {ChoiceInput,Panel,t} from "@platform/ui";
import type {Api} from "@platform/kernel";
import type {VariableResult} from "../runtime/variables";
export function ChoiceInputRenderer({value,fields,title,enabled,onChange}:{value?:VariableResult;fields?:Api.PageChoiceInput;title:string;enabled?:boolean;onChange?:(value:string)=>void}){
 if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
 if(value?.status!=="value"||typeof value.value!=="string"||!fields||!onChange)return <Panel role="alert">{t("Choice input is unavailable or incompatible.")}</Panel>;
 return <ChoiceInput {...fields} value={value.value} title={title} disabled={enabled===false} onChange={onChange}/>;
}
