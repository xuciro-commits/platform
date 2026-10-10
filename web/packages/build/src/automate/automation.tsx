// Automations (ADR-0053 §7): a rule reads as one sentence - when a record of
// an object type reaches a state, [if a condition holds,] run these effects.
// An automation is a build.process authored as `kind: "automation"`: the host
// holds it to that linear shape (checkAutomation), the Automate app shows it as
// cards and compiles the cards to steps. Anything richer is a flow on the map.
import { useHost, useReadQuery } from "@platform/app";
import { ActionMenu, Button, Input, PageHeader, ProblemList, Select, Tag, Workbench, cn, t, useUnsavedChanges, type WorkbenchProblem } from "@platform/ui";
import { ArrowDown, ArrowUp, Brain, Clock, MoreHorizontal, Plus, Trash2, Workflow, Zap } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useDraftSession } from "../session/DraftSession";
import { DraftStatus, PublishMenu, WorkbenchMessage, savingState, useAutoSave } from "../editor/workbench";
import { WorkflowInspector } from "./workflow-inspector";
import { PERIODS, capabilityKey, initialStep, nextStepName, periodLabel, type Capability, type WorkflowDraft, type WorkflowStep } from "./workflow-model";
import { WorkflowRuns } from "./workflow-runs";
import { workflowInputs } from "./workflow-session";

type Row = WorkflowDraft & { version?: number; published?: string };
const CONDITION = "when", STOP = "stop";
const operators = ["eq", "neq", "gt", "gte", "lt", "lte", "contains", "exists"] as const;
const operatorLabel: Record<(typeof operators)[number], string> = { eq: "is", neq: "is not", gt: "is greater than", gte: "is at least", lt: "is less than", lte: "is at most", contains: "contains", exists: "is set" };

/** The cards of an automation, read off its steps; `relink` writes them back in the host's shape. */
function parts(steps: WorkflowStep[]) {
  const condition = steps.find((step) => step.name === CONDITION && step.kind === "branch");
  const effects = steps.filter((step) => step.kind !== "end" && step !== condition);
  return { condition, effects };
}
function relink(condition: WorkflowStep | undefined, effects: WorkflowStep[]): WorkflowStep[] {
  const chain = effects.map((step, i) => ({ ...step, next: effects[i + 1]?.name ?? "", cases: undefined, branches: undefined, body: undefined }));
  if (!condition) return chain;
  return [{ ...condition, name: CONDITION, kind: "branch", cases: { true: chain[0]?.name ?? "", false: STOP } }, ...chain, { name: STOP, kind: "end", value: { source: "input" } }];
}
const effectKinds = (steps: WorkflowStep[]) => {
  const counts: Record<string, number> = {};
  for (const step of parts(steps ?? []).effects) counts[step.kind] = (counts[step.kind] ?? 0) + 1;
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
    <PageHeader title={t("Automations")} description={t("When a record reaches a state, run effects. Each automation is one sentence; open it to shape the effects on the map.")}
      actions={role("build") === "builder" && <Button onClick={() => open({ view: "automation", params: { id: "new" } })}><Plus />{t("New automation")}</Button>} />
    <div className="flex items-center gap-2">
      <Input type="search" aria-label={t("Filter automations")} placeholder={t("Filter automations")} value={search} onChange={(event) => setSearch(event.target.value)} className="max-w-xs" />
      <Button variant="ghost" size="sm" onClick={() => open({ view: "runs" })}><Clock />{t("Runs")}</Button>
      <Button variant="ghost" size="sm" onClick={() => open({ view: "flow" })}><Workflow />{t("All flows")}</Button>
    </div>
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
  const { decide, role, entities } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: WorkflowDraft }>(`/v1/records/build.process/${encodeURIComponent(id)}`, undefined, id !== "new");
  const catalogQuery = useReadQuery<Capability[]>("/v1/capabilities");
  const flowQuery = useReadQuery<{ id: string; title: string; version: number }[]>("/v1/flows");
  const session = useDraftSession<WorkflowDraft>(empty());
  const [createID]=useState(()=>crypto.randomUUID());
  const { draft, dirty } = session;
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const [chosen, setChosen] = useState<Chosen>("trigger");
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => {
    const saved = query.data?.record ?? empty(); session.load(saved); baseRevision.current = saved.revision; loaded.current = `${saved.id}:${saved.revision}`; setError("");
  });
  useEffect(() => {
    const saved = query.data?.record;
    if (saved && saved.revision >= baseRevision.current && !dirty && !busy && loaded.current !== `${saved.id}:${saved.revision}`) { session.load(saved); baseRevision.current = saved.revision; loaded.current = `${saved.id}:${saved.revision}`; }
  }, [query.data?.record, dirty, busy, session.load]);
  const objects = useMemo(() => entities.filter((entity) => entity.lifecycle).map((entity) => ({ type: entity.type, title: entity.title, states: entity.lifecycle!.states, fields: entity.fields })), [entities]);
  const object = objects.find((item) => item.type === draft.object);
  const capabilities = catalogQuery.data ?? [];
  const offered = capabilities.filter((capability) => (capability.kind === "action" || capability.kind === "ai") && (!draft.object || capability.ref.name.startsWith(draft.object + ".") || capability.kind === "ai"));
  const { condition, effects } = parts(draft.steps);
  const change = (patch: Partial<WorkflowDraft>) => { if (!lock.current) { session.edit(patch); setError(""); } };
  const setSteps = (nextCondition: WorkflowStep | undefined, nextEffects: WorkflowStep[]) => change({ steps: relink(nextCondition, nextEffects) });
  const addEffect = (capability: Capability) => {
    const step = { ...initialStep(capability, draft.steps), name: nextStepName(draft.steps, capability.ref.name.split(".").at(-1) ?? capability.kind), target: { source: "subject" as const } };
    setSteps(condition, [...effects, step]); setChosen(step.name);
  };
  const problems: WorkbenchProblem[] = [
    ...(!draft.title ? [{ id: "title", text: t("Give the automation a title."), locate: () => setChosen("trigger") }] : []),
    ...(!draft.every && (!draft.object || !draft.when) ? [{ id: "trigger", text: t("Choose the object type and the state that start it, or a schedule."), locate: () => setChosen("trigger") }] : []),
    ...(!effects.length ? [{ id: "effects", text: t("Add at least one effect.") }] : []),
    ...(error ? [{ id: "host", text: error }] : []),
  ];
  const invalid = problems.some((problem) => problem.id !== "host");
  const save = async () => {
    const submitted = structuredClone(draft), target = submitted.id || createID, revision = submitted.id ? baseRevision.current : 0;
    if (!submitted.name) submitted.name = (submitted.title.toLowerCase().replace(/[^a-z0-9]/g, "") || "automation") + Date.now().toString(36).slice(-4);
    if (!await decide(`build.process.${submitted.id ? "edit" : "create"}`, { type: "build.process", id: target }, { ...workflowInputs(submitted), kind: "automation" }, { expectedRevision: submitted.id ? revision : undefined, quiet: true, onRefused: setError })) return false;
    baseRevision.current = revision + 1; loaded.current = `${target}:${revision + 1}`;
    if (!submitted.id) { session.saved(submitted, { ...submitted, id: target, revision: 1 }); markSaved(); open({ view: "automation", params: { id: target } }); close({ view: "automation", params: { id } }); return true; }
    const fresh = await query.refetch();
    session.saved(submitted, fresh.isSuccess && fresh.data?.record?.revision === revision + 1 ? fresh.data.record : { ...submitted, revision: revision + 1 }); markSaved(); return true;
  };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch { setError(t("The automation request could not be completed.")); } finally { lock.current = false; setBusy(false); } };
  const publish = async () => { if (dirty && !await save()) return; await decide("build.process.publish", { type: "build.process", id: draft.id }, {}, { onRefused: setError }); await query.refetch(); };
  const saveRef = useRef<() => Promise<unknown>>(async () => false); saveRef.current = () => perform(save);
  useAutoSave({ generation:JSON.stringify(draft), dirty, invalid, busy, save: () => saveRef.current() });
  if (role("build") !== "builder") return <Workbench storageKey="automation" title={t("Automation")}><WorkbenchMessage>{t("Only a builder can edit automations.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="automation" title={t("Automation")}><WorkbenchMessage>{query.isError ? t("The automation could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const chosenStep = effects.find((step) => step.name === chosen);
  const card = (key: string, label: string, body: React.ReactNode, extra?: React.ReactNode) => <div key={key} role="button" tabIndex={0} aria-pressed={chosen === key} onClick={() => setChosen(key)} onKeyDown={(event) => { if (event.key === "Enter" || event.key === " ") setChosen(key); }}
    className={cn("grid gap-2 rounded-lg border bg-surface p-3 text-left", chosen === key ? "border-primary ring-1 ring-primary" : "border-border hover:border-primary/60")}>
    <div className="flex items-center gap-2"><span className="rounded bg-row-hover px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-muted">{label}</span><span className="ml-auto flex items-center gap-1">{extra}</span></div>
    {body}
  </div>;
  const fieldTitle = (name: string) => object?.fields.find((field) => field.name === name)?.title ?? name;
  const conditionText = condition?.condition ? `${fieldTitle(condition.condition.left?.path?.[0] ?? "")} ${t(operatorLabel[(condition.condition.op as (typeof operators)[number]) ?? "eq"] ?? condition.condition.op)} ${condition.condition.op === "exists" ? "" : JSON.stringify(condition.condition.right?.value ?? "")}` : "";
  return <Workbench storageKey="automation" crumbs={[{ label: t("Automate"), onClick: () => open({ view: "automation" }) }, { label: t("Automations"), onClick: () => open({ view: "automation" }) }]} title={draft.title || t("New automation")}
    status={<DraftStatus state={draft.version ? "published" : "draft"} problems={problems.length} />} saving={savingState(dirty, busy, error || undefined)}
    history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: session.undo, redo: session.redo }}
    actions={<>
      <ActionMenu label={t("More automation commands")} icon={<MoreHorizontal />} commands={[{ id: "map", label: t("Open on the flow map"), icon: <Workflow />, disabled: !draft.id, run: () => open({ view: "flow", params: { id: draft.id } }) }]} />
      <PublishMenu type="build.process" record={draft} dirty={dirty} busy={busy} invalid={invalid} empty={!effects.length} onReview={() => open({ view: "release-review", params: { kind: "flow", id: draft.id } })} onInstall={() => void perform(publish)} onDiscard={discardChanges} route={{ view: "automation", params: { id } }} />
    </>}
    right={{ label: t("Automation inspector"),scope:String(chosen),locate:()=>setChosen(chosen), content: <div className="p-2">
      {chosen === "trigger" && <div className="grid gap-3">
        <label className="grid gap-1 text-xs">{t("Title")}<Input value={draft.title} onChange={(event) => change({ title: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Start kind")}<Select value={draft.every ? draft.every : ""} disabled={!!draft.published} onChange={(event) => change(event.target.value ? { every: event.target.value, object: "", when: "" } : { every: undefined })}><option value="">{t("When a record reaches a state")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>
        {draft.every && <p className="text-[11px] text-muted">{t("A scheduled automation has no subject record; each period starts one run, acting as the member who published it.")}</p>}
        {!draft.every && <label className="grid gap-1 text-xs">{t("Object type")}<Select value={draft.object} disabled={!!draft.published} onChange={(event) => change({ object: event.target.value, when: objects.find((item) => item.type === event.target.value)?.states[0]?.name ?? "" })}><option value="">{t("Choose an object type")}</option>{objects.map((item) => <option key={item.type} value={item.type}>{item.title}</option>)}</Select></label>}
        {!draft.every && <label className="grid gap-1 text-xs">{t("When it reaches")}<Select value={draft.when} onChange={(event) => change({ when: event.target.value })}><option value="">{t("Choose a state")}</option>{object?.states.map((state) => <option key={state.name} value={state.name}>{state.title}</option>)}</Select></label>}
        {!draft.every && <p className="text-[11px] text-muted">{t("The record that reached the state is the subject of every effect: actions run on it, functions read it.")}</p>}
      </div>}
      {chosen === "condition" && condition && <div className="grid gap-3">
        <label className="grid gap-1 text-xs">{t("Field")}<Select value={condition.condition?.left?.path?.[0] ?? ""} onChange={(event) => setSteps({ ...condition, condition: { ...condition.condition!, left: { source: "subject", path: [event.target.value] } } }, effects)}><option value="">{t("Choose a field")}</option>{object?.fields.map((field) => <option key={field.name} value={field.name}>{field.title}</option>)}</Select></label>
        <label className="grid gap-1 text-xs">{t("Comparison")}<Select value={condition.condition?.op ?? "eq"} onChange={(event) => setSteps({ ...condition, condition: { ...condition.condition!, op: event.target.value as (typeof operators)[number] } }, effects)}>{operators.map((op) => <option key={op} value={op}>{t(operatorLabel[op])}</option>)}</Select></label>
        {condition.condition?.op !== "exists" && <label className="grid gap-1 text-xs">{t("Value")}<Input value={String(condition.condition?.right?.value ?? "")} onChange={(event) => { const raw = event.target.value; const value = raw === "true" ? true : raw === "false" ? false : raw !== "" && !Number.isNaN(Number(raw)) ? Number(raw) : raw; setSteps({ ...condition, condition: { ...condition.condition!, right: { source: "literal", value } } }, effects); }} /></label>}
        <Button variant="ghost" size="sm" onClick={() => { setSteps(undefined, effects); setChosen("trigger"); }}><Trash2 />{t("Remove the condition")}</Button>
      </div>}
      {chosenStep && <WorkflowInspector step={chosenStep} steps={draft.steps} lanes={draft.lanes ?? []} capabilities={capabilities} flows={flowQuery.data ?? []}
        onChange={(patch) => setSteps(condition, effects.map((step) => step.name === chosen ? { ...step, ...patch } : step))}
        onRename={(name) => { if (/^[a-z][a-z0-9]*$/.test(name) && !draft.steps.some((step) => step.name === name)) { setSteps(condition, effects.map((step) => step.name === chosen ? { ...step, name } : step)); setChosen(name); } }}
        onMakeEntry={() => {}} onClose={() => setChosen("trigger")} />}
    </div> }}
    dock={{ label: t("Automation dock"), tabs: [
      { id: "problems", title: t("Problems"), badge: problems.length, content: <ProblemList problems={problems} empty={t("No problems. The automation can be published.")} /> },
      { id: "runs", title: t("Runs"), content: <div className="p-3">{draft.id ? <WorkflowRuns name={draft.name} versions={draft.versions} onStepSelect={(name) => setChosen(name)} /> : <p className="text-xs text-muted">{t("Runs appear once the automation is saved and published.")}</p>}</div> },
    ] }}>
    <div className="mx-auto grid w-full max-w-2xl content-start gap-2 p-4">
      {card("trigger", t("When"), <p className="text-sm">{draft.object && draft.when ? sentence((type) => objects.find((item) => item.type === type)?.title ?? type, (type, name) => objects.find((item) => item.type === type)?.states.find((state) => state.name === name)?.title ?? name, draft) : <span className="text-muted">{t("Choose the object type and the state that start it.")}</span>}</p>)}
      {condition ? card("condition", t("If"), <p className="text-sm">{conditionText || <span className="text-muted">{t("Choose a field to compare.")}</span>}</p>)
        : <Button variant="ghost" size="sm" className="w-fit" disabled={!draft.object} onClick={() => { setSteps({ name: CONDITION, kind: "branch", condition: { op: "eq", left: { source: "subject", path: [object?.fields[0]?.name ?? ""] }, right: { source: "literal", value: "" } } }, effects); setChosen("condition"); }}><Plus />{t("Add a condition")}</Button>}
      {effects.map((step, at) => card(step.name, at === 0 ? t("Then") : t("And then"), <p className="flex items-center gap-2 text-sm">{step.kind === "ai" ? <Brain className="size-4 text-muted" /> : <Zap className="size-4 text-muted" />}<span className="min-w-0 flex-1 truncate">{step.title || step.name}</span><span className="font-mono text-[11px] text-muted">{step.act ?? step.function?.name}</span></p>,
        <>
          <Button variant="ghost" size="sm" aria-label={t("Move up")} disabled={at === 0} onClick={(event) => { event.stopPropagation(); const next = [...effects]; [next[at - 1], next[at]] = [next[at]!, next[at - 1]!]; setSteps(condition, next); }}><ArrowUp /></Button>
          <Button variant="ghost" size="sm" aria-label={t("Move down")} disabled={at === effects.length - 1} onClick={(event) => { event.stopPropagation(); const next = [...effects]; [next[at + 1], next[at]] = [next[at]!, next[at + 1]!]; setSteps(condition, next); }}><ArrowDown /></Button>
          <Button variant="ghost" size="sm" aria-label={t("Remove")} onClick={(event) => { event.stopPropagation(); setSteps(condition, effects.filter((item) => item !== step)); if (chosen === step.name) setChosen("trigger"); }}><Trash2 /></Button>
        </>))}
      {draft.object && offered.length > 0 && <div className="w-fit"><ActionMenu label={t("Add an effect")} icon={<Plus />} commands={offered.map((capability) => ({ id: capabilityKey(capability), label: `${t(capability.title)} · ${capability.ref.app}`, icon: capability.kind === "ai" ? <Brain /> : <Zap />, run: () => addEffect(capability) }))}>{t("Add an effect")}</ActionMenu></div>}
      {draft.object && !offered.length && !catalogQuery.isLoading && <p className="text-xs text-muted">{t("No published action of this object type yet. Publish the object type's actions, then add them here.")}</p>}
    </div>
  </Workbench>;
}
