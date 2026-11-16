import {useId, useRef, useState, type ChangeEvent, type ComponentProps, type Ref, type SelectHTMLAttributes, type TextareaHTMLAttributes} from "react";
import {TextArea, type TextAreaProps} from "@astryxdesign/core/TextArea";
import {TextInput, type TextInputProps} from "@astryxdesign/core/TextInput";
import {useDraftInput} from "../fields/draft";
import {t} from "../i18n";
import {cn} from "../lib/cn";

type FieldProps={draftKey?:string;optional?:boolean};
type SelectProps=SelectHTMLAttributes<HTMLSelectElement>&{placeholder?:string;width?:number|string};

function text(value: string | number | readonly string[] | undefined) {
  return value === undefined || value === null ? "" : String(value);
}
function labelFor(props: {name?: string; id?: string; "aria-label"?: string; title?: string; placeholder?: string}, fallback: string) {
  return props["aria-label"] || props.title || props.placeholder || props.name || props.id || fallback;
}
function fieldClass(className: string | undefined) {
  return className?.split(/\s+/).filter(Boolean).filter(token => /^(w-|min-w-|max-w-|flex-|shrink|grow|self-|m[trblxy]?-|col-|row-|order-)/.test(token)).join(" ");
}
function controlClass(className: string | undefined) {
  return className?.split(/\s+/).filter(Boolean).filter(token => !/^(w-|min-w-|max-w-|flex-|shrink|grow|self-|m[trblxy]?-|col-|row-|order-)/.test(token)).join(" ");
}

function useFieldCaption(fallback: string, explicit: boolean, id?: string) {
  const [caption, setCaption] = useState<string>();
  const seen = useRef(false);
  const capture = (node: unknown) => {
    const element = node instanceof HTMLElement ? node : null;
    if (!element || seen.current || explicit) return;
    seen.current = true;
    const wrapped = element.closest("label")?.cloneNode(true) as HTMLElement | undefined;
    const external = id ? document.querySelector<HTMLLabelElement>(`label[for="${CSS.escape(id)}"]`) : null;
    const clone = wrapped ?? external?.cloneNode(true) as HTMLElement | undefined;
    clone?.querySelectorAll("input,textarea,select,label,[role=alert]").forEach(child => child.remove());
    const text = clone?.textContent?.replace(/\s+/g, " ").trim();
    if (text && text !== fallback) setCaption(text);
  };
  return [caption ?? fallback, caption, capture] as const;
}

export function Input(props: ComponentProps<"input"> & FieldProps) {
  if (props.type === "number") return <NumericInput {...props}/>;
  if (props.type && !["text", "search", "password", "email", "tel", "url"].includes(props.type)) return <NativeInput {...props}/>;
  const {className, draftKey, optional, onChange, onKeyDown, value, defaultValue, disabled, readOnly, required, name, autoFocus, spellCheck, type, ...rest} = props;
  const [internal, setInternal] = useState(text(defaultValue));
  const controlled = value !== undefined;
  const current = controlled ? text(value) : internal;
  const [label, caption, captureLabel] = useFieldCaption(labelFor(props, "Text"), !!props["aria-label"], props.id);
  return <TextInput
    {...(rest as Omit<TextInputProps, "label" | "value">)}
    type={type as "text" | "search" | "password" | "email" | "tel" | "url" | undefined}
    label={label}
    aria-label={caption ?? props["aria-label"]}
    isLabelHidden
    value={current}
    placeholder={props.placeholder}
    size="sm"
    width={props.width}
    containerClassName={fieldClass(className)}
    controlClassName={controlClass(className)}
    isDisabled={disabled}
    isReadOnly={readOnly}
    isRequired={required}
    isOptional={optional}
    htmlName={name}
    autoComplete={props.autoComplete}
    hasAutoFocus={autoFocus}
    hasSpellCheck={spellCheck === true || spellCheck === "true"}
    status={props["aria-invalid"] ? {type: "error"} : undefined}
    onChange={(next, event) => {
      if (!controlled) setInternal(next);
      onChange?.(event);
    }}
    onKeyDown={onKeyDown}
    ref={node => { captureLabel(node); if (typeof props.ref === "function") props.ref(node); else if (props.ref) props.ref.current = node; }}
  />;
}

function NativeInput({className, draftKey, optional, ...props}: ComponentProps<"input"> & FieldProps) {
  return <input data-ui="field" className={cn("h-[var(--ctrl-h-sm)] w-full rounded-md border border-border bg-[var(--field-surface)] px-[var(--ctrl-px-xs)] text-sm outline-none focus-visible:border-ring focus-visible:outline-1 focus-visible:outline-ring aria-invalid:border-[var(--tone-danger)]", className)} {...props}/>;
}

function NumericInput({className,draftKey,optional,...props}:ComponentProps<"input">&FieldProps){
 const instanceID=useId(),errorID=useId(),captionSet=useRef(false);
 const [label,setLabel]=useState(props.name??props["aria-label"]??props.id??"number");
 const input=useDraftInput(`number:${draftKey??props.name??props.id??instanceID}`,String(props.value??props.defaultValue??"")),[touched,setTouched]=useState(false);
 const empty=useRef(optional??(props.value===undefined||props.value===""));
 const publish=(event:ChangeEvent<HTMLInputElement>)=>{
  const raw=event.target.value,n=Number(raw),step=props.step===undefined?undefined:Number(props.step);
  const valid=raw===""?(optional??empty.current):/^[+-]?(?:\d+(?:\.\d+)?|\.\d+)(?:[eE][+-]?\d+)?$/.test(raw)&&Number.isFinite(n)&&(step!==1||Number.isSafeInteger(n))&&(props.min===undefined||n>=Number(props.min))&&(props.max===undefined||n<=Number(props.max));
  const retained=input.write(raw,valid?"":t("Enter a valid value."));
  if(!valid||!retained)return;
  const target=new Proxy(event.target,{get:(target,key)=>key==="valueAsNumber"?(raw===""?NaN:n):Reflect.get(target,key,target)});
  props.onChange?.({...event,target,currentTarget:target});
 };
 return <span className="grid min-w-0 gap-1"><input {...props} type="text" role="spinbutton" inputMode="decimal" aria-label={props["aria-label"]??(label!=="number"&&!props.name&&!props.id?label:undefined)} aria-describedby={[props["aria-describedby"],touched&&input.problem?errorID:undefined].filter(Boolean).join(" ")||undefined} data-ui="field" data-draft-key={input.path} className={cn("h-[var(--ctrl-h-sm)] w-full rounded-md border border-border bg-[var(--field-surface)] px-[var(--ctrl-px-xs)] text-sm outline-none focus-visible:border-ring focus-visible:outline-1 focus-visible:outline-ring aria-invalid:border-[var(--tone-danger)]",className)} value={input.text} aria-invalid={props["aria-invalid"]||touched&&!!input.problem} aria-valuenow={input.text.trim()!==""&&Number.isFinite(Number(input.text))?Number(input.text):undefined} aria-valuemin={props.min===undefined?undefined:Number(props.min)} aria-valuemax={props.max===undefined?undefined:Number(props.max)}
  ref={node=>{if(node&&!captionSet.current&&!props.name&&!props["aria-label"]&&!props.id){captionSet.current=true;const clone=node.closest("label")?.cloneNode(true) as HTMLElement|undefined;clone?.querySelectorAll("[role=alert]").forEach(node=>node.remove());const caption=clone?.textContent?.trim();if(caption&&caption!==label)setLabel(caption);}if(typeof props.ref==="function")props.ref(node);else if(props.ref)props.ref.current=node;}}
  onChange={publish} onKeyDown={event=>{props.onKeyDown?.(event);if(!event.defaultPrevented&&!props.disabled&&!props.readOnly&&["ArrowUp","ArrowDown"].includes(event.key)){event.preventDefault();const parsed=Number(input.text),next=(Number.isFinite(parsed)?parsed:Number(props.value)||0)+(event.key==="ArrowUp"?1:-1)*(Number(props.step)||1);event.currentTarget.value=String(Math.max(Number(props.min??-Infinity),Math.min(Number(props.max??Infinity),next)));publish(event as unknown as ChangeEvent<HTMLInputElement>);}}} onBlur={event=>{setTouched(true);if(!input.problem)input.clear();props.onBlur?.(event);}}/>{touched&&input.problem&&<span id={errorID} role="alert" className="text-xs text-danger">{input.problem}</span>}</span>;
}

export function Select({className, children, ...props}: SelectProps) {
  return <select data-ui="field" className={cn("h-[var(--ctrl-h-sm)] w-full rounded-md border border-border bg-[var(--field-surface)] px-[var(--ctrl-px-xs)] pr-6 text-sm outline-none focus-visible:border-ring focus-visible:outline-1 focus-visible:outline-ring aria-invalid:border-[var(--tone-danger)]", className)} {...props}>{children}</select>;
}

export function Textarea(props: TextareaHTMLAttributes<HTMLTextAreaElement> & {parse?: "json"; draftKey?: string; ref?: Ref<HTMLTextAreaElement>}) {
  if (props.parse === "json") return <JSONTextarea {...props}/>;
  const {className, parse, draftKey, value, defaultValue, onChange, disabled, readOnly, required, name, ...rest} = props;
  const [internal, setInternal] = useState(text(defaultValue));
  const controlled = value !== undefined;
  const [label, caption, captureLabel] = useFieldCaption(labelFor(props, "Text"), !!props["aria-label"], props.id);
  return <TextArea {...(rest as Omit<TextAreaProps, "label" | "value">)} label={label} aria-label={caption ?? props["aria-label"]} isLabelHidden value={controlled ? text(value) : internal} placeholder={props.placeholder} rows={props.rows} size="sm" containerClassName={fieldClass(className)} controlClassName={controlClass(className)} isDisabled={disabled} isReadOnly={readOnly} isRequired={required} htmlName={name} status={props["aria-invalid"] ? {type: "error"} : undefined} ref={node => { captureLabel(node); if (typeof props.ref === "function") props.ref(node); else if (props.ref) props.ref.current = node; }}
    onChange={(next, event) => { if (!controlled) setInternal(next); onChange?.(event); }}/>;
}

function JSONTextarea({className,parse,draftKey,defaultValue,...props}:TextareaHTMLAttributes<HTMLTextAreaElement>&{parse?:"json";draftKey?:string}){
 const instanceID=useId(),captionSet=useRef(false);
 const [label,setLabel]=useState(props.name??props["aria-label"]??props.id??"json");
 const input=useDraftInput(`json:${draftKey??props.name??props.id??instanceID}`,String(props.value??defaultValue??""));
 return <span className="grid min-w-0 gap-1"><textarea {...props} aria-label={props["aria-label"]??(label!=="json"&&!props.name&&!props.id?label:undefined)} data-ui="field" data-draft-key={input.path} className={cn("h-auto min-h-16 w-full rounded-md border border-border bg-[var(--field-surface)] px-[var(--ctrl-px-xs)] py-1 text-sm outline-none focus-visible:border-ring focus-visible:outline-1 focus-visible:outline-ring aria-invalid:border-[var(--tone-danger)]",className)} value={input.text}
  ref={node=>{if(node&&!captionSet.current&&!props.name&&!props["aria-label"]&&!props.id){captionSet.current=true;const clone=node.closest("label")?.cloneNode(true) as HTMLElement|undefined;clone?.querySelectorAll("textarea,[role=alert]").forEach(node=>node.remove());const caption=clone?.textContent?.trim();if(caption&&caption!==label)setLabel(caption);}}}
  aria-invalid={props["aria-invalid"]||!!input.problem} onChange={event=>{const raw=event.target.value;try{if(raw.trim())JSON.parse(raw);if(input.write(raw))props.onChange?.(event);}catch{input.write(raw,t("Enter valid JSON. The original draft is retained."));}}}/>{input.problem&&<span role="alert" className="text-xs text-danger">{input.problem}</span>}</span>;
}
