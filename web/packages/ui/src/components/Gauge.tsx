import {t} from "../i18n";
export function gaugeModel(value:number,max:number,warnAt?:number) {
 if(!Number.isFinite(value)||!Number.isFinite(max)||max<=0||warnAt!==undefined&&!Number.isFinite(warnAt))return;
 const fraction=Math.max(0,Math.min(1,value/max)),angle=Math.PI*(1-fraction);
 return {fraction,x:60+Math.cos(angle)*46,y:60-Math.sin(angle)*46,danger:warnAt!==undefined&&value>=warnAt,outside:value<0||value>max};
}
/** Original finite host number in; a bounded display arc out. */
export function Gauge({value,max,warnAt,label,suffix=""}:{value:number;max:number;warnAt?:number;label:string;suffix?:string}) {
 const model=gaugeModel(value,max,warnAt);
 if(!model)return <p role="alert">{t("Gauge needs a finite value and positive maximum.")}</p>;
 const text=`${value.toLocaleString(undefined,{maximumFractionDigits:1})}${suffix}`,color=model.danger?"var(--tone-danger)":"var(--tone-success)";
 return <div className="grid min-w-0 justify-items-center gap-1"><span className="justify-self-start break-words text-xs font-medium">{label}</span><svg role="img" aria-label={`${label} · ${text} / ${max}${suffix}`} viewBox="0 0 120 70" className="w-40 max-w-full"><path d="M14 60 A46 46 0 0 1 106 60" fill="none" stroke="var(--row-hover)" strokeWidth="10"/>{model.fraction>0&&<path d={`M14 60 A46 46 0 0 1 ${model.x} ${model.y}`} fill="none" stroke={color} strokeWidth="10"/>}<text x="60" y="58" textAnchor="middle" className="fill-current text-[16px] font-semibold">{text}</text></svg><span className="text-[10px] tabular-nums text-muted">{t("of {max}",{max:`${max}${suffix}`})}</span>{model.outside&&<p role="status" className="text-xs text-warning">{t("Gauge value is outside its display range.")}</p>}</div>;
}
