/** Gregorian civil date, without parsing an instant or a local timezone. */
export function validCivilDate(value:string){
 const match=/^(\d{4})-(\d{2})-(\d{2})$/.exec(value);if(!match)return false;
 const year=Number(match[1]),month=Number(match[2]),day=Number(match[3]);if(year<1||year>9999||month<1||month>12||day<1)return false;
 const leap=year%4===0&&(year%100!==0||year%400===0),days=[31,leap?29:28,31,30,31,30,31,31,30,31,30,31];return day<=days[month-1]!;
}

export function validTimestampOffset(value:string){return value!=="-00:00"&&/^(Z|[+-]([01]\d|2[0-3]):[0-5]\d)$/.test(value);}
/** Preserve wall-clock fields and precision, never parse through the browser zone. */
export function timestampParts(value:string){
 const m=/^(\d{4}-\d{2}-\d{2})T([01]\d|2[0-3]):([0-5]\d):([0-5]\d)(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
 if(!m||!validCivilDate(m[1]!)||!validTimestampOffset(m[6]!))return undefined;
 return {local:`${m[1]}T${m[2]}:${m[3]}:${m[4]}`,fraction:m[5]??"",offset:m[6]!};
}
export function validTimestamp(value:string){return !!timestampParts(value);}
/** Explicit import interpretation of a complete naive datetime. */
export function withTimestampOffset(value:string,offset:string){
 if(!validTimestampOffset(offset))return undefined;
 if(value===""||validTimestamp(value))return value;
 const m=/^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2})(:\d{2}(?:\.\d{1,9})?)?$/.exec(value);
 if(!m)return undefined;
 const result=`${m[1]}${m[2]??":00"}${offset}`;return validTimestamp(result)?result:undefined;
}
