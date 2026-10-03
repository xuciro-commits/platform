import {t} from "../i18n";

/** Exact bounded decimal scalars in; only the display percentage is rounded. */
export function progressRatio(value:string,total:string) {
 const parts=(text:string)=>{if(text.length>128||!/^(-?)(0|[1-9][0-9]*)(?:\.([0-9]+))?$/.test(text))return;const [whole,fraction=""]=text.split(".");return {coefficient:BigInt(whole+fraction),scale:fraction.length};};
 const a=parts(value),b=parts(total);if(!a||!b||a.coefficient<0n||b.coefficient<=0n)return;
 const scale=Math.max(a.scale,b.scale),n=a.coefficient*10n**BigInt(scale-a.scale),d=b.coefficient*10n**BigInt(scale-b.scale),over=n>d;
 const bounded=over?d:n,basisPoints=(bounded*10000n+d/2n)/d,percentage=(n*100n+d/2n)/d;
 return {width:Number(basisPoints)/100,percentage:percentage.toString(),over,tone:n*100n>d*80n?"success":n*100n>d*40n?"info":"warning"};
}
export function Progress({value,total,label}:{value:string;total:string;label:string}) {
 const result=progressRatio(value,total);
 if(!result)return <p role="alert" className="text-sm text-danger">{t("Progress needs a valid nonnegative value and a positive total.")}</p>;
 const text=`${value} / ${total} · ${result.percentage}%`,color=result.tone==="success"?"bg-[var(--tone-success)]":result.tone==="info"?"bg-[var(--tone-info)]":"bg-[var(--tone-warning)]";
 return <div className="grid min-w-0 gap-2"><div className="flex min-w-0 flex-wrap justify-between gap-2 text-xs"><span className="font-medium">{label}</span><span className="break-all tabular-nums text-muted">{text}</span></div><div role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={result.width} aria-valuetext={text} className="h-2 overflow-hidden rounded bg-row-hover"><div className={`h-full ${color}`} style={{width:`${result.width}%`}}/></div>{result.over&&<p role="status" className="text-xs text-warning">{t("Value exceeds total; the bar is capped at 100%.")}</p>}</div>;
}
