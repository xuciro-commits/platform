import { Button, Checkbox, Disclosure, Input, Select, Tag, Textarea, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { ArrowDownToLine, Braces, Plus, X } from "lucide-react";
import { useContext, useEffect, useId, useState, type ReactNode } from "react";
import { BindingEditor, JSONEditor, PredicateEditor, SchemaEditor, WorkflowFormProblems } from "./workflow-binding";
import { PERIODS, parameterSchema, periodLabel, sourceCapability, type Binding, type Capability, type ValueSchema, type WorkflowDraft, type WorkflowStep } from "./workflow-model";
import {useHost} from "@platform/app";

const textSchema: ValueSchema = { type: "string" };
const emptyPredicate = () => ({ op: "eq" as const, left: { source: "input" as const }, right: { source: "literal" as const, value: true } });
const fieldClass = "grid gap-1 text-xs";

function IdentifierInput({ value, label, onCommit, invalid }: { value: string; label: string; onCommit: (name: string) => void; invalid?: (name: string) => boolean }) {
  const [text, setText] = useState(value), id = useId(), report = useContext(WorkflowFormProblems);
  useEffect(() => { setText(value); }, [value]);
  const problem = invalid?.(text) ? t("Choose a unique, valid identifier.") : "";
  useEffect(() => { report(id, problem); return () => report(id, ""); }, [id, problem, report]);
  const commit = () => { if (!problem && text !== value) onCommit(text); };
  return <><Input aria-label={label} aria-invalid={!!problem} value={text} onChange={(event) => setText(event.target.value)} onBlur={commit}
    onKeyDown={(event) => { if (event.key === "Enter") { event.preventDefault(); commit(); } }} />{problem && <span className="text-[10px] text-danger">{problem}</span>}</>;
}

function NamedInputs({ inputs = {}, schema, parameters = [], steps, onChange, editableNames = false }: { inputs?: Record<string, Binding>; schema?: ValueSchema; parameters?: Api.Field[]; steps: WorkflowStep[]; onChange: (inputs: Record<string, Binding>) => void; editableNames?: boolean }) {
  const [name, setName] = useState("");
  const fields = [...new Set([...Object.keys(schema?.properties ?? {}), ...parameters.map((field) => field.name), ...Object.keys(inputs)])];
  return <div className="grid gap-2">{fields.map((field) => {
    const parameter = parameters.find((entry) => entry.name === field), required = schema?.required?.includes(field) || parameter?.required;
    return <BindingEditor key={field} label={`${field}${required ? " *" : ""}`} value={inputs[field]}
      optional={!required} schema={schema?.properties?.[field] ?? (parameter ? parameterSchema(parameter) : undefined)} steps={steps}
      onChange={(value) => { const next = { ...inputs }; if (value) next[field] = value; else delete next[field]; onChange(next); }} />;
  })}
    {editableNames && <div className="flex gap-1"><Input aria-label={t("Output field name")} placeholder={t("Field name")} value={name} onChange={(event) => setName(event.target.value)} />
      <Button aria-label={t("Add binding")} disabled={!/^[a-zA-Z][a-zA-Z0-9_]*$/.test(name) || !!inputs[name]} onClick={() => { onChange({ ...inputs, [name]: { source: "input" } }); setName(""); }}><Plus className="size-3" /></Button></div>}
    {!fields.length && !editableNames && <p className="text-xs text-muted">{t("This capability has no input fields.")}</p>}
  </div>;
}

export function WorkflowSettings({ draft, onChange, objects, onClose }: { draft: WorkflowDraft; onChange: (patch: Partial<WorkflowDraft>) => void; objects: { type: string; title: string; states: { name: string; title: string }[] }[]; onClose: () => void }) {
  const object = objects.find((item) => item.type === draft.object);
  return <div role="region" aria-label={t("Workflow properties")} className="flex h-full flex-col overflow-hidden rounded-xl border border-border bg-surface">
    <div className="flex items-center justify-between border-b border-border px-3 py-3"><h3 className="text-sm font-semibold">{t("Workflow settings")}</h3><Button variant="ghost" type="button" onClick={onClose} aria-label={t("Close inspector")}><X className="size-4 text-muted" /></Button></div>
    <div className="grid gap-3 overflow-auto p-3">
      <label className={fieldClass}>{t("Workflow name")}<Input disabled={!!draft.published} value={draft.name} placeholder="stockallocation" onChange={(event) => onChange({ name: event.target.value })} /></label>
      <label className={fieldClass}>{t("Workflow title")}<Input value={draft.title} onChange={(event) => onChange({ title: event.target.value })} /></label>
      <Checkbox checked={draft.manual ?? false} disabled={!!draft.every} onChange={(manual) => onChange({ manual })}>{t("Manual or API start")}</Checkbox>
      <label className={fieldClass}>{t("Repeat every")}<Select disabled={!!draft.published} value={draft.every ?? ""} onChange={(event) => onChange(event.target.value ? { every: event.target.value, manual: false, object: "", when: "" } : { every: undefined })}><option value="">{t("Not scheduled")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>
      <label className={fieldClass}>{t("Source object")}<Select disabled={!!draft.published || !!draft.every} value={draft.object} onChange={(event) => onChange({ object: event.target.value, when: objects.find((item) => item.type === event.target.value)?.states[0]?.name ?? "" })}>
        <option value="">{t("No source record")}</option>{objects.map((item) => <option key={item.type} value={item.type}>{item.title}</option>)}
      </Select></label>
      {!draft.manual && !draft.every && <label className={fieldClass}>{t("Start state")}<Select value={draft.when} onChange={(event) => onChange({ when: event.target.value })}><option value="">{t("Choose a state")}</option>{object?.states.map((state) => <option key={state.name} value={state.name}>{state.title}</option>)}</Select></label>}
      <Disclosure defaultOpen summary={<span className="text-xs font-medium">{t("Workflow input schema")}</span>}>
        <SchemaEditor schema={draft.inputSchema ?? { type: "object", properties: {} }} onChange={(inputSchema) => onChange({ inputSchema })} /></Disclosure>
      <JSONEditor label={t("Default workflow input")} value={draft.input ?? {}} schema={draft.inputSchema} onChange={(input) => onChange({ input })} />
      <p className="text-[11px] leading-5 text-muted">{t("The entry is the first declared block. Use Make entry on a selected block to change it.")}</p>
    </div>
  </div>;
}

export function WorkflowInspector({ step, steps, capabilities, onChange, onRename, onMakeEntry, onClose, flows, output }: {
  step: WorkflowStep; steps: WorkflowStep[]; capabilities: Capability[]; onChange: (patch: Partial<WorkflowStep>) => void; onRename: (name: string) => void;
  onMakeEntry: () => void; onClose: () => void; flows: { id: string; title: string; version: number }[]; output?: unknown;
}) {
  const [tab, setTab] = useState<"settings" | "input" | "output">("input");
  const capability = sourceCapability(step, capabilities);
  const {definitions}=useHost();
  const queryDefinition=definitions.find(d=>d.ref.kind==="query"&&d.ref.app===step.app&&d.ref.name===step.query);
  const queryVersions=Object.keys(queryDefinition?.queryVersions??{}).map(version=>({source:version,ordinal:Number(version.match(/\.query-(\d+)$/)?.[1]??0)})).filter(v=>v.ordinal>0);
  const others = steps.filter((item) => item.name !== step.name);
  const field = (label: string, element: ReactNode) => <label className={fieldClass}>{t(label)}{element}</label>;
  const binding = (label: string, name: "value" | "target" | "collection", schema?: ValueSchema, optional = false) => <BindingEditor label={t(label)} value={step[name]} schema={schema} optional={optional} steps={others} onChange={(value) => onChange({ [name]: value })} />;
  const condition = <PredicateEditor value={step.condition ?? emptyPredicate()} steps={others} onChange={(condition) => onChange({ condition })} />;
  const paths = <div className="grid gap-2">
    {step.kind === "switch" && <Button onClick={() => { let key = "case"; while (key in (step.cases ?? {})) key += "2"; onChange({ cases: { ...step.cases, [key]: "" } }); }}>{t("Add case")}</Button>}
    {(step.kind === "branch" || step.kind === "switch" || step.kind === "ask") && Object.entries(step.cases ?? {}).map(([key, target]) => <div key={key} className="grid gap-1 rounded border border-border p-2">
      {step.kind === "switch" ? <IdentifierInput label={t("Case value")} value={key} invalid={(name) => !name || name !== key && name in (step.cases ?? {})} onCommit={(name) => { const next = { ...step.cases }; delete next[key]; next[name] = target; onChange({ cases: next }); }} /> : <span className="text-xs font-medium">{key}</span>}
      <Select aria-label={t("After {case}", { case: key })} value={target} onChange={(event) => onChange({ cases: { ...step.cases, [key]: event.target.value } })}><option value="">{t("Connect on canvas")}</option>{others.map((item) => <option key={item.name} value={item.name}>{item.title || item.name}</option>)}</Select>
    </div>)}
    {!["end", "join", "break", "continue", "fail", "branch"].includes(step.kind) && field(step.kind === "switch" ? "Default path" : "Next path", <Select value={step.next ?? ""} onChange={(event) => onChange({ next: event.target.value || undefined })}><option value="">{t("Connect on canvas")}</option>{others.map((item) => <option key={item.name} value={item.name}>{item.title || item.name}</option>)}</Select>)}
    {field("Error path", <Select value={step.error ?? ""} onChange={(event) => onChange({ error: event.target.value || undefined })}><option value="">{t("Stop with an error")}</option>{others.map((item) => <option key={item.name} value={item.name}>{item.title || item.name}</option>)}</Select>)}
  </div>;
  return <div role="region" aria-label={t("Workflow properties")} className="flex h-full flex-col overflow-hidden rounded-xl border border-border bg-surface">
    <div className="flex items-center gap-2 border-b border-border px-3 py-3"><span className="rounded-md bg-row-selected p-1.5 text-primary"><Braces className="size-4" /></span><div className="min-w-0 flex-1"><div className="truncate text-sm font-semibold">{step.title || step.name}</div><div className="truncate font-mono text-[10px] text-muted">{step.name}</div></div>
      <Button variant="ghost" type="button" onClick={onClose} aria-label={t("Close inspector")}><X className="size-4 text-muted" /></Button></div>
    <div className="flex border-b border-border p-1" role="tablist" aria-label={t("Block inspector")}>{(["settings", "input", "output"] as const).map((name) => <Button variant="ghost" type="button" key={name} role="tab" aria-selected={tab === name} className={`flex-1 rounded px-2 py-1.5 text-xs ${tab === name ? "bg-row-selected font-medium text-primary" : "text-muted hover:bg-row-hover"}`} onClick={() => setTab(name)}>{t(name === "settings" ? "Settings" : name === "input" ? "Input" : "Output")}</Button>)}</div>
    <div className="grid gap-3 overflow-auto p-3">
      {tab === "settings" && <>
        {field("Step title", <Input value={step.title ?? ""} onChange={(event) => onChange({ title: event.target.value })} />)}
        {field("Step name", <IdentifierInput label={t("Step name")} value={step.name} onCommit={onRename} invalid={(name) => !/^[a-z][a-z0-9]*$/.test(name) || others.some((item) => item.name === name)} />)}
        <div className="flex items-center gap-1"><Tag label={t(capability?.group ?? step.kind)} /><Tag label={capability?.source ?? "platform"} /></div>
        {capability && <><p className="text-[11px] leading-5 text-muted">{t(capability.description)}</p><code className="break-all text-[10px] text-muted">{capability.ref.app}/{capability.ref.kind}/{capability.ref.name}@{capability.version}</code></>}
        {steps[0]?.name !== step.name && <Button onClick={onMakeEntry}><ArrowDownToLine className="mr-1 size-3" />{t("Make entry")}</Button>}
        {field("Timeout (seconds)", <Input type="number" min={0} max={2592000} value={step.timeoutSeconds ?? 0} onChange={(event) => onChange({ timeoutSeconds: Number(event.target.value) })} />)}
        <Disclosure summary={<span className="text-xs font-medium">{t("Control paths")}</span>}><div className="mt-2">{paths}</div></Disclosure>
      </>}
      {tab === "input" && <>
        {step.kind==="query"&&step.app==="build"&&<>{field("Retained query version",<Select aria-label={t("Retained query version")} value={step.queryVersion??0} onChange={event=>onChange({queryVersion:Number(event.target.value)})}><option value={0}>{t("Choose a retained query version")}</option>{step.queryVersion&&!queryVersions.some(v=>v.ordinal===step.queryVersion)&&<option value={step.queryVersion}>{t("Unavailable query version")}: {step.queryVersion}</option>}{queryVersions.map(v=><option key={v.source} value={v.ordinal}>{v.source}</option>)}</Select>)}<p className="text-xs text-muted">{t("Query versions keep their fixed conditions. Reads use the initiating member's current permissions.")}</p>{!capability&&<p role="alert" className="text-xs text-danger">{t("The retained query source could not be loaded.")}</p>}</>}
        {(step.kind === "query" || step.kind === "action" || step.kind === "compute") && <>
          {step.kind === "compute" && capability?.input?.type === "object" && !capability.input.nullable && <Checkbox checked={!!step.value} onChange={(whole) => onChange({ value: whole ? { source: "input" } : undefined, inputs: undefined })}>{t("Bind the whole input value")}</Checkbox>}
          {step.kind === "compute" && (step.value || capability?.input?.type !== "object" || capability?.input?.nullable)
            ? binding("Complete input value", "value", capability?.input)
            : <NamedInputs inputs={step.inputs} schema={capability?.input} parameters={capability?.parameters} steps={others} onChange={(inputs) => onChange({ inputs })} />}
          {step.kind === "action" && <>{binding("Target record ID", "target", textSchema)}{field("Protocol (if cross-app)", <Input value={step.protocol ?? ""} onChange={(event) => onChange({ protocol: event.target.value || undefined })} />)}</>}
          {step.kind === "compute" && step.operation && field("Retained code version", <Input type="number" min={step.operation.app === "build" ? 1 : 0} disabled={step.operation.app !== "build"} value={step.operation.version} onChange={(event) => onChange({ operation: { ...step.operation!, version: Number(event.target.value) } })} />)}
        </>}
        {step.kind === "ai" && <>{binding("Source record ID", "target", textSchema)}{step.function && field("Retained AI version", <Input type="number" min={step.function.app === "build" ? 1 : 0} disabled={step.function.app !== "build"} value={step.function.version} onChange={(event) => onChange({ function: { ...step.function!, version: Number(event.target.value) } })} />)}<p className="text-xs text-muted">{t("AI calls keep the existing source permissions, model budget and evaluation gate.")}</p></>}
        {(step.kind === "transform" || step.kind === "payload") && <>
          <NamedInputs inputs={step.inputs} steps={others} editableNames onChange={(inputs) => onChange({ inputs, value: undefined })} />
          {binding("Whole output value", "value", undefined, true)}
        </>}
        {step.kind === "branch" && condition}
        {step.kind === "switch" && <>{binding("Switch value", "value")}{paths}</>}
        {(step.kind === "foreach" || step.kind === "while") && <>
          {step.kind === "foreach" ? binding("Collection", "collection", { type: "array" }) : <>{binding("Initial loop state", "value")}{condition}</>}
          {field("Maximum iterations", <Input type="number" min={1} max={10000} value={step.maxIterations ?? 100} onChange={(event) => onChange({ maxIterations: Number(event.target.value) })} />)}
          {field("Concurrency", <Input type="number" min={1} max={step.kind === "while" ? 1 : 32} value={step.concurrency ?? 1} onChange={(event) => onChange({ concurrency: Number(event.target.value) })} />)}
          {field("Loop body", <Select value={step.body ?? ""} onChange={(event) => onChange({ body: event.target.value || undefined })}><option value="">{t("Connect on canvas")}</option>{others.map((item) => <option key={item.name} value={item.name}>{item.title || item.name}</option>)}</Select>)}
          <p className="text-[11px] leading-5 text-muted">{t("End the body with Return. Item and index bindings are available only inside this scope.")}</p>
        </>}
        {step.kind === "fork" && <>{field("Join mode", <Select value={step.mode ?? "all"} onChange={(event) => onChange({ mode: event.target.value as "all" | "any" })}><option value="all">{t("All paths")}</option><option value="any">{t("First successful path")}</option></Select>)}
          <div className="grid gap-2">{(step.branches ?? []).map((target, i) => <Select key={i} aria-label={t("Parallel path {n}", { n: i + 1 })} value={target} onChange={(event) => onChange({ branches: step.branches!.map((old, index) => index === i ? event.target.value : old) })}><option value="">{t("Connect on canvas")}</option>{others.map((item) => <option key={item.name} value={item.name}>{item.title || item.name}</option>)}</Select>)}
            <Button onClick={() => onChange({ branches: [...(step.branches ?? []), ""] })}>{t("Add parallel path")}</Button></div></>}
        {step.kind === "ask" && <>{field("Recipient role", <Input value={step.ask ?? "user"} onChange={(event) => onChange({ ask: event.target.value })} />)}
          {field("Answers, one per line", <Textarea rows={3} value={(step.answers ?? []).join("\n")} onChange={(event) => {
            const answers = event.target.value.split("\n"); onChange({ answers, cases: Object.fromEntries(answers.map((answer) => [answer, step.cases?.[answer] ?? ""])) });
          }} />)}{paths}<p className="text-[11px] text-muted">{t("People answer in the existing inbox; this block suspends without occupying a worker.")}</p></>}
        {step.kind === "wait" && <>{field("Wait duration (seconds)", <Input type="number" min={0} max={2592000} value={step.untilSeconds ?? 60} onChange={(event) => onChange({ untilSeconds: Number(event.target.value), condition: undefined })} />)}
          <Checkbox checked={!!step.condition} onChange={(checked) => onChange({ condition: checked ? emptyPredicate() : undefined })}>{t("Wait for a condition")}</Checkbox>{step.condition && condition}</>}
        {step.kind === "subflow" && <>{field("Workflow", <Select value={step.flow ?? ""} onChange={(event) => onChange({ flow: event.target.value, flowVersion: flows.find((flow) => flow.id === event.target.value)?.version ?? 1 })}><option value="">{t("Choose a workflow")}</option>{flows.map((flow) => <option key={`${flow.id}:${flow.version}`} value={flow.id}>{flow.title} · v{flow.version}</option>)}</Select>)}
          {field("Retained workflow version", <Input type="number" min={1} value={step.flowVersion ?? 1} onChange={(event) => onChange({ flowVersion: Number(event.target.value) })} />)}<NamedInputs inputs={step.inputs} steps={others} editableNames onChange={(inputs) => onChange({ inputs })} /></>}
        {["end", "join", "continue"].includes(step.kind) && binding("Return value", "value", undefined, true)}
        {["break", "continue"].includes(step.kind) && <p className="text-[11px] leading-5 text-muted">{t("This control exits or continues the innermost loop scope. Accepted actions remain accepted.")}</p>}
        {step.kind === "fail" && <p className="text-xs text-muted">{t("The block title becomes the explicit failure message.")}</p>}
      </>}
      {tab === "output" && <>
        <h4 className="text-xs font-medium">{t("Output schema")}</h4><pre className="max-h-60 overflow-auto rounded bg-background p-2 text-[10px]">{JSON.stringify(capability?.output ?? { type: "value", description: t("Derived from this block's bindings.") }, null, 2)}</pre>
        <h4 className="text-xs font-medium">{t("Last accepted output")}</h4>{output !== undefined ? <pre className="max-h-72 overflow-auto rounded bg-background p-2 text-[11px]">{JSON.stringify(output, null, 2)}</pre> : <p className="text-[11px] text-muted">{t("Select a run to inspect this block's accepted output.")}</p>}
      </>}
    </div>
  </div>;
}
