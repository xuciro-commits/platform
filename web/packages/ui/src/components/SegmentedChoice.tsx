import type {ReactNode} from 'react';
import {Button} from '../primitives/button';
import {cn} from '../lib/cn';

/** One controlled choice. Icon-only segments always have names and tooltips. */
export function SegmentedChoice({label,value,options,compact=false,disabled,onChange}:{label:string;value:string;options:readonly {value:string;label:string;icon?:ReactNode;disabled?:boolean}[];compact?:boolean;disabled?:boolean;onChange?:(value:string)=>void}){
 const active=options.find(option=>option.value===value&&!option.disabled)??options.find(option=>!option.disabled);
 return <div role="group" aria-label={label} className={cn('flex min-w-0',compact?'overflow-hidden rounded-md border border-border bg-canvas p-0.5':'flex-wrap gap-1')} onKeyDown={event=>{
  if(event.altKey||event.ctrlKey||event.metaKey||disabled||!onChange)return;
  const buttons=Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'));
  const at=buttons.indexOf(event.target as HTMLButtonElement);
  const to=event.key==='ArrowRight'||event.key==='ArrowDown'?(at+1)%buttons.length:event.key==='ArrowLeft'||event.key==='ArrowUp'?(at+buttons.length-1)%buttons.length:event.key==='Home'?0:event.key==='End'?buttons.length-1:undefined;
  if(to===undefined||!buttons.length)return;event.preventDefault();event.stopPropagation();buttons[to]!.focus();buttons[to]!.click();
 }}>
  {options.map(option=><Button key={option.value} aria-pressed={value===option.value} aria-label={option.label} title={option.label} tabIndex={active===option?0:-1} disabled={disabled||option.disabled||!onChange} size="sm" variant={compact?'ghost':value===option.value?'primary':'default'} className={cn(compact?'h-6 min-w-0 flex-1 rounded-sm px-1 text-xs':'h-auto min-w-0 whitespace-normal break-words text-left',compact&&value===option.value&&'bg-surface text-primary shadow-sm')} onClick={()=>onChange?.(option.value)}>
   {option.icon?<span aria-hidden className="flex [&_svg]:size-3.5">{option.icon}</span>:option.label}
  </Button>)}
 </div>;
}
