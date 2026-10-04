import {useEffect,useRef,useState} from "react";
import type {PointerEvent as ReactPointerEvent} from "react";

export type SvgView={k:number;x:number;y:number};
export function clampSvgView(v:SvgView,width:number,height:number,maxZoom=40):SvgView {const k=Math.min(maxZoom,Math.max(1,v.k));return {k,x:Math.min(0,Math.max(width-width*k,v.x)),y:Math.min(0,Math.max(height-height*k,v.y))};}
export function revealSvgPoint(view:SvgView,point:{x:number;y:number},width:number,height:number,inset=40,minZoom=4):SvgView {
 const x=point.x*view.k+view.x,y=point.y*view.k+view.y;if(x>=inset&&x<=width-inset&&y>=inset&&y<=height-inset)return view;const k=Math.max(view.k,minZoom);return clampSvgView({k,x:width/2-point.x*k,y:height/2-point.y*k},width,height);
}
/** Local camera state is shared by maps and annotation; it never changes data coordinates. */
export function useSvgViewport(width:number,height:number,pan=true,maxZoom=40){
 const ref=useRef<SVGSVGElement>(null),[view,setView]=useState<SvgView>({k:1,x:0,y:0}),current=useRef(view),drag=useRef<{x:number;y:number;view:SvgView;moved:boolean}|undefined>(undefined),moved=useRef(false);current.current=view;
 const set=(next:SvgView)=>setView(clampSvgView(next,width,height,maxZoom));
 const point=(x:number,y:number)=>{const svg=ref.current,ctm=svg?.getScreenCTM();if(!svg||!ctm)return;const p=svg.createSVGPoint();p.x=x;p.y=y;return p.matrixTransform(ctm.inverse());};
 const imagePoint=(x:number,y:number)=>{const p=point(x,y),v=current.current;return p?{x:Math.max(0,Math.min(width,(p.x-v.x)/v.k)),y:Math.max(0,Math.min(height,(p.y-v.y)/v.k))}:undefined;};
 const zoom=(factor:number,x=width/2,y=height/2)=>{const v=current.current,k=Math.min(maxZoom,Math.max(1,v.k*factor));set({k,x:x-(x-v.x)*k/v.k,y:y-(y-v.y)*k/v.k});};
 useEffect(()=>{const svg=ref.current;if(!svg)return;const wheel=(e:WheelEvent)=>{e.preventDefault();const p=point(e.clientX,e.clientY);if(p)zoom(Math.exp(-e.deltaY*.0016),p.x,p.y);};svg.addEventListener("wheel",wheel,{passive:false});return()=>svg.removeEventListener("wheel",wheel);});
 const handlers={onPointerDown:(e:ReactPointerEvent<SVGSVGElement>)=>{if(!pan||e.button!==0)return;const p=point(e.clientX,e.clientY);if(!p)return;moved.current=false;drag.current={x:p.x,y:p.y,view:current.current,moved:false};},onPointerMove:(e:ReactPointerEvent<SVGSVGElement>)=>{const d=drag.current,p=point(e.clientX,e.clientY);if(!d||!p||!pan)return;const dx=p.x-d.x,dy=p.y-d.y;if(Math.hypot(dx,dy)>3){d.moved=true;e.currentTarget.setPointerCapture(e.pointerId);}if(d.moved)set({...d.view,x:d.view.x+dx,y:d.view.y+dy});},onPointerUp:()=>{const d=drag.current;if(d)moved.current=d.moved;drag.current=undefined;},onPointerCancel:()=>{drag.current=undefined;}};
 const fit=(points:{x:number;y:number}[])=>{if(!points.length){set({k:1,x:0,y:0});return;}const x0=Math.min(...points.map(p=>p.x)),x1=Math.max(...points.map(p=>p.x)),y0=Math.min(...points.map(p=>p.y)),y1=Math.max(...points.map(p=>p.y)),k=Math.min(maxZoom,Math.max(1,.8*Math.min(width/Math.max(30,x1-x0),height/Math.max(30,y1-y0))));set({k,x:width/2-(x0+x1)/2*k,y:height/2-(y0+y1)/2*k});};
 return {ref,view,zoom,fit,reveal:(point:{x:number;y:number})=>set(revealSvgPoint(current.current,point,width,height)),reset:()=>set({k:1,x:0,y:0}),imagePoint,handlers,moved:()=>moved.current};
}
