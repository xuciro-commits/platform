import {Checkbox} from "../primitives/controls";

/** Authorized options/counts and selected values belong to the caller. */
export function FacetChoices({title,options,value,onChange,histogram=false}:{title:string;options:{value:string;count:number}[];value:string[];onChange:(values:string[])=>void;histogram?:boolean}){
 const maximum=Math.max(1,...options.map(o=>o.count));
 return <fieldset className="grid min-w-40 gap-1"><legend className="mb-1 text-xs text-muted">{title}</legend>{options.map(option=><div key={option.value} className="flex items-center gap-2 text-xs"><Checkbox checked={value.includes(option.value)} onChange={checked=>onChange(checked?[...value,option.value]:value.filter(v=>v!==option.value))}>{option.value}</Checkbox>{histogram&&<span className="h-2 flex-1 rounded bg-hover"><span className="block h-full rounded bg-primary" style={{width:`${100*option.count/maximum}%`}}/></span>}<span className="ml-auto tabular-nums text-muted">{option.count}</span></div>)}</fieldset>;
}
