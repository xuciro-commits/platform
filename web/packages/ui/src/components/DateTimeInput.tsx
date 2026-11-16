import { DateTimeInput as AstryxDateTimeInput, type ISODateTimeString } from "@astryxdesign/core/DateTimeInput";
import { useEffect, useState } from "react";
import { Input } from "../primitives/input";
import { t } from "../i18n";
import { timestampParts, validTimestampOffset } from "./date";

const normalize=(value:string)=>value.length===16?`${value}:00`:value;

export function DateTimeInput({value:source,offset,label,title,disabled,onChange}:{value:string;offset:string;label?:string;title:string;disabled?:boolean;onChange:(value:string)=>void}){
 const caption=label?.trim()||title;
 const parts=timestampParts(source);
 const fallback=validTimestampOffset(offset)?offset:"Z";
 const zone=parts?.offset??fallback;
 const fraction=parts?.fraction??"";
 const [zoneDraft,setZoneDraft]=useState(zone);
 const [fractionDraft,setFractionDraft]=useState(fraction);
 useEffect(()=>{setZoneDraft(zone);setFractionDraft(fraction);},[zone,fraction]);

 const write=(local:string,nextFraction=fractionDraft,nextZone=zoneDraft)=>{
  if(!local||!validTimestampOffset(nextZone)||!/^\d{0,9}$/.test(nextFraction))return;
  onChange(`${normalize(local)}${nextFraction?`.${nextFraction}`:""}${nextZone}`);
 };

 return <div className="grid min-w-0 gap-2">
  <AstryxDateTimeInput
   label={caption}
   isLabelHidden={!label?.trim()}
   value={parts?parts.local as ISODateTimeString:undefined}
   onChange={next=>{if(!next)onChange("");else write(next);}}
   isDisabled={disabled||!validTimestampOffset(zone)}
   hasClear
   status={source&&!parts?{type:"error",message:t("Invalid datetime value. Use a real date and time with seconds and Z or a UTC offset; the original draft is retained.")}:undefined}
   size="sm"
  />
  <div className="grid gap-2 sm:grid-cols-2">
   <label className="grid min-w-0 gap-1 text-xs">{t("UTC offset")}<Input aria-label={t("{label} UTC offset",{label:caption})} maxLength={6} value={zoneDraft} disabled={disabled||!parts} onChange={e=>{const next=e.target.value;setZoneDraft(next);if(parts)write(parts.local,fractionDraft,next);}} onBlur={()=>{if(!validTimestampOffset(zoneDraft))setZoneDraft(zone);}}/></label>
   <label className="grid min-w-0 gap-1 text-xs">{t("Fractional seconds")}<Input aria-label={t("{label} fractional seconds",{label:caption})} inputMode="numeric" maxLength={9} value={fractionDraft} disabled={disabled||!parts} onChange={e=>{const next=e.target.value.replace(/\D/g,"");setFractionDraft(next);if(parts)write(parts.local,next,zoneDraft);}} onBlur={()=>{if(!/^\d{0,9}$/.test(fractionDraft))setFractionDraft(fraction);}}/></label>
  </div>
  <p className="text-xs text-muted">{t("Date and time use the displayed UTC offset, independently of your browser timezone.")}</p>
 </div>;
}
