import {originalActionDefaults,validActionDefaults} from "./widgets/action-defaults";
import type {Api} from "@platform/kernel";
// Every declared action has an entry in the generated views (F-33): an action
// that makes a new record of a type is offered on the type's list; every other
// action on the type, and a lifecycle transition that takes input (F-27), on
// each record's page, with a form generated from its declared payload. A
// hand-written view only adds entries; it never needs to repeat these.
import type { ActionDeclaration } from "@platform/kernel";
import { Button, Checkbox, Dialog, Input, RecordLookup, Select, Textarea, Panel, t, type EntityInfo, type EntityRecord } from "@platform/ui";
import { useQuery } from "@tanstack/react-query";
import { useId, useRef, useState } from "react";
import { GeneratedForm, newId, useHost } from "./index";

type Field = ActionDeclaration["payload"][number];

/** Inputs for an action's declared payload fields. */
export function PayloadFields({ fields, values, onChange, preview = false }: { fields: Field[]; values: Record<string, unknown>; onChange: (v: Record<string, unknown>) => void; preview?: boolean }) {
  const prefix = useId();
  return <>{fields.map((f) => {
    const value = values[f.name];
    const set = (v: unknown) => onChange({ ...values, [f.name]: v });
    const label = `${f.description || f.name}${f.required ? " *" : ""}`;
    const id = `${prefix}-payload-${f.name}`;
    if (f.type === "boolean" && !f.choices?.length && !f.ref)
      return <Checkbox key={f.name} className="text-xs text-muted" checked={!!value} onChange={set}>{label}</Checkbox>;
    return (
      <div key={f.name} className="grid gap-1 text-xs text-muted"><label htmlFor={id}>{label}</label>
        {f.choices?.length ? <Select id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value || undefined)}>
            <option value="">—</option>{f.choices.map((c) => <option key={c} value={c}>{t(c)}</option>)}</Select>
          : f.ref ? preview ? <Input id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
            : <RecordPicker id={id} type={f.ref} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
          : f.from ? preview ? <Input id={id} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
            : <ReadPicker id={id} field={f} value={String(value ?? "")} onChange={(v) => set(v || undefined)} />
          : f.type === "string" && String(value ?? "").length > 60 ? <Textarea id={id} rows={4} value={String(value ?? "")} onChange={(e) => set(e.target.value)} />
          : <Input id={id} type={f.type === "integer" || f.type === "number" ? "number" : f.type === "date" ? "date" : f.type === "datetime" ? "datetime-local" : "text"} value={value === undefined ? "" : String(value)}
              onChange={(e) => set(f.type === "integer" || f.type === "number" ? (e.target.value === "" ? undefined : Number(e.target.value)) : e.target.value)} />}
      </div>
    );
  })}</>;
}

/** A list of the items of an app's read, for a payload field whose values are not records here (#129). */
function ReadPicker({ id, field, value, onChange }: { id: string; field: Field; value: string; onChange: (key: string) => void }) {
  const { client } = useHost();
  const items = useQuery({ queryKey: ["read", field.from], queryFn: () => client.get<Record<string, unknown>[]>(`/v1/${field.from}`) }).data ?? [];
  const key = (x: Record<string, unknown>) => String(x[field.key ?? "id"] ?? "");
  return (
    <Select id={id} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">—</option>
      {items.map((x) => <option key={key(x)} value={key(x)}>{field.label ? `${String(x[field.label] ?? "")} · ${key(x)}` : key(x)}</option>)}
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
 const [values,setValues]=useState<Record<string,unknown>>(initial),[submitting,setSubmitting]=useState(false),[refusal,setRefusal]=useState("");
 const missing=declared.payload.some(f=>f.required&&(values[f.name]===undefined||values[f.name]===""));
 const submit=async()=>{if(!enabled||preview||lock.current||missing||!target.id)return;lock.current=true;setSubmitting(true);onBusy?.(true);setRefusal("");try{if(await decide(declared.schema,target,values,{expectedRevision:revision,quiet:true,onRefused:setRefusal}))onCompleted();}catch{setRefusal(t("The action could not be completed. Try again."));}finally{lock.current=false;setSubmitting(false);onBusy?.(false);}};
 return <div className="grid gap-3">
 {declared.description&&<p className="text-sm text-muted">{declared.description}</p>}
 {declared.schema===`${target.type}.archive`&&<p className="text-sm text-muted">{t("Archive this saved record? It will leave active lists; its history is retained.")}</p>}
 {refusal&&<p role="alert" className="text-sm text-danger">{refusal}</p>}
 {preview&&<p className="text-xs text-muted">{t("Actions do not run while you compose.")}</p>}
 <fieldset disabled={submitting||!enabled} className="grid gap-3"><PayloadFields fields={declared.payload} values={values} onChange={setValues} preview={preview}/></fieldset>
 <div className="flex flex-wrap justify-end gap-2"><Button onClick={onCancel} disabled={submitting||!enabled}>{t("Cancel")}</Button><Button variant="primary" disabled={!enabled||preview||missing||!target.id||submitting} onClick={submit}>{submitting?t("Executing…"):declared.title}</Button></div>
 </div>;
}

/** One action taken through the existing modal entry point. */
function ActionDialog({ declared, type, record, onClose, onCompleted, initial={} }: { declared: ActionDeclaration; type: string; record?: Pick<EntityRecord, "id" | "revision">; onClose: () => void; onCompleted?: (target: { type: string; id: string }) => void; initial?:Record<string,unknown> }) {
 const {decide,source}=useHost(),info=source.entity(type);
 const [id,setId]=useState(()=>newId(prefixOf(type))),[submitting,setSubmitting]=useState(false),[refusal,setRefusal]=useState("");
 const target={type,id:record?.id??id},options={expectedRevision:record?.revision??0,quiet:true,onRefused:setRefusal};
 const done=(ok:boolean)=>{if(ok){onClose();onCompleted?.(target);}},generated=declared.schema===`${type}.create`;
 return <Dialog open wide={generated&&info?.fields.some(f=>f.type==="lines")} onOpenChange={open=>!open&&!submitting&&onClose()} title={record?`${declared.title} ${record.id}`:declared.title}>
 <div className="grid gap-3">
 {!record&&<label className="grid gap-1 text-xs text-muted">ID *<Input value={id} onChange={e=>setId(e.target.value.trim())} disabled={submitting}/></label>}
 {generated?<>{declared.description&&<p className="text-sm text-muted">{declared.description}</p>}{refusal&&<p role="alert" className="text-sm text-danger">{refusal}</p>}<GeneratedForm type={type} submitLabel={t("Create")} onCancel={onClose} onSubmit={async values=>done(!!id&&await decide(declared.schema,target,values,options))}/></>:<DeclaredActionForm declared={declared} target={target} revision={record?.revision??0} onCancel={onClose} onCompleted={()=>done(true)} onBusy={setSubmitting} initial={initial}/>}
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
  const [taking, setTaking] = useState<{ declared: ActionDeclaration; type: string; record?: Pick<EntityRecord, "id" | "revision">; settle: (ok: boolean) => void }>();
  const take = (schema: string, record?: Pick<EntityRecord, "id" | "revision">): Promise<boolean> => {
    const declared = action(schema);
    if (!declared || (declared.new ? !!record : !record)) return Promise.resolve(false);
    if (!declared.payload.length && record) return decide(schema, { type: declared.target, id: record.id }, {}, { expectedRevision: record.revision, quiet: true });
    return new Promise((settle) => setTaking({ declared, type: declared.target, record, settle }));
  };
  const close = (ok: boolean) => { taking?.settle(ok); setTaking(undefined); };
  const dialog = taking && <ActionDialog declared={taking.declared} type={taking.type} record={taking.record} onClose={() => close(false)} onCompleted={() => close(true)} />;
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
