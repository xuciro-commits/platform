import {useId,useState} from "react";
import {Button} from "../primitives/button";
import {t} from "../i18n";
import {validTermCounts,type TermCount} from "../components/TermCounts";

/** Balanced binary splits partition the whole rectangle by original count weights, with no minimum-area inflation. */
export function treemapRectangles(terms:TermCount[],width=1000,height=400) {
 if(!validTermCounts(terms)||!Number.isFinite(width)||!Number.isFinite(height)||width<=0||height<=0)return;
 const ordered=terms.map((term,index)=>({...term,index,key:JSON.stringify(term.value)})).sort((a,b)=>b.count-a.count||a.index-b.index),total=ordered.reduce((sum,term)=>sum+BigInt(term.count),0n),rectangles:{value:string|null;count:number;key:string;x:number;y:number;width:number;height:number;share:string}[]=[];
 const sum=(items:typeof ordered)=>items.reduce((n,item)=>n+BigInt(item.count),0n);
 const layout=(items:typeof ordered,x:number,y:number,w:number,h:number)=>{
  if(!items.length)return;
  if(items.length===1){const term=items[0]!,basis=(BigInt(term.count)*10000n+total/2n)/total;rectangles.push({value:term.value,count:term.count,key:term.key,x,y,width:w,height:h,share:`${basis/100n}.${(basis%100n).toString().padStart(2,"0")}%`});return;}
  const weight=sum(items);let prefix=0n,cut=1,best=weight;
  for(let i=1;i<items.length;i++){prefix+=BigInt(items[i-1]!.count);const distance=prefix*2n-weight,absolute=distance<0n?-distance:distance;if(absolute<best){best=absolute;cut=i;}}
  const left=items.slice(0,cut),right=items.slice(cut),fraction=Number(sum(left))/Number(weight);
  if(w>=h){const split=w*fraction;layout(left,x,y,split,h);layout(right,x+split,y,w-split,h);}else{const split=h*fraction;layout(left,x,y,w,split);layout(right,x,y+split,w,h-split);}
 };
 layout(ordered,0,0,width,height);
 if(rectangles.some(r=>![r.x,r.y,r.width,r.height].every(Number.isFinite)||r.width<=0||r.height<=0))return;
 return {rectangles,total,width,height};
}

export function CountTreemap({terms,label,selected=[],enabled=true,onSelect}:{terms:TermCount[];label:string;selected?:readonly string[];enabled?:boolean;onSelect?:(value?:string)=>void}) {
 const clip=useId().replace(/[^A-Za-z0-9_-]/g,""),[focus,setFocus]=useState<string>(),model=treemapRectangles(terms);
 if(!model)return <p role="alert">{t("Treemap counts or geometry are invalid or exceed their budget.")}</p>;
 const name=(value:string|null)=>value===null?t("No value (missing)"):value===""?t("Empty text"):value,title=(r:typeof model.rectangles[number])=>`${name(r.value)} · ${r.count} · ${r.share}`,color=(index:number)=>`var(${["--tone-info","--tone-success","--tone-warning","--tone-danger","--chart-5","--chart-6","--tone-neutral"][index%7]})`,active=model.rectangles.find(r=>r.key===focus),allowed=(value:string|null)=>!!onSelect&&enabled&&value!==null,picked=(value:string|null)=>value!==null&&selected.includes(value);
 return <div className="grid min-w-0 gap-2"><p role="status" className="text-xs text-muted">{t("{count} matching records · rectangle area follows group count",{count:model.total.toString()})}</p>{!model.rectangles.length?<p role="status">{t("No groups in the matching records.")}</p>:<><svg viewBox={`0 0 ${model.width} ${model.height}`} role="group" aria-label={label} className="w-full">
 <defs>{model.rectangles.map((r,index)=><clipPath key={r.key} id={`${clip}-${index}`}><rect x={r.x} y={r.y} width={r.width} height={r.height}/></clipPath>)}</defs>
 {model.rectangles.map((r,index)=><g key={r.key} role={onSelect?"button":undefined} tabIndex={onSelect?allowed(r.value)?0:-1:0} aria-label={title(r)} aria-disabled={onSelect?!allowed(r.value):undefined} aria-pressed={onSelect?picked(r.value):undefined} className={onSelect?"cursor-pointer focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring":"focus-visible:outline focus-visible:outline-2 focus-visible:outline-ring"} onMouseEnter={()=>setFocus(r.key)} onMouseLeave={()=>setFocus(undefined)} onFocus={()=>setFocus(r.key)} onBlur={()=>setFocus(undefined)} onClick={()=>{if(allowed(r.value))onSelect!(r.value!);}} onKeyDown={e=>{if(allowed(r.value)&&(e.key==="Enter"||e.key===" ")){e.preventDefault();onSelect!(r.value!);}}}><title>{title(r)}</title><rect x={r.x} y={r.y} width={r.width} height={r.height} fill={color(index)} fillOpacity={picked(r.value)?0.85:0.55} stroke={picked(r.value)?"var(--foreground)":"var(--surface)"} strokeWidth={picked(r.value)?3:1}/>{r.width>=140&&r.height>=60&&<text clipPath={`url(#${clip}-${index})`} x={r.x+8} y={r.y+22} fill="var(--foreground)" fontSize="15"><tspan>{name(r.value).slice(0,Math.max(1,Math.floor((r.width-16)/9)))}</tspan><tspan x={r.x+8} dy="20">{r.count} · {r.share}</tspan></text>}</g>)}
 </svg><p role="status" className="min-h-4 break-all text-xs text-muted">{active?title(active):t("Hover or focus a group to inspect its original count and share.")}</p><ul aria-label={t("Treemap groups")} className="grid min-w-0 grid-cols-1 gap-1 sm:grid-cols-2">{model.rectangles.map((r,index)=><li key={r.key} className="min-w-0">{onSelect?<Button disabled={!allowed(r.value)} aria-pressed={picked(r.value)} variant={picked(r.value)?"primary":"ghost"} className="h-auto w-full justify-start whitespace-normal break-all text-left text-xs" onClick={()=>onSelect(r.value!)}><span aria-hidden="true" className="h-2 w-2 shrink-0" style={{background:color(index)}}/>{title(r)}</Button>:<span className="flex min-w-0 items-center gap-2 break-all text-xs"><span aria-hidden="true" className="h-2 w-2 shrink-0" style={{background:color(index)}}/>{title(r)}</span>}</li>)}</ul></>}{onSelect&&<Button size="sm" disabled={!enabled} onClick={()=>onSelect()}>{t("Clear group filter")}</Button>}{onSelect&&<p className="text-xs text-muted">{t("Small rectangles keep their true area and remain available in the group list. Missing groups cannot be written to text filters.")}</p>}</div>;
}
