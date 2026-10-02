import {ContextMenu,DropdownMenu} from "radix-ui";
import {useRef,type ReactNode} from "react";
import {MoreHorizontal} from "lucide-react";
import {Button} from "../primitives/button";

export type ContextCommand={id:string;label:string;disabled?:boolean;run:()=>void};
const panel="z-50 max-h-80 min-w-44 overflow-auto rounded-md border border-border bg-surface p-1 text-sm shadow-lg";
const item="flex cursor-default items-center rounded px-2 py-1 outline-none data-[highlighted]:bg-row-selected data-[disabled]:pointer-events-none data-[disabled]:opacity-40";

/** Pointer context menus and an explicit keyboard/touch trigger share the
 * caller's commands. Radix owns focus return, arrow navigation and Escape. */
export function CommandMenu({children,label,commands,focusKey}:{children:ReactNode;label:string;commands:ContextCommand[];focusKey?:string}) {
 const previous=useRef<HTMLElement|null>(null);
 const remember=()=>{previous.current=document.activeElement instanceof HTMLElement?document.activeElement:null;};
 const restore=(event:Event)=>{event.preventDefault();requestAnimationFrame(()=>{if(previous.current?.isConnected)previous.current.focus();else if(focusKey)document.querySelector<HTMLButtonElement>(`button[data-command-focus="${CSS.escape(focusKey)}"]`)?.focus();});};
 return <ContextMenu.Root><ContextMenu.Trigger asChild><div className="flex min-w-0 items-center" onContextMenu={remember} onKeyDown={event=>{if(event.shiftKey&&event.key==="F10"){event.preventDefault();event.stopPropagation();remember();const rect=event.currentTarget.getBoundingClientRect();event.currentTarget.dispatchEvent(new MouseEvent("contextmenu",{bubbles:true,cancelable:true,clientX:rect.left+8,clientY:rect.top+8}));}}}>
 <div className="min-w-0 flex-1">{children}</div>
 <DropdownMenu.Root><DropdownMenu.Trigger asChild><Button variant="ghost" size="sm" aria-label={label} data-command-focus={focusKey} onPointerDown={remember} onKeyDown={remember}><MoreHorizontal/></Button></DropdownMenu.Trigger>
 <DropdownMenu.Portal><DropdownMenu.Content aria-label={label} sideOffset={4} align="end" className={panel} onKeyDown={event=>event.stopPropagation()} onCloseAutoFocus={restore}>{commands.map(c=><DropdownMenu.Item key={c.id} className={item} disabled={c.disabled} onSelect={c.run}>{c.label}</DropdownMenu.Item>)}</DropdownMenu.Content></DropdownMenu.Portal></DropdownMenu.Root>
 </div></ContextMenu.Trigger><ContextMenu.Portal><ContextMenu.Content aria-label={label} className={panel} onKeyDown={event=>event.stopPropagation()} onCloseAutoFocus={restore}>{commands.map(c=><ContextMenu.Item key={c.id} className={item} disabled={c.disabled} onSelect={c.run}>{c.label}</ContextMenu.Item>)}</ContextMenu.Content></ContextMenu.Portal></ContextMenu.Root>;
}
