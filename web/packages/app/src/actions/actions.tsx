import {useActionTrial} from "./trial";
import {originalActionDefaults,validActionDefaults} from "../widgets/action-defaults";
import {OfflineReadError,type Api} from "@platform/kernel";
// Every declared action has an entry in the generated views (F-33): an action
// that makes a new record of a type is offered on the type's list; every other
// action on the type, and a lifecycle transition that takes input (F-27), on
// each record's page, with a form generated from its declared payload. A
// hand-written view only adds entries; it never needs to repeat these.
import type { ActionDeclaration } from "@platform/kernel";
import { Button, ContextHelp, StructuredEditor, InputDraftProvider, useInputDrafts, Checkbox, DateRangeInput, DateTimeInput, Dialog, Form, Input, inputIssues, RecordLookup, Select, Textarea, Panel, field, humanizeKernelError, notify, recordSchema, t, type EntityInfo, type EntityRecord, type FieldType, validCivilDate } from "@platform/ui";
import { useQuery } from "@tanstack/react-query";
import { useId, useRef, useState } from "react";
import { GeneratedForm, newId, useHost } from "../index";
import {actionEntryInputs, actionInputSignature, ActionRefusal} from "./refusal";
import type {Entry} from "@platform/kernel";
import type {LocalFormDraft} from "@platform/kernel";
import {actionDraftContext,actionDraftKey,actionDrafts,completedLocalDraft} from "./local-draft";
import {LocalDraftControls} from "./LocalDraftControls";

type Field = ActionDeclaration["payload"][number];

export function payloadSchema(fields: Field[]) {
  return recordSchema({ name: "action", primary: "id", fields: Object.fromEntries(fields.map(f => {
    const common = { input:f, label: f.description || f.name, required: f.required };
    const type: FieldType = f.choices?.length ? field.singleSelect({ ...common, options: f.choices.map(value => ({ value, label: value })) })
      : f.type === "integer" || f.type === "number" ? field.number(common)
      : f.type === "boolean" ? field.checkbox(common) : f.type === "date" ? field.date(common)
      : f.type === "datetime" ? field.datetime(common) : f.type === "json" ? { ...field.json(common), readOnly: false } : field.text(common);
    if(f.type==="date"&&f.constraints?.dateTime) type.schema=field.text(common).schema.refine(value=>validCivilDate(value)||/^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d$/.test(value)&&validCivilDate(value.slice(0,10)),t("Enter a valid value."));
    if (f.type === "integer") type.schema = type.schema.refine(value => typeof value === "number" && Number.isSafeInteger(value), t("Enter a whole number."));
    return [f.name, type];
  })) });
}

/** Inputs for an action's declared payload fields. */
export function PayloadFields({ fields, values, onChange, app, preview = false, submitted = false, issues = [] }: { fields: Field[]; values: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void; app?:string; preview?: boolean; submitted?: boolean; issues?:Api.FieldIssue[] }) {
  const prefix = useId(), [touched, setTouched] = useState<Record<string, boolean>>({}),[units,setUnits]=useState<Record<string,string>>({});
  const knownUnit=Object.values(units).find(Boolean),hourly=knownUnit?knownUnit==="hour":fields.some(f=>f.constraints?.dateTime&&String(values[f.name]??"").includes("T"));
  const parsed = payloadSchema(fields).safeParse(values);
  const errors = {...(parsed.success ? {} : Object.fromEntries(parsed.error.issues.map(issue => [String(issue.path[0]), issue.message]))),...Object.fromEntries([...inputIssues(fields,values),...issues].map(issue=>[issue.path[0]!,issue.message]))};
  return <>{fields.map((f,index) => {
    if(fields.some(other=>other.range?.end===f.name&&(!other.constraints?.dateTime||!hourly)))return null;
    const value = values[f.name],end=f.range&&fields.find(other=>other.name===f.range?.end);
    const range=!!end&&(!f.constraints?.dateTime||!hourly),endError=range&&(submitted||touched[end!.name])?errors[end!.name]:undefined;
    const set = (v: unknown) => {onChange({ ...values, [f.name]: v });if(f.choices?.length||f.ref||f.from||f.type==="date"||f.type==="datetime")setTouched(old=>({...old,[f.name]:true}));};
    const label = `${f.description || f.name}${f.required ? " *" : ""}`;
    const id = `${prefix}-payload-${f.name}`;
    const error = submitted || touched[f.name] ? errors[f.name] : undefined;
    if (f.type === "boolean" && !f.choices?.length && !f.ref)
      return <Checkbox key={f.name} className="text-xs text-muted" checked={!!value} onChange={set}>{label}</Checkbox>;
    return (
      <div key={f.name} className="grid gap-1 text-xs text-muted" onBlurCapture={() => setTouched(old => ({ ...old, [f.name]: true }))}><label htmlFor={id}>{label}</label>
        {f.group&&fields[index-1]?.group!==f.group&&<span className="text-xs font-medium">{f.group}</span>}
        {range?<DateRangeInput id={id} start={String(value??"")} end={String(values[end!.name]??"")} startLabel={f.description||f.name} endLabel={end!.description||end!.name} inclusive={f.range?.inclusive} invalid={!!error||!!endError} describedBy={[error?`${id}-error`:undefined,endError?`${id}-end-error`:undefined].filter(Boolean).join(" ")||undefined} onChange={(start,finish)=>{setTouched(old=>({...old,[f.name]:true,[end!.name]:!!finish||old[end!.name]===true}));onChange({...values,[f.name]:start,[end!.name]:finish});}}/>:f.choices?.length ? <Select id={id} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined} value={String(value ?? "")} onChange={(e) => set(e.target.value || undefined)}>
            <option value="">—</option>{f.choices.map((c) => <option key={c} value={c}>{t(c)}</option>)}</Select>
          : f.ref ? preview ? <Input id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
            : f.ref === "enterprise.element" ? <ElementPicker id={id} stereotype={f.stereotype} stereotypes={f.stereotypes} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
            : <RecordPicker id={id} type={f.ref} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
          : f.from ? preview ? <Input id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
            : <ReadPicker id={id} field={f} app={app} value={String(value ?? "")} onChange={(v) => set(v || undefined)} onUnit={unit=>setUnits(old=>({...old,[f.name]:unit}))} />
          : f.type === "datetime" ? <DateTimeInput title={f.description||f.name} value={String(value??"")} offset="Z" onChange={set}/>
          : f.type === "json" ? field.json({label:f.description||f.name,required:f.required}).editor!({id,value,onChange:set,invalid:!!error,describedBy:error?`${id}-error`:undefined})
          : f.type === "string" && String(value ?? "").length > 60 ? <Textarea id={id} rows={4} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
          : <Input id={id} type={f.type === "integer" || f.type === "number" ? "number" : f.type === "date" ? hourly&&f.constraints?.dateTime?"datetime-local":"date" : f.type === "datetime" ? "datetime-local" : "text"} value={value === undefined ? "" : String(value)}
              aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined}
              onChange={(e) => set(f.type === "integer" || f.type === "number" ? (e.target.value === "" ? undefined : Number(e.target.value)) : e.target.value)} />}
        {endError&&<p id={`${id}-end-error`} role="alert" className="text-danger">{end?.description}: {endError}</p>}
        {error && <p id={`${id}-error`} role="alert" className="text-danger">{error}</p>}
      </div>
    );
  })}</>;
}

/** A list of the items of an app's read, for a payload field whose values are not records here (#129). */
function ReadPicker({ id, field, app, value, onChange, onUnit }: { id: string; field: Field; app?:string; value: string; onChange: (key: string) => void; onUnit?:(unit:string)=>void }) {
 const {client,source}=useHost();
 const path=app?`/v1/apps/${encodeURIComponent(app)}/reads/${encodeURIComponent(field.from!)}`:`/v1/${field.from}`;
 const query=useQuery({queryKey:[client.connection.token,client.connection.tenant,source.scope,"read",app,field.from],queryFn:()=>client.get<Record<string,unknown>[]|Api.InputOptions>(path)});
 if(query.isError)return <p role="alert">{query.error instanceof OfflineReadError?t(query.error.message):t("The options could not be loaded. Try again.")} <Button size="sm" onClick={()=>void query.refetch()}>{t("Retry")}</Button></p>;
 if(query.isPending)return <p role="status">{t("Loading…")}</p>;
 if(!Array.isArray(query.data)&&query.data.manual&&field.manual)return <label className="grid gap-1">{t("Manual source: no room-type directory is connected.")}<Input id={id} value={value} onChange={e=>onChange(e.target.value)}/></label>;
 const items=(Array.isArray(query.data)?query.data:query.data.items) as unknown as Record<string,unknown>[];
 const key=(item:Record<string,unknown>)=>String(item[field.key??"id"]??"");
 return <><Select id={id} value={value} onChange={e=>{onChange(e.target.value);onUnit?.(String(items.find(item=>key(item)===e.target.value)?.unit??""));}}><option value="">—</option>{items.map(item=><option key={key(item)} value={key(item)}>{field.label?`${String(item[field.label]??"")} · ${key(item)}`:key(item)}</option>)}{value&&!items.some(item=>key(item)===value)&&<option value={value}>{t("{value} · unavailable",{value})}</option>}</Select>{!items.length&&<p role="status">{t("No options are available.")}</p>}</>;
}

/** The live elements of the enterprise model, narrowed to a UAF stereotype, for a payload
 * field tagged ref:"enterprise.element" — read through the typed query and
 * resolve contracts, never the whole graph. The model is every member's to read. */
function ElementPicker({id,stereotype,stereotypes,value,onChange}:{id:string;stereotype?:string;stereotypes?:string[];value:string;onChange:(id:string)=>void}) {
 const {source}=useHost();
 return <RecordLookup id={id} source={source} type="enterprise.element" value={value} stereotypes={[...(stereotype?[stereotype]:[]),...(stereotypes??[])]} onChange={id=>onChange(id??"")} />;
}

/** A list of the records of a type the member may read, for a payload field that names one. */
function RecordPicker({ id, type, value, onChange }: { id: string; type: string; value: string; onChange: (id: string) => void }) {
  const { source } = useHost();
  return <RecordLookup id={id} source={source} type={type} value={value} onChange={(selected) => onChange(selected ?? "")} />;
}

/** Source mode inserts the same authorized values as the visual form, never sample record IDs. */
export function SourceFieldInput({field,value,onChange,app,input,preview=false}:{field:Api.FieldInfo;value:unknown;onChange:(value:unknown)=>void;app?:string;input?:Field;preview?:boolean}) {
 const {source}=useHost(),id=useId();
 if(preview||field.readOnly)return null;
 if(field.choices?.length)return <Select id={id} aria-label={field.title||field.name} value={typeof value==="string"?value:""} onChange={event=>onChange(event.target.value||undefined)}><option value="">—</option>{field.choices.map((choice,index)=><option key={choice} value={choice}>{t(field.choiceTitles?.[index]||choice)} · {choice}</option>)}</Select>;
 if(input?.from)return <ReadPicker id={id} field={input} app={app} value={typeof value==="string"?value:""} onChange={next=>onChange(next||undefined)}/>;
 if(!field.ref)return null;
 const stereotypes=[...(field.stereotype?[field.stereotype]:[]),...(field.stereotypes??[])];
 if(field.type==="references"){
  const selected=Array.isArray(value)?value.filter((item):item is string=>typeof item==="string"):[];
  return <div className="grid gap-2">{selected.map((item,index)=><div key={index} className="flex items-center gap-2"><span className="min-w-0 flex-1"><RecordLookup source={source} type={field.ref!} stereotypes={stereotypes} ariaLabel={field.title||field.name} value={item} onChange={next=>onChange(next?selected.map((old,at)=>at===index?next:old):selected.filter((_,at)=>at!==index))}/></span><Button size="sm" onClick={()=>onChange(selected.filter((_,at)=>at!==index))}>{t("Remove")}</Button></div>)}<RecordLookup source={source} type={field.ref} stereotypes={stereotypes} ariaLabel={field.title||field.name} onChange={next=>{if(next&&!selected.includes(next))onChange([...selected,next]);}}/></div>;
 }
 return <RecordLookup id={id} source={source} type={field.ref} stereotypes={stereotypes} ariaLabel={field.title||field.name} value={typeof value==="string"?value:undefined} onChange={onChange}/>;
}
export const sourceFieldInput=(field:Api.FieldInfo,value:unknown,onChange:(value:unknown)=>void)=><SourceFieldInput field={field} value={value} onChange={onChange}/>;

/** A short ID prefix from a type's name, never its translated title: "build.customer" → CUS, "build.request" → REQ. */
export const prefixOf = (type: string) => (type.split(".").pop() ?? type).slice(0, 3).toUpperCase();

/** One submission state for generated record forms, declared payloads and inline hosts. */
export function DeclaredActionForm(props:Parameters<typeof DeclaredActionFormBody>[0]) {
 return <InputDraftProvider isolated scope={props.declared.schema+":"+props.target.id}><DeclaredActionFormBody {...props}/></InputDraftProvider>;
}
function DeclaredActionFormBody({declared:openedDeclaration,target,revision,onCancel,onCompleted,preview=false,onBusy,onPending,initial={},enabled=true,fixed={},submitLabel,originalOutcome,generated=false}:{declared:ActionDeclaration;target:{type:string;id:string};revision?:number;onCancel:()=>void;onCompleted:(target:{type:string;id:string})=>void;preview?:boolean;onBusy?:(busy:boolean)=>void;onPending?:(pending:boolean)=>void;initial?:Record<string,unknown>;enabled?:boolean;fixed?:Record<string,unknown>;submitLabel?:string;originalOutcome?:Entry;generated?:boolean}) {
 const host=useHost(),{decide,catalog,source}=host,lock=useRef(false),inputsDraft=useInputDrafts(),trial=useActionTrial();
 const [local]=useState<{context?:ReturnType<typeof actionDraftContext>;saved?:LocalFormDraft;problem?:string}>(()=>{
  if(preview)return {};
  try{const context=actionDraftContext(host,openedDeclaration),saved=context?host.client.prepared?.form(actionDraftKey(context,openedDeclaration,target,fixed)):undefined;return {context,saved:saved&&!completedLocalDraft(host,saved)?saved:undefined};}
  catch(problem){return {problem:problem instanceof Error?problem.message:String(problem)};}
 });
 const savedDraft=local.saved?.target.type===target.type&&local.saved.target.id===target.id?local.saved:undefined;
 const [startingValues]=useState<Record<string,unknown>>(()=>{
  const defaults={...Object.fromEntries(openedDeclaration.payload.filter(field=>field.default!==undefined).map(field=>[field.name,structuredClone(field.default)])),...initial,...savedDraft?.values,...fixed};
  return originalOutcome&&(originalOutcome.state==="SUBMISSION_STATE_PENDING"||originalOutcome.state==="SUBMISSION_STATE_UNKNOWN")?actionEntryInputs(originalOutcome,defaults):defaults;
 });
 const [values,setValues]=useState<Record<string,unknown>>(startingValues),[submitting,setSubmitting]=useState(false),[refusal,setRefusal]=useState(originalOutcome?humanizeKernelError(originalOutcome.reason??originalOutcome.outcome):""),[submitted,setSubmitted]=useState(false),[pending,setPending]=useState(originalOutcome?.state==="SUBMISSION_STATE_PENDING"||originalOutcome?.state==="SUBMISSION_STATE_UNKNOWN"),[ownerIssues,setOwnerIssues]=useState<Api.FieldIssue[]>(originalOutcome?.issues??[]);
 const [attempt,setAttempt]=useState<{entry:Entry;values:Record<string,unknown>}|undefined>(()=>originalOutcome?{entry:originalOutcome,values:actionEntryInputs(originalOutcome,startingValues)}:undefined);
 const current=catalog.find(action=>action.schema===openedDeclaration.schema&&action.target===target.type);
 const refused=attempt&&["SUBMISSION_STATE_REJECTED","SUBMISSION_STATE_CONFLICT"].includes(attempt.entry.state);
 const attemptedVersion=attempt?.entry.schemaVersion??openedDeclaration.schemaVersion;
 const schemaChanged=!!refused&&!!attemptedVersion&&!!current?.schemaVersion&&actionInputSignature(current.schemaVersion)!==actionInputSignature(attemptedVersion);
 const declared=schemaChanged&&current?current:openedDeclaration;
 // Generated forms expose writable model fields in both editing modes;
 // fixed bindings remain part of the same original owner submission.
 const generatedInputs=(next:Record<string,unknown>)=>{
  const inputs={...next,...fixed},model=source.entity(target.type)?.fields??[];
  const allowed=new Set([...declared.payload.map(field=>field.name),...model.filter(field=>!field.readOnly&&(field.aside||Object.hasOwn(startingValues,field.name))).map(field=>field.name)]);
  return Object.fromEntries(Object.entries(inputs).filter(([name])=>allowed.has(name)&&!model.some(field=>field.name===name&&field.readOnly)));
 };
 const sameTarget=attempt?.entry.submission.target?.type===target.type&&attempt.entry.submission.target.id===target.id;
 const changed=!!attempt&&(!sameTarget||schemaChanged||actionInputSignature(values)!==actionInputSignature(attempt.values));
 const submit=async(inputs=values,original=false)=>{
  if(!enabled||preview||lock.current||inputsDraft?.invalid&&!original)return false;
  setSubmitted(true);
  if(!original&&!generated&&!payloadSchema(declared.payload).safeParse(inputs).success)return false;
  const same=!!attempt&&(original||!schemaChanged&&sameTarget&&actionInputSignature(inputs)===actionInputSignature(attempt.values));
  const requested=same?{type:attempt!.entry.submission.target!.type!,id:attempt!.entry.submission.target!.id!}:{...target};
  if(!requested.id)return false;
  inputs=structuredClone(same?attempt!.values:inputs);
  lock.current=true;setSubmitting(true);onBusy?.(true);setRefusal("");
  try{
   const accepted=await decide(declared.schema,requested,inputs,{expectedRevision:same?attempt!.entry.submission.expectedRevision:!attempt?savedDraft?.expectedRevision??revision:revision,schemaVersion:same?attempt!.entry.schemaVersion:declared.schemaVersion,quiet:true,originalKey:same?attempt?.entry.submission.idempotencyKey:undefined,correctionOf:!same&&sameTarget?attempt?.entry.submission.idempotencyKey:undefined,onRefused:setRefusal,onOutcome:entry=>{
    setOwnerIssues(entry.issues??[]);setAttempt({entry,values:actionEntryInputs(entry,inputs)});
    const waiting=entry.state==="SUBMISSION_STATE_PENDING"||entry.state==="SUBMISSION_STATE_UNKNOWN";setPending(waiting);onPending?.(waiting);
   }});
   if(accepted){
    if(local.context)try{for(const key of new Set([savedDraft?.key,actionDraftKey(local.context,openedDeclaration,target,fixed),actionDraftKey(local.context,declared,target,fixed)]))if(key)host.client.prepared?.removeForm(key);}
    catch{notify.error(t("The request was accepted, but its local draft could not be removed. Review it in Offline applications."));}
    setPending(false);onPending?.(false);onCompleted(requested);
   }return accepted;
  }catch{setPending(true);onPending?.(true);setRefusal(t("The action could not be completed. Try again."));return false;}
  finally{lock.current=false;setSubmitting(false);onBusy?.(false);}
 };
 const change=(next:Record<string,unknown>)=>{setOwnerIssues(old=>old.filter(issue=>[...issue.path,...(issue.relatedPaths??[]).flat()].every(name=>next[name]===values[name])));setValues({...next,...fixed});};
 const refusalView=refusal&&<ActionRefusal entry={attempt?.entry} message={refusal} original={!pending?attempt?.values:undefined}/>;
 const retry=attempt&&!pending&&<Button disabled={submitting||!enabled||preview} onClick={()=>void submit(attempt.values,true)}>{t("Retry original inputs")}</Button>;
 const label=submitting?t("Executing…"):pending?t("Retry confirmation"):schemaChanged?t("Submit with refreshed definition"):sameTarget?t("Submit corrected inputs"):submitLabel??declared.title;
 const localControls=<>{savedDraft&&<p role="status" className="text-xs text-muted">{t("Restored local draft from {time}.",{time:new Date(savedDraft.at).toLocaleString()})}</p>}{local.problem&&<p role="alert" className="text-sm text-danger">{t(local.problem)}</p>}<LocalDraftControls context={local.context} saved={savedDraft} action={declared} target={target} expectedRevision={!attempt?savedDraft?.expectedRevision??revision:revision} values={values} fixed={fixed} disabled={pending||submitting||preview||!enabled||inputsDraft?.invalid}/></>;
 if(generated)return <div className="grid gap-3">{declared.description&&<ContextHelp title={declared.title}>{declared.description}</ContextHelp>}{localControls}{refusalView}<GeneratedForm type={target.type} actionSchema={declared.schema} initial={startingValues} submitLabel={label} submitDisabled={!enabled||preview||!target.id||!!attempt&&!pending&&!changed} locked={pending} onCancel={onCancel} onBusy={onBusy} issues={ownerIssues} onTrial={preview?undefined:next=>trial(declared.schema,target,generatedInputs(next),revision)} context={`${target.type} · ${target.id} · ${declared.schema} · ${revision??0}`} onValuesChange={next=>change(generatedInputs(next))} onSubmit={async inputs=>await(pending&&attempt?submit(attempt.values,true):submit(generatedInputs(inputs as Record<string,unknown>)))||undefined}/>{retry}</div>;
 return <Form className="grid gap-3" noValidate onSubmit={()=>pending&&attempt?submit(attempt.values,true):submit()}>
 {declared.description&&<ContextHelp title={declared.title}>{declared.description}</ContextHelp>}
 {declared.schema===`${target.type}.archive`&&<p className="text-sm text-muted">{t("Archive this saved record? It will leave active lists; its history is retained.")}</p>}
 {refusalView}
 {localControls}
 {preview&&<p className="text-xs text-muted">{t("Actions do not run while you compose.")}</p>}
 <StructuredEditor scope={declared.schema} value={Object.fromEntries(Object.entries(values).filter(([key])=>!Object.hasOwn(fixed,key)))} disabled={submitting||pending||!enabled} fields={declared.payload.filter(field=>!Object.hasOwn(fixed,field.name)).map(field=>({name:field.name,title:field.description,type:({string:field.ref?"reference":"text",number:"decimal","string[]":field.ref?"references":"tags"} as Record<string,string>)[field.type]??field.type,required:field.required,choices:field.choices,constraints:field.constraints,ref:field.ref,stereotype:field.stereotype,stereotypes:field.stereotypes} as Api.FieldInfo))} fieldInput={(field,value,onChange)=><SourceFieldInput field={field} value={value} onChange={onChange} input={declared.payload.find(input=>input.name===field.name)} app={declared.schema.split(".")[0]} preview={preview}/>} onChange={change} onTrial={preview?undefined:next=>trial(declared.schema,target,{...next,...fixed},revision)} context={`${target.type} · ${target.id} · ${declared.schema} · ${revision??0}`}>
 <fieldset disabled={submitting||pending||!enabled} className="grid gap-3"><PayloadFields fields={declared.payload.filter(field=>!Object.hasOwn(fixed,field.name))} app={declared.schema.split(".")[0]} values={values} onChange={change} preview={preview} submitted={submitted} issues={ownerIssues}/></fieldset>
 </StructuredEditor>
 <div className="flex flex-wrap justify-end gap-2"><Button onClick={onCancel} disabled={submitting||!enabled}>{pending?t("Close"):t("Cancel")}</Button>{retry}<Button type="submit" variant="primary" disabled={!enabled||preview||!target.id||submitting||inputsDraft?.invalid||!!attempt&&!pending&&!changed}>{label}</Button></div>
 </Form>;
}

/** One action taken through the existing modal entry point. */
function ActionDialog({ declared, type, record, onClose, onCompleted, initial={},originalOutcome, prefix }: { declared: ActionDeclaration; type: string; record?: Pick<EntityRecord, "id" | "revision">; onClose: () => void; onCompleted?: (target: { type: string; id: string }) => void; initial?:Record<string,unknown>;originalOutcome?:Entry; prefix?:string }) {
 const host=useHost(),{source}=host,info=source.entity(type);
 const [baseline]=useState(record);
 const [local]=useState(()=>{try{return {drafts:actionDrafts(host,declared,type,record?.id)};}catch(problem){return {drafts:[],error:problem instanceof Error?problem.message:String(problem)};}});
 const [id,setId]=useState(()=>originalOutcome?.submission.target?.id??(local.drafts.length===1?local.drafts[0]!.target.id:prefix===""?"":newId(prefix??declared.idPrefix??prefixOf(type)))),[round,setRound]=useState(0),[submitting,setSubmitting]=useState(false);
 const target={type,id:baseline?.id??id},generated=[`${type}.create`,`${type}.edit`].includes(declared.schema)&&!declared.payload.some(f=>f.from||f.constraints?.dateTime);
 const saved=local.drafts.find(draft=>draft.target.id===target.id),entries=[...host.client.authorities.outbox].reverse().filter(entry=>entry.submission.schema?.name===declared.schema&&entry.submission.target?.type===type&&entry.submission.target.id===target.id);
 const recovered=originalOutcome??entries.find(entry=>entry.state==="SUBMISSION_STATE_PENDING"||entry.state==="SUBMISSION_STATE_UNKNOWN")??(saved?entries.find(entry=>(entry.state==="SUBMISSION_STATE_REJECTED"||entry.state==="SUBMISSION_STATE_CONFLICT")&&(entry.submission.expectedRevision??0)===(saved.expectedRevision??0)):undefined);
 const [pending,setPending]=useState(recovered?.state==="SUBMISSION_STATE_PENDING"||recovered?.state==="SUBMISSION_STATE_UNKNOWN");
 return <Dialog open wide={generated&&info?.fields.some(f=>f.type==="lines")} onOpenChange={open=>!open&&!submitting&&onClose()} title={record?`${declared.title} ${record.id}`:declared.title}>
 <div className="grid gap-3">
 {local.error&&<p role="alert" className="text-sm text-danger">{t(local.error)}</p>}
 {!record&&local.drafts.length>1&&<div className="grid gap-2"><p className="text-xs text-muted">{t("Choose a local draft to continue its original record.")}</p>{local.drafts.map(draft=><Button key={draft.key} size="sm" disabled={submitting||pending} onClick={()=>{setId(draft.target.id);setRound(value=>value+1);}}>{t("Resume draft {id}",{id:draft.target.id})}</Button>)}</div>}
 {pending&&<p role="status" className="text-xs text-warning">{t("This operation is awaiting confirmation. Retry its original target and inputs.")}</p>}
 {!record&&<label className="grid gap-1 text-xs text-muted">ID *<Input value={id} onChange={e=>setId(e.target.value.trim())} disabled={submitting||pending}/></label>}
 <DeclaredActionForm key={round} declared={declared} target={target} revision={baseline?.revision??0} onCancel={onClose} onCompleted={confirmed=>{onClose();onCompleted?.(confirmed);}} onBusy={setSubmitting} onPending={setPending} initial={initial} originalOutcome={recovered} generated={generated} submitLabel={generated?(record?t("Save"):t("Create")):undefined}/>
 </div></Dialog>;
}
export {ActionDialog as DeclaredActionDialog};

/** One explicit record action. Ordinary revisions retain the opened form's
 * baseline; identity, declaration and caller scope replace it completely. */
export function InlineActionForm({type,schema,record,live=true,scope,defaults=[],ready=true}:{type:string;schema:string;record?:EntityRecord;live?:boolean;scope?:string;defaults?:Api.PageActionParameter[];ready?:boolean}) {
 const {catalog,source}=useHost(),declared=catalog.find(a=>a.schema===schema&&a.target===type&&!a.new);
 if(!declared||!validActionDefaults(source.entity(type),declared,defaults))return <Panel role="alert">{t("The inline action is unavailable.")}</Panel>;
 if(live&&!record)return <p className="text-sm text-muted">{t("Select a record to act on it.")}</p>;
 return <InlineActionInstance key={JSON.stringify([source.scope,scope,type,schema,record?.id,declared,defaults,live])} type={type} record={record} declared={declared} live={live} defaults={defaults} ready={ready}/>;
}
function InlineActionInstance({type,record,declared,live,defaults,ready}:{type:string;record?:EntityRecord;declared:ActionDeclaration;live:boolean;defaults:Api.PageActionParameter[];ready:boolean}) {
 const {source}=useHost(),[round,setRound]=useState(0),reset=()=>setRound(r=>r+1);
 return <InlineActionRound key={round} record={record} declared={declared} type={type} info={source.entity(type)} live={live} reset={reset} defaults={defaults} ready={ready}/>;
}
function InlineActionRound({type,record,declared,info,live,reset,defaults,ready}:{type:string;record?:EntityRecord;declared:ActionDeclaration;info?:EntityInfo;live:boolean;reset:()=>void;defaults:Api.PageActionParameter[];ready:boolean}) {
 const [initial]=useState(()=>originalActionDefaults(info,declared,defaults,record)),[baseline]=useState(record),[submitted,setSubmitted]=useState(false);
 const transition=info?.lifecycle?.transitions.find(step=>step.schema===declared.schema);
 if(initial===undefined)return <Panel role="alert">{t("The inline action defaults are unavailable.")}</Panel>;
 if(live&&(!baseline||baseline.archived||transition&&!transition.from.includes(String(baseline[info!.lifecycle!.field]))))return <p role="status">{t("This action is unavailable for the selected record.")}</p>;
 if(submitted)return <div className="grid gap-2"><p role="status">{t("Request submitted. Check the record or My requests for its result.")}</p><Button onClick={reset} disabled={!ready||record?.revision===baseline?.revision}>{t("Prepare another action")}</Button></div>;
 return <DeclaredActionForm declared={declared} target={{type,id:baseline?.id??""}} revision={baseline?.revision??0} preview={!live} initial={initial} enabled={ready} onCancel={reset} onCompleted={()=>setSubmitted(true)}/>;
}

/** The type's actions that make a new record, except those a hand-written view already offers (`covers`). */
export function NewActions({ type, covers = [], allowed, disabled = false, onCreated }: { type: string; covers?: string[]; allowed?: string[]; disabled?: boolean; onCreated?: (target: { type: string; id: string }) => void }) {
  const create = useNewRecord(type, onCreated);
  const offered = create.offered.filter((a) => !covers.includes(a.schema) && (!allowed || allowed.includes(a.schema)));
  return <>
    {offered.map((a) => <Button key={a.schema} variant="primary" disabled={disabled} onClick={() => create.take(a.schema)}><span aria-hidden="true">+</span> {a.title}</Button>)}
    {create.dialog}
  </>;
}

/** Create a record of `type` from a menu or command palette: the caller
 * renders the trigger, this hook offers the declared create actions and the
 * dialog that takes the chosen one. */
export function useNewRecord(type: string, onCreated?: (target: { type: string; id: string }) => void, prefix?: string) {
  const { catalog } = useHost();
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; initial?: Record<string, unknown> }>();
  const offered = catalog.filter((a) => a.target === type && a.new);
  const dialog = taking ? <ActionDialog declared={taking.declared} initial={taking.initial} type={type} prefix={prefix} onClose={() => setTaking(undefined)} onCompleted={onCreated} /> : null;
  return { offered, available: offered.length > 0, take: (schema?: string, initial?: Record<string, unknown>) => {
    const declared = schema ? offered.find(action => action.schema === schema) : offered[0];
    setTaking(declared ? { declared, initial: initial && structuredClone(initial) } : undefined);
  }, dialog };
}

/** The actions on one record the member may take, besides its lifecycle's transitions and the generated edit and archive. */
export function RecordActions({ type, record, allowed, steps: withSteps = false }: {
  type: string; record: EntityRecord; allowed?: string[];
  /** Also the lifecycle's steps from where the record stands — where no status bar shows them (a composed page's actions). */
  steps?: boolean;
}) {
  const { catalog, source } = useHost();
  const [taking, setTaking] = useState<ActionDeclaration>();
  const transition = useTransition(type);
  const lifecycle = source.entity(type)?.lifecycle;
  const transitions = new Set(lifecycle?.transitions.map((x) => x.schema) ?? []);
  const offered = catalog.filter((a) => a.target === type && !a.new && !transitions.has(a.schema) && a.schema !== `${type}.edit` && a.schema !== `${type}.archive` && (!allowed || allowed.includes(a.schema)));
  const state = lifecycle ? String(record[lifecycle.field] ?? "") : "";
  const steps = withSteps ? (lifecycle?.transitions ?? []).filter((x) => (!allowed || allowed.includes(x.schema)) && x.from.includes(state) && catalog.some((a) => a.schema === x.schema)) : [];
  return <>
    {steps.map((x) => <Button key={x.schema} size="sm" onClick={() => transition.take(x.schema, record)}>{x.title}</Button>)}
    {offered.map((a) => <Button key={a.schema} size="sm" onClick={() => setTaking(a)}>{a.title}</Button>)}
    {taking && <ActionDialog declared={taking} type={type} record={record} onClose={() => setTaking(undefined)} />}
    {transition.dialog}
  </>;
}

/** A lifecycle transition taken from a record's page: at once, or through a form when it takes input (F-27). */
export function useTransition(type: string) {
  const { action, decide } = useHost();
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; record: EntityRecord; initial?:Record<string,unknown>;originalOutcome?:Entry }>();
  const take = (schema: string, record: EntityRecord, destination?:{parameter:string;value:string}) => {
    const declared = action(schema);
    if(destination&&!declared?.payload.some(f=>f.name===destination.parameter&&f.type==="string"&&f.choices?.includes(destination.value)))return;
    if (declared?.payload.length) setTaking({ declared, record,initial:destination?{[destination.parameter]:destination.value}:undefined });
    else if(declared)void (async()=>{let originalOutcome:Entry|undefined,reason="";const ok=await decide(schema,{type,id:record.id},{},{expectedRevision:record.revision,onOutcome:entry=>{originalOutcome=entry;},onRefused:message=>{reason=message;}});if(!ok&&originalOutcome)setTaking({declared,record,originalOutcome});else if(!ok&&reason)notify.error(reason);})();
  };
  const dialog = taking && <ActionDialog declared={taking.declared} type={type} record={taking.record} initial={taking.initial} originalOutcome={taking.originalOutcome} onClose={() => setTaking(undefined)} />;
  return { take, dialog };
}

/** An action taken as one effect of a page event chain: at once
 * when it takes no input, through its form otherwise. Resolves true when the
 * host accepted it, false when the person cancelled, so the chain can stop. */
export function useActionEffect() {
  const { action, decide } = useHost();
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; type: string; record?: Pick<EntityRecord, "id" | "revision">; initial: Record<string, unknown>; settle: (ok: boolean) => void;originalOutcome?:Entry }>();
  const take = (schema: string, record?: Pick<EntityRecord, "id" | "revision">, initial: Record<string, unknown> = {}): Promise<boolean> => {
    const declared = action(schema);
    if (!declared || (declared.new ? !!record : !record)) return Promise.resolve(false);
    if (!declared.payload.length && record) {let originalOutcome:Entry|undefined,reason="";return decide(schema,{type:declared.target,id:record.id},{},{expectedRevision:record.revision,quiet:true,onRefused:message=>{reason=message;},onOutcome:entry=>{originalOutcome=entry;}}).then(ok=>{if(!ok&&!originalOutcome&&reason)notify.error(reason);return ok||!originalOutcome?ok:new Promise<boolean>(settle=>setTaking({declared,type:declared.target,record,initial:{},settle,originalOutcome}));});}
    return new Promise((settle) => setTaking({ declared, type: declared.target, record, initial, settle }));
  };
  const close = (ok: boolean) => { taking?.settle(ok); setTaking(undefined); };
  const dialog = taking && <ActionDialog declared={taking.declared} type={taking.type} record={taking.record} initial={taking.initial} originalOutcome={taking.originalOutcome} onClose={() => close(false)} onCompleted={() => close(true)} />;
  return { take, dialog };
}

/** Uses the original declared archive action, with confirmation and revision checking. */
export function useRecordArchive(type: string) {
  const { action } = useHost();
  const declared = action(`${type}.archive`);
  const [taking, setTaking] = useState<{ record: Pick<EntityRecord, "id" | "revision">; completed?: () => void }>();
  return {
    available: !!declared,
    take: (record: Pick<EntityRecord, "id" | "revision">, completed?: () => void) => setTaking({ record, completed }),
    dialog: taking && declared ? <ActionDialog declared={declared} type={type} record={taking.record}
      onClose={() => setTaking(undefined)} onCompleted={taking.completed} /> : null,
  };
}
