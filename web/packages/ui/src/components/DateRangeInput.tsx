import { DateRangeInput as AstryxDateRangeInput, type DateRange } from "@astryxdesign/core/DateRangeInput";
import type { ISODateString } from "@astryxdesign/core/Calendar";
import { t } from "../i18n";
import { validCivilDate } from "./date";

const day=(value:string)=>new Date(`${value}T00:00:00Z`);

export function DateRangeInput({id,start,end,startLabel,endLabel,inclusive=false,disabled,invalid,describedBy,onChange}:{id?:string;start:string;end:string;startLabel:string;endLabel:string;inclusive?:boolean;disabled?:boolean;invalid?:boolean;describedBy?:string;onChange:(start:string,end:string)=>void}){
 const valid=validCivilDate(start)&&validCivilDate(end);
 const count=valid&&end>=start?Math.round((day(end).getTime()-day(start).getTime())/86400000)+(inclusive?1:0):undefined;
 const value:DateRange|null=valid?{start:start as ISODateString,end:end as ISODateString}:null;
 return <AstryxDateRangeInput
   id={id}
   label={startLabel===endLabel?startLabel:`${startLabel} – ${endLabel}`}
   placeholder={`${startLabel} → ${endLabel}`}
   value={value}
   onChange={next=>onChange(next?.start??"",next?.end??"")}
   isDisabled={disabled}
   status={invalid?{type:"error"}:undefined}
   aria-describedby={describedBy}
   description={count===undefined?undefined:inclusive?(count===1?t("{count} day",{count}):t("{count} days",{count})):(count===1?t("{count} night",{count}):t("{count} nights",{count}))}
   hasClear
   size="sm"
 />;
}
