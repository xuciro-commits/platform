import {ChoiceInput,MultipleChoiceInput,Panel,t} from "@platform/ui";
import {isStringSet} from "../runtime/decimal";
import type {Api} from "@platform/kernel";
import type {VariableResult} from "../runtime/variables";
export function ChoiceInputRenderer({value,setValue,fields,title,enabled,onChange,onSet}:{value?:VariableResult;setValue?:VariableResult;fields?:Api.PageChoiceInput;title:string;enabled?:boolean;onChange?:(value:string)=>void;onSet?:(value:string[])=>void}){
 if(fields?.variant==="multiple"){
  if(setValue?.status==="error")return <Panel role="alert">{t(setValue.code)}</Panel>;
  if(setValue?.status!=="value"||!isStringSet(setValue.value)||!onSet)return <Panel role="alert">{t("Multiple choice input is unavailable or incompatible.")}</Panel>;
  return <MultipleChoiceInput {...fields} value={setValue.value.values} title={title} disabled={enabled===false} onChange={onSet}/>;
 }
 if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
 if(value?.status!=="value"||typeof value.value!=="string"||!fields||!onChange)return <Panel role="alert">{t("Choice input is unavailable or incompatible.")}</Panel>;
 return <ChoiceInput {...fields} value={value.value} title={title} disabled={enabled===false} onChange={onChange}/>;
}
