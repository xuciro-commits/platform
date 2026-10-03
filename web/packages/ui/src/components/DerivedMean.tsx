import {t} from "../i18n";
/** Count and average are the original complete-set answers, never window arithmetic. */
export function DerivedMean({count,mean,field,unit}:{count:string;mean?:number;field:string;unit:string}) {
 if(!/^(0|[1-9][0-9]*)$/.test(count)||!field||typeof unit!=="string"||mean!==undefined&&!Number.isFinite(mean))return <p role="alert">{t("Original derived mean values are unavailable or incompatible.")}</p>;
 return <div className="grid min-w-0 gap-2 text-xs"><p>{t("Original source set: {count} records",{count})}</p><p className="break-words">{t("Expression")}: <code>mean({field})</code></p><p className="text-lg font-semibold tabular-nums">{mean===undefined?t("No numeric values"):mean.toFixed(2)}{unit&&` ${unit}`}</p><p className="text-muted">{t("The complete set count includes missing values; the average uses the original host's numeric-value semantics.")}</p></div>;
}
