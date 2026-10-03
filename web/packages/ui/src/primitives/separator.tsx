/** A noninteractive group boundary; names and labels remain caller-owned text. */
export function Separator({label,name}:{label?:string;name:string}){
 return <div role="separator" aria-orientation="horizontal" aria-label={label?.trim()?label:name} className="flex min-w-0 items-center gap-2 py-1"><span aria-hidden className="h-px min-w-0 flex-1 bg-border"/>{label&&<span className="min-w-0 whitespace-pre-wrap break-words text-xs text-muted">{label}</span>}<span aria-hidden className="h-px min-w-0 flex-1 bg-border"/></div>;
}
