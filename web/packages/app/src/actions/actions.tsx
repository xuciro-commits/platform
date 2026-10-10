import {originalActionDefaults,validActionDefaults} from "../widgets/action-defaults";
import {queryElements,resolveElements,type Api,type EnterpriseStereotype} from "@platform/kernel";
// Every declared action has an entry in the generated views (F-33): an action
// that makes a new record of a type is offered on the type's list; every other
// action on the type, and a lifecycle transition that takes input (F-27), on
// each record's page, with a form generated from its declared payload. A
// hand-written view only adds entries; it never needs to repeat these.
import type { ActionDeclaration } from "@platform/kernel";
import { Button, Checkbox, DateRangeInput, DateTimeInput, Dialog, Form, Input, inputIssues, RecordLookup, Select, Textarea, Panel, field, recordSchema, t, type EntityInfo, type EntityRecord, type FieldType, validCivilDate } from "@platform/ui";
import { useQuery } from "@tanstack/react-query";
import { useId, useRef, useState } from "react";
import { GeneratedForm, newId, useHost } from "../index";

type Field = ActionDeclaration["payload"][number];

function payloadSchema(fields: Field[]) {
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
export function PayloadFields({ fields, values, onChange, preview = false, submitted = false, onInvalid, issues = [] }: { fields: Field[]; values: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void; preview?: boolean; submitted?: boolean; onInvalid?: (name: string, invalid: boolean) => void; issues?:Api.FieldIssue[] }) {
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
        {range?<DateRangeInput id={id} start={String(value??"")} end={String(values[end!.name]??"")} startLabel={f.description||f.name} endLabel={end!.description||end!.name} inclusive={f.range?.inclusive} invalid={!!error||!!endError} describedBy={error?`${id}-error`:undefined} onChange={(start,finish)=>{setTouched(old=>({...old,[f.name]:true,[end!.name]:!!finish||old[end!.name]===true}));onChange({...values,[f.name]:start,[end!.name]:finish});}}/>:f.choices?.length ? <Select id={id} aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined} value={String(value ?? "")} onChange={(e) => set(e.target.value || undefined)}>
            <option value="">—</option>{f.choices.map((c) => <option key={c} value={c}>{t(c)}</option>)}</Select>
          : f.ref ? preview ? <Input id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
            : f.ref === "enterprise.element" ? <ElementPicker id={id} stereotype={f.stereotype} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
            : <RecordPicker id={id} type={f.ref} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
          : f.from ? preview ? <Input id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
            : <ReadPicker id={id} field={f} value={String(value ?? "")} onChange={(v) => set(v || undefined)} onUnit={unit=>setUnits(old=>({...old,[f.name]:unit}))} />
          : f.type === "datetime" ? <DateTimeInput title={f.description||f.name} value={String(value??"")} offset="Z" onChange={set}/>
          : f.type === "json" ? <JsonInput id={id} value={value} onChange={set} onInvalid={invalid => onInvalid?.(f.name, invalid)} />
          : f.type === "string" && String(value ?? "").length > 60 ? <Textarea id={id} rows={4} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
          : <Input id={id} type={f.type === "integer" || f.type === "number" ? "number" : f.type === "date" ? hourly&&f.constraints?.dateTime?"datetime-local":"date" : f.type === "datetime" ? "datetime-local" : "text"} value={value === undefined ? "" : String(value)}
              aria-invalid={!!error} aria-describedby={error ? `${id}-error` : undefined}
              onChange={(e) => set(f.type === "integer" || f.type === "number" ? (e.target.value === "" ? undefined : Number(e.target.value)) : e.target.value)} />}
        {endError&&<p role="alert" className="text-danger">{end?.description}: {endError}</p>}
        {error && <p id={`${id}-error`} role="alert" className="text-danger">{error}</p>}
      </div>
    );
  })}</>;
}

/** A structured payload field (type json, e.g. a dataset load's rows): typed as text, submitted
 * as the parsed value once it parses; until then the text stays and the field reads as invalid (UX-07). */
function JsonInput({ id, value, onChange, onInvalid }: { id: string; value: unknown; onChange: (v: unknown) => void; onInvalid?: (invalid: boolean) => void }) {
  const [text, setText] = useState(() => value === undefined ? "" : typeof value === "string" ? value : JSON.stringify(value, null, 2));
  const [invalid, setInvalid] = useState(false);
  return <Textarea id={id} rows={6} className="font-mono" aria-invalid={invalid} value={text} placeholder='[{"id": 1}]' onChange={(e) => {
    const next = e.target.value; setText(next);
    if (!next.trim()) { setInvalid(false); onInvalid?.(false); onChange(undefined); return; }
    try { onChange(JSON.parse(next)); setInvalid(false); onInvalid?.(false); } catch { setInvalid(true); onInvalid?.(true); onChange(next); }
  }} />;
}

/** A list of the items of an app's read, for a payload field whose values are not records here (#129). */
function ReadPicker({ id, field, value, onChange, onUnit }: { id: string; field: Field; value: string; onChange: (key: string) => void; onUnit?:(unit:string)=>void }) {
 const {client,source}=useHost();
 const query=useQuery({queryKey:[source.scope,"read",field.from],queryFn:()=>client.get<Record<string,unknown>[]|Api.InputOptions>(`/v1/${field.from}`)});
 if(query.isError)return <p role="alert">{t("The options could not be loaded. Try again.")} <Button size="sm" onClick={()=>void query.refetch()}>{t("Retry")}</Button></p>;
 if(query.isPending)return <p role="status">{t("Loading…")}</p>;
 if(!Array.isArray(query.data)&&query.data.manual&&field.manual)return <label className="grid gap-1">{t("Manual source: no room-type directory is connected.")}<Input id={id} value={value} onChange={e=>onChange(e.target.value)}/></label>;
 const items=(Array.isArray(query.data)?query.data:query.data.items) as unknown as Record<string,unknown>[];
 const key=(item:Record<string,unknown>)=>String(item[field.key??"id"]??"");
 return <><Select id={id} value={value} onChange={e=>{onChange(e.target.value);onUnit?.(String(items.find(item=>key(item)===e.target.value)?.unit??""));}}><option value="">—</option>{items.map(item=><option key={key(item)} value={key(item)}>{field.label?`${String(item[field.label]??"")} · ${key(item)}`:key(item)}</option>)}{value&&!items.some(item=>key(item)===value)&&<option value={value}>{t("{value} · unavailable",{value})}</option>}</Select>{!items.length&&<p role="status">{t("No options are available.")}</p>}</>;
}

/** The live elements of the enterprise model, narrowed to a UAF stereotype, for a payload
 * field tagged ref:"enterprise.element" (ADR-0067 D8) — read through the typed query and
 * resolve contracts (ADR-0094), never the whole graph. The model is every member's to read. */
function ElementPicker({ id, stereotype, value, onChange }: { id: string; stereotype?: string; value: string; onChange: (id: string) => void }) {
  const { client } = useHost();
  const items = useQuery({
    queryKey: ["enterprise", "query", stereotype ?? ""],
    queryFn: () => queryElements(client.get, stereotype ? { stereotype: stereotype as EnterpriseStereotype } : {}),
  }).data?.elements ?? [];
  // A record keeps naming what it named: an element that has since been closed
  // (or sits beyond the read page) is resolved by id and stays visible, marked,
  // instead of the value silently reading as empty.
  const kept = useQuery({
    queryKey: ["enterprise", "resolve", value],
    enabled: !!value && !items.some((e) => e.id === value),
    queryFn: () => resolveElements(client.get, [value]),
  }).data?.elements[0];
  return (
    <Select id={id} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">—</option>
      {items.map((e) => <option key={e.id} value={e.id}>{e.name}{e.kind ? ` · ${e.kind}` : ""}</option>)}
      {kept && <option value={kept.id}>{kept.closed ? t("{name} · closed {until}", { name: kept.name, until: kept.until ?? "" }) : `${kept.name}${kept.kind ? ` · ${kept.kind}` : ""}`}</option>}
    </Select>
  );
}

/** A list of the records of a type the member may read, for a payload field that names one (ADR-0028 D5). */
function RecordPicker({ id, type, value, onChange }: { id: string; type: string; value: string; onChange: (id: string) => void }) {
  const { source } = useHost();
  return <RecordLookup id={id} source={source} type={type} value={value} onChange={(selected) => onChange(selected ?? "")} />;
}

/** A short ID prefix from a type's name, never its translated title: "crm.account" → ACC, "hcm.leave" → LEA. */
export const prefixOf = (type: string) => (type.split(".").pop() ?? type).slice(0, 3).toUpperCase();

/** The original action submission form, shared by dialogs and inline hosts. */
function DeclaredActionForm({declared,target,revision,onCancel,onCompleted,preview=false,onBusy,initial={},enabled=true}:{declared:ActionDeclaration;target:{type:string;id:string};revision:number;onCancel:()=>void;onCompleted:()=>void;preview?:boolean;onBusy?:(busy:boolean)=>void;initial?:Record<string,unknown>;enabled?:boolean}) {
 const {decide}=useHost(),lock=useRef(false);
 const [values,setValues]=useState<Record<string,unknown>>(initial),[submitting,setSubmitting]=useState(false),[refusal,setRefusal]=useState(""),[submitted,setSubmitted]=useState(false),[pending,setPending]=useState(false),[invalidJSON,setInvalidJSON]=useState<Record<string,boolean>>({}),[ownerIssues,setOwnerIssues]=useState<Api.FieldIssue[]>([]);
 const submit=async()=>{if(!enabled||preview||lock.current||!target.id)return;setSubmitted(true);if(Object.values(invalidJSON).some(Boolean)||!payloadSchema(declared.payload).safeParse(values).success||inputIssues(declared.payload,values).length>0)return;lock.current=true;setSubmitting(true);onBusy?.(true);setRefusal("");try{if(await decide(declared.schema,target,values,{expectedRevision:revision,quiet:true,onRefused:setRefusal,onOutcome:entry=>{setOwnerIssues(entry.issues??[]);setPending(entry.state==="SUBMISSION_STATE_PENDING"||entry.state==="SUBMISSION_STATE_UNKNOWN");}}))onCompleted();}catch{setRefusal(t("The action could not be completed. Try again."));}finally{lock.current=false;setSubmitting(false);onBusy?.(false);}};
 return <Form className="grid gap-3" noValidate onSubmit={()=>void submit()}>
 {declared.description&&<p className="text-sm text-muted">{declared.description}</p>}
 {declared.schema===`${target.type}.archive`&&<p className="text-sm text-muted">{t("Archive this saved record? It will leave active lists; its history is retained.")}</p>}
 {refusal&&<p role="alert" className="text-sm text-danger">{refusal}</p>}
 {preview&&<p className="text-xs text-muted">{t("Actions do not run while you compose.")}</p>}
 <fieldset disabled={submitting||pending||!enabled} className="grid gap-3"><PayloadFields fields={declared.payload} values={values} onChange={next=>{setOwnerIssues(old=>old.filter(issue=>[...issue.path,...(issue.relatedPaths??[]).flat()].every(name=>next[name]===values[name])));setValues(next);}} preview={preview} submitted={submitted} issues={ownerIssues} onInvalid={(name,invalid)=>setInvalidJSON(old=>({...old,[name]:invalid}))}/></fieldset>
 <div className="flex flex-wrap justify-end gap-2"><Button onClick={onCancel} disabled={submitting||!enabled}>{pending?t("Close"):t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!enabled||preview||!target.id||submitting}>{submitting?t("Executing…"):pending?t("Retry confirmation"):declared.title}</Button></div>
 </Form>;
}

/** One action taken through the existing modal entry point. */
function ActionDialog({ declared, type, record, onClose, onCompleted, initial={} }: { declared: ActionDeclaration; type: string; record?: Pick<EntityRecord, "id" | "revision">; onClose: () => void; onCompleted?: (target: { type: string; id: string }) => void; initial?:Record<string,unknown> }) {
 const {decide,source}=useHost(),info=source.entity(type);
 const [baseline]=useState(record);
 const [id,setId]=useState(()=>newId(prefixOf(type))),[submitting,setSubmitting]=useState(false),[issues,setIssues]=useState<Api.FieldIssue[]>([]);
 const target={type,id:baseline?.id??id},options={expectedRevision:baseline?.revision??0,quiet:true,onOutcome:(entry:{issues?:Api.FieldIssue[]})=>setIssues(entry.issues??[])};
 const done=(ok:boolean)=>{if(ok){onClose();onCompleted?.(target);}return ok;},generated=declared.schema===`${type}.create`&&!declared.payload.some(f=>f.from||f.constraints?.dateTime);
 return <Dialog open wide={generated&&info?.fields.some(f=>f.type==="lines")} onOpenChange={open=>!open&&!submitting&&onClose()} title={record?`${declared.title} ${record.id}`:declared.title}>
 <div className="grid gap-3">
 {!record&&<label className="grid gap-1 text-xs text-muted">ID *<Input value={id} onChange={e=>setId(e.target.value.trim())} disabled={submitting}/></label>}
 {generated?<>{declared.description&&<p className="text-sm text-muted">{declared.description}</p>}<GeneratedForm type={type} submitLabel={t("Create")} onCancel={onClose} onBusy={setSubmitting} issues={issues} onSubmit={async values=>{let reason="";const ok=!!id&&await decide(declared.schema,target,values,{...options,onRefused:message=>{reason=message;}});if(!ok)throw new Error(reason||t("The request was not confirmed. Your input is retained."));return done(ok);}}/></>:<DeclaredActionForm declared={declared} target={target} revision={baseline?.revision??0} onCancel={onClose} onCompleted={()=>done(true)} onBusy={setSubmitting} initial={initial}/>}
 </div></Dialog>;
}

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
  const { catalog } = useHost();
  const [taking, setTaking] = useState<ActionDeclaration>();
  const offered = catalog.filter((a) => a.target === type && a.new && !covers.includes(a.schema) && (!allowed || allowed.includes(a.schema)));
  return <>
    {offered.map((a) => <Button key={a.schema} variant="primary" disabled={disabled} onClick={() => setTaking(a)}>+ {a.title}</Button>)}
    {taking && <ActionDialog declared={taking} type={type} onClose={() => setTaking(undefined)} onCompleted={onCreated} />}
  </>;
}

/** Create a record of `type` from a menu or command palette: the caller
 * renders the trigger, this hook offers the declared create actions and the
 * dialog that takes the chosen one. */
export function useNewRecord(type: string, onCreated?: (target: { type: string; id: string }) => void) {
  const { catalog } = useHost();
  const [taking, setTaking] = useState<ActionDeclaration>();
  const offered = catalog.filter((a) => a.target === type && a.new);
  const dialog = taking ? <ActionDialog declared={taking} type={type} onClose={() => setTaking(undefined)} onCompleted={onCreated} /> : null;
  return { offered, available: offered.length > 0, take: (schema?: string) => setTaking(schema ? offered.find((a) => a.schema === schema) : offered[0]), dialog };
}

/** The actions on one record the member may take, besides its lifecycle's transitions and the generated edit and archive. */
export function RecordActions({ type, record, allowed, steps: withSteps = false }: {
  type: string; record: EntityRecord; allowed?: string[];
  /** Also the lifecycle's steps from where the record stands — where no status bar shows them (a composed page's actions, ADR-0037). */
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
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; record: EntityRecord; initial?:Record<string,unknown> }>();
  const take = (schema: string, record: EntityRecord, destination?:{parameter:string;value:string}) => {
    const declared = action(schema);
    if(destination&&!declared?.payload.some(f=>f.name===destination.parameter&&f.type==="string"&&f.choices?.includes(destination.value)))return;
    if (declared?.payload.length) setTaking({ declared, record,initial:destination?{[destination.parameter]:destination.value}:undefined });
    else void decide(schema, { type, id: record.id }, {}, { expectedRevision: record.revision });
  };
  const dialog = taking && <ActionDialog declared={taking.declared} type={type} record={taking.record} initial={taking.initial} onClose={() => setTaking(undefined)} />;
  return { take, dialog };
}

/** An action taken as one effect of a page event chain (ADR-0053 §11): at once
 * when it takes no input, through its form otherwise. Resolves true when the
 * host accepted it, false when the person cancelled, so the chain can stop. */
export function useActionEffect() {
  const { action, decide } = useHost();
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; type: string; record?: Pick<EntityRecord, "id" | "revision">; initial: Record<string, unknown>; settle: (ok: boolean) => void }>();
  const take = (schema: string, record?: Pick<EntityRecord, "id" | "revision">, initial: Record<string, unknown> = {}): Promise<boolean> => {
    const declared = action(schema);
    if (!declared || (declared.new ? !!record : !record)) return Promise.resolve(false);
    if (!declared.payload.length && record) return decide(schema, { type: declared.target, id: record.id }, {}, { expectedRevision: record.revision, quiet: true });
    return new Promise((settle) => setTaking({ declared, type: declared.target, record, initial, settle }));
  };
  const close = (ok: boolean) => { taking?.settle(ok); setTaking(undefined); };
  const dialog = taking && <ActionDialog declared={taking.declared} type={taking.type} record={taking.record} initial={taking.initial} onClose={() => close(false)} onCompleted={() => close(true)} />;
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
