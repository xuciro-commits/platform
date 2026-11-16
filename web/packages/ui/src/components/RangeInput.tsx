import {Button} from "../primitives/button";
import {t} from "../i18n";
import { Slider } from "@astryxdesign/core/Slider";

import {rangeGrid,rangeDrafts} from "./range";
export {rangeGrid,rangeDrafts} from "./range";
export function RangeInput({min,max,step,lower,upper,label,unit="",onChange}:{min:string;max:string;step:string;lower:string;upper:string;label:string;unit?:string;onChange:(lower:string,upper:string)=>void}){
 const grid=rangeGrid(min,max,step),drafts=rangeDrafts(grid,lower,upper),text=(value:string)=>value===""?t("Any bound"):value;
 return <fieldset className="grid min-w-0 gap-2"><legend className="break-words text-xs font-medium">{label}</legend><output className="break-words text-sm tabular-nums">{text(lower)} – {text(upper)}{unit?` ${unit}`:""}</output><Slider label={label} isLabelHidden min={0} max={grid?.ticks??1} step={1} value={[drafts?.a??0,drafts?.b??grid?.ticks??1]} isDisabled={!drafts} formatValue={(value)=>text(grid?.text(value)??String(value))} onChange={([a,b]: [number, number])=>{if(!grid||!drafts)return;if(!Number.isInteger(a)||!Number.isInteger(b)||a<0||b>grid.ticks)return;onChange(grid.text(Math.min(a,b)),grid.text(Math.max(a,b)));}} /><div className="flex justify-between text-xs text-muted"><span>{min}{unit?` ${unit}`:""}</span><span>{max}{unit?` ${unit}`:""}</span></div>{!drafts&&<p role="alert" className="break-words text-xs text-danger">{t("Range drafts must be valid ordered values on the configured step grid. Edit the original inputs or clear both bounds.")}</p>}<Button size="sm" variant="ghost" className="justify-self-start" onClick={()=>onChange("","")}>{t("Clear range")}</Button></fieldset>;
}
