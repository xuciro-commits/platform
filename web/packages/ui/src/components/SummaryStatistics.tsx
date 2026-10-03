import {t} from "../i18n";
export type StatisticsValue={kind:"statistics";count:string;min?:number;mean?:number;max?:number;sum?:number};
export function SummaryStatistics({value,fieldTitle}:{value:StatisticsValue;fieldTitle:string}) {
 return <div className="grid min-w-0 gap-2"><p className="text-xs text-muted">{fieldTitle} · {t("Complete authorized set")}</p><dl className="grid min-w-0 grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">{([["Count",value.count],["Min",value.min],["Mean",value.mean],["Max",value.max],["Sum",value.sum]] as const).map(([label,number])=><div key={label} className="min-w-0"><dt className="text-[10px] uppercase text-muted">{t(label)}</dt><dd className="break-all text-sm font-semibold tabular-nums">{number===undefined?t("No value"):typeof number==="string"?number:number.toLocaleString(undefined,{maximumFractionDigits:1})}</dd></div>)}</dl></div>;
}
