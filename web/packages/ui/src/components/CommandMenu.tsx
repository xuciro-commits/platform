import {ContextMenu,DropdownMenu} from 'radix-ui';
import {useId,useRef,type ReactNode} from 'react';
import {MoreHorizontal} from 'lucide-react';
import {Button} from '../primitives/button';

export type ContextCommand={id:string;label:string;disabled?:boolean;icon?:ReactNode;shortcut?:string;danger?:boolean;separatorBefore?:boolean;run:()=>void};
const panel='z-[100] max-h-80 min-w-44 overflow-auto rounded-md border border-border bg-surface p-1 text-sm shadow-lg';
const item='flex cursor-default items-center rounded px-2 py-1 outline-none data-[highlighted]:bg-row-selected data-[disabled]:pointer-events-none data-[disabled]:opacity-40';
const commandClass=(command:ContextCommand)=>`${item} ${command.danger?'text-[var(--tone-danger)]':''} ${command.separatorBefore?'mt-1 border-t border-border pt-2':''}`;
function CommandLabel({command}:{command:ContextCommand}){
 return <>{command.icon&&<span className="mr-2 [&_svg]:size-3.5">{command.icon}</span>}<span className="flex-1">{command.label}</span>{command.shortcut&&<span className="ml-4 text-[10px] text-muted">{command.shortcut}</span>}</>;
}
/** Pointer menus and an explicit keyboard/touch trigger share caller-owned commands. */
export function CommandMenu({children,label,commands,focusKey,compact=false}:{children:ReactNode;label:string;commands:ContextCommand[];focusKey?:string;compact?:boolean}){
 const trigger=useId();
 const previous=useRef<HTMLElement|null>(null);
 const remember=()=>{previous.current=document.activeElement instanceof HTMLElement?document.activeElement:null;};
 const restore=(event:Event)=>{event.preventDefault();requestAnimationFrame(()=>{
  if(compact)document.getElementById(trigger)?.focus();
  else if(previous.current?.isConnected)previous.current.focus();
  else if(focusKey)document.querySelector<HTMLButtonElement>(`button[data-command-focus="${CSS.escape(focusKey)}"]`)?.focus();
 });};
 return <ContextMenu.Root>
  <ContextMenu.Trigger asChild><div className="flex min-w-0 items-center" onContextMenu={remember} onKeyDown={event=>{
   if(!event.shiftKey||event.key!=='F10')return;event.preventDefault();event.stopPropagation();remember();
   const rect=event.currentTarget.getBoundingClientRect();event.currentTarget.dispatchEvent(new MouseEvent('contextmenu',{bubbles:true,cancelable:true,clientX:rect.left+8,clientY:rect.top+8}));
  }}>
   <div className="min-w-0 flex-1">{children}</div>
   <DropdownMenu.Root>
    <DropdownMenu.Trigger asChild><Button id={trigger} variant="ghost" size="sm" className={compact?'h-5 w-5 rounded-sm p-0 text-inherit hover:bg-white/15':''} aria-label={label} title={label} data-command-focus={focusKey} onPointerDown={remember} onKeyDown={remember}><MoreHorizontal/></Button></DropdownMenu.Trigger>
    <DropdownMenu.Portal><DropdownMenu.Content aria-label={label} sideOffset={4} align="end" className={panel} onKeyDown={event=>event.stopPropagation()} onCloseAutoFocus={restore}>
     {commands.map(command=><DropdownMenu.Item key={command.id} className={commandClass(command)} disabled={command.disabled} onSelect={command.run}><CommandLabel command={command}/></DropdownMenu.Item>)}
    </DropdownMenu.Content></DropdownMenu.Portal>
   </DropdownMenu.Root>
  </div></ContextMenu.Trigger>
  <ContextMenu.Portal><ContextMenu.Content aria-label={label} className={panel} onKeyDown={event=>event.stopPropagation()} onCloseAutoFocus={restore}>
   {commands.map(command=><ContextMenu.Item key={command.id} className={commandClass(command)} disabled={command.disabled} onSelect={command.run}><CommandLabel command={command}/></ContextMenu.Item>)}
  </ContextMenu.Content></ContextMenu.Portal>
 </ContextMenu.Root>;
}
