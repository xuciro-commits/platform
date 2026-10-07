// The action type workbench (ADR-0053 §6, built under ADR-0054): an action is a
// first-class resource with its own sections — Overview, Parameters, Form,
// Rules, Submission criteria, Side effects, Approval, Permissions. Left: the
// object's actions and this action's sections. Main: the section as a table or
// preview. Right: the row in hand. Dock: this action's problems. The action
// still lives in its object's record: the object draft session is shared.
import { PayloadFields, useHost, useRecordInventory } from "@platform/app";
import { Button, Card, Checkbox, DataTable, Input, PageHeader, Panel, PanelSection, ProblemList, Select, StructureRow, Textarea, Toggles, Workbench, cn, t, type EntityInfo, type WorkbenchProblem } from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { DraftStatus } from "../shared/workbench";
import { Boxes, CheckSquare, FileInput, ListChecks, Plus, Shield, Sparkles, Trash2, Wand2, Zap } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { PublishMenu, WorkbenchMessage, savingState } from "../shared/workbench";
import { assignmentInputFits } from "./process-rules";
import { Label } from "./process";
import { comparisonFits, conditionSubjects, inputTypes, nameOf, operators, type Action, type ApproverLevel, type Condition, type Create_, type Input_, type JournalLine_, type Post_, type Set_ } from "./object-model";
import { useObjectDraft } from "./object-draft";

type Section = "overview" | "parameters" | "form" | "rules" | "criteria" | "effects" | "approval" | "permissions";
type Pick = { kind: "parameter" | "rule" | "criterion" | "effect" | "posting"; at: number } | undefined;
const sections: { id: Section; title: () => string; icon: ReactNode }[] = [
  { id: "overview", title: () => t("Overview"), icon: <Zap /> },
  { id: "parameters", title: () => t("Parameters"), icon: <FileInput /> },
  { id: "form", title: () => t("Form"), icon: <ListChecks /> },
  { id: "rules", title: () => t("Rules"), icon: <Wand2 /> },
  { id: "criteria", title: () => t("Submission criteria"), icon: <CheckSquare /> },
  { id: "effects", title: () => t("Side effects"), icon: <Sparkles /> },
  { id: "approval", title: () => t("Approval"), icon: <Shield /> },
  { id: "permissions", title: () => t("Permissions"), icon: <Shield /> },
];

type ObjectDraft = { id: string; name: string; title: string; state: string; archived?: boolean; actions?: Action[]; states?: { name: string; title: string }[] };
type ActionRow = { key: string; object: ObjectDraft; action: Action };

/** Every action type of every object type this tenant drafts (Ontology Manager's Action types list): one row per action, opening its workbench. */
export function ActionTypes() {
  const { role } = useHost(), { open } = useApplicationWorkspace();
  const inventory = useRecordInventory<ObjectDraft>("build.object");
  if (role("build") !== "builder") return <PageHeader title={t("Action types")} description={t("Only a builder can edit action types.")} />;
  const rows: ActionRow[] = (inventory.data?.records ?? []).filter((object) => !object.archived).flatMap((object) => (object.actions ?? []).map((action) => ({ key: `${object.id}:${action.name}`, object, action })));
  const stateTitle = (object: ObjectDraft, name?: string) => object.states?.find((s) => s.name === name)?.title ?? name ?? "";
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={t("Action types")} description={t("An action type is how people change an object: its parameters, rules, criteria and side effects. Every action belongs to one object type; open the object type to add one.")} />
    <DataTable<ActionRow> data={rows} getRowId={(row) => row.key} height={520} loading={inventory.isLoading} empty={t("No action types yet. Add a lifecycle state and an action to an object type.")}
      onRowClick={(row) => open({ view: "action-type", params: { id: row.object.id, action: row.action.name } })}
      columns={[
        { id: "title", header: t("Action type"), accessorFn: (row) => row.action.title || row.action.name, meta: { width: 220, pin: "left" } },
        { id: "object", header: t("Object type"), accessorFn: (row) => row.object.title || row.object.name, meta: { width: 180 } },
        { id: "identity", header: t("Identity"), accessorFn: (row) => `build.${row.object.name}.${row.action.name}`, meta: { width: 260 } },
        { id: "transition", header: t("Transition"), accessorFn: (row) => `${row.action.from.map((f) => stateTitle(row.object, f)).join(", ")} → ${row.action.to ? stateTitle(row.object, row.action.to) : row.action.toInput ? t("From input") : t("Where it was")}`, meta: { width: 240 } },
        { id: "parameters", header: t("Parameters"), accessorFn: (row) => row.action.inputs?.length ?? 0, meta: { width: 100 } },
        { id: "approval", header: t("Approval"), accessorFn: (row) => row.action.approval ? t("Yes") : "", meta: { width: 90 } },
        { id: "state", header: t("Status"), accessorFn: (row) => row.object.state, cell: ({ row }) => <DraftStatus state={row.original.object.state} />, meta: { width: 110 } },
      ]} />
  </div>;
}

export function ActionTypeEditor({ id, action: initial }: { id: string; action?: string }) {
  const [pick, setPick] = useState<Pick>();
  const draft = useObjectDraft(id, () => setPick(undefined));
  const { object, process, dirty, busy, refused, change, parent, targets, entities, roles, approverRoles, session, discardChanges, open } = draft;
  const [name, setName] = useState(initial);
  const [section, setSection] = useState<Section>("overview");
  useEffect(() => { if (initial) setName(initial); }, [initial]);
  if (!object) return <Workbench storageKey="action-type" title={t("Action type")}><WorkbenchMessage>{draft.failed ? t("The object type could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const at = process.actions.findIndex((a) => a.name === name);
  const action = process.actions[at];
  const patch = (next: Partial<Action>) => { if (at >= 0) change({ ...process, actions: process.actions.map((a, i) => i === at ? { ...a, ...next } : a) }); };
  const addAction = () => {
    const fresh = nameOf("action", process.actions.map((a) => a.name)), from = process.states[0]?.name;
    change({ ...process, actions: [...process.actions, { name: fresh, title: t("Step {n}", { n: process.actions.length + 1 }), from: from ? [from] : [], to: process.states[1]?.name }] });
    setName(fresh); setSection("overview"); setPick(undefined);
  };
  const issues = action ? draft.issuesOf(action.name) : [];
  const problems: WorkbenchProblem[] = issues.map((text, i) => ({ id: `issue:${i}`, text }));
  const inputs = action?.inputs ?? [], sets = action?.sets ?? [], conditions = action?.conditions ?? [], creates = action?.creates ?? [], posts = action?.posts ?? [];
  const balances = entities.filter((e) => e.type.startsWith("build.") && e.type !== parent && e.fields.some((f) => !f.readOnly && ["integer", "decimal", "money"].includes(f.type)));
  const sources = [...inputs.map((i) => ({ value: i.name, label: t("Input: {name}", { name: i.title }) })), { value: "$me", label: t("The person taking it") }, { value: "$now", label: t("Now") }];
  const postSources = [...sources.filter((s) => s.value !== "$now"), ...process.fields.map((f) => ({ value: `record.${f.name}`, label: t("This record: {name}", { name: f.title || f.name }) }))];
  const subjects = action ? conditionSubjects(process, action, parent, entities) : [];
  const stateTitle = (s?: string) => process.states.find((x) => x.name === s)?.title ?? s ?? "";
  const go = (next: Section, picked?: Pick) => { setSection(next); setPick(picked); };
  const list = <T,>(items: T[], kind: NonNullable<Pick>["kind"], onChange: (next: T[]) => void, add: { label: string; make: () => T; disabled?: boolean }, row: (item: T, i: number) => ReactNode, empty: string) => <div className="grid content-start gap-2 p-4">
    <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{sections.find((s) => s.id === section)?.title()}</h2>
      <Button size="sm" disabled={add.disabled} onClick={() => { onChange([...items, add.make()]); setPick({ kind, at: items.length }); }}><Plus />{add.label}</Button></div>
    {items.length ? <ul className="divide-y divide-border rounded-md border border-border">{items.map((item, i) => <li key={i}>
      <Button variant="row" type="button" aria-pressed={pick?.kind === kind && pick.at === i} className={cn("flex w-full items-center gap-3 px-3 py-2 text-left text-sm hover:bg-row-hover", pick?.kind === kind && pick.at === i && "bg-row-selected")} onClick={() => setPick({ kind, at: i })}>{row(item, i)}</Button>
    </li>)}</ul> : <p className="text-sm text-muted">{empty}</p>}
  </div>;

  const main: Record<Section, ReactNode> = !action ? {} as Record<Section, ReactNode> : {
    overview: <div className="grid max-w-2xl content-start gap-3 p-4">
      <Label text={t("What people call it")}><Input value={action.title} onChange={(e) => patch({ title: e.target.value })} /></Label>
      <Label text={t("Name")}><Input value={action.name} className="font-mono" onChange={(e) => { patch({ name: e.target.value }); setName(e.target.value); }} /></Label>
      <Label text={t("What it does")}><Textarea rows={2} value={action.description ?? ""} onChange={(e) => patch({ description: e.target.value })} /></Label>
      <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Taken from")}</legend>
        <Toggles options={process.states.map((s) => ({ value: s.name, label: s.title }))} value={action.from} onChange={(from) => patch({ from })} /></fieldset>
      <Label text={t("Leaves it in")}>
        <Select value={action.to ?? ""} onChange={(e) => patch({ to: e.target.value || undefined, toInput: undefined })}>
          <option value="">{t("Where it was")}</option>{process.states.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}
        </Select></Label>
      <Label text={t("State from input")}><Select value={action.toInput ?? ""} disabled={!!action.approval && !action.toInput} onChange={(e) => patch({ toInput: e.target.value || undefined, to: undefined })}>
        <option value="">{t("Use a fixed state or keep the current state")}</option>
        {inputs.filter((i) => i.type === "choice" && i.required).map((i) => <option key={i.name} value={i.name}>{i.title}</option>)}
      </Select></Label>
      <dl className="grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
        {([["parameters", inputs.length], ["rules", sets.length], ["criteria", conditions.length], ["effects", creates.length]] as const).map(([s, n]) => <Button key={s} variant="row" type="button" className="rounded-md border border-border p-3 text-left hover:border-primary" onClick={() => go(s)}>
          <dt className="text-xs text-muted">{sections.find((x) => x.id === s)!.title()}</dt><dd className="text-lg font-semibold">{n}</dd></Button>)}
      </dl>
    </div>,
    parameters: list<Input_>(inputs, "parameter", (next) => patch({ inputs: next }), { label: t("Add a parameter"), make: () => ({ name: nameOf("input", inputs.map((i) => i.name)), title: t("Input"), type: "text" }) },
      (i) => <><span className="min-w-0 flex-1 truncate font-medium">{i.title}</span><code className="text-xs text-muted">{i.name}</code><span className="text-xs text-muted">{t(i.type)}{i.required ? " *" : ""}</span></>, t("No parameters. People take the action with no form.")),
    form: <div className="grid max-w-xl content-start gap-3 p-4">
      <p className="text-xs text-muted">{t("The form as people will see it. Nothing here is saved or taken.")}</p>
      <Card className="grid gap-2 p-3">
        <h3 className="text-sm font-semibold">{action.title}</h3>
        {action.description && <p className="text-sm text-muted">{action.description}</p>}
        <FormPreview inputs={inputs} />
        {conditions.length > 0 && <ul className="grid gap-1 text-xs text-muted">{conditions.map((c, i) => <li key={i}>{c.message || `${c.field} ${c.operator} ${c.value ?? c.valueField ?? ""}`}</li>)}</ul>}
        <Button variant="primary" size="sm" className="w-fit" disabled>{action.title}</Button>
      </Card>
    </div>,
    rules: list<Set_>(sets, "rule", (next) => patch({ sets: next }), { label: t("Set a field"), make: () => ({ field: process.fields[0]?.name ?? "", from: sources[0]?.value ?? "" }), disabled: !process.fields.length },
      (s) => <><span className="min-w-0 flex-1 truncate font-medium">{process.fields.find((f) => f.name === s.field)?.title ?? s.field}</span><span className="text-xs text-muted">← {s.from.startsWith("=") ? `"${s.from.slice(1)}"` : sources.find((x) => x.value === s.from)?.label ?? s.from}</span></>, t("No rules. The action changes only the state.")),
    criteria: list<Condition>(conditions, "criterion", (next) => patch({ conditions: next }), { label: t("Add a criterion"), make: () => ({ field: subjects[0]?.value ?? "", operator: "=", value: "", message: "" }) },
      (c) => <><span className="min-w-0 flex-1 truncate font-medium">{subjects.find((s) => s.value === c.field)?.label ?? c.field} {t(c.operator)} {c.valueField ? subjects.find((s) => s.value === c.valueField)?.label : c.value}</span><span className="truncate text-xs text-muted">{c.message}</span></>, t("No criteria. The action can always be submitted from its starting states.")),
    effects: <>{list<Create_>(creates, "effect", (next) => patch({ creates: next }), { label: t("Create a related record"), make: () => relatedCreate(targets[0]?.type ?? "", undefined, targets, parent, inputs), disabled: !targets.length },
      (c) => <><span className="min-w-0 flex-1 truncate font-medium">{t("Create {object}", { object: targets.find((x) => x.type === c.object)?.title ?? c.object })}</span><span className="text-xs text-muted">{t("via {field}", { field: c.via })}</span></>, targets.length ? t("No side effects. Related records can be created when the action commits.") : t("Publish a related object with a reference to this object first."))}
      <p className="px-4 text-xs text-muted">{t("Related records commit with this action. The target object's create permissions still apply.")}</p>
      {list<Post_>(posts, "posting", (next) => patch({ posts: next }), { label: t("Post to a balance"), make: () => ({ object: balances[0]?.type ?? "", match: [], field: balances[0]?.fields.find((f) => ["integer", "decimal", "money"].includes(f.type))?.name ?? "", amount: inputs[0]?.name ?? "=1" }), disabled: !balances.length },
        (p) => <><span className="min-w-0 flex-1 truncate font-medium">{t("{sign} {amount} to {object}.{field}", { sign: p.subtract ? "−" : "+", amount: p.amount.startsWith("=") ? p.amount.slice(1) : postSources.find((x) => x.value === p.amount)?.label ?? p.amount, object: balances.find((x) => x.type === p.object)?.title ?? p.object, field: p.field })}</span><span className="text-xs text-muted">{p.match.map((m) => m.field).join(", ")}</span></>, balances.length ? t("No postings. Quantities and amounts can accumulate on a balance object when the action commits.") : t("Publish an object with an integer, decimal or money field to keep balances."))}
      <p className="px-4 text-xs text-muted">{t("A balance is one record per distinct set of match values; it is created on the first posting. Reverse with a posting of the other sign.")}</p>
      <BooksEditor action={action} actions={process.actions} sources={postSources} patch={patch} /></>,
    approval: <div className="grid max-w-xl content-start gap-3 p-4 text-sm">
      <Checkbox checked={!!action.approval} disabled={!!action.toInput && !action.approval} onChange={(enabled) => patch({ approval: enabled ? { pending: process.states.find((s) => s.name !== action.from[0] && s.name !== action.to)?.name ?? "", levels: [{ title: t("Approver"), role: approverRoles[0] ?? "builder" }] } : undefined })}>{t("Wait for approval")}</Checkbox>
      {action.approval && <>
        <p className="text-xs text-muted">{t("Use one starting state. The approver decides in the work inbox; the action runs after the last approval.")}</p>
        <Label text={t("While it waits")}><Select value={action.approval.pending} onChange={(e) => patch({ approval: { ...action.approval!, pending: e.target.value } })}>
          <option value="">{t("Choose a state")}</option>{process.states.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}</Select></Label>
        <Label text={t("If rejected")}><Select value={action.approval.rejected ?? ""} onChange={(e) => patch({ approval: { ...action.approval!, rejected: e.target.value || undefined } })}>
          <option value="">{t("Return to the starting state")}</option>{process.states.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}</Select></Label>
        <Rows<ApproverLevel> title={t("Approver levels, in order")} add={t("Add a level")} items={action.approval.levels} make={() => ({ title: t("Approver"), role: approverRoles[0] ?? "builder" })}
          onChange={(levels) => patch({ approval: { ...action.approval!, levels } })}
          row={(level, set) => <>
            <Label text={t("What this level is called")}><Input value={level.title} onChange={(e) => set({ title: e.target.value })} /></Label>
            <Label text={t("Approver role")}><Select value={level.role} onChange={(e) => set({ role: e.target.value })}>{approverRoles.map((role) => <option key={role} value={role}>{role}</option>)}</Select></Label>
            <Checkbox checked={!!level.all} onChange={(all) => set({ all })}>{t("Everyone in the role must approve")}</Checkbox>
          </>} />
      </>}
    </div>,
    permissions: <div className="grid max-w-xl content-start gap-3 p-4 text-sm">
      {roles.length ? <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Taken by")}</legend>
        <Toggles options={roles.map((r) => ({ value: r, label: r }))} value={action.roles ?? []} onChange={(r) => patch({ roles: r.length ? r : undefined })} />
        <p className="text-muted">{t("None chosen: every role that may read the object. The builder always may.")}</p></fieldset>
        : <p className="text-muted">{t("The object has no roles yet; only builders may take this action.")}</p>}
      <Button size="sm" className="w-fit" onClick={() => open({ view: "object-type", params: { id, tab: "permissions" } })}><Shield />{t("Edit the object's roles")}</Button>
    </div>,
  };

  const inspector = action && pick ? <div className="p-2">
    {pick.kind === "parameter" && inputs[pick.at] && <RowCard title={t("Parameter")} onRemove={() => { patch({ inputs: inputs.filter((_, i) => i !== pick.at) }); setPick(undefined); }}>
      <ParameterEditor input={inputs[pick.at]!} entities={entities} onChange={(p) => patch({ inputs: inputs.map((x, i) => i === pick.at ? { ...x, ...p } : x) })} /></RowCard>}
    {pick.kind === "rule" && sets[pick.at] && <RowCard title={t("Rule")} onRemove={() => { patch({ sets: sets.filter((_, i) => i !== pick.at) }); setPick(undefined); }}>
      <AssignmentEditor fields={process.fields} sources={sources} set={sets[pick.at]!} onChange={(p) => patch({ sets: sets.map((x, i) => i === pick.at ? { ...x, ...p } : x) })} /></RowCard>}
    {pick.kind === "criterion" && conditions[pick.at] && <RowCard title={t("Submission criterion")} onRemove={() => { patch({ conditions: conditions.filter((_, i) => i !== pick.at) }); setPick(undefined); }}>
      <ConditionEditor condition={conditions[pick.at]!} subjects={subjects} onChange={(p) => patch({ conditions: conditions.map((x, i) => i === pick.at ? { ...x, ...p } : x) })} /></RowCard>}
    {pick.kind === "effect" && creates[pick.at] && <RowCard title={t("Side effect")} onRemove={() => { patch({ creates: creates.filter((_, i) => i !== pick.at) }); setPick(undefined); }}>
      <CreateEditor create={creates[pick.at]!} inputs={inputs} sources={sources} parent={parent} targets={targets} onChange={(next) => patch({ creates: creates.map((x, i) => i === pick.at ? next : x) })} /></RowCard>}
    {pick.kind === "posting" && posts[pick.at] && <RowCard title={t("Posting")} onRemove={() => { patch({ posts: posts.filter((_, i) => i !== pick.at) }); setPick(undefined); }}>
      <PostEditor post={posts[pick.at]!} sources={postSources} balances={balances} onChange={(next) => patch({ posts: posts.map((x, i) => i === pick.at ? next : x) })} /></RowCard>}
  </div> : <p className="p-3 text-xs text-muted">{t("Select a row to edit it here.")}</p>;

  return <Workbench storageKey="action-type" title={action?.title || t("Action type")}
    crumbs={[{ label: t("Object types"), onClick: () => open({ view: "object-type" }) }, { label: object.title, onClick: () => open({ view: "object-type", params: { id, tab: "actions" } }) }]}
    status={<DraftStatus state={object.state} problems={issues.length} />} saving={savingState(dirty, busy, refused)}
    history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: () => { session.undo(); setPick(undefined); }, redo: () => { session.redo(); setPick(undefined); } }}
    actions={<PublishMenu type="build.object" record={object} dirty={dirty} busy={busy} invalid={draft.issues.length > 0} onReview={() => void draft.perform(draft.review)} onInstall={() => void draft.perform(draft.publish)} onDiscard={discardChanges} route={{ view: "action-type", params: { id, action: name ?? "" } }} />}
    left={{ label: t("Action structure"), content: <div className="grid min-w-0 content-start">
      <PanelSection title={object.title} actions={<Button size="sm" variant="ghost" aria-label={t("Add an action")} title={t("Add an action")} disabled={!process.states.length} onClick={addAction}><Plus /></Button>}>
        <StructureRow icon={<Boxes />} label={t("Object type")} onClick={() => open({ view: "object-type", params: { id } })} />
        {process.actions.map((a) => <StructureRow key={a.name} depth={1} icon={<Zap />} label={a.title || a.name} meta={a.to ? `→ ${stateTitle(a.to)}` : undefined} selected={a.name === name} onClick={() => { setName(a.name); setPick(undefined); }} />)}
        {!process.actions.length && <p className="px-2 text-[11px] text-muted">{process.states.length ? t("No actions yet.") : t("Add a lifecycle state first: every action starts from a state.")}</p>}
      </PanelSection>
      {action && <PanelSection title={t("Sections")}>
        {sections.map((s) => <StructureRow key={s.id} icon={s.icon} label={s.title()} selected={section === s.id} onClick={() => go(s.id)}
          meta={s.id === "parameters" ? String(inputs.length) : s.id === "rules" ? String(sets.length) : s.id === "criteria" ? String(conditions.length) : s.id === "effects" ? String(creates.length) : s.id === "approval" && action.approval ? "✓" : undefined} />)}
      </PanelSection>}
    </div> }}
    right={{ label: t("Action inspector"), content: inspector }}
    dock={{ label: t("Action dock"), tabs: [{ id: "problems", title: t("Problems"), badge: issues.length, content: <ProblemList problems={problems} empty={t("No problems. The action can be published with its object type.")} /> }] }}>
    {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {refused}</Panel>}
    {action ? <fieldset disabled={busy} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto">{main[section]}</fieldset>
      : <WorkbenchMessage>{process.actions.length ? t("Choose an action on the left.") : t("This object type has no actions yet.")}</WorkbenchMessage>}
  </Workbench>;
}

function RowCard({ title, children, onRemove }: { title: string; children: ReactNode; onRemove: () => void }) {
  return <Card className="grid content-start gap-2 p-3">
    <div className="flex items-center justify-between"><span className="text-xs font-semibold text-muted">{title}</span><Button size="sm" variant="ghost" onClick={onRemove}><Trash2 className="size-3" />{t("Remove")}</Button></div>
    {children}
  </Card>;
}

function FormPreview({ inputs }: { inputs: Input_[] }) {
  const [values, setValues] = useState<Record<string, unknown>>({});
  return <PayloadFields preview values={values} onChange={setValues} fields={inputs.map((i) => ({
    name: i.name, type: i.type === "integer" ? "integer" : i.type === "decimal" ? "number" : i.type === "date" ? "date" : i.type === "boolean" ? "boolean" : "string",
    required: i.required, description: i.title, choices: i.type === "choice" ? (i.choices ?? "").split(",").map((c) => c.trim()).filter(Boolean) : undefined,
  }))} />;
}

function ParameterEditor({ input, entities, onChange }: { input: Input_; entities: EntityInfo[]; onChange: (patch: Partial<Input_>) => void }) {
  return <>
    <Label text={t("Label")}><Input value={input.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
    <Label text={t("Name")}><Input className="font-mono" value={input.name} onChange={(e) => onChange({ name: e.target.value })} /></Label>
    <Label text={t("Type")}><Select value={input.type} onChange={(e) => onChange({ type: e.target.value, ref: undefined, choices: undefined, minLength: undefined })}>{inputTypes.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select></Label>
    {input.type === "choice" && <Label text={t("Choices")}><Input placeholder="a, b, c" value={input.choices ?? ""} onChange={(e) => onChange({ choices: e.target.value })} /></Label>}
    {input.type === "reference" && <Label text={t("Reference object")}><Select value={input.ref ?? ""} onChange={(e) => onChange({ ref: e.target.value || undefined })}><option value="">{t("Choose an object")}</option>{entities.map((e) => <option key={e.type} value={e.type}>{e.title} · {e.type}</option>)}</Select></Label>}
    {["text", "longtext"].includes(input.type) && <Label text={t("Minimum length")}><Input type="number" min={0} max={4096} value={input.minLength ?? ""} onChange={(e) => onChange({ minLength: e.target.value === "" ? undefined : e.target.valueAsNumber })} /></Label>}
    <Checkbox className="text-xs" checked={!!input.required} onChange={(required) => onChange({ required })}>{t("Required")}</Checkbox>
  </>;
}

function AssignmentEditor({ fields, sources, set, onChange }: { fields: { name: string; title: string }[]; sources: { value: string; label: string }[]; set: Set_; onChange: (patch: Partial<Set_>) => void }) {
  return <>
    <Label text={t("Field")}><Select value={set.field} onChange={(event) => onChange({ field: event.target.value })}>
      <option value="">{t("Choose a field")}</option>{fields.map((field) => <option key={field.name} value={field.name}>{field.title}</option>)}</Select></Label>
    <Label text={t("From")}><Select value={set.from.startsWith("=") ? "=" : set.from} onChange={(event) => onChange({ from: event.target.value })}>
      <option value="">{t("Choose a source")}</option>{sources.map((source) => <option key={source.value} value={source.value}>{source.label}</option>)}<option value="=">{t("A fixed value")}</option></Select></Label>
    {set.from.startsWith("=") && <Label text={t("Value")}><Input value={set.from.slice(1)} onChange={(event) => onChange({ from: `=${event.target.value}` })} /></Label>}
  </>;
}

type Subject = { value: string; label: string; type: string; ref?: string };
function ConditionEditor({ condition: c, subjects, onChange: patch, guard = false }: { condition: Condition; subjects: Subject[]; onChange: (patch: Partial<Condition>) => void; guard?: boolean }) {
  const current = subjects.find((s) => s.value === c.field) ?? { type: "" };
  return <>
    <Label text={t("Field")}><Select value={c.field} onChange={(e) => patch({ field: e.target.value, valueField: undefined })}>{subjects.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}</Select></Label>
    <Label text={t("Operator")}><Select value={c.operator} onChange={(e) => patch({ operator: e.target.value, ...(e.target.value.includes("empty") ? { value: undefined, valueField: undefined } : {}) })}>{operators.map((o) => <option key={o} value={o}>{t(o)}</option>)}</Select></Label>
    {c.operator !== "empty" && c.operator !== "not empty" && <>
      <Label text={t("Compare with")}><Select value={c.valueField ? "field" : "literal"} onChange={(e) => patch(e.target.value === "field"
        ? { value: undefined, valueField: subjects.find((s) => s.value !== c.field && comparisonFits(current, s))?.value ?? c.field } : { value: "", valueField: undefined })}>
        <option value="literal">{t("A fixed value")}</option><option value="field">{t("Another field")}</option></Select></Label>
      {c.valueField ? <Label text={t("Comparison field")}><Select value={c.valueField} onChange={(e) => patch({ valueField: e.target.value })}>
        {subjects.filter((s) => comparisonFits(current, s)).map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}</Select></Label>
        : <Label text={t("Value")}><Input placeholder={t("a value, or $me")} value={c.value ?? ""} onChange={(e) => patch({ value: e.target.value })} /></Label>}
    </>}
    {!guard && <>
      <Label text={t("Message when it does not hold")}><Input placeholder={t("What a person reads when it does not hold")} value={c.message} onChange={(e) => patch({ message: e.target.value })} /></Label>
      <Checkbox checked={!!c.when} onChange={(on) => patch({ when: on ? { field: subjects[0]?.value ?? "", operator: "=", value: "", message: "" } : undefined })}>{t("Only when")}</Checkbox>
      {c.when && <div role="group" aria-label={t("Condition guard")} className="grid gap-1 rounded border border-border p-2">
        <ConditionEditor guard condition={c.when} subjects={subjects} onChange={(p) => patch({ when: { ...c.when!, ...p } })} /></div>}
    </>}
  </>;
}

function relatedCreate(object: string, via: string | undefined, targets: EntityInfo[], parent: string, inputs: Input_[]): Create_ {
  const target = targets.find((item) => item.type === object);
  const reference = via ?? target?.fields.find((field) => field.type === "reference" && field.ref === parent)?.name ?? "";
  return { object, via: reference, sets: target?.fields.filter((field) => field.required && !field.readOnly && field.name !== reference)
    .map((field) => ({ field: field.name, from: inputs.find((input) => input.name === field.name && assignmentInputFits(input, field, true))?.name ?? "" })) ?? [] };
}

function CreateEditor({ create, inputs, sources, parent, targets, onChange }: { create: Create_; inputs: Input_[]; sources: { value: string; label: string }[]; parent: string; targets: EntityInfo[]; onChange: (next: Create_) => void }) {
  const target = targets.find((item) => item.type === create.object);
  const references = target?.fields.filter((field) => field.type === "reference" && field.ref === parent) ?? [];
  const fields = (target?.fields.filter((field) => !field.readOnly && field.name !== create.via) ?? []).map((field) => ({ ...field, title: `${field.title}${field.required ? " *" : ""}` }));
  const sets = create.sets ?? [];
  return <>
    <Label text={t("Related object")}><Select value={create.object} onChange={(event) => onChange(relatedCreate(event.target.value, undefined, targets, parent, inputs))}>
      <option value="">{t("Choose an object")}</option>{targets.map((item) => <option key={item.type} value={item.type}>{item.title} · {item.type}</option>)}</Select></Label>
    <Label text={t("Parent reference")}><Select value={create.via} onChange={(event) => onChange(relatedCreate(create.object, event.target.value, targets, parent, inputs))}>
      <option value="">{t("Choose a field")}</option>{references.map((field) => <option key={field.name} value={field.name}>{field.title} · {field.name}</option>)}</Select></Label>
    <p className="text-xs text-muted">{t("The parent reference is filled automatically. Required target fields need a mapping.")}</p>
    <Rows<Set_> title={t("Fields of the new record")} add={t("Map a field")} items={sets} make={() => ({ field: fields[0]?.name ?? "", from: sources[0]?.value ?? "" })} onChange={(next) => onChange({ ...create, sets: next })}
      row={(set, patch) => <AssignmentEditor fields={fields} sources={sources} set={set} onChange={patch} />} />
  </>;
}

function PostEditor({ post, sources, balances, onChange }: { post: Post_; sources: { value: string; label: string }[]; balances: EntityInfo[]; onChange: (next: Post_) => void }) {
  const balance = balances.find((item) => item.type === post.object);
  const numeric = balance?.fields.filter((f) => !f.readOnly && ["integer", "decimal", "money"].includes(f.type)) ?? [];
  const keys = balance?.fields.filter((f) => !f.readOnly && f.name !== post.field) ?? [];
  return <>
    <Label text={t("Balance object")}><Select value={post.object} onChange={(event) => { const next = balances.find((item) => item.type === event.target.value); onChange({ ...post, object: event.target.value, match: [], field: next?.fields.find((f) => ["integer", "decimal", "money"].includes(f.type))?.name ?? "" }); }}>
      <option value="">{t("Choose an object")}</option>{balances.map((item) => <option key={item.type} value={item.type}>{item.title} · {item.type}</option>)}</Select></Label>
    <Label text={t("Balance field")}><Select value={post.field} onChange={(event) => onChange({ ...post, field: event.target.value })}>
      <option value="">{t("Choose a field")}</option>{numeric.map((field) => <option key={field.name} value={field.name}>{field.title} · {field.type}</option>)}</Select></Label>
    <Label text={t("Amount")}><Select value={post.amount.startsWith("=") ? "=" : post.amount} onChange={(event) => onChange({ ...post, amount: event.target.value })}>
      <option value="">{t("Choose a source")}</option>{sources.filter((s) => s.value !== "$me").map((source) => <option key={source.value} value={source.value}>{source.label}</option>)}<option value="=">{t("A fixed value")}</option></Select></Label>
    {post.amount.startsWith("=") && <Label text={t("Value")}><Input value={post.amount.slice(1)} onChange={(event) => onChange({ ...post, amount: `=${event.target.value}` })} /></Label>}
    <Checkbox checked={!!post.subtract} onChange={(subtract) => onChange({ ...post, subtract })}>{t("Subtract instead of add")}</Checkbox>
    <Checkbox checked={!!post.floor} onChange={(floor) => onChange({ ...post, floor })}>{t("Refuse when the balance would fall below zero")}</Checkbox>
    <Rows<Set_> title={t("Balance identified by")} add={t("Add a match field")} items={post.match} make={() => ({ field: keys[0]?.name ?? "", from: sources[0]?.value ?? "" })} onChange={(next) => onChange({ ...post, match: next })}
      row={(set, patch) => <AssignmentEditor fields={keys} sources={sources} set={set} onChange={patch} />} />
  </>;
}

/** What the action books (ADR-0076): a balanced journal entry the host lands in the books through core, or the reversal of another action's entry and postings. */
function BooksEditor({ action, actions, sources, patch }: { action: Action; actions: Action[]; sources: { value: string; label: string }[]; patch: (next: Partial<Action>) => void }) {
  const journal = action.journal, others = actions.filter((a) => a.name !== action.name && (a.journal || a.posts?.length));
  const amountSources = sources.filter((s) => s.value !== "$me");
  const pick = (value: string | undefined, onChange: (next: string | undefined) => void, fixed: string) => <div className="flex gap-1">
    <Select value={value?.startsWith("=") ? "=" : value ?? ""} onChange={(e) => onChange(e.target.value || undefined)}>
      <option value="">{t("Nothing")}</option>{amountSources.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}<option value="=">{fixed}</option></Select>
    {value?.startsWith("=") && <Input value={value.slice(1)} onChange={(e) => onChange(`=${e.target.value}`)} />}
  </div>;
  return <div className="grid gap-2 border-t border-border p-4 text-sm">
    <h3 className="text-xs font-semibold text-muted">{t("Books")}</h3>
    <Label text={t("Reverses action")}><Select value={action.reverses ?? ""} disabled={!!journal || !!action.posts?.length} onChange={(e) => patch({ reverses: e.target.value || undefined })}>
      <option value="">{t("Nothing: this action books its own entry, if any")}</option>{others.map((a) => <option key={a.name} value={a.name}>{a.title}</option>)}</Select></Label>
    {!action.reverses && <Checkbox checked={!!journal} onChange={(on) => patch({ journal: on ? { lines: [{ account: "=", debit: amountSources[0]?.value }, { account: "=", credit: amountSources[0]?.value }] } : undefined })}>{t("Book a journal entry when it commits")}</Checkbox>}
    {journal && <>
      <p className="text-xs text-muted">{t("Debits and credits must balance. The entry lands in the fiscal period of its date, or the next open one; it waits while none is open, and the accountant can only reverse it.")}</p>
      <Label text={t("Posting date")}><Select value={journal.date ?? ""} onChange={(e) => patch({ journal: { ...journal, date: e.target.value || undefined } })}>
        <option value="">{t("When the action is taken")}</option>{sources.filter((s) => s.value.startsWith("record.") || s.value.startsWith("$") === false).map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}</Select></Label>
      <Label text={t("Entry text")}><Input value={journal.text?.startsWith("=") ? journal.text.slice(1) : journal.text ?? ""} placeholder={action.title} onChange={(e) => patch({ journal: { ...journal, text: e.target.value ? `=${e.target.value}` : undefined } })} /></Label>
      <Rows<JournalLine_> title={t("Lines")} add={t("Add a line")} items={journal.lines} make={() => ({ account: "=" })} onChange={(lines) => patch({ journal: { ...journal, lines } })}
        row={(line, set) => <>
          <Label text={t("Account")}>{pick(line.account, (account) => set({ account: account ?? "=" }), t("Account code"))}</Label>
          <Label text={t("Debit")}>{pick(line.debit, (debit) => set({ debit, credit: debit ? undefined : line.credit }), t("A fixed amount"))}</Label>
          <Label text={t("Credit")}>{pick(line.credit, (credit) => set({ credit, debit: credit ? undefined : line.debit }), t("A fixed amount"))}</Label>
          <Label text={t("Partner")}>{pick(line.partner, (partner) => set({ partner }), t("A name"))}</Label>
          <Label text={t("About")}>{pick(line.object, (object) => set({ object }), t("A reference"))}</Label>
        </>} />
    </>}
  </div>;
}

/** A small list of rows to add to, change and remove, for the nested lists (approver levels, mapped fields). */
function Rows<T>({ title, add, items, make, onChange, row }: { title: string; add: string; items: T[]; make: () => T; onChange: (next: T[]) => void; row: (item: T, set: (patch: Partial<T>) => void) => ReactNode }) {
  return <fieldset className="grid gap-1 border-t border-border pt-2 text-xs"><legend className="mb-1 font-semibold text-muted">{title}</legend>
    {items.map((item, i) => <div key={i} className="grid gap-1 rounded-md border border-border p-2">
      {row(item, (patch) => onChange(items.map((x, at) => at === i ? { ...x, ...patch } : x)))}
      <Button size="sm" variant="ghost" className="justify-self-end" onClick={() => onChange(items.filter((_, at) => at !== i))}><Trash2 className="size-3" />{t("Remove")}</Button>
    </div>)}
    <Button size="sm" className="justify-self-start" onClick={() => onChange([...items, make()])}><Plus className="size-3" />{add}</Button>
  </fieldset>;
}

