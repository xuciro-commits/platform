import {useId,useRef,useState,type ChangeEvent} from "react";
import {useDraftInput} from "../fields/draft";
import {t} from "../i18n";
import type { ComponentProps, SelectHTMLAttributes, TextareaHTMLAttributes } from "react";
import { cn } from "../lib/cn";

const field =
  "h-7 w-full rounded-md border border-border bg-surface px-2 text-sm outline-none focus-visible:border-ring focus-visible:outline-1 focus-visible:outline-ring aria-invalid:border-[var(--tone-danger)]";

export function Input(props:ComponentProps<"input">&{draftKey?:string;optional?:boolean}){
 if(props.type==="number")return <NumericInput {...props}/>;
 const {className,draftKey,optional,...input}=props;
 return <input data-ui="field" className={cn(field,className)} {...input}/>;
}
function NumericInput({className,draftKey,optional,...props}:ComponentProps<"input">&{draftKey?:string;optional?:boolean}){
 const errorID=useId(),captionSet=useRef(false);
 const [label,setLabel]=useState(props.name??props["aria-label"]??props.id??"number");
 const input=useDraftInput(`number:${draftKey??props.name??props["aria-label"]??props.id??label}`,String(props.value??props.defaultValue??"")),[touched,setTouched]=useState(false);
 const empty=useRef(optional??(props.value===undefined||props.value===""));
 const publish=(event:ChangeEvent<HTMLInputElement>)=>{
  const raw=event.target.value,n=Number(raw),step=props.step===undefined?undefined:Number(props.step);
  const valid=raw===""?(optional??empty.current):/^[+-]?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(raw)&&Number.isFinite(n)&&(step!==1||Number.isSafeInteger(n))&&(props.min===undefined||n>=Number(props.min))&&(props.max===undefined||n<=Number(props.max));
  const retained=input.write(raw,valid?"":t("Enter a valid value."));
  if(!valid||!retained)return;
  const target=new Proxy(event.target,{get:(target,key)=>key==="valueAsNumber"?(raw===""?NaN:n):Reflect.get(target,key,target)});
  props.onChange?.({...event,target,currentTarget:target});
 };
 return <span className="grid min-w-0 gap-1"><input {...props} type="text" role="spinbutton" inputMode="decimal" aria-label={props["aria-label"]??(label!=="number"&&!props.name&&!props.id?label:undefined)} aria-describedby={[props["aria-describedby"],touched&&input.problem?errorID:undefined].filter(Boolean).join(" ")||undefined} data-ui="field" data-draft-key={input.path} className={cn(field,className)} value={input.text} aria-invalid={props["aria-invalid"]||touched&&!!input.problem} aria-valuenow={Number.isFinite(Number(input.text))?Number(input.text):undefined}
  ref={node=>{if(node&&!captionSet.current&&!props.name&&!props["aria-label"]&&!props.id){captionSet.current=true;const clone=node.closest("label")?.cloneNode(true) as HTMLElement|undefined;clone?.querySelectorAll("[role=alert]").forEach(node=>node.remove());const caption=clone?.textContent?.trim();if(caption&&caption!==label)setLabel(caption);}if(typeof props.ref==="function")props.ref(node);else if(props.ref)props.ref.current=node;}}
  onChange={publish} onKeyDown={event=>{props.onKeyDown?.(event);if(!event.defaultPrevented&&!props.disabled&&!props.readOnly&&["ArrowUp","ArrowDown"].includes(event.key)){event.preventDefault();const parsed=Number(input.text),next=(Number.isFinite(parsed)?parsed:Number(props.value)||0)+(event.key==="ArrowUp"?1:-1)*(Number(props.step)||1);event.currentTarget.value=String(Math.max(Number(props.min??-Infinity),Math.min(Number(props.max??Infinity),next)));publish(event as unknown as ChangeEvent<HTMLInputElement>);}}} onBlur={event=>{setTouched(true);if(!input.problem)input.clear();props.onBlur?.(event);}}/>{touched&&input.problem&&<span id={errorID} role="alert" className="text-xs text-danger">{input.problem}</span>}</span>;
}

/** Native select: accessible, fast with long option lists, styled like Input. */
export function Select({ className, ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return <select data-ui="field" className={cn(field, "pr-6", className)} {...props} />;
}

/** Several lines of text, styled like Input. */
export function Textarea(props:TextareaHTMLAttributes<HTMLTextAreaElement>&{parse?:"json";draftKey?:string}){
 if(props.parse==="json")return <JSONTextarea {...props}/>;
 const {className,parse,draftKey,...input}=props;
 return <textarea data-ui="field" className={cn(field,"h-auto min-h-16 py-1",className)} {...input}/>;
}
function JSONTextarea({className,parse,draftKey,...props}:TextareaHTMLAttributes<HTMLTextAreaElement>&{parse?:"json";draftKey?:string}){
 const captionSet=useRef(false);
 const [label,setLabel]=useState(props.name??props["aria-label"]??props.id??"json");
 const input=useDraftInput(`json:${draftKey??props.name??props["aria-label"]??props.id??label}`,String(props.value??""));
 return <span className="grid min-w-0 gap-1"><textarea {...props} aria-label={props["aria-label"]??(label!=="json"&&!props.name&&!props.id?label:undefined)} data-ui="field" data-draft-key={input.path} className={cn(field,"h-auto min-h-16 py-1",className)} value={input.text}
  ref={node=>{if(node&&!captionSet.current&&!props.name&&!props["aria-label"]&&!props.id){captionSet.current=true;const clone=node.closest("label")?.cloneNode(true) as HTMLElement|undefined;clone?.querySelectorAll("[role=alert]").forEach(node=>node.remove());const caption=clone?.textContent?.trim();if(caption&&caption!==label)setLabel(caption);}}}
  aria-invalid={props["aria-invalid"]||!!input.problem} onChange={event=>{const raw=event.target.value;try{if(raw.trim())JSON.parse(raw);if(input.write(raw))props.onChange?.(event);}catch{input.write(raw,t("Enter valid JSON. The original draft is retained."));}}}/>{input.problem&&<span role="alert" className="text-xs text-danger">{input.problem}</span>}</span>;
}
