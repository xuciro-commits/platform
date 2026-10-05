import type {InputHTMLAttributes,ReactNode} from 'react';
import {Input} from '../primitives/input';
import {cn} from '../lib/cn';

/** Short visual prefixes retain a full accessible property name. */
export function InspectorField({label,prefix,unit,className,...input}:Omit<InputHTMLAttributes<HTMLInputElement>,'prefix'> & {label:string;prefix:ReactNode;unit?:string}){
 return <label title={label} className={cn('flex min-w-0 items-center gap-1 rounded-md border border-border bg-canvas px-2 focus-within:border-ring',className)}>
  <span aria-hidden className="flex w-4 shrink-0 items-center text-[11px] text-muted [&_svg]:size-3.5">{prefix}</span>
  <Input {...input} aria-label={label} className="h-7 min-w-0 border-0 bg-transparent px-0 text-xs focus-visible:outline-none"/>
  {unit&&<span aria-hidden className="text-[10px] text-muted">{unit}</span>}
 </label>;
}

/** The caller owns grouping and which less frequent properties to disclose. */
export function InspectorSection({title,actions,children}:{title:string;actions?:ReactNode;children:ReactNode}){
 return <section aria-label={title} className="grid min-w-0 gap-2 border-b border-border p-3 last:border-b-0">
  <div className="flex min-w-0 items-center justify-between gap-2"><h3 className="text-xs font-semibold">{title}</h3>{actions&&<div className="flex items-center gap-1">{actions}</div>}</div>
  {children}
 </section>;
}
