import {useEffect,useRef,type ReactNode,type ComponentProps} from 'react';
import {ArrowDown,ArrowLeft,ArrowRight,ArrowUp,GripVertical} from 'lucide-react';
import {Button} from '../primitives/button';
import {CommandMenu,type ContextCommand} from '../components/CommandMenu';
import {t} from '../i18n';
import type {CanvasRect} from './CanvasEditor';

export type CanvasCommand=ContextCommand&{primary?:boolean};
/** One attached, screen-sized strip. The caller supplies context-valid commands. */
export function CanvasSelectionToolbar({label,icon,bounds,viewport,horizontal,canMove,canPrevious,canNext,onDrag,onMove,commands}: {
 label:string;icon?:ReactNode;bounds:CanvasRect;viewport?:CanvasRect;horizontal:boolean;canMove:boolean;canPrevious:boolean;canNext:boolean;
 onDrag:ComponentProps<typeof Button>['onPointerDown'];onMove:(delta:-1|1)=>void;commands:CanvasCommand[];
}){
 const toolbar=useRef<HTMLDivElement>(null),lastFocus=useRef<HTMLElement|undefined>(undefined);
 const clipped=viewport&&(bounds.x+bounds.width<=viewport.x||bounds.x>=viewport.x+viewport.width||bounds.y+bounds.height<=viewport.y||bounds.y>=viewport.y+viewport.height);
 const available=Math.min(viewport?.width??innerWidth,Math.max(124,Math.min(bounds.width,300)));
 const direct=commands.filter(c=>c.primary).slice(0,Math.max(0,Math.min(5,Math.floor((available-124)/22))));
 const tabStops=()=>Array.from(toolbar.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)')??[]).filter(button=>button.offsetParent!==null);
 const refresh=()=>{const items=tabStops(),active=lastFocus.current?.isConnected?lastFocus.current:items[0];for(const item of items)item.tabIndex=item===active?0:-1;};
 useEffect(refresh,[commands,canPrevious,canNext,available]);
 if(clipped)return null;
 const inside=!!viewport&&bounds.y-22<viewport.y;
 return <div ref={toolbar} role="toolbar" aria-label={t('Canvas selection tools')} style={{position:'fixed',left:Math.max(viewport?.x??0,Math.min(bounds.x,(viewport?viewport.x+viewport.width:innerWidth)-available))-1,top:inside?Math.max(viewport!.y,bounds.y):bounds.y-22,height:22,width:available,zIndex:70}}
  className="flex items-center gap-0 rounded-t-sm border border-primary bg-primary px-0.5 text-primary-foreground [&_svg]:size-3 [&_button]:shrink-0"
  onClick={event=>event.stopPropagation()} onFocusCapture={event=>{if(event.target instanceof HTMLButtonElement&&toolbar.current?.contains(event.target)){lastFocus.current=event.target;refresh();}}}
  onKeyDown={event=>{
   if(event.altKey||event.ctrlKey||event.metaKey)return;
   const items=tabStops(),at=items.indexOf(document.activeElement as HTMLButtonElement);
   const index=event.key==='ArrowRight'?(at+1)%items.length:event.key==='ArrowLeft'?(at+items.length-1)%items.length:event.key==='Home'?0:event.key==='End'?items.length-1:undefined;
   if(index===undefined||!items.length)return;event.preventDefault();event.stopPropagation();items[index]!.focus();
  }}>
  <Button variant="ghost" size="sm" className="h-5 w-5 rounded-sm p-0 text-inherit hover:bg-white/15" aria-label={t('Drag selected region')} title={t('Drag selected region')} disabled={!canMove} style={{touchAction:'none',cursor:'grab'}} onPointerDown={onDrag}><GripVertical/></Button>
  {icon&&<span aria-hidden className="flex shrink-0">{icon}</span>}
  <span className="mx-1 min-w-0 flex-1 truncate text-[10px] font-medium" title={label}>{label}</span>
  <Button variant="ghost" size="sm" className="h-5 w-5 rounded-sm p-0 text-inherit hover:bg-white/15" disabled={!canPrevious} aria-label={t(horizontal?'Move left':'Move up')} title={t(horizontal?'Move left':'Move up')} onClick={()=>onMove(-1)}>{horizontal?<ArrowLeft/>:<ArrowUp/>}</Button>
  <Button variant="ghost" size="sm" className="h-5 w-5 rounded-sm p-0 text-inherit hover:bg-white/15" disabled={!canNext} aria-label={t(horizontal?'Move right':'Move down')} title={t(horizontal?'Move right':'Move down')} onClick={()=>onMove(1)}>{horizontal?<ArrowRight/>:<ArrowDown/>}</Button>
  {direct.map(command=><Button key={command.id} variant="ghost" size="sm" className="h-5 w-5 rounded-sm p-0 text-inherit hover:bg-white/15" disabled={command.disabled} title={`${command.label}${command.shortcut?' · '+command.shortcut:''}`} aria-label={command.label} onClick={command.run}>{command.icon??command.label}</Button>)}
  {commands.length>0&&<CommandMenu compact label={t('More canvas actions')} commands={commands}><span/></CommandMenu>}
 </div>;
}
