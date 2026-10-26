/** Logical CSS pixels: guides stay invariant when the editor is zoomed. */
export type ResizeGuide={position:number;kind:'edge'|'grid'};
export function snapDimension(value:number,min:number,max:number,step=8,candidates:number[]=[],tolerance=6):{value:number;guide?:ResizeGuide} {
 if(!Number.isFinite(value)||!Number.isFinite(min)||!Number.isFinite(max)||min>max||!Number.isFinite(step)||step<=0)throw new Error('Invalid canvas size bounds');
 const bounded=Math.max(min,Math.min(max,value));
 const nearby=candidates.filter(n=>Number.isFinite(n)&&n>=min&&n<=max&&Math.abs(n-bounded)<=tolerance).sort((a,b)=>Math.abs(a-bounded)-Math.abs(b-bounded)||a-b);
 if(nearby[0]!==undefined)return {value:Math.round(nearby[0]),guide:{position:nearby[0],kind:'edge'}};
 const first=Math.ceil(min/step)*step,last=Math.floor(max/step)*step;
 const snapped=first<=last?Math.max(first,Math.min(last,Math.round(bounded/step)*step)):bounded;
 return {value:Math.round(snapped),guide:{position:snapped,kind:'grid'}};
}
