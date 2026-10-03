/** Gregorian civil date, without parsing an instant or a local timezone. */
export function validCivilDate(value:string){
 const match=/^(\d{4})-(\d{2})-(\d{2})$/.exec(value);if(!match)return false;
 const year=Number(match[1]),month=Number(match[2]),day=Number(match[3]);if(year<1||year>9999||month<1||month>12||day<1)return false;
 const leap=year%4===0&&(year%100!==0||year%400===0),days=[31,leap?29:28,31,30,31,30,31,31,30,31,30,31];return day<=days[month-1]!;
}
