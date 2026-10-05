import {CanvasSelectionToolbar,type CanvasCommand} from "./CanvasSelectionToolbar";
import {createContext,useContext,useEffect,useRef,useState,type ReactNode,type PointerEvent as ReactPointerEvent} from 'react';
import {createPortal} from 'react-dom';
import {t} from '../i18n';
import type {LayoutSize} from './LayoutRegion';

export type CanvasRect={x:number;y:number;width:number;height:number};
export type CanvasModel=Record<string,{label:string;kind:'widget'|'container';layout?:string;parent?:string;children:string[];horizontal:boolean;fixed?:boolean}>;
export type CanvasPayload={kind:'move';id:string;label:string}|{kind:'new';type:string;label:string};
export type CanvasDrop={kind:'insert';parent:string;index:number}|{kind:'into';parent:string}|{kind:'swap';target:string}|{kind:'wrap';target:string;side:'left'|'right'|'top'|'bottom'};
type Target={intent:CanvasDrop;rect:CanvasRect;line?:'horizontal'|'vertical';label:string};
type State={payload:CanvasPayload;x:number;y:number;target?:Target};
type Gesture={start:(payload:CanvasPayload,event:ReactPointerEvent)=>void;select:(id:string)=>void;selected?:string;disabled?:boolean};
const Context=createContext<Gesture|undefined>(undefined);
export function useCanvasGesture(){return useContext(Context);}
/** Editor chrome only. Layout ownership, permissions and accepted edits remain with the caller. */
export function CanvasRegion({id,children}: {id:string;children:ReactNode}){
 const canvas=useCanvasGesture();
 return <div data-canvas-node={id} className="relative flex min-h-0 min-w-0 flex-1 flex-col" onClick={event=>{
  if((event.target as HTMLElement).closest('[data-canvas-node]')!==event.currentTarget)return;
  event.stopPropagation();canvas?.select(id);
 }}>{children}</div>;
}
const rect=(el:Element):CanvasRect=>{const r=el.getBoundingClientRect();return {x:r.left,y:r.top,width:r.width,height:r.height};};
const same=(a?:CanvasRect,b?:CanvasRect)=>JSON.stringify(a)===JSON.stringify(b);
const box=(r:CanvasRect)=>({position:'fixed' as const,left:r.x,top:r.y,width:r.width,height:r.height});
function visibleRect(el:Element):CanvasRect {
 const r=el.getBoundingClientRect();let left=Math.max(0,r.left),top=Math.max(0,r.top),right=Math.min(innerWidth,r.right),bottom=Math.min(innerHeight,r.bottom);
 for(let p=el.parentElement;p;p=p.parentElement){const css=getComputedStyle(p),b=p.getBoundingClientRect();if(/auto|scroll|hidden|clip/.test(css.overflowX)){left=Math.max(left,b.left);right=Math.min(right,b.right);}if(/auto|scroll|hidden|clip/.test(css.overflowY)){top=Math.max(top,b.top);bottom=Math.min(bottom,b.bottom);}}
 return {x:left,y:top,width:Math.max(0,right-left),height:Math.max(0,bottom-top)};
}

export function CanvasEditor({model,selected,onSelect,onDrop,onMove,resize,onResize,onResetSize,children,zoom=1,disabled=false,revision,commands=[],icon}: {
 model:CanvasModel;selected?:string;onSelect:(id:string)=>void;onDrop:(payload:CanvasPayload,target:CanvasDrop)=>void;onMove:(id:string,delta:-1|1)=>void;
 resize:(id:string,axis:'width'|'height',pixels:number,rects:Record<string,CanvasRect>)=>Record<string,LayoutSize>|undefined;
 onResize:(sizes:Record<string,LayoutSize>)=>void;onResetSize:(id:string,axis:'width'|'height')=>void;
 children:ReactNode;commands?:CanvasCommand[];icon?:ReactNode;zoom?:number;disabled?:boolean;revision?:unknown;
}){
 const root=useRef<HTMLDivElement>(null),current=useRef({model,selected,onSelect,onDrop,onMove,resize,onResize,onResetSize,zoom,disabled});
 current.current={model,selected,onSelect,onDrop,onMove,resize,onResize,onResetSize,zoom,disabled};
 const cancel=useRef<()=>void>(()=>{}),[drag,setDrag]=useState<State>(),[selectionBox,setSelectionBox]=useState<CanvasRect>(),[resizeLabel,setResizeLabel]=useState(''),[resizeGuide,setResizeGuide]=useState<CanvasRect>();
 const elements=(id:string)=>Array.from(root.current?.querySelectorAll<HTMLElement>('[data-canvas-node]')??[]).filter(el=>el.dataset.canvasNode===id);
 const element=(id:string)=>elements(id).find(el=>{const r=el.getBoundingClientRect();return r.width>0&&r.height>0;});
 const region=(id:string)=>element(id)?.closest<HTMLElement>('.platform-layout-region')??element(id);
 const measurements=()=>Object.fromEntries(Object.keys(model).flatMap(id=>{const el=region(id);if(!el)return [];const r=rect(el);return [[id,{x:r.x/zoom,y:r.y/zoom,width:r.width/zoom,height:r.height/zoom}]];}));
 useEffect(()=>{cancel.current();},[revision,disabled]);
 useEffect(()=>()=>cancel.current(),[]);
 useEffect(()=>{
  if(!selected){setSelectionBox(undefined);return;}
  let frame=0;
  const tick=()=>{const el=selected?region(selected):undefined,next=el?rect(el):undefined;setSelectionBox(old=>same(old,next)?old:next);frame=requestAnimationFrame(tick);};
  frame=requestAnimationFrame(tick);return()=>cancelAnimationFrame(frame);
 },[selected]);
 const resolve=(payload:CanvasPayload,x:number,y:number):Target|undefined=>{
  const nodes=current.current.model,excluded=new Set<string>();
  const walk=(id:string)=>{if(excluded.has(id))return;excluded.add(id);for(const child of nodes[id]?.children??[])walk(child);};
  if(payload.kind==='move')walk(payload.id);
  const hits=document.elementsFromPoint(x,y).filter(el=>root.current?.contains(el)) as HTMLElement[];
  const tree=hits.find(el=>el.dataset.canvasTree&&!excluded.has(el.dataset.canvasTree));
  if(tree){
   const id=tree.dataset.canvasTree!,node=nodes[id];if(!node)return;const r=rect(tree),fraction=(y-r.y)/r.height;
   if(node.kind==='container'&&fraction>=.25&&fraction<=.75)return {intent:{kind:'into',parent:id},rect:r,label:t('Place inside {name}',{name:node.label})};
   if(!node.parent)return;const index=nodes[node.parent]!.children.indexOf(id)+(fraction>.5?1:0);
   return {intent:{kind:'insert',parent:node.parent,index},rect:{...r,y:fraction>.5?r.y+r.height:r.y,height:3},line:'horizontal',label:t('Place by {name}',{name:node.label})};
  }
  const el=hits.find(el=>el.dataset.canvasNode&&!excluded.has(el.dataset.canvasNode));if(!el)return;
  const id=el.dataset.canvasNode!,node=nodes[id];if(!node)return;const r=rect(region(id)??el);
  const bx=node.kind==='widget'?Math.max(14,Math.min(r.width*.28,96)):Math.min(14,r.width*.1),by=node.kind==='widget'?Math.max(14,Math.min(r.height*.28,72)):Math.min(14,r.height*.2);
  const edges=[['left',x-r.x,bx],['right',r.x+r.width-x,bx],['top',y-r.y,by],['bottom',r.y+r.height-y,by]] as const;
  const side=edges.filter(([,d,b])=>d>=0&&d<b).sort((a,b)=>a[1]/a[2]-b[1]/b[2])[0]?.[0];
  if(side&&node.parent&&!node.fixed){
   const horizontal=side==='left'||side==='right',after=side==='right'||side==='bottom',parent=nodes[node.parent]!;
   if(horizontal===parent.horizontal||parent.layout==='tabs'||parent.layout==='loop'){
    const index=parent.children.indexOf(id)+(after?1:0);
    const line=horizontal?{x:after?r.x+r.width:r.x,y:r.y,width:3,height:r.height}:{x:r.x,y:after?r.y+r.height:r.y,width:r.width,height:3};
    return {intent:{kind:'insert',parent:node.parent,index},rect:line,line:horizontal?'vertical':'horizontal',label:t('Place by {name}',{name:node.label})};
   }
   return {intent:{kind:'wrap',target:id,side},rect:horizontal?{...r,x:r.x+(after?r.width/2:0),width:r.width/2}:{...r,y:r.y+(after?r.height/2:0),height:r.height/2},label:t('Group beside {name}',{name:node.label})};
  }
  if(node.kind==='widget'){
   if(payload.kind==='move'&&!node.fixed)return {intent:{kind:'swap',target:id},rect:r,label:t('Swap with {name}',{name:node.label})};
   if(!node.parent)return;const after=nodes[node.parent]?.horizontal?x>r.x+r.width/2:y>r.y+r.height/2;
   return {intent:{kind:'insert',parent:node.parent,index:nodes[node.parent]!.children.indexOf(id)+(after?1:0)},rect:r,label:t('Place by {name}',{name:node.label})};
  }
  const children=node.children.filter(child=>!excluded.has(child)),horizontal=node.horizontal;let index=node.children.length;
  for(const child of children){const childEl=element(child);if(!childEl)continue;const c=rect(region(child)??childEl);if((horizontal?x:y)<(horizontal?c.x+c.width/2:c.y+c.height/2)){index=node.children.indexOf(child);break;}}
  return {intent:{kind:'insert',parent:id,index},rect:r,label:t('Place inside {name}',{name:node.label})};
 };
 const start=(payload:CanvasPayload,event:ReactPointerEvent)=>{
  if(event.button!==0||current.current.disabled||payload.kind==='move'&&(!model[payload.id]?.parent||model[payload.id]?.fixed))return;
  cancel.current();let active=false,frame=0,scrollFrame=0,x=event.clientX,y=event.clientY;const sx=x,sy=y,pointer=event.pointerId;
  const update=()=>{frame=0;setDrag({payload,x,y,target:resolve(payload,x,y)});};
  const scroll=()=>{
   const sc=root.current?.querySelector<HTMLElement>('[data-canvas-scroll]');if(sc){const r=sc.getBoundingClientRect(),edge=56,speed=18;const velocity=(p:number,a:number,b:number)=>p<a+edge&&p>a-40?-speed*(1-(p-a)/edge):p>b-edge&&p<b+40?speed*(1-(b-p)/edge):0;
    const dx=velocity(x,r.left,r.right),dy=velocity(y,r.top,r.bottom);if(dx||dy){sc.scrollBy(dx,dy);if(!frame)frame=requestAnimationFrame(update);}}
   scrollFrame=requestAnimationFrame(scroll);
  };
  const move=(e:PointerEvent)=>{if(e.pointerId!==pointer)return;x=e.clientX;y=e.clientY;if(!active){if(Math.hypot(x-sx,y-sy)<5)return;active=true;scrollFrame=requestAnimationFrame(scroll);}e.preventDefault();if(!frame)frame=requestAnimationFrame(update);};
  const finish=(commit=false)=>{
   const target=active&&commit?resolve(payload,x,y):undefined;window.removeEventListener('pointermove',move);window.removeEventListener('pointerup',up);window.removeEventListener('pointercancel',abort);window.removeEventListener('keydown',key,true);window.removeEventListener('blur',abort);
   cancelAnimationFrame(frame);cancelAnimationFrame(scrollFrame);cancel.current=()=>{};setDrag(undefined);
   if(active){const stop=(e:Event)=>{e.preventDefault();e.stopPropagation();};window.addEventListener('click',stop,{capture:true,once:true});setTimeout(()=>window.removeEventListener('click',stop,true),80);}
   if(target)current.current.onDrop(payload,target.intent);
  };
  const up=(e:PointerEvent)=>{if(e.pointerId===pointer){x=e.clientX;y=e.clientY;finish(true);}},abort=()=>finish(),key=(e:KeyboardEvent)=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();finish();}};
  cancel.current=abort;window.addEventListener('pointermove',move,{passive:false});window.addEventListener('pointerup',up);window.addEventListener('pointercancel',abort);window.addEventListener('keydown',key,true);window.addEventListener('blur',abort);
 };
 const startResize=(axis:'width'|'height',event:ReactPointerEvent)=>{
  const id=current.current.selected;if(!id||event.button!==0||current.current.disabled)return;event.preventDefault();event.stopPropagation();cancel.current();
  const measured:Record<string,CanvasRect>=measurements(),styles=new Map<HTMLElement,Record<string,string>>();
  const initial=measured[id];if(!initial)return;const startPoint=axis==='width'?event.clientX:event.clientY,pointer=event.pointerId;let sizes:Record<string,LayoutSize>|undefined,changed=false;
  const preview=(e:PointerEvent)=>{
   if(e.pointerId!==pointer)return;const delta=((axis==='width'?e.clientX:e.clientY)-startPoint)/zoom;
   const pixels=initial[axis]+(axis==='width'?e.clientX-startPoint:e.clientY-startPoint)/zoom;
   sizes=current.current.resize(id,axis,pixels,measured);if(!sizes)return;changed=changed||Math.abs(delta)>0;
   for(const [key,size]of Object.entries(sizes)){const el=region(key);if(!el)continue;if(!styles.has(el))styles.set(el,Object.fromEntries(['width','height','flex','overflow'].map(name=>[name,el.style.getPropertyValue('--platform-preview-'+name)])));
    if(size.width!==undefined){el.style.setProperty('--platform-preview-width',`min(${size.width}px, 100%)`);el.style.setProperty('--platform-preview-flex','0 0 auto');}if(size.height!==undefined){el.style.setProperty('--platform-preview-height',`${size.height}px`);el.style.setProperty('--platform-preview-flex','0 0 auto');}if(size.scroll==='auto')el.style.setProperty('--platform-preview-overflow','auto');}
   const snapped=sizes[id]?.[axis];
   const peer=snapped===undefined?undefined:Object.entries(measured).find(([key,r])=>key!==id&&model[key]?.parent===model[id]?.parent&&Math.abs((axis==='width'?r.x:r.y)-(axis==='width'?initial.x:initial.y))<1&&Math.abs(r[axis]-snapped)<1)?.[1];
   if(snapped!==undefined){const start=peer??initial;setResizeGuide(axis==='width'?{x:(initial.x+snapped)*zoom,y:Math.min(initial.y,start.y)*zoom,width:1,height:(Math.max(initial.y+initial.height,start.y+start.height)-Math.min(initial.y,start.y))*zoom}:{x:Math.min(initial.x,start.x)*zoom,y:(initial.y+snapped)*zoom,width:(Math.max(initial.x+initial.width,start.x+start.width)-Math.min(initial.x,start.x))*zoom,height:1});}
   setResizeLabel(`${Math.round(sizes[id]?.width??initial.width)} × ${Math.round(sizes[id]?.height??initial.height)} px`);
  };
  const finish=(commit=false)=>{window.removeEventListener('pointermove',preview);window.removeEventListener('pointerup',up);window.removeEventListener('pointercancel',abort);window.removeEventListener('keydown',key,true);window.removeEventListener('blur',abort);for(const [el,style]of styles)for(const [name,value]of Object.entries(style)){if(value)el.style.setProperty('--platform-preview-'+name,value);else el.style.removeProperty('--platform-preview-'+name);}cancel.current=()=>{};setResizeLabel('');setResizeGuide(undefined);if(commit&&changed&&sizes)current.current.onResize(sizes);};
  const up=(e:PointerEvent)=>{if(e.pointerId===pointer){preview(e);finish(true);}},abort=()=>finish(),key=(e:KeyboardEvent)=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();finish();}};
  cancel.current=abort;window.addEventListener('pointermove',preview);window.addEventListener('pointerup',up);window.addEventListener('pointercancel',abort);window.addEventListener('keydown',key,true);window.addEventListener('blur',abort);
 };
 const node=selected?model[selected]:undefined,siblings=node?.parent?model[node.parent]?.children??[]:[],index=selected?siblings.indexOf(selected):-1,horizontal=!!node?.parent&&!!model[node.parent]?.horizontal;
 const scroller=root.current?.querySelector('[data-canvas-scroll]'),viewport=scroller?visibleRect(scroller):undefined;
 const clipped=viewport&&selectionBox&&(selectionBox.x+selectionBox.width<=viewport.x||selectionBox.x>=viewport.x+viewport.width||selectionBox.y+selectionBox.height<=viewport.y||selectionBox.y>=viewport.y+viewport.height);
 const clipPath=viewport?`polygon(${viewport.x}px ${viewport.y}px, ${viewport.x+viewport.width}px ${viewport.y}px, ${viewport.x+viewport.width}px ${viewport.y+viewport.height}px, ${viewport.x}px ${viewport.y+viewport.height}px)`:undefined;
 const selection=selectionBox&&node&&!disabled&&!drag&&!clipped?<>
  <div style={{position:'fixed',inset:0,clipPath,pointerEvents:'none',zIndex:60}}><div style={{...box(selectionBox),outline:'2px solid var(--color-primary)'}}/></div>
  <CanvasSelectionToolbar label={node.label} icon={icon} bounds={selectionBox} viewport={viewport}
    horizontal={horizontal} canMove={!!node.parent&&!node.fixed} canPrevious={index>0&&!node.fixed} canNext={index>=0&&index<siblings.length-1&&!node.fixed}
    onDrag={event=>start({kind:'move',id:selected!,label:node.label},event)} onMove={delta=>onMove(selected!,delta)} commands={commands}/>
  <div style={{position:'fixed',inset:0,clipPath,pointerEvents:'none',zIndex:61}}>{(['width','height'] as const).map(axis=><div key={axis} role="separator" tabIndex={0} aria-orientation={axis==='width'?'vertical':'horizontal'} aria-label={t(axis==='width'?'Resize region width':'Resize region height')} title={t('Drag to resize; double-click to reset')} style={{position:'fixed',left:axis==='width'?selectionBox.x+selectionBox.width-5:selectionBox.x,top:axis==='height'?selectionBox.y+selectionBox.height-5:selectionBox.y,width:axis==='width'?10:selectionBox.width,height:axis==='height'?10:selectionBox.height,zIndex:61,pointerEvents:'auto',cursor:axis==='width'?'col-resize':'row-resize',touchAction:'none'}}
   onPointerDown={e=>startResize(axis,e)} onDoubleClick={()=>onResetSize(selected!,axis)} onKeyDown={e=>{if(e.key==='Home'){e.preventDefault();onResetSize(selected!,axis);return;}const delta=e.key===(axis==='width'?'ArrowRight':'ArrowDown')?8:e.key===(axis==='width'?'ArrowLeft':'ArrowUp')?-8:0;if(!delta)return;e.preventDefault();const rects=measurements(),r=rects[selected!];if(!r)return;const sizes=resize(selected!,axis,r[axis]+delta,rects);if(sizes)onResize(sizes);}}>
   <span className="absolute left-1/2 top-1/2 block -translate-x-1/2 -translate-y-1/2 rounded bg-primary" style={{width:axis==='width'?3:40,height:axis==='width'?40:3}}/>
  </div>)}</div>
  {resizeGuide&&<div style={{...box(resizeGuide),zIndex:71,pointerEvents:'none',background:'var(--color-primary)'}}/>}
  {resizeLabel&&<span role="status" style={{position:'fixed',left:selectionBox.x+12,top:selectionBox.y+12,zIndex:62}} className="rounded bg-surface p-1 text-xs shadow">{resizeLabel}</span>}
 </>:null;
 return <Context.Provider value={{start,select:onSelect,selected,disabled}}><div ref={root} className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>{typeof document!=='undefined'&&createPortal(<>{selection}{drag&&<>
  {drag.target&&<div style={{...box(drag.target.rect),zIndex:62,pointerEvents:'none',background:'color-mix(in srgb, var(--color-primary) 18%, transparent)',outline:'2px solid var(--color-primary)'}}/>}
  <div role="status" style={{position:'fixed',left:drag.x+14,top:drag.y+14,zIndex:63,pointerEvents:'none'}} className="rounded bg-surface p-2 text-xs shadow">{drag.payload.label} · {drag.target?.label??t('Choose a drop location')}</div>
 </>}</>,document.body)}</Context.Provider>;
}
