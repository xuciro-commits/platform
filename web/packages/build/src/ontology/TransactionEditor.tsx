import type { Api } from "@platform/kernel";
import { useHost, useProvidedCapabilities, useRecordInventory } from "@platform/app";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { Button, Checkbox, Disclosure, Input, InputDraftProvider, Panel, Select, Textarea, t, type EntityInfo } from "@platform/ui";
import { useState } from "react";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";
import { nameOf, type Input_, type Process } from "./object-model";
import { recordPaths } from "./record-paths";
import { authoringObject, useAuthoringModel } from "./authoring-model";

const fieldClass = "grid min-w-0 gap-1 text-xs";
const labels: Record<string, string> = { create: "Create record", update: "Update record", archive: "Archive record", link: "Link records", unlink: "Unlink records", sequence: "Take next number", post: "Post to a balance", journal: "Request journal entry", effect: "Request external write", work: "Create or close work", call: "Read a pure capability", "for-each": "For each record" };
const expressionKind = (type: string) => ({ longtext: "text", number: "decimal", string: "text" } as Record<string, string>)[type] ?? type;
const resultChoice = (step: Api.TxnStep): ExpressionChoice[] => step.kind === "create" ? [{ value: `step.${step.name}`, label: t("Result of {step}", { step: step.name }), type: "reference", ref: step.target?.type }] : step.kind === "sequence" ? [{ value: `step.${step.name}`, label: t("Number from {step}", { step: step.name }), type: "text" }] : step.call ? [{ value: `step.${step.name}`, label: t("Result of {step}", { step: step.name }), type: step.call.resultType.kind, ref: step.call.resultType.object }] : [];
type EditorContext = {
  subject: string; actionName?: string; process: Process; inputs: Input_[]; choices: ExpressionChoice[]; objects: EntityInfo[];
  lookup: (type: string) => EntityInfo | undefined; roles: string[]; definitions: Api.Definition[];
  pure: Api.ProvidedCapability[]; writes: { id: string; name: string; title: string; object: string; on: string; state: string }[];
};

/** One editor for action and creation transactions. Each card edits the original
 * typed step; runtime checks and legacy conversion remain in the Build owner. */
export function TransactionEditor({ value, onChange, process, inputs = [], roles }: { value: Api.BusinessTxn; onChange: (value: Api.BusinessTxn) => void; process: Process; inputs?: Input_[]; roles: string[] }) {
  const host = useHost(), capabilities = useProvidedCapabilities("query", "pure");
  const writes = useRecordInventory<EditorContext["writes"][number]>("build.writeback");
  const authoring = useAuthoringModel();
  const subject = value.subject;
  const entities = new Map(authoring.entities.map(entity => [entity.type, entity]));
  entities.set(subject, authoringObject({ ...process, name: subject.slice("build.".length), title: authoring.entity(subject)?.title ?? subject }, authoring.entity(subject)));
  const lookup = (type: string): EntityInfo | undefined => entities.get(type) ?? authoring.entity(type);
  const choices: ExpressionChoice[] = [{ value: "input.$subject", label: t("Subject record ID"), type: "reference", ref: subject }, { value: "caller.id", label: t("The person taking it"), type: "text" }, ...lookup(subject)!.fields.map(field => ({ value: field.name, label: t("This record: {name}", { name: field.title }), type: expressionKind(field.type), ref: field.ref })), ...inputs.map(input => ({ value: `input.${input.name}`, label: t("Input: {name}", { name: input.title }), type: expressionKind(input.type), ref: input.ref }))];
  const context: EditorContext = { subject, actionName: process.actions.find(action => action.transaction === value)?.name, process, inputs, choices, lookup, roles, objects: [lookup(subject)!, ...entities.values()].filter((entity, index) => entity.app === subject.split(".")[0] && (index === 0 || entity.type !== subject)), definitions: host.definitions, pure: capabilities.data ?? [], writes: writes.data?.records ?? [] };
  return <Panel title={t("Transaction steps")} className="grid min-w-0 gap-3">
    <p className="text-sm text-muted">{t("Records and numbers are saved together. Journal requests, external writes and tasks run after the transaction is accepted.")}</p>
    <Checkbox checked={!!value.guard} onChange={enabled => onChange({ ...value, guard: enabled ? literalExpression("boolean") : undefined, message: enabled ? value.message : undefined })}>{t("Require a transaction guard")}</Checkbox>
    {value.guard && <div className="grid gap-2 rounded border border-border p-3"><ExpressionEditor value={value.guard} choices={choices} lookup={lookup} onChange={guard => onChange({ ...value, guard })} /><label className={fieldClass}>{t("Message when the transaction guard fails")}<Input value={value.message ?? ""} onChange={event => onChange({ ...value, message: event.target.value })} /></label></div>}
    <StepList steps={value.steps} onChange={steps => onChange({ ...value, steps })} context={context} />
    <Disclosure summary={t("Transaction source")}><label className={fieldClass}>{t("Transaction definition")}<Textarea parse="json" rows={18} value={JSON.stringify(value, null, 2)} onChange={event => onChange(JSON.parse(event.target.value))} /></label></Disclosure>
  </Panel>;
}

function initialStep(kind: string, name: string, context: EditorContext, item?: string): Api.TxnStep {
  const step: Api.TxnStep = { name, kind };
  if (["create", "update", "archive", "post", "effect", "work"].includes(kind)) step.target = { type: item ?? context.subject, ...(kind === "create" || kind === "post" ? {} : { id: item ? "$item" : "$subject" }) };
  if (kind === "create" || kind === "update") step.changes = [];
  if (kind === "sequence") step.sequence = { Name: name, Pattern: "{n:6}", Yearly: false };
  if (kind === "post") step.post = { match: [], field: "", amount: literalExpression("decimal") };
  if (kind === "journal") step.journal = { lines: [{ account: literalExpression("text"), debit: literalExpression("integer") }, { account: literalExpression("text"), credit: literalExpression("integer") }] };
  if (kind === "effect") step.effect = { writeback: "", payload: [] };
  if (kind === "work") step.work = { title: "", role: context.roles[0] ?? "builder" };
  if (kind === "call") step.call = { ref: { app: "", kind: "query", name: "" }, version: 1, view: "snapshot", inputs: [], resultType: { kind: "text" } };
  if (kind === "for-each") step.forEach = { set: context.process.sets?.[0]?.name ?? "", steps: [] };
  if (kind === "link" || kind === "unlink") step.link = { binding: { ref: { app: "build", kind: "link-type", name: "" }, sourceVersion: "" }, parent: { type: context.subject, id: "$subject" }, child: { type: "", id: "" } };
  return step;
}

function StepList({ steps, onChange, context, before = [], item }: { steps: Api.TxnStep[]; onChange: (steps: Api.TxnStep[]) => void; context: EditorContext; before?: Api.TxnStep[]; item?: string }) {
  const [kind, setKind] = useState("update");
  const replace = (at: number, step: Api.TxnStep) => onChange(steps.map((entry, index) => index === at ? step : entry));
  const move = (index: number, delta: number) => { const next = [...steps], [step] = next.splice(index, 1); next.splice(index + delta, 0, step!); onChange(next); };
  return <div className="grid min-w-0 gap-3">
    {!steps.length && <p className="text-xs text-muted">{t("Add the steps that make up this business operation.")}</p>}
    {steps.map((step, index) => <InputDraftProvider key={index} scope={`transaction-step:${index}`}><div className="grid min-w-0 gap-3 rounded-md border border-border p-3" role="group" aria-label={t("Transaction step {n}", { n: index + 1 })}>
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><strong className="text-sm">{index + 1}. {t(labels[step.kind] ?? step.kind)}</strong><div className="flex gap-1"><Button size="sm" variant="ghost" aria-label={t("Move step up")} disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp /></Button><Button size="sm" variant="ghost" aria-label={t("Move step down")} disabled={index === steps.length - 1} onClick={() => move(index, 1)}><ArrowDown /></Button><Button size="sm" variant="ghost" aria-label={t("Remove transaction step")} onClick={() => onChange(steps.filter((_, at) => at !== index))}><Trash2 /></Button></div></div>
      <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Step name")}<Input value={step.name} onChange={event => replace(index, { ...step, name: event.target.value })} /></label><label className={fieldClass}>{t("Step kind")}<Select value={step.kind} onChange={event => replace(index, { ...initialStep(event.target.value, step.name, context, item), when: step.when, refusals: step.refusals })}>{Object.entries(labels).filter(([kind]) => !item || kind !== "for-each").map(([kind, label]) => <option key={kind} value={kind}>{t(label)}</option>)}</Select></label></div>
      <StepEditor step={step} onChange={next => replace(index, next)} context={context} previous={[...before, ...steps.slice(0, index)]} item={item} />
    </div></InputDraftProvider>)}
    <div className="flex flex-wrap gap-2"><Select aria-label={t("New transaction step kind")} value={kind} onChange={event => setKind(event.target.value)}>{Object.entries(labels).filter(([kind]) => !item || kind !== "for-each").map(([kind, label]) => <option key={kind} value={kind}>{t(label)}</option>)}</Select><Button size="sm" disabled={steps.length >= 256} onClick={() => onChange([...steps, initialStep(kind, nameOf(kind, [...before, ...steps].map(step => step.name)), context, item)])}><Plus />{t("Add transaction step")}</Button></div>
  </div>;
}

function StepEditor({ step, onChange, context, previous, item }: { step: Api.TxnStep; onChange: (step: Api.TxnStep) => void; context: EditorContext; previous: Api.TxnStep[]; item?: string }) {
  const patch = (change: Partial<Api.TxnStep>) => onChange({ ...step, ...change });
  const target = step.target?.type ? step.target : { type: context.subject, id: "$subject" };
  const row = item ? context.lookup(item) : undefined;
  const choices = [...context.choices, ...previous.flatMap(resultChoice), ...(row?.fields.map(field => ({ value: `row.${field.name}`, label: t("Current item: {name}", { name: field.title }), type: expressionKind(field.type), ref: field.ref })) ?? [])];
  const fields = context.lookup(step.target?.type ?? "")?.fields.filter(field => !field.readOnly || ["create", "update"].includes(step.kind) && step.target?.type === context.subject && field.name === "state") ?? [];
  const journalActions = context.process.actions.filter(action => action.name !== context.actionName && action.transaction?.steps.some(step => [step, ...step.forEach?.steps ?? []].some(step => step.journal && !step.journal.reverses)));
  const setType = (type: string) => patch({ target: { type, ...step.kind === "create" || step.kind === "post" ? {} : type === context.subject ? { id: "$subject" } : item === type ? { id: "$item" } : { id: "" } }, changes: step.changes ? [] : undefined });
  return <>
    {["create", "update", "archive", "post", "effect", "work"].includes(step.kind) && <label className={fieldClass}>{t("Step object")}<Select value={target.type} onChange={event => setType(event.target.value)}>{context.objects.map(object => <option key={object.type} value={object.type}>{object.title}</option>)}</Select></label>}
    {["update", "archive", "effect", "work"].includes(step.kind) && <RecordTarget value={target} onChange={target => patch({ target })} context={context} previous={previous} item={item} />}
    {(step.kind === "create" || step.kind === "update") && <Assignments value={step.changes ?? []} onChange={changes => patch({ changes })} fields={fields} choices={choices} context={context} />}
    {step.sequence && <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Sequence name")}<Input value={step.sequence.Name} onChange={event => patch({ sequence: { ...step.sequence!, Name: event.target.value } })} /></label><label className={fieldClass}>{t("Number pattern")}<Input value={step.sequence.Pattern} onChange={event => patch({ sequence: { ...step.sequence!, Pattern: event.target.value } })} placeholder="DOC-{n:6}" /></label><Checkbox checked={step.sequence.Yearly} onChange={Yearly => patch({ sequence: { ...step.sequence!, Yearly } })}>{t("Restart every year")}</Checkbox><p className="text-xs text-muted">{t("Use {n:6} for a padded counter. A refused transaction consumes no number. Assign this step's result in a later record step.")}</p></div>}
    {step.link && <>
      <label className={fieldClass}>{t("Declared relationship")}<Select value={JSON.stringify(step.link.binding)} onChange={event => { const binding = JSON.parse(event.target.value) as Api.AssetBinding, declaration = context.definitions.find(definition => definition.ref.name === binding.ref.name && definition.version === binding.sourceVersion)?.linkType; if (declaration) patch({ link: { binding, parent: { type: declaration.parent.name, id: declaration.parent.name === context.subject ? "$subject" : "" }, child: { type: declaration.child.name, id: declaration.child.name === item ? "$item" : "" } } }); }}><option value="">{t("Choose a relationship")}</option>{context.definitions.filter(definition => definition.linkType && definition.ref.app === context.subject.split(".")[0]).map(definition => <option key={definition.ref.name} value={JSON.stringify({ ref: definition.ref, sourceVersion: definition.version })}>{definition.linkType!.title} · {definition.version}</option>)}</Select></label>
      <fieldset className="grid gap-2 rounded border border-border p-2"><legend className="text-xs">{t("Parent record")}</legend><RecordTarget value={step.link.parent} onChange={parent => patch({ link: { ...step.link!, parent } })} context={context} previous={previous} item={item} /></fieldset>
      <fieldset className="grid gap-2 rounded border border-border p-2"><legend className="text-xs">{t("Child record")}</legend><RecordTarget value={step.link.child} onChange={child => patch({ link: { ...step.link!, child } })} context={context} previous={previous} item={item} /></fieldset>
    </>}
    {step.post && <>
      <label className={fieldClass}>{t("Balance field")}<Select value={step.post.field} onChange={event => patch({ post: { ...step.post!, field: event.target.value } })}><option value="">{t("Choose a numeric field")}</option>{fields.filter(field => ["integer", "decimal", "money"].includes(field.type)).map(field => <option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>
      <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Balance identity fields")}</legend><Assignments value={step.post.match} onChange={match => patch({ post: { ...step.post!, match } })} fields={fields.filter(field => field.name !== step.post!.field)} choices={choices} context={context} /></fieldset>
      <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Posting amount")}</legend><ExpressionEditor value={step.post.amount} kind={step.post.amount.kind} choices={choices} lookup={context.lookup} onChange={amount => patch({ post: { ...step.post!, amount } })} /></fieldset>
      <div className="flex flex-wrap gap-3"><Checkbox checked={!!step.post.subtract} onChange={subtract => patch({ post: { ...step.post!, subtract } })}>{t("Subtract from the balance")}</Checkbox><Checkbox checked={!!step.post.floor} onChange={floor => patch({ post: { ...step.post!, floor } })}>{t("Refuse a negative balance")}</Checkbox></div>
    </>}
    {step.journal && <>
      <p className="text-xs text-muted">{t("This transaction saves the journal intent atomically with its records. Core independently validates and posts the entry after acceptance; this is not an atomic commit across authorities.")}</p>
      <Checkbox checked={!!step.journal.reverses} disabled={!step.journal.reverses && !journalActions.length} onChange={reverse => patch({ journal: reverse ? { reverses: journalActions[0]!.name, date: step.journal!.date, lines: [] } : { date: step.journal!.date, lines: [{ account: literalExpression("text"), debit: literalExpression("integer") }, { account: literalExpression("text"), credit: literalExpression("integer") }] } })}>{t("Reverse an earlier journal request")}</Checkbox>
      <OptionalExpression label="Posting date" kind="date" value={step.journal.date} onChange={date => patch({ journal: { ...step.journal!, date } })} choices={choices} context={context} />
      {step.journal.reverses ? <>
        <label className={fieldClass}>{t("Reverse journal from action")}<Select value={step.journal.reverses} onChange={event => patch({ journal: { reverses: event.target.value, date: step.journal!.date, lines: [] } })}>{journalActions.map(action => <option key={action.name} value={action.name}>{action.title || action.name}</option>)}</Select></label>
        <p className="text-xs text-muted">{t("Core reverses the original entry for this record and action. Current inputs do not recalculate its amounts.")}</p>
      </> : <>
      {(["text", "currency"] as const).map(key => <OptionalExpression key={key} label={{ text: "Entry text", currency: "Journal currency" }[key]} kind="text" value={step.journal![key]} onChange={expression => patch({ journal: { ...step.journal!, [key]: expression } })} choices={choices} context={context} />)}
      {step.journal.lines.map((line, index) => <fieldset key={index} className="grid min-w-0 gap-2 rounded border border-border p-3"><legend className="text-xs">{t("Journal line {n}", { n: index + 1 })}</legend>
        <label className="text-xs font-medium">{t("Account")}</label><ExpressionEditor value={line.account} kind="text" choices={choices} lookup={context.lookup} onChange={account => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, account } : entry) } })} />
        <Select aria-label={t("Debit or credit")} value={line.credit ? "credit" : "debit"} onChange={event => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, debit: event.target.value === "debit" ? line.credit ?? literalExpression("integer") : undefined, credit: event.target.value === "credit" ? line.debit ?? literalExpression("integer") : undefined } : entry) } })}><option value="debit">{t("Debit")}</option><option value="credit">{t("Credit")}</option></Select>
        <ExpressionEditor value={line.debit ?? line.credit ?? literalExpression("integer")} kind={(line.debit ?? line.credit)?.kind ?? "integer"} choices={choices} lookup={context.lookup} onChange={amount => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, [line.credit ? "credit" : "debit"]: amount } : entry) } })} />
        <Disclosure summary={t("Journal line details")}>{(["text", "object", "partner"] as const).map(key => <OptionalExpression key={key} label={{ text: "Line text", object: "Business reference", partner: "Partner" }[key]} kind="text" value={line[key]} onChange={expression => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, [key]: expression } : entry) } })} choices={choices} context={context} />)}</Disclosure>
        <Button size="sm" variant="ghost" className="w-fit" onClick={() => patch({ journal: { ...step.journal!, lines: step.journal!.lines.filter((_, at) => at !== index) } })}>{t("Remove line")}</Button>
      </fieldset>)}<Button size="sm" className="w-fit" onClick={() => patch({ journal: { ...step.journal!, lines: [...step.journal!.lines, { account: literalExpression("text"), debit: literalExpression("integer") }] } })}>{t("Add a line")}</Button>
      </>}
    </>}
    {step.effect && <>
      <label className={fieldClass}>{t("Transaction writeback")}<Select value={context.writes.find(write => write.id === step.effect!.writeback || write.name === step.effect!.writeback)?.id ?? step.effect.writeback} onChange={event => patch({ effect: { ...step.effect!, writeback: event.target.value } })}><option value="">{t("Choose a transaction writeback")}</option>{context.writes.filter(write => write.state === "published" && write.on === "transaction" && write.object === target.type).map(write => <option key={write.id} value={write.id}>{write.title}</option>)}</Select></label>
      <p className="text-xs text-muted">{t("Only explicit transaction writebacks are offered. Delivery starts after acceptance and may be retried with the same key.")}</p>
      <Assignments value={step.effect.payload ?? []} onChange={payload => patch({ effect: { ...step.effect!, payload } })} choices={choices} context={context} />
    </>}
    {step.work && <>
      <label className={fieldClass}>{t("Task title")}<Input value={step.work.title} onChange={event => patch({ work: { ...step.work!, title: event.target.value } })} /></label>
      <Checkbox checked={!!step.work.close} onChange={close => patch({ work: { ...step.work!, close } })}>{t("Close the task with this stable key")}</Checkbox>
      <label className={fieldClass}>{t("Task reviewer role")}<Select value={step.work.role ?? ""} onChange={event => patch({ work: { ...step.work!, role: event.target.value || undefined, member: event.target.value ? undefined : step.work!.member } })}><option value="">{t("Use a specific member")}</option>{context.roles.map(role => <option key={role}>{role}</option>)}</Select></label>
      {(["body", "member", "due", "key"] as const).map(key => <OptionalExpression key={key} label={{ body: "Task instructions", member: "Task member", due: "Task due date", key: "Stable task key" }[key]} kind={key === "due" ? "datetime" : "text"} value={step.work![key]} choices={choices} context={context} onChange={expression => patch({ work: { ...step.work!, [key]: expression, ...(key === "member" && expression ? {role:undefined} : {}) } })} />)}
      {!step.work.close && <label className={fieldClass}>{t("Task answers (one per line)")}<Textarea rows={3} value={step.work.answers?.join("\n") ?? ""} onChange={event => patch({ work: { ...step.work!, answers: event.target.value ? event.target.value.split("\n") : undefined } })} /></label>}
    </>}
    {step.call && <PureCallEditor value={step.call} onChange={call => patch({ call })} choices={choices} context={context} />}
    {step.forEach && <>
      <label className={fieldClass}>{t("Ordered record set")}<Select value={step.forEach.set} onChange={event => patch({ forEach: { ...step.forEach!, set: event.target.value } })}><option value="">{t("Choose a declared record set")}</option>{context.process.sets?.map(set => <option key={set.name} value={set.name}>{set.name} · {context.lookup(set.object)?.title ?? set.object}</option>)}</Select></label>
      <p className="text-xs text-muted">{t("The original set order is retained. Child steps can use the current item; nested iteration is unavailable.")}</p>
      <StepList steps={step.forEach.steps} onChange={steps => patch({ forEach: { ...step.forEach!, steps } })} context={context} before={previous} item={context.process.sets?.find(set => set.name === step.forEach!.set)?.object ?? context.subject} />
    </>}
    <Checkbox checked={!!step.when} onChange={enabled => patch({ when: enabled ? literalExpression("boolean") : undefined })}>{t("Run this step only when")}</Checkbox>
    {step.when && <><ExpressionEditor value={step.when} choices={choices} lookup={context.lookup} onChange={when => patch({ when })} /><p className="text-xs text-muted">{t("A false step condition skips this step. Use the transaction guard to refuse the entire operation.")}</p></>}
  </>;
}

function RecordTarget({ value, onChange, context, previous, item }: { value: Api.TxnRef; onChange: (value: Api.TxnRef) => void; context: EditorContext; previous: Api.TxnStep[]; item?: string }) {
  const references = recordPaths(context.subject, context.lookup).filter(path => path.field.type === "reference" && path.field.ref === value.type);
  const options = [...value.type === context.subject ? [{ value: "$subject", title: t("Subject record") }] : [], ...item === value.type ? [{ value: "$item", title: t("Current iteration item") }] : [], ...context.inputs.filter(input => input.type === "reference" && input.ref === value.type).map(input => ({ value: `input.${input.name}`, title: t("Input: {name}", { name: input.title }) })), ...previous.filter(step => step.kind === "create" && step.target?.type === value.type).map(step => ({ value: `step.${step.name}`, title: t("Created by {step}", { step: step.name }) })), ...references.map(path => ({ value: `via:${path.path.join(".")}`, title: path.label }))];
  const selected = value.via ? `via:${value.via}` : options.some(option => option.value === value.id) ? value.id : "fixed";
  return <div className="grid min-w-0 gap-2"><label className={fieldClass}>{t("Record to change")}<Select value={selected} onChange={event => onChange({ type: value.type, ...event.target.value.startsWith("via:") ? { via: event.target.value.slice(4) } : { id: event.target.value === "fixed" ? "" : event.target.value } })}>{options.map(option => <option key={option.value} value={option.value}>{option.title}</option>)}<option value="fixed">{t("A specific record ID")}</option></Select></label>{selected === "fixed" && <label className={fieldClass}>{t("Record ID")}<Input value={value.id ?? ""} onChange={event => onChange({ type: value.type, id: event.target.value })} /></label>}</div>;
}

function Assignments({ value, onChange, fields, choices, context }: { value: Api.TxnSet[]; onChange: (value: Api.TxnSet[]) => void; fields?: Api.FieldInfo[]; choices: ExpressionChoice[]; context: EditorContext }) {
  return <div className="grid min-w-0 gap-2">{value.map((assignment, index) => {
    const field = fields?.find(field => field.name === assignment.field), kind = expressionKind(field?.type ?? assignment.expr?.kind ?? "text");
    return <div key={index} className="grid min-w-0 gap-2 rounded border border-border p-2">
      <label className={fieldClass}>{t(fields ? "Record field" : "Parameter name")}{fields ? <Select value={assignment.field} onChange={event => { const field = fields.find(field => field.name === event.target.value); onChange(value.map((entry, at) => at === index ? { field: event.target.value, expr: literalExpression(expressionKind(field?.type ?? "text")) } : entry)); }}><option value="">{t("Choose a field")}</option>{fields.filter(field => field.name === assignment.field || !value.some(other => other.field === field.name)).map(field => <option key={field.name} value={field.name}>{field.title}{field.required ? " *" : ""}</option>)}</Select> : <Input value={assignment.field} onChange={event => onChange(value.map((entry, at) => at === index ? { ...entry, field: event.target.value } : entry))} />}</label>
      {field?.choices?.length ? <label className={fieldClass}>{t("State or input value")}<Select value={assignment.expr?.ref ?? String(assignment.expr?.lit?.value ?? "")} onChange={event => onChange(value.map((entry, at) => at === index ? { field: entry.field, expr: event.target.value.startsWith("input.") ? { op: "", kind, ref: event.target.value } : { op: "", kind, lit: { value: event.target.value } } } : entry))}><option value="">{t("Choose a value")}</option>{field.choices.map(value => <option key={value} value={value}>{value}</option>)}{context.inputs.filter(input => input.type === "choice" && input.required && input.choices?.split(",").every(value => field.choices!.includes(value.trim()))).map(input => <option key={input.name} value={`input.${input.name}`}>{t("Input: {name}", { name: input.title })}</option>)}</Select></label> : <ExpressionEditor value={assignment.expr ?? literalExpression(kind)} kind={kind} choices={choices} lookup={context.lookup} onChange={expr => onChange(value.map((entry, at) => at === index ? { field: entry.field, expr } : entry))} />}
      <Button size="sm" variant="ghost" className="w-fit" onClick={() => onChange(value.filter((_, at) => at !== index))}>{t("Remove assignment")}</Button>
    </div>;
  })}<Button size="sm" className="w-fit" disabled={fields !== undefined && !fields.some(field => !value.some(assignment => assignment.field === field.name))} onClick={() => { const field = fields?.find(field => !value.some(assignment => assignment.field === field.name)); onChange([...value, { field: field?.name ?? "", expr: literalExpression(expressionKind(field?.type ?? "text")) }]); }}>{t("Add field assignment")}</Button></div>;
}

function OptionalExpression({ label, value, kind, onChange, choices, context }: { label: string; value?: Api.Expr; kind: string; onChange: (value?: Api.Expr) => void; choices: ExpressionChoice[]; context: EditorContext }) {
  return <fieldset className="grid min-w-0 gap-2"><Checkbox checked={!!value} onChange={enabled => onChange(enabled ? literalExpression(kind) : undefined)}>{t(label)}</Checkbox>{value && <ExpressionEditor value={value} kind={kind} choices={choices} lookup={context.lookup} onChange={onChange} />}</fieldset>;
}

function PureCallEditor({ value, onChange, choices, context }: { value: Api.TxnCall; onChange: (value: Api.TxnCall) => void; choices: ExpressionChoice[]; context: EditorContext }) {
  const selected = context.pure.find(capability => capability.ref.app === value.ref.app && capability.ref.name === value.ref.name);
  const outputs: { path: string[]; kind: string; title: string }[] = [];
  const leaves = (schema: Api.ValueSchema, path: string[] = []) => { if (schema.type === "object") Object.entries(schema.properties ?? {}).forEach(([name, child]) => leaves(child, [...path, name])); else if (["string", "boolean", "integer", "number"].includes(schema.type)) outputs.push({ path, kind: expressionKind(schema.type), title: path.join(".") || t("Entire result") }); };
  if (selected) leaves(selected.capability.outputs);
  return <>
    <label className={fieldClass}>{t("Pure read capability")}<Select value={selected ? `${selected.ref.app}:${selected.ref.name}` : ""} onChange={event => { const capability = context.pure.find(capability => `${capability.ref.app}:${capability.ref.name}` === event.target.value); if (capability) onChange({ ref: capability.ref, version: Number(/\.query-(\d+)$/.exec(capability.version)?.[1] ?? 0), view: "snapshot", inputs: [], resultType: { kind: "text" } }); }}><option value="">{t("Choose a pure query")}</option>{context.pure.map(capability => <option key={`${capability.ref.app}:${capability.ref.name}`} value={`${capability.ref.app}:${capability.ref.name}`}>{capability.ref.app} · {capability.ref.name}</option>)}</Select></label>
    <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Capability version")}<Input type="number" min={0} value={value.version} onChange={event => onChange({ ...value, version: Number(event.target.value) })} /></label><label className={fieldClass}>{t("Read view")}<Select value={value.view} onChange={event => onChange({ ...value, view: event.target.value })}><option value="snapshot">{t("Transaction start snapshot")}</option><option value="current">{t("Results of preceding steps")}</option></Select></label></div>
    <p className="text-xs text-muted">{t("Only pure reads are available. Snapshot reads the records as they were when the transaction started. Preceding results include earlier changes in this transaction.")}</p>
    <Assignments value={value.inputs ?? []} onChange={inputs => onChange({ ...value, inputs })} fields={selected ? (selected.capability.parameters?.map(field => ({ name: field.name, title: field.description || field.name, type: expressionKind(field.type), required: field.required, choices: field.choices, ref: field.ref })) ?? Object.entries(selected.capability.inputs.properties ?? {}).map(([name, schema]) => ({ name, title: schema.description || name, type: expressionKind(schema.type), required: selected.capability.inputs.required?.includes(name), choices: schema.enum }))) as Api.FieldInfo[] : undefined} choices={choices} context={context} />
    <label className={fieldClass}>{t("Retained result value")}<Select value={JSON.stringify(value.resultPath ?? [])} onChange={event => { const output = outputs.find(output => JSON.stringify(output.path) === event.target.value); if (output) onChange({ ...value, resultPath: output.path, resultType: { kind: output.kind } }); }}><option value="">{t("Choose a scalar result")}</option>{outputs.map(output => <option key={JSON.stringify(output.path)} value={JSON.stringify(output.path)}>{output.title} · {t(output.kind)}</option>)}</Select></label>
  </>;
}
