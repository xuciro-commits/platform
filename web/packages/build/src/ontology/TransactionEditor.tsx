import type {Definition as WorkspaceDefinition} from "@platform/app";
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery, useProvidedCapabilities, useRecordInventory } from "@platform/app";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { ContextHelp, Button, Checkbox, Disclosure, Input, InputDraftProvider, Panel, Select, Textarea, t, useInputDrafts, type EntityInfo } from "@platform/ui";
import { useState } from "react";
import { ExpressionEditor, literalExpression, observationContextChoices, type ExpressionChoice } from "../editor/ExpressionEditor";
import { nameOf, type Input_, type Process } from "./object-model";
import { recordPaths } from "./record-paths";
import { authoringObject, useAuthoringModel } from "./authoring-model";
import { renameTransactionStep, transactionIssues, transactionIssueText } from "./transaction-edit";
import { RuleCollectionsEditor } from "./RuleCollectionsEditor";

const fieldClass = "grid min-w-0 gap-1 text-xs";
const labels: Record<string, string> = { create: "Create record", update: "Update record", archive: "Archive record", link: "Link records", unlink: "Unlink records", sequence: "Take next number", post: "Post to a balance", journal: "Request journal entry", effect: "Request external write", work: "Create or close work", call: "Read a pure capability", intervals: "Reconcile observed intervals", "for-each": "For each item" };
const expressionKind = (type: string) => ({ longtext: "text", number: "decimal", string: "text" } as Record<string, string>)[type] ?? type;
const resultChoice = (step: Api.TxnStep): ExpressionChoice[] => step.kind === "create" || step.kind === "post" ? [{ value: `step.${step.name}`, label: t("Result of {step}", { step: step.name }), type: "reference", ref: step.target?.type }] : step.kind === "sequence" ? [{ value: `step.${step.name}`, label: t("Number from {step}", { step: step.name }), type: "text" }] : step.call ? [{ value: `step.${step.name}`, label: t("Result of {step}", { step: step.name }), type: step.call.resultType.kind, ref: step.call.resultType.object }] : [];
type EditorContext = {
  collections?:Api.RuleReadSpec["collections"];subject: string; actionName?: string; process: Process; inputs: Input_[]; choices: ExpressionChoice[]; objects: EntityInfo[];
  lookup: (type: string) => EntityInfo | undefined; roles: string[]; definitions: WorkspaceDefinition[];
  rename: (path: number[], after: string) => void;
  pure: Api.ProvidedCapability[]; writes: { id: string; name: string; title: string; object: string; on: string; state: string }[];
};

export function TransactionEditor({ value, onChange, process, inputs = [], roles }: { value: Api.BusinessTxn; onChange: (value: Api.BusinessTxn) => void; process: Process; inputs?: Input_[]; roles: string[] }) {
  const host = useHost(), capabilities = useProvidedCapabilities("query", "pure");
  const writes = useRecordInventory<EditorContext["writes"][number]>("build.writeback");
  const authoring = useAuthoringModel();
  const subject = value.subject;
  const entities = new Map(authoring.entities.map(entity => [entity.type, entity]));
  entities.set(subject, authoringObject({ ...process, name: subject.slice("build.".length), title: authoring.entity(subject)?.title ?? subject }, authoring.entity(subject)));
  const lookup = (type: string): EntityInfo | undefined => entities.get(type) ?? authoring.entity(type);
  const choices: ExpressionChoice[] = [...observationContextChoices(), { value: "id", label: t("Record ID"), type: "text" }, { value: "input.$subject", label: t("Subject record ID"), type: "reference", ref: subject }, { value: "caller.id", label: t("The person taking it"), type: "text" }, ...lookup(subject)!.fields.map(field => ({ value: field.name, label: t("This record: {name}", { name: field.title }), type: expressionKind(field.type), ref: field.ref })), ...inputs.map(input => ({ value: `input.${input.name}`, label: t("Input: {name}", { name: input.title }), type: expressionKind(input.type), ref: input.ref }))];
  const context: EditorContext = { collections:value.reads?.collections,subject, actionName: process.actions.find(action => action.transaction === value)?.name, process, inputs, choices, lookup, roles, objects: [lookup(subject)!, ...entities.values()].filter((entity, index) => entity.app === subject.split(".")[0] && (index === 0 || entity.type !== subject)), definitions: host.definitions, rename: (path, after) => onChange(renameTransactionStep(value, path, after)), pure: capabilities.data ?? [], writes: writes.data?.records ?? [] };
  const issues = transactionIssues(value);
  const actionIndex = process.actions.findIndex(action => action.transaction === value);
  return <InputDraftProvider scope={actionIndex >= 0 ? JSON.stringify({ kind: "action", at: actionIndex }) : "creation-transaction"}><Panel data-transaction-editor title={t("Transaction steps")} className="grid min-w-0 gap-3">
    <ContextHelp title={t("Transaction steps")}><p className="text-sm text-muted">{t("Records and numbers are saved together. Journal requests, external writes and tasks run after the transaction is accepted.")}</p></ContextHelp>
    <RuleCollectionsEditor value={value.reads} onChange={reads=>onChange({...value,reads})} subject={lookup(subject)!} inputs={inputs}/>
    <Checkbox checked={!!value.guard} onChange={enabled => onChange({ ...value, guard: enabled ? literalExpression("boolean") : undefined, message: enabled ? value.message : undefined })}>{t("Require a transaction guard")}</Checkbox>
    {value.guard && <div className="grid gap-2 rounded border border-border p-3"><ExpressionEditor draftKey="transaction-guard" value={value.guard} choices={choices} lookup={lookup} collections={value.reads?.collections} onChange={guard => onChange({ ...value, guard })} /><label className={fieldClass}>{t("Message when the transaction guard fails")}<Input value={value.message ?? ""} onChange={event => onChange({ ...value, message: event.target.value })} /></label></div>}
    {issues.length > 0 && <ul role="alert" className="grid gap-1 text-xs text-danger">{issues.map((issue, at) => <li key={at}><Button size="sm" variant="ghost" className="h-auto justify-start whitespace-normal text-left underline" onClick={event => {
      const panel = event.currentTarget.closest("[data-transaction-editor]");
      const step = [...panel?.querySelectorAll<HTMLElement>("[data-transaction-step]") ?? []].find(node => node.dataset.transactionStep === issue.path.join("."));
      step?.scrollIntoView({ block: "nearest" }); step?.focus();
    }}>{transactionIssueText(issue, t)}</Button></li>)}</ul>}
    <StepList steps={value.steps} onChange={steps => onChange({ ...value, steps })} context={context} />
    <Disclosure summary={t("Transaction source")}><label className={fieldClass}>{t("Transaction definition")}<Textarea parse="json" rows={18} value={JSON.stringify(value, null, 2)} onChange={event => onChange(JSON.parse(event.target.value))} /></label></Disclosure>
  </Panel></InputDraftProvider>;
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
  if (kind === "intervals") step.intervals = { source: "", set: context.process.sets?.[0]?.name ?? "", predecessors: { ref: { app: "build", kind: "link-type", name: "" }, sourceVersion: "" }, fields: { subject: "", source: "", sourceVersion: "", sourceRevision: "", processingVersion: "", since: "", until: "", openStart: "", openEnd: "", retired: "", review: "" } };
  if (kind === "for-each") step.forEach = { set: context.process.sets?.[0]?.name ?? "", steps: [] };
  if (kind === "link" || kind === "unlink") step.link = { binding: { ref: { app: "build", kind: "link-type", name: "" }, sourceVersion: "" }, parent: { type: context.subject, id: "$subject" }, child: { type: "", id: "" } };
  return step;
}

function StepList({ steps, onChange, context, before = [], item, rangeKind, path = [] }: { steps: Api.TxnStep[]; onChange: (steps: Api.TxnStep[]) => void; context: EditorContext; before?: Api.TxnStep[]; item?: string; rangeKind?: string; path?: number[] }) {
  const [kind, setKind] = useState("update"), buffers = useInputDrafts();
  const change = (next: Api.TxnStep[]) => {
    const prefix = `${buffers?.scope ?? ""}/transaction-step:`;
    const moves = Object.entries(buffers?.values ?? {}).flatMap(([key, value]) => {
      if (!key.startsWith(prefix)) return [];
      const match = /^(\d+)(\/.*)?$/.exec(key.slice(prefix.length));
      if (!match) return [];
      const at = Number(match[1]), index = next.indexOf(steps[at]!);
      return [{ key, next: index < 0 ? undefined : `${prefix}${index}${match[2] ?? ""}`, value }];
    });
    for (const move of moves) buffers?.set(move.key);
    for (const move of moves) if (move.next) buffers?.set(move.next, move.value);
    onChange(next);
  };
  const replace = (at: number, step: Api.TxnStep) => onChange(steps.map((entry, index) => index === at ? step : entry));
  const move = (index: number, delta: number) => { const next = [...steps], [step] = next.splice(index, 1); next.splice(index + delta, 0, step!); change(next); };
  return <div className="grid min-w-0 gap-3">
    {!steps.length && <p className="text-xs text-muted">{t("Add the steps that make up this business operation.")}</p>}
    {steps.map((step, index) => <InputDraftProvider key={index} scope={`transaction-step:${index}`}><div className="grid min-w-0 gap-3 rounded-md border border-border p-3" role="group" tabIndex={-1} data-transaction-step={[...path, index].join(".")} aria-label={t("Transaction step {n}", { n: index + 1 })}>
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2"><strong className="text-sm">{index + 1}. {t(labels[step.kind] ?? step.kind)}</strong><div className="flex gap-1"><Button size="sm" variant="ghost" aria-label={t("Move step up")} disabled={index === 0} onClick={() => move(index, -1)}><ArrowUp /></Button><Button size="sm" variant="ghost" aria-label={t("Move step down")} disabled={index === steps.length - 1} onClick={() => move(index, 1)}><ArrowDown /></Button><Button size="sm" variant="ghost" aria-label={t("Remove transaction step")} onClick={() => change(steps.filter((_, at) => at !== index))}><Trash2 /></Button></div></div>
      <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Step name")}<Input value={step.name} onChange={event => context.rename([...path, index], event.target.value)} /></label><label className={fieldClass}>{t("Step kind")}<Select value={step.kind} onChange={event => replace(index, { ...initialStep(event.target.value, step.name, context, item), when: step.when, refusals: step.refusals })}>{Object.entries(labels).filter(([kind]) => !item && !rangeKind || !["for-each", "intervals"].includes(kind)).map(([kind, label]) => <option key={kind} value={kind}>{t(label)}</option>)}</Select></label></div>
      <StepEditor step={step} onChange={next => replace(index, next)} context={context} previous={[...before, ...steps.slice(0, index)]} item={item} rangeKind={rangeKind} path={[...path, index]} />
    </div></InputDraftProvider>)}
    <div className="flex flex-wrap gap-2"><Select aria-label={t("New transaction step kind")} value={kind} onChange={event => setKind(event.target.value)}>{Object.entries(labels).filter(([kind]) => !item && !rangeKind || !["for-each", "intervals"].includes(kind)).map(([kind, label]) => <option key={kind} value={kind}>{t(label)}</option>)}</Select><Button size="sm" disabled={steps.length >= 256} onClick={() => onChange([...steps, initialStep(kind, nameOf(kind, [...before, ...steps].map(step => step.name)), context, item)])}><Plus />{t("Add transaction step")}</Button></div>
  </div>;
}

function StepEditor({ step, onChange, context, previous, item, rangeKind, path }: { step: Api.TxnStep; onChange: (step: Api.TxnStep) => void; context: EditorContext; previous: Api.TxnStep[]; item?: string; rangeKind?: string; path: number[] }) {
  const patch = (change: Partial<Api.TxnStep>) => onChange({ ...step, ...change });
  const target = step.target?.type ? step.target : { type: context.subject, id: "$subject" };
  const row = item ? context.lookup(item) : undefined;
  const choices = [...context.choices, ...previous.flatMap(resultChoice), ...(row ? [{value:"row.$subject",label:t("Current collection record"),type:"reference",ref:row.type},...row.fields.map(field => ({ value: `row.${field.name}`, label: t("Current item: {name}", { name: field.title }), type: expressionKind(field.type), ref: field.ref }))] : []), ...(rangeKind ? [{ value: "row.start", label: t("Interval start"), type: rangeKind }, { value: "row.end", label: t("Interval end"), type: rangeKind }, { value: "row.index", label: t("Iteration number"), type: "integer" }] : [])];
  const iterationReferences=recordPaths(context.subject,context.lookup).filter(path=>path.field.type==="reference"&&path.field.ref?.startsWith("build."));
  const iterationObject=step.forEach?.of?iterationReferences.find(path=>path.path.join(".")===step.forEach!.of)?.field.ref:context.subject;
  const iterationSets=(iterationObject===context.subject?context.process.sets:context.lookup(iterationObject??"")?.sets)??[];
  const journalSet = context.process.sets?.find(set => set.name === step.journal?.set);
  const journalChoices = [...choices, ...(journalSet ? context.lookup(journalSet.object)?.fields.map(field => ({ value: `row.${field.name}`, label: t("Current item: {name}", { name: field.title }), type: expressionKind(field.type), ref: field.ref })) ?? [] : [])];
  const fields = context.lookup(step.target?.type ?? "")?.fields.filter(field => !field.readOnly || ["create", "update"].includes(step.kind) && step.target?.type === context.subject && field.name === "state") ?? [];
  const journalActions = context.process.actions.filter(action => action.name !== context.actionName && action.transaction?.steps.some(step => [step, ...step.forEach?.steps ?? []].some(step => step.journal && !step.journal.reverses)));
  const setType = (type: string) => patch({ target: { type, ...step.kind === "create" || step.kind === "post" ? {} : type === context.subject ? { id: "$subject" } : item === type ? { id: "$item" } : { id: "" } }, changes: step.changes ? [] : undefined });
  return <>
    {["create", "update", "archive", "post", "effect", "work"].includes(step.kind) && <label className={fieldClass}>{t("Step object")}<Select value={target.type} onChange={event => setType(event.target.value)}>{context.objects.map(object => <option key={object.type} value={object.type}>{object.title}</option>)}</Select></label>}
    {["update", "archive", "effect", "work"].includes(step.kind) && <RecordTarget value={target} onChange={target => patch({ target })} context={context} previous={previous} item={item} />}
    {(step.kind === "create" || step.kind === "update") && <Assignments value={step.changes ?? []} onChange={changes => patch({ changes })} fields={fields} choices={choices} context={context} />}
    {step.intervals && <IntervalProjectionEditor value={step.intervals} onChange={intervals => patch({ intervals })} context={context} />}
    {step.sequence && <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Sequence name")}<Input value={step.sequence.Name} onChange={event => patch({ sequence: { ...step.sequence!, Name: event.target.value } })} /></label><label className={fieldClass}>{t("Number pattern")}<Input value={step.sequence.Pattern} onChange={event => patch({ sequence: { ...step.sequence!, Pattern: event.target.value } })} placeholder="DOC-{n:6}" /></label><Checkbox checked={step.sequence.Yearly} onChange={Yearly => patch({ sequence: { ...step.sequence!, Yearly } })}>{t("Restart every year")}</Checkbox><ContextHelp title={t("Number pattern")}><p className="text-xs text-muted">{t("Use {n:6} for a padded counter. A refused transaction consumes no number. Assign this step's result in a later record step.")}</p></ContextHelp></div>}
    {step.link && <>
      <label className={fieldClass}>{t("Declared relationship")}<Select value={JSON.stringify(step.link.binding)} onChange={event => { const binding = JSON.parse(event.target.value) as Api.AssetBinding, declaration = context.definitions.find(definition => definition.ref.name === binding.ref.name && definition.version === binding.sourceVersion)?.linkType; if (declaration) patch({ link: { binding, parent: { type: declaration.parent.name, id: declaration.parent.name === context.subject ? "$subject" : "" }, child: { type: declaration.child.name, id: declaration.child.name === item ? "$item" : "" } } }); }}><option value="">{t("Choose a relationship")}</option>{context.definitions.filter(definition => definition.linkType && definition.ref.app === context.subject.split(".")[0]).map(definition => <option key={definition.ref.name} value={JSON.stringify({ ref: definition.ref, sourceVersion: definition.version })}>{definition.linkType!.title} · {definition.version}</option>)}</Select></label>
      <fieldset className="grid gap-2 rounded border border-border p-2"><legend className="text-xs">{t("Parent record")}</legend><RecordTarget value={step.link.parent} onChange={parent => patch({ link: { ...step.link!, parent } })} context={context} previous={previous} item={item} /></fieldset>
      <fieldset className="grid gap-2 rounded border border-border p-2"><legend className="text-xs">{t("Child record")}</legend><RecordTarget value={step.link.child} onChange={child => patch({ link: { ...step.link!, child } })} context={context} previous={previous} item={item} /></fieldset>
    </>}
    {step.post && <>
      <label className={fieldClass}>{t("Balance field")}<Select value={step.post.field} onChange={event => patch({ post: { ...step.post!, field: event.target.value } })}><option value="">{t("Choose a numeric field")}</option>{fields.filter(field => ["integer", "decimal", "money"].includes(field.type)).map(field => <option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>
      <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Balance identity fields")}</legend><Assignments scope="post-match" value={step.post.match} onChange={match => patch({ post: { ...step.post!, match } })} fields={fields.filter(field => field.name !== step.post!.field)} choices={choices} context={context} /></fieldset>
      <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Posting amount")}</legend><ExpressionEditor draftKey="posting-amount" value={step.post.amount} kind={step.post.amount.kind} choices={choices} lookup={context.lookup} collections={context.collections} onChange={amount => patch({ post: { ...step.post!, amount } })} /></fieldset>
      <div className="flex flex-wrap gap-3"><Checkbox checked={!!step.post.subtract} onChange={subtract => patch({ post: { ...step.post!, subtract } })}>{t("Subtract from the balance")}</Checkbox><Checkbox checked={!!step.post.floor} onChange={floor => patch({ post: { ...step.post!, floor } })}>{t("Refuse a negative balance")}</Checkbox></div>
    </>}
    {step.journal && <>
      <ContextHelp title={t("Request journal entry")}><p className="text-xs text-muted">{t("This transaction saves the journal intent atomically with its records. Core independently validates and posts the entry after acceptance; this is not an atomic commit across authorities.")}</p></ContextHelp>
      <Checkbox checked={!!step.journal.reverses} disabled={!step.journal.reverses && !journalActions.length} onChange={reverse => patch({ journal: reverse ? { reverses: journalActions[0]!.name, date: step.journal!.date, lines: [] } : { date: step.journal!.date, lines: [{ account: literalExpression("text"), debit: literalExpression("integer") }, { account: literalExpression("text"), credit: literalExpression("integer") }] } })}>{t("Reverse an earlier journal request")}</Checkbox>
      <OptionalExpression label="Posting date" kind="date" value={step.journal.date} onChange={date => patch({ journal: { ...step.journal!, date } })} choices={choices} context={context} />
      {step.journal.reverses ? <>
        <label className={fieldClass}>{t("Reverse journal from action")}<Select value={step.journal.reverses} onChange={event => patch({ journal: { reverses: event.target.value, date: step.journal!.date, lines: [] } })}>{journalActions.map(action => <option key={action.name} value={action.name}>{action.title || action.name}</option>)}</Select></label>
        <p className="text-xs text-muted">{t("Core reverses the original entry for this record and action. Current inputs do not recalculate its amounts.")}</p>
      </> : <>
      <div className="grid gap-2 sm:grid-cols-2">
        <label className={fieldClass}>{t("Journal line collection")}<Select disabled={!!item || !!rangeKind} value={step.journal.set ?? ""} onChange={event => patch({ journal: { ...step.journal!, set: event.target.value || undefined } })}><option value="">{t("Fixed lines")}</option>{context.process.sets?.map(set => <option key={set.name} value={set.name}>{set.name} · {context.lookup(set.object)?.title ?? set.object}</option>)}</Select></label>
        <label className={fieldClass}>{t("Closed period policy")}<Select value={step.journal.closedPeriod ?? ""} onChange={event => patch({ journal: { ...step.journal!, closedPeriod: (event.target.value || undefined) as Api.TxnJournal["closedPeriod"] } })}><option value="">{t("Default: next open period")}</option><option value="reject">{t("Reject posting")}</option><option value="next-open">{t("Next open period")}</option></Select></label>
      </div>
      {(["text", "currency"] as const).map(key => <OptionalExpression key={key} label={{ text: "Entry text", currency: "Journal currency" }[key]} kind="text" value={step.journal![key]} onChange={expression => patch({ journal: { ...step.journal!, [key]: expression } })} choices={choices} context={context} />)}
      {step.journal.lines.map((line, index) => <fieldset key={index} className="grid min-w-0 gap-2 rounded border border-border p-3"><legend className="text-xs">{t("Journal line {n}", { n: index + 1 })}</legend>
        <OptionalExpression label="Include this line when" draftKey={`journal:${index}:when`} kind="boolean" value={line.when} onChange={when => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, when } : entry) } })} choices={journalChoices} context={context} />
        <label className="text-xs font-medium">{t("Account")}</label><ExpressionEditor draftKey={`journal:${index}:account`} value={line.account} kind="text" choices={journalChoices} lookup={context.lookup} collections={context.collections} onChange={account => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, account } : entry) } })} />
        <Select aria-label={t("Debit or credit")} value={line.credit ? "credit" : "debit"} onChange={event => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, debit: event.target.value === "debit" ? line.credit ?? literalExpression("integer") : undefined, credit: event.target.value === "credit" ? line.debit ?? literalExpression("integer") : undefined } : entry) } })}><option value="debit">{t("Debit")}</option><option value="credit">{t("Credit")}</option></Select>
        <ExpressionEditor draftKey={`journal:${index}:amount`} value={line.debit ?? line.credit ?? literalExpression("integer")} kind={(line.debit ?? line.credit)?.kind ?? "integer"} choices={journalChoices} lookup={context.lookup} collections={context.collections} onChange={amount => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, [line.credit ? "credit" : "debit"]: amount } : entry) } })} />
        <Disclosure summary={t("Journal line details")}>{(["text", "object", "partner"] as const).map(key => <OptionalExpression key={key} label={{ text: "Line text", object: "Business reference", partner: "Partner" }[key]} kind="text" value={line[key]} onChange={expression => patch({ journal: { ...step.journal!, lines: step.journal!.lines.map((entry, at) => at === index ? { ...entry, [key]: expression } : entry) } })} choices={journalChoices} context={context} />)}</Disclosure>
        <Button size="sm" variant="ghost" className="w-fit" onClick={() => patch({ journal: { ...step.journal!, lines: step.journal!.lines.filter((_, at) => at !== index) } })}>{t("Remove line")}</Button>
      </fieldset>)}<Button size="sm" className="w-fit" onClick={() => patch({ journal: { ...step.journal!, lines: [...step.journal!.lines, { account: literalExpression("text"), debit: literalExpression("integer") }] } })}>{t("Add a line")}</Button>
      </>}
    </>}
    {step.effect && <>
      <label className={fieldClass}>{t("Transaction writeback")}<Select value={context.writes.find(write => write.id === step.effect!.writeback || write.name === step.effect!.writeback)?.id ?? step.effect.writeback} onChange={event => patch({ effect: { ...step.effect!, writeback: event.target.value } })}><option value="">{t("Choose a transaction writeback")}</option>{context.writes.filter(write => write.state === "published" && write.on === "transaction" && write.object === target.type).map(write => <option key={write.id} value={write.id}>{write.title}</option>)}</Select></label>
      <p className="text-xs text-muted">{t("Only explicit transaction writebacks are offered. Delivery starts after acceptance and may be retried with the same key.")}</p>
      <Assignments scope="effect-payload" value={step.effect.payload ?? []} onChange={payload => patch({ effect: { ...step.effect!, payload } })} choices={choices} context={context} />
    </>}
    {step.work && <>
      <label className={fieldClass}>{t("Task title")}<Input value={step.work.title} onChange={event => patch({ work: { ...step.work!, title: event.target.value } })} /></label>
      <Checkbox checked={!!step.work.close} onChange={close => patch({ work: { ...step.work!, close } })}>{t("Close the task with this stable key")}</Checkbox>
      <label className={fieldClass}>{t("Task reviewer role")}<Select value={step.work.role ?? ""} onChange={event => patch({ work: { ...step.work!, role: event.target.value || undefined, ...(event.target.value ? {} : {structure:undefined,unit:undefined}), member: event.target.value ? undefined : step.work!.member } })}><option value="">{t("Use a specific member")}</option>{context.roles.map(role => <option key={role}>{role}</option>)}</Select></label>
      <Checkbox checked={!!step.work.structure} onChange={enabled => patch({ work: { ...step.work!, structure: enabled ? "management" : undefined, unit: enabled ? literalExpression("text") : undefined, member: enabled ? undefined : step.work!.member, role: enabled ? step.work!.role || context.roles[0] : step.work!.role } })}>{t("Address work through an organizational role")}</Checkbox>
      {step.work.structure && <><label className={fieldClass}>{t("Organizational structure")}<Input value={step.work.structure} onChange={event => patch({ work: { ...step.work!, structure: event.target.value } })} /></label><label className={fieldClass}>{t("Organizational unit")}<ExpressionEditor draftKey="work-unit" value={step.work.unit ?? literalExpression("text")} kind={step.work.unit?.kind ?? "text"} choices={choices} lookup={context.lookup} onChange={unit => patch({ work: { ...step.work!, unit } })} /></label></>}
      {(["body", "member", "due", "key"] as const).map(key => <OptionalExpression key={key} label={{ body: "Task instructions", member: "Task member", due: "Task due date", key: "Stable task key" }[key]} kind={key === "due" ? "datetime" : "text"} value={step.work![key]} choices={choices} context={context} onChange={expression => patch({ work: { ...step.work!, [key]: expression, ...(key === "member" && expression ? {role:undefined,structure:undefined,unit:undefined} : {}) } })} />)}
      {!step.work.close && <label className={fieldClass}>{t("Task answers (one per line)")}<Textarea rows={3} value={step.work.answers?.join("\n") ?? ""} onChange={event => patch({ work: { ...step.work!, answers: event.target.value ? event.target.value.split("\n") : undefined } })} /></label>}
    </>}
    {step.call && <PureCallEditor value={step.call} onChange={call => patch({ call })} choices={choices} context={context} />}
    {step.forEach && <>
      <label className={fieldClass}>{t("Iteration source")}<Select value={step.forEach.range ? "range" : "set"} onChange={event => patch({ forEach: event.target.value === "range" ? { range: { from: literalExpression("date"), until: literalExpression("date"), unit: "day", step: 1, limit: 366 }, steps: step.forEach!.steps } : { set: context.process.sets?.[0]?.name ?? "", steps: step.forEach!.steps } })}><option value="set">{t("Ordered record set")}</option><option value="range">{t("Bounded interval")}</option></Select></label>
      {step.forEach.range ? <IntervalEditor value={step.forEach.range} onChange={range => patch({ forEach: { ...step.forEach!, range } })} choices={choices} context={context} /> : <>
        <label className={fieldClass}>{t("Collection record")}<Select value={step.forEach.of??""} onChange={event=>patch({forEach:{...step.forEach!,of:event.target.value||undefined,set:""}})}><option value="">{t("Subject record")}</option>{iterationReferences.filter(path=>context.lookup(path.field.ref??"")?.sets?.length).map(path=><option key={path.path.join(".")} value={path.path.join(".")}>{path.label}</option>)}</Select></label>
        <label className={fieldClass}>{t("Ordered record set")}<Select value={step.forEach.set ?? ""} onChange={event => patch({ forEach: { ...step.forEach!, set: event.target.value } })}><option value="">{t("Choose a declared record set")}</option>{iterationSets.map(set => <option key={set.name} value={set.name}>{set.name} · {context.lookup(set.object)?.title ?? set.object}</option>)}</Select></label>
        <p className="text-xs text-muted">{t("The original set order is retained. Child steps can use the current item; nested iteration is unavailable.")}</p>
      </>}
      <StepList path={path} steps={step.forEach.steps} onChange={steps => patch({ forEach: { ...step.forEach!, steps } })} context={context} before={previous} rangeKind={step.forEach.range?.from.kind} item={step.forEach.range ? undefined : iterationSets.find(set => set.name === step.forEach!.set)?.object} />
    </>}
    <Checkbox checked={!!step.when} onChange={enabled => patch({ when: enabled ? literalExpression("boolean") : undefined })}>{t("Run this step only when")}</Checkbox>
    {step.when && <><ExpressionEditor draftKey="step-condition" value={step.when} choices={choices} lookup={context.lookup} collections={context.collections} onChange={when => patch({ when })} /><p className="text-xs text-muted">{t("A false step condition skips this step. Use the transaction guard to refuse the entire operation.")}</p></>}
  </>;
}

function RecordTarget({ value, onChange, context, previous, item }: { value: Api.TxnRef; onChange: (value: Api.TxnRef) => void; context: EditorContext; previous: Api.TxnStep[]; item?: string }) {
  const references = recordPaths(context.subject, context.lookup).filter(path => path.field.type === "reference" && path.field.ref === value.type);
  const options = [...value.type === context.subject ? [{ value: "$subject", title: t("Subject record") }] : [], ...item === value.type ? [{ value: "$item", title: t("Current iteration item") }] : [], ...context.inputs.filter(input => input.type === "reference" && input.ref === value.type).map(input => ({ value: `input.${input.name}`, title: t("Input: {name}", { name: input.title }) })), ...previous.filter(step => step.kind === "create" && step.target?.type === value.type).map(step => ({ value: `step.${step.name}`, title: t("Created by {step}", { step: step.name }) })), ...references.map(path => ({ value: `via:${path.path.join(".")}`, title: path.label }))];
  const selected = value.via ? `via:${value.via}` : options.some(option => option.value === value.id) ? value.id : "fixed";
  return <div className="grid min-w-0 gap-2"><label className={fieldClass}>{t("Record to change")}<Select value={selected} onChange={event => onChange({ type: value.type, ...event.target.value.startsWith("via:") ? { via: event.target.value.slice(4) } : { id: event.target.value === "fixed" ? "" : event.target.value } })}>{options.map(option => <option key={option.value} value={option.value}>{option.title}</option>)}<option value="fixed">{t("A specific record ID")}</option></Select></label>{selected === "fixed" && <label className={fieldClass}>{t("Record ID")}<Input value={value.id ?? ""} onChange={event => onChange({ type: value.type, id: event.target.value })} /></label>}</div>;
}

function Assignments({ value, onChange, fields, choices, context, scope = "assignments" }: { value: Api.TxnSet[]; onChange: (value: Api.TxnSet[]) => void; fields?: Api.FieldInfo[]; choices: ExpressionChoice[]; context: EditorContext; scope?: string }) {
  const buffers = useInputDrafts();
  const remove = (index: number) => {
    const prefix = `${buffers?.scope ?? ""}/${scope}/assignment:`;
    const moves = Object.entries(buffers?.values ?? {}).flatMap(([key, draft]) => {
      if (!key.startsWith(prefix)) return [];
      const at = Number(key.slice(prefix.length));
      if (!Number.isInteger(at) || at < index) return [];
      return [{ key, next: at === index ? undefined : `${prefix}${at - 1}`, draft }];
    });
    for (const move of moves) buffers?.set(move.key);
    for (const move of moves) if (move.next) buffers?.set(move.next, move.draft);
    onChange(value.filter((_, at) => at !== index));
  };
  return <InputDraftProvider scope={scope}><div className="grid min-w-0 gap-2">{value.map((assignment, index) => {
    const field = fields?.find(field => field.name === assignment.field), kind = expressionKind(field?.type ?? assignment.expr?.kind ?? "text");
    return <div key={index} className="grid min-w-0 gap-2 rounded border border-border p-2">
      <label className={fieldClass}>{t(fields ? "Record field" : "Parameter name")}{fields ? <Select value={assignment.field} onChange={event => { const field = fields.find(field => field.name === event.target.value); onChange(value.map((entry, at) => at === index ? { field: event.target.value, expr: literalExpression(expressionKind(field?.type ?? "text")) } : entry)); }}><option value="">{t("Choose a field")}</option>{fields.filter(field => field.name === assignment.field || !value.some(other => other.field === field.name)).map(field => <option key={field.name} value={field.name}>{field.title}{field.required ? " *" : ""}</option>)}</Select> : <Input value={assignment.field} onChange={event => onChange(value.map((entry, at) => at === index ? { ...entry, field: event.target.value } : entry))} />}</label>
      {field?.choices?.length ? <label className={fieldClass}>{t("State or input value")}<Select value={assignment.expr?.ref ?? String(assignment.expr?.lit?.value ?? "")} onChange={event => onChange(value.map((entry, at) => at === index ? { field: entry.field, expr: event.target.value.startsWith("input.") ? { op: "", kind, ref: event.target.value } : { op: "", kind, lit: { value: event.target.value } } } : entry))}><option value="">{t("Choose a value")}</option>{field.choices.map(value => <option key={value} value={value}>{value}</option>)}{context.inputs.filter(input => input.type === "choice" && input.required && input.choices?.split(",").every(value => field.choices!.includes(value.trim()))).map(input => <option key={input.name} value={`input.${input.name}`}>{t("Input: {name}", { name: input.title })}</option>)}</Select></label> : <ExpressionEditor draftKey={`assignment:${index}`} value={assignment.expr ?? literalExpression(kind)} kind={kind} choices={choices} lookup={context.lookup} collections={context.collections} onChange={expr => onChange(value.map((entry, at) => at === index ? { field: entry.field, expr } : entry))} />}
      <Button size="sm" variant="ghost" className="w-fit" onClick={() => remove(index)}>{t("Remove assignment")}</Button>
    </div>;
  })}<Button size="sm" className="w-fit" disabled={fields !== undefined && !fields.some(field => !value.some(assignment => assignment.field === field.name))} onClick={() => { const field = fields?.find(field => !value.some(assignment => assignment.field === field.name)); onChange([...value, { field: field?.name ?? "", expr: literalExpression(expressionKind(field?.type ?? "text")) }]); }}>{t("Add field assignment")}</Button></div></InputDraftProvider>;
}

function IntervalEditor({ value, onChange, choices, context }: { value: Api.TxnRange; onChange: (value: Api.TxnRange) => void; choices: ExpressionChoice[]; context: EditorContext }) {
  return <div className="grid min-w-0 gap-3">
    <div className="grid gap-2 sm:grid-cols-2">
      <label className={fieldClass}>{t("Interval value type")}<Select value={value.from.kind} onChange={event => { const kind = event.target.value; onChange({ ...value, from: { ...value.from, kind }, until: { ...value.until, kind }, unit: kind === "integer" ? "item" : value.unit === "item" ? "day" : value.unit }); }}><option value="date">{t("Date")}</option><option value="datetime">{t("Date and time")}</option><option value="integer">{t("Integer")}</option></Select></label>
      <label className={fieldClass}>{t("Interval unit")}<Select value={value.unit} onChange={event => onChange({ ...value, unit: event.target.value as Api.TxnRange["unit"] })}><option value="day" disabled={value.from.kind === "integer"}>{t("Days")}</option><option value="hour" disabled={value.from.kind !== "datetime"}>{t("Hours")}</option><option value="item" disabled={value.from.kind !== "integer"}>{t("Items")}</option></Select></label>
    </div>
    <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Interval start")}</legend><ExpressionEditor draftKey="range:from" value={value.from} kind={value.from.kind} choices={choices} lookup={context.lookup} collections={context.collections} onChange={from => onChange({ ...value, from })} /></fieldset>
    <fieldset className="grid gap-2"><legend className="text-xs font-medium">{t("Interval end")}</legend><ExpressionEditor draftKey="range:until" value={value.until} kind={value.until.kind} choices={choices} lookup={context.lookup} collections={context.collections} onChange={until => onChange({ ...value, until })} /></fieldset>
    <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Interval step")}<Input type="number" min={1} max={1000} value={value.step} onChange={event => onChange({ ...value, step: Number(event.target.value) })} /></label><label className={fieldClass}>{t("Maximum iterations")}<Input type="number" min={1} max={1000} value={value.limit} onChange={event => onChange({ ...value, limit: Number(event.target.value) })} /></label></div>
    <p className="text-xs text-muted">{t("The end is excluded. Every step must fit the interval. Child steps use its start, end and iteration number; the whole transaction succeeds or fails together.")}</p>
  </div>;
}

function OptionalExpression({ label, draftKey = label, value, kind, onChange, choices, context }: { label: string; draftKey?: string; value?: Api.Expr; kind: string; onChange: (value?: Api.Expr) => void; choices: ExpressionChoice[]; context: EditorContext }) {
  return <fieldset className="grid min-w-0 gap-2"><Checkbox checked={!!value} onChange={enabled => onChange(enabled ? literalExpression(kind) : undefined)}>{t(label)}</Checkbox>{value && <ExpressionEditor draftKey={`optional:${draftKey}`} value={value} kind={kind} choices={choices} lookup={context.lookup} collections={context.collections} onChange={onChange} />}</fieldset>;
}

function PureCallEditor({ value, onChange, choices, context }: { value: Api.TxnCall; onChange: (value: Api.TxnCall) => void; choices: ExpressionChoice[]; context: EditorContext }) {
  const selected = context.pure.find(capability => capability.ref.app === value.ref.app && capability.ref.name === value.ref.name);
  const retained = useReadQuery<Api.CapabilityDescriptor>(`/v1/capabilities/${encodeURIComponent(value.ref.app)}/query/${encodeURIComponent(value.ref.name)}?version=${value.version}`, undefined, !!value.ref.app && !!value.ref.name);
  const described = retained.data;
  const outputs: { path: string[]; kind: string; title: string }[] = [];
  const leaves = (schema: Api.ValueSchema, path: string[] = []) => { if (schema.type === "object") Object.entries(schema.properties ?? {}).forEach(([name, child]) => leaves(child, [...path, name])); else if (["string", "boolean", "integer", "number"].includes(schema.type)) outputs.push({ path, kind: expressionKind(schema.type), title: path.join(".") || t("Entire result") }); };
  if (described?.output) leaves(described.output);
  return <>
    <label className={fieldClass}>{t("Pure read capability")}<Select value={value.ref.app && value.ref.name ? `${value.ref.app}:${value.ref.name}` : ""} onChange={event => { const capability = context.pure.find(capability => `${capability.ref.app}:${capability.ref.name}` === event.target.value); if (capability) onChange({ ref: capability.ref, version: Number(/\.query-(\d+)$/.exec(capability.version)?.[1] ?? 0), view: "snapshot", inputs: [], resultType: { kind: "text" } }); }}><option value="">{t("Choose a pure query")}</option>{!selected && value.ref.name && <option value={`${value.ref.app}:${value.ref.name}`}>{value.ref.app} · {value.ref.name}</option>}{context.pure.map(capability => <option key={`${capability.ref.app}:${capability.ref.name}`} value={`${capability.ref.app}:${capability.ref.name}`}>{capability.ref.app} · {capability.ref.name}</option>)}</Select></label>
    <div className="grid gap-2 sm:grid-cols-2"><label className={fieldClass}>{t("Capability version")}<Input type="number" min={0} value={value.version} onChange={event => onChange({ ...value, version: Number(event.target.value) })} /></label><label className={fieldClass}>{t("Read view")}<Select value={value.view} onChange={event => onChange({ ...value, view: event.target.value })}><option value="snapshot">{t("Transaction start snapshot")}</option><option value="current">{t("Results of preceding steps")}</option></Select></label></div>
    <p className="text-xs text-muted">{t("Only pure reads are available. Snapshot reads the records as they were when the transaction started. Preceding results include earlier changes in this transaction.")}</p>
    {retained.isLoading && <p className="text-xs text-muted">{t("Loading…")}</p>}
    {retained.isError && <p role="alert" className="text-xs text-danger">{t("The retained query version could not be loaded. Its inputs and result stay unchanged.")}</p>}
    <fieldset disabled={!described} className="min-w-0 border-0 p-0"><Assignments scope="call-inputs" value={value.inputs ?? []} onChange={inputs => onChange({ ...value, inputs })} fields={described ? (described.parameters?.map(field => ({ name: field.name, title: field.description || field.name, type: expressionKind(field.type), required: field.required, choices: field.choices, ref: field.ref })) ?? Object.entries(described.input?.properties ?? {}).map(([name, schema]) => ({ name, title: schema.description || name, type: expressionKind(schema.type), required: described.input?.required?.includes(name), choices: schema.enum }))) as Api.FieldInfo[] : undefined} choices={choices} context={context} /></fieldset>
    <label className={fieldClass}>{t("Retained result value")}<Select disabled={!described} value={JSON.stringify(value.resultPath ?? [])} onChange={event => { const output = outputs.find(output => JSON.stringify(output.path) === event.target.value); if (output) onChange({ ...value, resultPath: output.path, resultType: { kind: output.kind } }); }}><option value="">{t("Choose a scalar result")}</option>{!outputs.some(output => JSON.stringify(output.path) === JSON.stringify(value.resultPath ?? [])) && <option value={JSON.stringify(value.resultPath ?? [])}>{(value.resultPath ?? []).join(".") || t("Entire result")} · {t(value.resultType.kind)}</option>}{outputs.map(output => <option key={JSON.stringify(output.path)} value={JSON.stringify(output.path)}>{output.title} · {t(output.kind)}</option>)}</Select></label>
  </>;
}

function IntervalProjectionEditor({ value, onChange, context }: { value: Api.TxnIntervals; onChange: (value: Api.TxnIntervals) => void; context: EditorContext }) {
  const sets = context.process.sets?.filter(set => set.object !== context.subject && !!set.order.length) ?? [];
  const output = context.lookup(sets.find(set => set.name === value.set)?.object ?? "");
  const relations = context.definitions.filter(definition => definition.linkType?.storage === "edge" && definition.linkType.cardinality === "many-to-many" && definition.linkType.parent.name === output?.type && definition.linkType.child.name === output?.type);
  const fields: [keyof Api.TxnIntervalFields, string, string][] = [
    ["subject", "Observed record", "reference"], ["source", "Observation source", "text"], ["sourceVersion", "Source version", "text"], ["sourceRevision", "Source revision", "text"], ["processingVersion", "Processing version", "text"],
    ["since", "Known from", "datetime"], ["until", "Known until", "datetime"], ["openStart", "Open start", "boolean"], ["openEnd", "Open end", "boolean"], ["retired", "Retired interval", "boolean"], ["review", "Requires review", "boolean"],
  ];
  return <div className="grid gap-3">
    <p className="text-xs text-muted">{t("Only the accepted source can reconcile this collection. Unchanged intervals retain their identity and human fields; split and merged events keep predecessors for review.")}</p>
    <label className={fieldClass}>{t("Logical observation name")}<Input value={value.source} onChange={event => onChange({ ...value, source: event.target.value })} /></label>
    <label className={fieldClass}>{t("Current interval collection")}<Select value={value.set} onChange={event => onChange({ ...value, set: event.target.value })}><option value="">{t("Choose a collection")}</option>{sets.map(set => <option key={set.name} value={set.name}>{set.name} · {context.lookup(set.object)?.title ?? set.object}</option>)}</Select></label>
    <p className="text-xs text-muted">{t("Use the complete ordered collection for this source and observed record, excluding retired intervals.")}</p>
    <label className={fieldClass}>{t("Predecessor relationship")}<Select value={JSON.stringify(value.predecessors)} onChange={event => onChange({ ...value, predecessors: JSON.parse(event.target.value) as Api.AssetBinding })}><option value="">{t("Choose a relationship")}</option>{relations.map(definition => <option key={definition.ref.name} value={JSON.stringify({ ref: definition.ref, sourceVersion: definition.version })}>{definition.linkType!.title} · {definition.version}</option>)}</Select></label>
    <div className="grid gap-2 sm:grid-cols-2">{fields.map(([key, label, kind]) => <label key={key} className={fieldClass}>{t(label)}<Select value={value.fields[key]} onChange={event => onChange({ ...value, fields: { ...value.fields, [key]: event.target.value } })}><option value="">{t("Choose an output field")}</option>{output?.fields.filter(field => !field.readOnly && field.type === kind && (kind !== "reference" || field.ref === context.subject)).map(field => <option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>)}</div>
  </div>;
}
