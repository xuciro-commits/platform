import {Button} from "../primitives/button";
import {t} from "../i18n";

import {rangeGrid,rangeDrafts} from "./range";
export {rangeGrid,rangeDrafts} from "./range";
export function RangeInput({min,max,step,lower,upper,label,unit="",onChange}:{min:string;max:string;step:string;lower:string;upper:string;label:string;unit?:string;onChange:(lower:string,upper:string)=>void}){
 const grid=rangeGrid(min,max,step),drafts=rangeDrafts(grid,lower,upper),text=(value:string)=>value===""?t("Any bound"):value;
 return <fieldset className="grid min-w-0 gap-2"><legend className="break-words text-xs font-medium">{label}</legend><output className="break-words text-sm tabular-nums">{text(lower)} – {text(upper)}{unit?` ${unit}`:""}</output>{["Minimum","Maximum"].map((side,index)=><label className="grid min-w-0 gap-1 text-xs" key={side}>{t(side)}<input aria-label={t("{label} {side}",{label,side:t(side)})} type="range" min={0} max={grid?.ticks??1} step={1} value={index===0?drafts?.a??0:drafts?.b??grid?.ticks??1} disabled={!drafts} onChange={event=>{if(!grid||!drafts)return;const tick=Number(event.target.value);if(!Number.isInteger(tick)||tick<0||tick>grid.ticks)return;onChange(index===0?grid.text(Math.min(tick,drafts.b)):lower,index===1?grid.text(Math.max(tick,drafts.a)):upper);}} className="w-full accent-primary"/><span className="text-muted">{index===0?min:max}{unit?` ${unit}`:""}</span></label>)}{!drafts&&<p role="alert" className="break-words text-xs text-danger">{t("Range drafts must be valid ordered values on the configured step grid. Edit the original inputs or clear both bounds.")}</p>}<Button size="sm" variant="ghost" className="justify-self-start" onClick={()=>onChange("","")}>{t("Clear range")}</Button></fieldset>;
}
