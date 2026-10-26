import { sourceFieldInput } from "@platform/app";
import { ContextHelp, StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue, replaceDeclaration } from "../editor/declaration";
// Automations: a rule reads as one sentence - when a record of
// an object type reaches a state, [if a condition holds,] run these effects.
// An automation is a build.process authored as `kind: "automation"`: the host
// holds it to that linear shape (checkAutomation), the Automate app shows it as
// cards and compiles the cards to steps. Anything richer is a flow on the map.
import { useHost, useReadQuery } from "@platform/app";
import { ActionMenu, Button, Input, PageHeader, ProblemList, Select, Tag, Workbench, cn, t, type WorkbenchProblem } from "@platform/ui";
import { ArrowDown, ArrowUp, Brain, Clock, MoreHorizontal, Plus, Trash2, Workflow, Zap } from "lucide-react";
import { useMemo, useRef, useState } from "react";
import { useApplicationWorkspace } from "../projects/application-scope";
import { SystemDefinitionReferences } from "../editor/SystemDefinitions";
import { DraftInputs } from "../session/DraftSession";
import { DraftStatus, ResourceControls, WorkbenchMessage, savingState, useAutoSave } from "../editor/workbench";
import { WorkflowInspector } from "./workflow-inspector";
import { PERIODS, capabilityKey, initialStep, nextStepName, periodLabel, type Capability, type WorkflowDraft, type WorkflowStep } from "./workflow-model";
import { WorkflowRuns } from "./workflow-runs";
import { useAuthoringCapabilities, useAuthoringModel } from "../ontology/authoring-model";
import { PredicateEditor } from "./workflow-binding";
import { renameWorkflowStep } from "./workflow-edit";
import { automationParts, relinkAutomation, removeAutomationStep } from "./automation-model";
import { useProcessDraft } from "./process-draft";

type Row = WorkflowDraft & { version?: number; published?: string };
const effectKinds = (steps: WorkflowStep[]) => {
  const counts: Record<string, number> = {};
  for (const step of automationParts({ steps: steps ?? [] } as WorkflowDraft).effects) counts[step.kind] = (counts[step.kind] ?? 0) + 1;
  return Object.entries(counts).map(([kind, n]) => `${n} × ${t(kind)}`).join(" · ");
};
const sentence = (titleOf: (type: string) => string, stateOf: (type: string, name: string) => string, row: WorkflowDraft) =>
  row.every ? periodLabel(row.every) : t("a {object} reaches {state}", { object: titleOf(row.object), state: stateOf(row.object, row.when) });

/** Automations as cards: trigger on the left, effects on the right, state as a tag. */
export function Automations() {
  const { role, entities } = useHost(), { open } = useApplicationWorkspace();
  const query = useReadQuery<{ records?: Row[] }>("/v1/records/build.process?limit=200");
  const [search, setSearch] = useState("");
  const rows = (query.data?.records ?? []).filter((row) => row.kind === "automation");
  const needle = search.trim().toLowerCase();
  const visible = rows.filter((row) => !needle || `${row.title} ${row.name} ${row.object} ${row.when}`.toLowerCase().includes(needle));
  const titleOf = (type: string) => entities.find((entity) => entity.type === type)?.title ?? type;
  const stateOf = (type: string, name: string) => entities.find((entity) => entity.type === type)?.lifecycle?.states.find((state) => state.name === name)?.title ?? name;
  return <div className="grid content-start gap-3 p-4">
    <PageHeader title={t("Automations")} help={t("When a record reaches a state, run effects. Each automation is one sentence; open it to shape the effects on the map.")}
      actions={role("build") === "builder" && <Button onClick={() => open({ view: "automation", params: { id: "new" } })}><Plus />{t("New automation")}</Button>} />
    <div className="flex flex-wrap items-center gap-2">
      <Input type="search" aria-label={t("Filter automations")} placeholder={t("Filter automations")} value={search} onChange={(event) => setSearch(event.target.value)} className="max-w-xs" />
      <Button variant="ghost" size="sm" onClick={() => open({ view: "runs" })}><Clock />{t("Runs")}</Button>
      <Button variant="ghost" size="sm" onClick={() => open({ view: "flow" })}><Workflow />{t("All flows")}</Button>
    </div>
    <SystemDefinitionReferences type="build.process" search={search} title="System flows" description="System flows are available here as read-only references." />
    {query.isLoading && <p className="text-sm text-muted">{t("Loading…")}</p>}
    {!query.isLoading && !visible.length && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">
      <p>{rows.length ? t("No automation matches the filter.") : t("No automations yet.")}</p>
      {!rows.length && <p className="mt-1 text-xs">{t("Create one: choose the object type and the state that triggers it, then add the effects.")}</p>}
    </div>}
    <ul className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">{visible.map((row) => <li key={row.id}>
      <Button variant="row" onClick={() => open({ view: "automation", params: { id: row.id } })} className={cn("grid w-full gap-2 border-border bg-surface p-3 text-left hover:border-primary")}>
        <span className="flex items-center gap-2"><Zap className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate text-sm font-medium">{row.title || row.name}</span>
          <Tag label={row.version ? t("Active · v{n}", { n: row.version }) : t("Draft")} tone={row.version ? "success" : "warning"} /></span>
        <span className="text-xs text-muted"><span className="font-medium text-foreground">{t("When")}</span> {sentence(titleOf, stateOf, row)}</span>
        <span className="text-xs text-muted"><span className="font-medium text-foreground">{t("Then")}</span> {effectKinds(row.steps) || t("nothing yet")}</span>
      </Button>
    </li>)}</ul>
  </div>;
}

const empty = (): WorkflowDraft => ({ id: "", revision: 0, name: "", title: "", kind: "automation", object: "", when: "", manual: false, input: {}, inputSchema: { type: "object", properties: {} }, steps: [], layout: {} });
type Chosen = "trigger" | "condition" | string;

/** One automation as a sentence of cards: trigger → condition → effects, each effect's parameters in the inspector. */
export function AutomationEditor({ id }: { id: string }) {
  const { role } = useHost(), { open } = useApplicationWorkspace();
  const model = useAuthoringModel();
  const catalogQuery = useAuthoringCapabilities<Capability>();
  const flowQuery = useReadQuery<{ id: string; title: string; version: number }[]>("/v1/flows");
  const authoringHost = useHost(), trial = useActionTrial();
  const declarationFields = authoringHost.source.entity("build.process")?.fields ?? [];
  const [sourceApplied, setSourceApplied] = useState(false);
  const process = useProcessDraft({ id, view: "automation", empty, failure: t("The automation request could not be completed.") });
  const { session, draft, dirty, createID, query, busy, error, edit, save, publish, perform, discardChanges, revision } = process;
  const [chosen, setChosen] = useState<Chosen>("trigger");
  const objects = useMemo(() => model.entities.filter((entity) => entity.lifecycle).map((entity) => ({ type: entity.type, title: entity.title, states: entity.lifecycle!.states, fields: entity.fields })), [model.entities]);
  const object = objects.find((item) => item.type === draft.object);
  const capabilities = catalogQuery.data;
  const offered = capabilities.filter((capability) => (["action", "ai", "compute"].includes(capability.kind)) && (!draft.object || capability.ref.name.startsWith(draft.object + ".") || capability.kind === "ai" || capability.kind === "compute"));
  const { condition, effects, editable } = automationParts(draft);
  const change = (patch: Partial<WorkflowDraft>) => { if (!busy) { setSourceApplied(false); edit(patch); } };
  const setSteps = (nextCondition: WorkflowStep | undefined, nextEffects: WorkflowStep[], steps = draft.steps) => change({ steps: relinkAutomation(steps, nextCondition, nextEffects) });
  const removeStep = (name: string) => {
    change(removeAutomationStep(draft, name));
    for (const key of Object.keys(session.inputs.values)) if (key.startsWith(`step:${name}/`) || name === condition?.name && key.startsWith("condition/")) session.inputs.set(key);
    if (chosen === `step:${name}` || name === condition?.name && chosen === "condition") setChosen("trigger");
  };
  const addEffect = (capability: Capability) => {
    const step = { ...initialStep(capability, draft.steps), ...(capability.kind !== "compute" && !draft.every ? { target: { source: "subject" as const } } : {}) };
    setSteps(condition, [...effects, step]); setChosen(`step:${step.name}`);
  };
  const problems: WorkbenchProblem[] = [
    ...Object.entries(session.inputProblems).filter(([, problem]) => problem).map(([key, problem]) => ({ id: `form:${key}`, text: problem, locate: session.inputs.values[key]?.reveal })),
    ...(!draft.title ? [{ id: "title", text: t("Give the automation a title."), locate: () => setChosen("trigger") }] : []),
    ...(!draft.every && (!draft.object || !draft.when) ? [{ id: "trigger", text: t("Choose the object type and the state that start it, or a schedule."), locate: () => setChosen("trigger") }] : []),
    ...(!effects.length ? [{ id: "effects", text: t("Add at least one effect.") }] : []),
    ...(catalogQuery.isError ? [{ id: "catalog", text: t("The capability library could not be loaded."), locate: () => void catalogQuery.refetch() }] : []),
    ...(error ? [{ id: "host", text: error }] : []),
  ];
  const invalid = !editable || problems.some((problem) => problem.id !== "host");
  const review = async () => {
    const target = await process.reviewTarget();
    if (target) open({ view: "release-review", params: { kind: "flow", id: target } });
  };
  const saveRef = useRef<() => Promise<unknown>>(async () => false); saveRef.current = () => perform(save);
  useAutoSave({enabled: !sourceApplied && editable, generation:JSON.stringify(draft), dirty, invalid, busy, save: () => saveRef.current() });
  if (role("build") !== "builder") return <Workbench storageKey="automation" title={t("Automation")}><WorkbenchMessage>{t("Only a builder can edit automations.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="automation" title={t("Automation")}><WorkbenchMessage>{query.isError ? t("The automation could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const chosenStep = effects.find((step) => `step:${step.name}` === chosen);
  const card = (key: string, label: string, body: React.ReactNode, extra?: React.ReactNode) => <div key={key} role="button" tabIndex={0} aria-pressed={chosen === key} onClick={() => setChosen(key)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") setChosen(key); }}
    className={cn("grid gap-2 rounded-lg border bg-surface p-3 text-left", chosen === key ? "border-primary ring-1 ring-primary" : "border-border hover:border-primary/60")}>
    <div className="flex items-center gap-2"><span className="rounded bg-row-hover px-1.5 py-0.5 text-2xs font-semibold uppercase tracking-wide text-muted">{label}</span><span className="ml-auto flex items-center gap-1">{extra}</span></div>
    {body}
  </div>;
  const openMap = () => perform(async () => {
    if ((dirty || !draft.id) && !await save()) return;
    open({ view: "flow", params: { id: draft.id || createID } });
  });
  return <Workbench storageKey="automation" crumbs={[{ label: t("Automate"), onClick: () => open({ view: "automation" }) }, { label: t("Automations"), onClick: () => open({ view: "automation" }) }]} title={draft.title || t("New automation")}
    status={<DraftStatus state={draft.version ? "published" : "draft"} problems={problems.length} />} saving={savingState(dirty, busy, error || undefined)}
    history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: session.undo, redo: session.redo }}
    actions={<>
      <ActionMenu label={t("More automation commands")} icon={<MoreHorizontal />} commands={[{ id: "map", label: t("Open on the flow map"), icon: <Workflow />, disabled: busy || Object.values(session.inputProblems).some(Boolean), run: () => void openMap() }]} />
      <ResourceControls type="build.process" record={draft} dirty={dirty} busy={busy} invalid={invalid} empty={!effects.length} onReview={() => void perform(review)} onInstall={() => void perform(publish)} onCancel={discardChanges} route={{ view: "automation", params: { id } }} />
    </>}
    right={editable ? { label: t("Automation inspector"), heading: chosenStep?.title || chosenStep?.name || t(chosen === "condition" ? "If" : "When"), scope:String(chosen),locate:()=>setChosen(chosen), content: <DraftInputs.Provider value={{ ...session.inputs, scope: chosenStep ? `step:${chosenStep.name}` : chosen, reveal: () => setChosen(chosen) }}><div className="p-2">
      {chosen === "trigger" && <div className="grid gap-3">
        <label className="grid gap-1 text-xs">{t("Title")}<Input value={draft.title} onChange={(event) => change({ title: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Start kind")}<Select value={draft.every ? draft.every : ""} disabled={!!draft.published} onChange={(event) => change(event.target.value ? { every: event.target.value, object: "", when: "" } : { every: undefined })}><option value="">{t("When a record reaches a state")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>
        {draft.every && <ContextHelp title={t("Schedule")}><p className="text-xs text-muted">{t("A scheduled automation has no subject record; each period starts one run, acting as the member who published it.")}</p></ContextHelp>}
        {!draft.every && <label className="grid gap-1 text-xs">{t("Object type")}<Select value={draft.object} disabled={!!draft.published} onChange={(event) => change({ object: event.target.value, when: objects.find((item) => item.type === event.target.value)?.states[0]?.name ?? "" })}><option value="">{t("Choose an object type")}</option>{objects.map((item) => <option key={item.type} value={item.type}>{item.title}</option>)}</Select></label>}
        {!draft.every && <label className="grid gap-1 text-xs">{t("When it reaches")}<Select value={draft.when} onChange={(event) => change({ when: event.target.value })}><option value="">{t("Choose a state")}</option>{object?.states.map((state) => <option key={state.name} value={state.name}>{state.title}</option>)}</Select></label>}
        {!draft.every && <ContextHelp title={t("Trigger")}><p className="text-xs text-muted">{t("The record that reached the state is the subject of every effect: actions run on it, functions read it.")}</p></ContextHelp>}
      </div>}
      {chosen === "condition" && condition && <div className="grid gap-3">
        <PredicateEditor value={condition.condition ?? { op: "eq", left: { source: "subject" }, right: { source: "literal", value: "" } }} steps={draft.steps.filter((step) => step !== condition)} onChange={(predicate) => setSteps({ ...condition, condition: predicate }, effects)} />
        <Button variant="ghost" size="sm" onClick={() => removeStep(condition.name)}><Trash2 />{t("Remove the condition")}</Button>
      </div>}
      {chosenStep && <WorkflowInspector step={chosenStep} steps={draft.steps} lanes={draft.lanes ?? []} capabilities={capabilities} flows={flowQuery.data ?? []}
        onChange={(patch) => setSteps(condition, effects.map((step) => step.name === chosenStep.name ? { ...step, ...patch } : step))}
        onRename={(name) => { const old = chosenStep.name; if (/^[a-z][a-z0-9]*$/.test(name) && !draft.steps.some((step) => step.name === name)) {
          setSourceApplied(false); session.edit((current) => renameWorkflowStep(current, old, name));
          for (const [key, value] of Object.entries(session.inputs.values)) if (key.startsWith(`step:${old}/`)) { session.inputs.set(key); if (key !== `step:${old}/identifier:${t("Step name")}:${old}`) session.inputs.set(key.replace(`step:${old}/`, `step:${name}/`), { ...value, reveal: () => setChosen(`step:${name}`) }); }
          setChosen(`step:${name}`);
        } }}
        linear onMakeEntry={() => {}} />}
    </div></DraftInputs.Provider> } : undefined}
    dock={{ label: t("Automation dock"), tabs: [
      { id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} empty={t("No problems. The automation can be published.")} /> },
      { id: "runs", title: t("Runs"), content: <div className="p-3">{draft.id ? <WorkflowRuns name={draft.name} versions={draft.versions} onStepSelect={(name) => setChosen(`step:${name}`)} /> : <p className="text-xs text-muted">{t("Runs appear once the automation is saved and published.")}</p>}</div> },
    ] }}>
    <StructuredEditor expectedRevision={draft.id ? revision : 0} fieldInput={sourceFieldInput} value={declarationValue(draft, declarationFields)} fields={declarationFields} scope={`build.process:${id}:${authoringHost.source.scope}`} disabled={busy} onChange={next => { setSourceApplied(true); session.edit(() => ({ ...empty(), ...replaceDeclaration(draft, next, declarationFields) })); setChosen("trigger"); }} onTrial={next => trial(`build.process.${draft.id ? "edit" : "create"}`, { type: "build.process", id: draft.id || createID }, next, draft.id ? revision : 0)}>
    {!editable ? <div className="grid content-start justify-items-start gap-3 p-4">
      <p className="text-sm">{t("This automation uses settings that need the flow map. Open it there to keep every step and result.")}</p>
      <Button disabled={busy || Object.values(session.inputProblems).some(Boolean)} onClick={() => void openMap()}><Workflow />{t("Open on the flow map")}</Button>
    </div> : <div className="mx-auto grid w-full max-w-2xl content-start gap-2 p-4">
      {card("trigger", t("When"), <p className="text-sm">{draft.every || draft.object && draft.when ? sentence((type) => objects.find((item) => item.type === type)?.title ?? type, (type, name) => objects.find((item) => item.type === type)?.states.find((state) => state.name === name)?.title ?? name, draft) : <span className="text-muted">{t("Choose the object type and the state that start it.")}</span>}</p>)}
      {condition ? card("condition", t("If"), <p className="text-sm">{t("Condition")}</p>)
        : <Button variant="ghost" size="sm" className="w-fit" disabled={!draft.object && !draft.every} onClick={() => { const conditionName = nextStepName(draft.steps, "when"), stop = nextStepName([...draft.steps, { name: conditionName } as WorkflowStep], "stop"); setSteps({ name: conditionName, kind: "branch", cases: { false: stop }, condition: { op: "eq", left: draft.every ? { source: "input" } : { source: "subject", path: [object?.fields[0]?.name ?? ""] }, right: { source: "literal", value: "" } } }, effects, [...draft.steps, { name: stop, kind: "end", value: { source: "input" } }]); setChosen("condition"); }}><Plus />{t("Add a condition")}</Button>}
      {effects.map((step, at) => card(`step:${step.name}`, at === 0 ? t("Then") : t("And then"), <p className="flex items-center gap-2 text-sm">{step.kind === "ai" ? <Brain className="size-4 text-muted" /> : <Zap className="size-4 text-muted" />}<span className="min-w-0 flex-1 truncate">{step.title || step.name}</span><span className="font-mono text-xs text-muted">{step.act ?? step.function?.name ?? step.operation?.name}</span></p>,
        <>
          <Button variant="ghost" size="sm" aria-label={t("Move up")} disabled={at === 0} onClick={(event) => { event.stopPropagation(); const next = [...effects]; [next[at - 1], next[at]] = [next[at]!, next[at - 1]!]; setSteps(condition, next); }}><ArrowUp /></Button>
          <Button variant="ghost" size="sm" aria-label={t("Move down")} disabled={at === effects.length - 1} onClick={(event) => { event.stopPropagation(); const next = [...effects]; [next[at + 1], next[at]] = [next[at]!, next[at + 1]!]; setSteps(condition, next); }}><ArrowDown /></Button>
          <Button variant="ghost" size="sm" aria-label={t("Remove")} onClick={(event) => { event.stopPropagation(); removeStep(step.name); }}><Trash2 /></Button>
        </>))}
      {(draft.object || draft.every) && offered.length > 0 && <div className="w-fit"><ActionMenu label={t("Add an effect")} icon={<Plus />} commands={offered.map((capability) => ({ id: capabilityKey(capability), label: `${t(capability.title)} · ${capability.ref.app}`, icon: capability.kind === "ai" ? <Brain /> : <Zap />, run: () => addEffect(capability) }))}>{t("Add an effect")}</ActionMenu></div>}
      {(draft.object || draft.every) && !offered.length && !catalogQuery.isLoading && !catalogQuery.isError && <p className="text-xs text-muted">{t("No actions yet.")}</p>}
    </div>}
    </StructuredEditor>
  </Workbench>;
}
