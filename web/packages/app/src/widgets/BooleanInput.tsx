import {Panel,Switch,t} from "@platform/ui";
import type {VariableResult} from "../runtime/variables";

export function BooleanInputRenderer({value,label,title,enabled,onChange}:{
  value?:VariableResult;label?:string;title:string;enabled?:boolean;onChange?:(checked:boolean)=>void;
}) {
  if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
  if(value?.status!=="value"||typeof value.value!=="boolean"||!onChange)
    return <Panel role="alert">{t("Boolean input is unavailable or incompatible.")}</Panel>;
  const caption=label??title;
  return <Switch checked={value.value} onChange={onChange} label={caption} ariaLabel={caption.trim()?undefined:title} disabled={enabled===false}/>;
}
