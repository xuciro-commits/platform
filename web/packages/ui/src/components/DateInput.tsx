import { DateInput as AstryxDateInput } from "@astryxdesign/core/DateInput";
import type { ISODateString } from "@astryxdesign/core/Calendar";
import { t } from "../i18n";
import { validCivilDate } from "./date";

export function DateInput({value,label,title,disabled,onChange}:{value:string;label?:string;title:string;disabled?:boolean;onChange:(value:string)=>void}){
 const caption=label?.trim()||title;
 const valid=!!value&&validCivilDate(value);
 return <AstryxDateInput
   label={caption}
   isLabelHidden={!label?.trim()}
   value={valid?value as ISODateString:undefined}
   onChange={next=>onChange(next??"")}
   isDisabled={disabled}
   hasClear
   status={value&&!valid?{type:"error",message:t("Invalid date value. Use a real YYYY-MM-DD date; the original draft is retained.")}:undefined}
   size="sm"
 />;
}
