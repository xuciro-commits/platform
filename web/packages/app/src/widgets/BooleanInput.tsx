import {Checkbox,Panel,Switch,t} from "@platform/ui";
import type {VariableResult} from "../runtime/variables";

export function BooleanInputRenderer({value,label,title,enabled,onChange,variant}:{
  value?:VariableResult;label?:string;title:string;enabled?:boolean;onChange?:(checked:boolean)=>void;variant?:string;
}) {
  if(value?.status==="error")return <Panel role="alert">{t(value.code)}</Panel>;
  if(value?.status!=="value"||typeof value.value!=="boolean"||!onChange)
    return <Panel role="alert">{t("Boolean input is unavailable or incompatible.")}</Panel>;
  const caption=label??title;
  if(variant&&variant!=="switch"&&variant!=="checkbox")return <Panel role="alert">{t("Boolean presentation is unsupported.")}</Panel>;
  if(variant==="checkbox")return <Checkbox checked={value.value} onChange={onChange} ariaLabel={caption.trim()?undefined:title} disabled={enabled===false} className="min-w-0 text-xs"><span className="min-w-0 break-words">{caption}</span></Checkbox>;
  return <Switch checked={value.value} onChange={onChange} label={caption} ariaLabel={caption.trim()?undefined:title} disabled={enabled===false}/>;
}
