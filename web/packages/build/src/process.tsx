// The object's process editor (ADR-0037): its states and the actions people
// take on its records, in the page editor's three panes — what there is on the
// left, what a person will see in the middle, the piece in hand on the right.
// It writes the object's own record through its own action; the host checks
// everything again when the object is published.
import { PayloadFields, useHost, useReadQuery } from "@platform/app";
import {
  Button, Card, Checkbox, Disclosure, Input, NodeCanvas, PageHeader, Panel, RecordList, Select, StatusBar, StatusTag, Textarea, Toggles, canvasNodeHeight, cn, defineStatuses, layout, notify, t, useWorkspace,
  type CanvasEdge, type CanvasNode, type NodeCatalog,
} from "@platform/ui";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

type Field = { name: string; title: string; type: string; choices?: string; required?: boolean; search?: boolean; ref?: string; read?: string[]; write?: string[] };
type State = { name: string; title: string; tone?: string; description?: string };
type Input_ = { name: string; title: string; type: string; choices?: string; required?: boolean };
type Set_ = { field: string; from: string };
type Condition = { field: string; operator: string; value?: string; message: string };
type ApproverLevel = { title: string; role: string; all?: boolean };
type Approval = { pending: string; rejected?: string; levels: ApproverLevel[] };
type Action = { name: string; title: string; description?: string; from: string[]; to?: string; inputs?: Input_[]; sets?: Set_[]; conditions?: Condition[]; roles?: string[]; approval?: Approval };
/** What one role of the builder app may do with the object (ADR-0037 18b). */
type Access = { role: string; read: "all" | "own" | "none"; create?: boolean; edit?: boolean; archive?: boolean };
type ObjectRecord = { id: string; revision: number; name: string; title: string; state: string; fields: Field[]; states?: State[]; actions?: Action[]; access?: Access[] };
type Process = { states: State[]; actions: Action[]; access: Access[]; fields: Field[] };
/** What is in hand: a state, an action, or who may do what, by its place. */
type Chosen = { kind: "field" | "state" | "action" | "access"; at: number } | undefined;

const objectStates = defineStatuses({ draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" } });
const tones = ["info", "success", "warning", "danger", "neutral"];
const inputTypes = ["text", "longtext", "integer", "decimal", "date", "boolean", "choice"];
const fieldTypes = ["text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference"];
const operators = ["=", "!=", "<", "<=", ">", ">=", "empty", "not empty"];
const inputFits = (input: string, field: string) => field === "text" || field === "longtext"
  ? ["text", "longtext", "choice"].includes(input)
  : field === "decimal" ? ["decimal", "integer"].includes(input) : input === field;
const valueFits = (kind: string, raw: string) => {
  switch (kind) {
    case "integer": return /^-?\d+$/.test(raw);
    case "decimal": return raw.trim() !== "" && Number.isFinite(Number(raw));
    case "date": return /^\d{4}-\d{2}-\d{2}$/.test(raw) && !Number.isNaN(Date.parse(raw));
    case "datetime": return !Number.isNaN(Date.parse(raw));
    case "boolean": return raw === "true" || raw === "false";
    default: return true;
  }
};
/** Early authoring hints; the host's publication check is authoritative. */
function actionIssues(action: Action, process: Process): string[] {
  const issues: string[] = [];
  if (action.from.length === 0) issues.push(t("Choose at least one starting state."));
  for (const set of action.sets ?? []) {
    const field = process.fields.find((f) => f.name === set.field);
    const input = action.inputs?.find((i) => i.name === set.from);
    if (field && input && !inputFits(input.type, field.type)) issues.push(t("{field} needs {type}; {input} is {inputType}.", {
      field: field.title, type: t(field.type), input: input.title, inputType: t(input.type),
    }));
    if (field?.type === "choice" && input?.type === "choice") {
      const allowed = (field.choices ?? "").split(",").map((x) => x.trim());
      if ((input.choices ?? "").split(",").map((x) => x.trim()).some((x) => x && !allowed.includes(x)))
        issues.push(t("{input} has a choice outside {field}.", { input: input.title, field: field.title }));
    }
  }
  for (const condition of action.conditions ?? []) {
    const field = process.fields.find((f) => f.name === condition.field);
    const input = action.inputs?.find((i) => `input.${i.name}` === condition.field);
    const kind = condition.field === "state" ? "choice" : field?.type ?? input?.type;
    if (!condition.message.trim()) issues.push(t("Write the message people see when a rule fails."));
    if (condition.operator === "empty" || condition.operator === "not empty" || !kind) continue;
    if (kind === "money") { issues.push(t("Money rules need a currency-aware comparison.")); continue; }
    if (!["=", "!="].includes(condition.operator) && !["integer", "decimal", "date", "datetime"].includes(kind))
      issues.push(t("{type} cannot be ordered.", { type: t(kind) }));
    if (condition.value === "$me" ? !["text", "longtext"].includes(kind) : !condition.value || !valueFits(kind, condition.value))
      issues.push(t("{field} needs a {type} value.", { field: field?.title ?? input?.title ?? t("State"), type: t(kind) }));
  }
  return issues;
}
/** A name from what people call it: lower-case letters and digits (the host's rule). */
const nameOf = (title: string, taken: string[]) => {
  const base = title.toLowerCase().replace(/[^a-z0-9]/g, "").replace(/^[0-9]+/, "") || "step";
  let name = base, n = 2;
  while (taken.includes(name)) name = `${base}${n++}`;
  return name;
};

/** The objects of this organisation: open one to give it states and actions. */
export function ProcessPicker() {
  const { source } = useHost();
  const { open } = useWorkspace();
  return (
    <div className="grid gap-3">
      <PageHeader title={t("Process and access")} description={t("Choose an object to give its records states, the steps people take on them, and who may do what.")} />
      <RecordList source={source} type="build.object" fields={["title", "name", "state"]} onOpen={(record) => open({ view: "process", params: { id: record.id } })} />
    </div>
  );
}

export function ProcessEditor({ id }: { id: string }) {
  const { decide } = useHost();
  const { open } = useWorkspace();
  const read = useReadQuery<{ record?: ObjectRecord }>(`/v1/records/${encodeURIComponent("build.object")}/${encodeURIComponent(id)}`).data;
  const object = read?.record;
  const [process, setProcess] = useState<Process>({ states: [], actions: [], access: [], fields: [] });
  const [chosen, setChosen] = useState<Chosen>();
  const [center, setCenter] = useState<"map" | "preview">("map");
  const [dirty, setDirty] = useState(false);
  const [refused, setRefused] = useState<string>(); // why the host refused, kept on screen
  useEffect(() => {
    if (object && !dirty) setProcess({ states: object.states ?? [], actions: object.actions ?? [], access: object.access ?? [], fields: object.fields ?? [] });
  }, [object, dirty]);
  if (!object) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const change = (next: Process) => { setProcess(next); setDirty(true); };
  const addState = () => {
    const title = process.states.length === 0 ? t("New") : t("State {n}", { n: process.states.length + 1 });
    change({ ...process, states: [...process.states, { name: nameOf("state", process.states.map((s) => s.name)), title, tone: "info" }] });
    setChosen({ kind: "state", at: process.states.length });
  };
  const addAction = () => {
    const title = t("Step {n}", { n: process.actions.length + 1 });
    const from = process.states[0]?.name;
    change({ ...process, actions: [...process.actions, { name: nameOf("action", process.actions.map((a) => a.name)), title, from: from ? [from] : [], to: process.states[1]?.name }] });
    setChosen({ kind: "action", at: process.actions.length });
  };
  const save = async () => {
    setRefused(undefined);
    const ok = await decide("build.object.edit", { type: "build.object", id }, process, { expectedRevision: object.revision, onRefused: setRefused });
    if (ok) setDirty(false);
    return ok;
  };
  const publish = async () => {
    setRefused(undefined);
    if (dirty && !(await save())) return; // what is published is what was saved
    if (await decide("build.object.publish", { type: "build.object", id }, {}, { onRefused: setRefused })) notify.success(t("The object is installed with its states and actions."));
  };
  const action = chosen?.kind === "action" ? process.actions[chosen.at] : undefined;
  const state = chosen?.kind === "state" ? process.states[chosen.at] : undefined;
  const field = chosen?.kind === "field" ? process.fields[chosen.at] : undefined;
  const issues = process.actions.flatMap((a) => actionIssues(a, process).map((message) => `${a.title || a.name}: ${message}`));
  return (
    <div className="flex flex-col gap-3 lg:h-[calc(100dvh-8rem)] lg:min-h-0">
      <PageHeader title={t("Design {object}", { object: object.title })}
        description={t("Define its fields, states, actions and access. Save keeps your work; publish installs it.")}
        actions={<div className="flex items-center gap-2">
          <StatusTag status={object.state} registry={objectStates} />
          <Button onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: "objects" } })}>{t("Back to objects")}</Button>
          <Button onClick={() => void save()} disabled={!dirty}>{t("Save")}</Button>
          <Button variant="primary" onClick={() => void publish()}>{t("Publish")}</Button>
        </div>} />
      {refused && <Panel role="alert" className="text-sm text-[var(--tone-danger)]">{t("The host refused it:")} {refused}</Panel>}
      {issues.length > 0 && <Panel role="alert" className="text-xs text-[var(--tone-danger)]"><strong>{t("Check these rules before publishing:")}</strong>
        <ul className="ml-4 list-disc">{issues.map((message, i) => <li key={i}>{message}</li>)}</ul></Panel>}
      {dirty && <Panel role="status" className="text-xs text-muted">{t("Not saved yet. Publishing saves first.")}</Panel>}
      <div className="grid gap-3 lg:min-h-0 lg:flex-1 lg:grid-cols-[15rem_minmax(0,1fr)_21rem]">
        <div role="region" aria-label={t("States and actions")} className="lg:min-h-0 lg:overflow-y-auto">
          <FieldsOutline fields={process.fields} chosen={chosen} onChoose={setChosen}
            onAdd={() => { change({ ...process, fields: [...process.fields, { name: nameOf("field", process.fields.map((f) => f.name)), title: t("Field"), type: "text" }] }); setChosen({ kind: "field", at: process.fields.length }); }} />
          <Outline process={process} chosen={chosen} onChoose={setChosen} onAddState={addState} onAddAction={addAction} onChange={change} />
          <AccessOutline access={process.access} chosen={chosen} onChoose={setChosen}
            onAdd={() => { change({ ...process, access: [...process.access, { role: nameOf("role", process.access.map((a) => a.role)), read: "own", create: true, edit: true }] }); setChosen({ kind: "access", at: process.access.length }); }}
            onRemove={(i) => { change({ ...process, access: process.access.filter((_, at) => at !== i) }); setChosen(undefined); }} />
        </div>
        <div role="region" aria-label={t("What people see")} className="min-w-0 rounded-md border border-dashed border-border p-3 lg:min-h-0 lg:overflow-y-auto">
          <div role="tablist" aria-label={t("Object view")} className="mb-3 flex gap-1 border-b border-border pb-2">
            <Button role="tab" aria-selected={center === "map"} size="sm" variant={center === "map" ? "primary" : "ghost"} onClick={() => setCenter("map")}>{t("Process map")}</Button>
            <Button role="tab" aria-selected={center === "preview"} size="sm" variant={center === "preview" ? "primary" : "ghost"} onClick={() => setCenter("preview")}>{t("Record preview")}</Button>
          </div>
          {center === "map" ? <ProcessGraph key={id} process={process} chosen={chosen} onChoose={setChosen} onChange={change} onAddState={addState} onAddAction={addAction} />
            : <Preview object={object} process={process} action={action} />}
        </div>
        <div role="region" aria-label={t("The piece in hand")} className="lg:min-h-0 lg:overflow-y-auto">
          {field && chosen && <FieldProperties field={field} onChange={(patch) => change({ ...process, fields: process.fields.map((f, i) => i === chosen.at ? { ...f, ...patch } : f) })} />}
          {state && chosen && <StateProperties state={state} onChange={(patch) => change({ ...process, states: process.states.map((s, i) => i === chosen.at ? { ...s, ...patch } : s) })} />}
          {chosen?.kind === "access" && process.access[chosen.at] && <AccessProperties access={process.access[chosen.at]!} fields={process.fields}
            onChange={(patch) => change({ ...process, access: process.access.map((a, i) => i === chosen.at ? { ...a, ...patch } : a) })}
            onFields={(fields) => change({ ...process, fields })} />}
          {action && chosen && <ActionProperties action={action} states={process.states} fields={process.fields} roles={process.access.map((a) => a.role).filter((r) => process.access.find((x) => x.role === r)?.read !== "none")} approverRoles={["builder", ...(process.access.length ? process.access.filter((a) => a.read === "all").map((a) => a.role) : ["user"])]}
            onChange={(patch) => change({ ...process, actions: process.actions.map((a, i) => i === chosen.at ? { ...a, ...patch } : a) })} />}
          {!chosen && <Card className="p-3 text-xs text-muted">{t("Choose a field, state or action to configure it.")}</Card>}
        </div>
      </div>
    </div>
  );
}

/** The lifecycle is the semantic source. Canvas edges only edit its From/To declarations. */
function ProcessGraph({ process, chosen, onChoose, onChange, onAddState, onAddAction }: {
  process: Process; chosen: Chosen; onChoose: (c: Chosen) => void; onChange: (p: Process) => void;
  onAddState: () => void; onAddAction: () => void;
}) {
  const catalog: NodeCatalog = [
    { id: "state", title: t("State"), category: "lifecycle", description: t("A record's named status"),
      inputs: [{ id: "result", label: t("Arrives here"), type: "action-result" }],
      outputs: [{ id: "take", label: t("May take"), type: "action-start" }] },
    { id: "action", title: t("Action"), category: "lifecycle", description: t("A governed step on one record"),
      inputs: [{ id: "from", label: t("Taken from"), type: "action-start" }],
      outputs: [{ id: "to", label: t("Leaves it in"), type: "action-result", limit: 1 }] },
  ];
  const links = process.actions.flatMap((a) => [
    ...a.from.map((s) => ({ from: `state:${s}`, to: `action:${a.name}` })),
    ...(a.to ? [{ from: `action:${a.name}`, to: `state:${a.to}` }] : []),
  ]);
  const places = layout([
    ...process.states.map((s) => ({ id: `state:${s.name}`, label: s.title })),
    ...process.actions.map((a) => ({ id: `action:${a.name}`, label: a.title })),
  ], links, "right", { width: 160, height: Math.max(...catalog.map(canvasNodeHeight)), gapX: 40, gapY: 30 });
  const nodes: CanvasNode[] = [
    ...process.states.map((s) => ({ id: `state:${s.name}`, kind: "state", label: s.title || s.name, detail: s.name, position: places.get(`state:${s.name}`) ?? { x: 0, y: 0 } })),
    ...process.actions.map((a) => ({ id: `action:${a.name}`, kind: "action", label: a.title || a.name, detail: a.name, position: places.get(`action:${a.name}`) ?? { x: 0, y: 0 } })),
  ];
  const edges: CanvasEdge[] = process.actions.flatMap((a) => [
    ...a.from.map((s) => ({ id: `from:${a.name}:${s}`, source: `state:${s}`, sourcePort: "take", target: `action:${a.name}`, targetPort: "from" })),
    ...(a.to ? [{ id: `to:${a.name}:${a.to}`, source: `action:${a.name}`, sourcePort: "to", target: `state:${a.to}`, targetPort: "result" }] : []),
  ]);
  const selected = chosen?.kind === "state" ? `state:${process.states[chosen.at]?.name}` : chosen?.kind === "action" ? `action:${process.actions[chosen.at]?.name}` : undefined;
  return <div className="mb-4 grid gap-2">
    <div><h3 className="text-sm font-semibold">{t("Process map")}</h3>
      <p className="text-xs text-muted">{t("Connect a state to an action to allow it; connect an action to a state for its result. Select a node to edit it. Delete a selected line to remove it.")}</p></div>
    <NodeCanvas label={t("Process map")} catalog={catalog} nodes={nodes} edges={edges} selected={selected}
      onAdd={(kind) => { if (kind === "state") onAddState(); else if (process.states.length) onAddAction(); }}
      onSelect={(id) => {
        const [kind, name] = id.split(":");
        const at = kind === "state" ? process.states.findIndex((s) => s.name === name) : process.actions.findIndex((a) => a.name === name);
        if (at >= 0) onChoose({ kind: kind as "state" | "action", at });
      }}
      onConnect={(c) => {
        if (c.source?.startsWith("state:") && c.target?.startsWith("action:")) {
          const state = c.source.slice(6), name = c.target.slice(7);
          onChange({ ...process, actions: process.actions.map((a) => a.name === name ? { ...a, from: [...a.from, state] } : a) });
        } else if (c.source?.startsWith("action:") && c.target?.startsWith("state:")) {
          const name = c.source.slice(7), state = c.target.slice(6);
          onChange({ ...process, actions: process.actions.map((a) => a.name === name ? { ...a, to: state } : a) });
        }
      }}
      onDisconnect={(removed) => {
        onChange({ ...process, actions: process.actions.map((a) => ({ ...a,
          from: a.from.filter((s) => !removed.some((e) => e.source === `state:${s}` && e.target === `action:${a.name}`)),
          to: removed.some((e) => e.source === `action:${a.name}` && e.target === `state:${a.to}`) ? undefined : a.to,
        })) });
      }} />
  </div>;
}

function FieldsOutline({ fields, chosen, onChoose, onAdd }: { fields: Field[]; chosen: Chosen; onChoose: (c: Chosen) => void; onAdd: () => void }) {
  return <Card className="mb-3 grid gap-2 p-3">
    <div className="flex items-center justify-between text-xs font-semibold text-muted">{t("Fields")}
      <Button size="sm" variant="ghost" onClick={onAdd}><Plus className="size-3" />{t("Add a field")}</Button></div>
    <ul className="grid gap-1">{fields.map((f, at) => <li key={`${f.name}:${at}`}>
      <Button size="sm" variant="ghost" className={cn("w-full justify-start", chosen?.kind === "field" && chosen.at === at && "bg-row-selected")}
        aria-pressed={chosen?.kind === "field" && chosen.at === at} onClick={() => onChoose({ kind: "field", at })}>{f.title || f.name} <span className="text-muted">· {t(f.type)}</span></Button>
    </li>)}</ul>
  </Card>;
}

function FieldProperties({ field, onChange }: { field: Field; onChange: (patch: Partial<Field>) => void }) {
  return <Card className="grid content-start gap-3 p-3">
    <div className="text-xs font-semibold text-muted">{t("Field")}</div>
    <Label text={t("What people call it")}><Input value={field.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
    <Label text={t("Name")}><Input className="font-mono" value={field.name} onChange={(e) => onChange({ name: e.target.value })} /></Label>
    <Label text={t("Type")}><Select value={field.type} onChange={(e) => onChange({ type: e.target.value })}>{fieldTypes.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select></Label>
    {field.type === "choice" && <Label text={t("Choices")}><Input value={field.choices ?? ""} onChange={(e) => onChange({ choices: e.target.value })} /></Label>}
    {field.type === "reference" && <Label text={t("Reference object")}><Input value={field.ref ?? ""} onChange={(e) => onChange({ ref: e.target.value })} /></Label>}
    <Checkbox checked={!!field.required} onChange={(required) => onChange({ required })}>{t("Required")}</Checkbox>
    <Checkbox checked={!!field.search} onChange={(search) => onChange({ search })}>{t("Searchable")}</Checkbox>
  </Card>;
}

/** The left pane: the states in order (a new record starts in the first), then the actions. */
function Outline({ process, chosen, onChoose, onAddState, onAddAction, onChange }: {
  process: Process; chosen: Chosen; onChoose: (c: Chosen) => void; onAddState: () => void; onAddAction: () => void; onChange: (p: Process) => void;
}) {
  const move = <T,>(list: T[], i: number, by: number) => {
    const next = [...list];
    const [x] = next.splice(i, 1);
    next.splice(Math.max(0, Math.min(next.length, i + by)), 0, x!);
    return next;
  };
  const row = (kind: "state" | "action", label: string, i: number, list: unknown[], set: (next: unknown[]) => void) => {
    const inHand = chosen?.kind === kind && chosen.at === i;
    return (
      <li key={`${kind}${i}`}>
        <div className={cn("flex items-center gap-0.5 rounded-md border px-1 py-0.5", inHand ? "border-primary bg-row-selected" : "border-border")}>
          <Button variant="ghost" size="sm" className="min-w-0 flex-1 justify-start" aria-pressed={inHand} onClick={() => onChoose({ kind, at: i })}>
            <span className="truncate">{label}</span>
          </Button>
          <Button size="sm" variant="ghost" aria-label={t("Move up")} onClick={() => { set(move(list, i, -1)); onChoose(undefined); }}><ArrowUp className="size-3" /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Move down")} onClick={() => { set(move(list, i, 1)); onChoose(undefined); }}><ArrowDown className="size-3" /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Remove {name}", { name: label })} onClick={() => { set(list.filter((_, at) => at !== i)); onChoose(undefined); }}><Trash2 className="size-3" /></Button>
        </div>
      </li>
    );
  };
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="grid gap-1">
        <div className="flex items-center justify-between text-xs font-semibold text-muted">{t("States")}
          <Button size="sm" variant="ghost" onClick={onAddState}><Plus className="size-3" />{t("Add a state")}</Button></div>
        <ul className="grid gap-1">{process.states.map((s, i) => row("state", s.title || s.name, i, process.states, (next) => onChange({ ...process, states: next as State[] })))}</ul>
        {process.states.length === 0 ? <p className="text-xs text-muted">{t("Without states its records have no status and no actions.")}</p>
          : <p className="text-xs text-muted">{t("A new record starts in the first.")}</p>}
      </div>
      <div className="grid gap-1 border-t border-border pt-3">
        <div className="flex items-center justify-between text-xs font-semibold text-muted">{t("Actions")}
          <Button size="sm" variant="ghost" disabled={process.states.length === 0} onClick={onAddAction}><Plus className="size-3" />{t("Add an action")}</Button></div>
        <ul className="grid gap-1">{process.actions.map((a, i) => row("action", a.title || a.name, i, process.actions, (next) => onChange({ ...process, actions: next as Action[] })))}</ul>
        {process.actions.length === 0 && <p className="text-xs text-muted">{t("Add the steps people take on a record.")}</p>}
      </div>
    </Card>
  );
}

/** The middle pane: the status bar a record will show, and the chosen action's form, as people meet them. */
function Preview({ object, process, action }: { object: ObjectRecord; process: Process; action?: Action }) {
  const [values, setValues] = useState<Record<string, unknown>>({});
  if (process.states.length === 0) return <Panel role="status" className="text-sm text-muted">{t("Add a state to see what people will see.")}</Panel>;
  const lifecycle = {
    field: "state", initial: process.states[0]!.name,
    states: process.states.map((s) => ({ name: s.name, title: s.title, tone: s.tone as never, description: s.description })),
    transitions: process.actions.map((a) => ({ name: a.name, schema: `build.${object.name}.${a.name}`, title: a.title, from: a.from, to: a.to ? [a.to] : a.from })),
  };
  return (
    <div className="grid gap-4">
      <Panel role="status" className="text-xs text-muted">{t("A record, as its page will show it. Nothing here is saved or taken.")}</Panel>
      <div className="grid gap-2">
        <h3 className="text-sm font-semibold">{t("A new {object}", { object: object.title.toLowerCase() })}</h3>
        <StatusBar lifecycle={lifecycle} state={lifecycle.initial} onTransition={() => undefined} />
      </div>
      {action && <Card className="grid gap-2 p-3">
        <h3 className="text-sm font-semibold">{action.title}</h3>
        {action.description && <p className="text-sm text-muted">{action.description}</p>}
        <p className="text-xs text-muted">{t("From {from} to {to}", {
          from: action.from.map((s) => process.states.find((x) => x.name === s)?.title ?? s).join(", ") || "—",
          to: action.to ? process.states.find((x) => x.name === action.to)?.title ?? action.to : t("where it was"),
        })}</p>
        {action.approval && <p className="text-xs text-muted">{t("Waits in {state} for {levels}", {
          state: process.states.find((s) => s.name === action.approval?.pending)?.title ?? action.approval.pending,
          levels: action.approval.levels.map((level) => `${level.title} (${level.role})`).join(" → "),
        })}</p>}
        <PayloadFields preview values={values} onChange={setValues} fields={(action.inputs ?? []).map((i) => ({
          name: i.name, type: i.type === "integer" ? "integer" : i.type === "decimal" ? "number" : i.type === "date" ? "date" : i.type === "boolean" ? "boolean" : "string",
          required: i.required, description: i.title, choices: i.type === "choice" ? (i.choices ?? "").split(",").map((c) => c.trim()).filter(Boolean) : undefined,
        }))} />
        {(action.conditions ?? []).length > 0 && <ul className="grid gap-1 text-xs text-muted">
          {action.conditions!.map((c, i) => <li key={i}>{t("Needs {field} {operator} {value}; otherwise: “{message}”", { field: c.field, operator: c.operator, value: c.value ?? "", message: c.message })}</li>)}
        </ul>}
      </Card>}
    </div>
  );
}

const Label = ({ text, children }: { text: string; children: ReactNode }) => <label className="grid gap-1 text-xs">{text}{children}</label>;

function StateProperties({ state, onChange }: { state: State; onChange: (patch: Partial<State>) => void }) {
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{t("State")}</div>
      <Label text={t("What people call it")}><Input value={state.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
      <Label text={t("Name")}><Input value={state.name} onChange={(e) => onChange({ name: e.target.value })} className="font-mono" /></Label>
      <Label text={t("Tone")}>
        <Select value={state.tone ?? "info"} onChange={(e) => onChange({ tone: e.target.value })}>{tones.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select>
      </Label>
      <Label text={t("What a record in this state means")}><Textarea rows={3} value={state.description ?? ""} onChange={(e) => onChange({ description: e.target.value })} /></Label>
    </Card>
  );
}

function ActionProperties({ action, states, fields, roles, approverRoles, onChange }: {
  action: Action; states: State[]; fields: Field[]; roles: string[]; approverRoles: string[]; onChange: (patch: Partial<Action>) => void;
}) {
  const inputs = action.inputs ?? [], sets = action.sets ?? [], conditions = action.conditions ?? [];
  const sources = [...inputs.map((i) => ({ value: i.name, label: t("Input: {name}", { name: i.title }) })),
    { value: "$me", label: t("The person taking it") }, { value: "$now", label: t("Now") }];
  const subjects = [...fields.map((f) => ({ value: f.name, label: f.title })), ...(states.length ? [{ value: "state", label: t("State") }] : []),
    ...inputs.map((i) => ({ value: `input.${i.name}`, label: t("Input: {name}", { name: i.title }) }))];
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{t("Action")}</div>
      <Label text={t("What people call it")}><Input value={action.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
      <Label text={t("Name")}><Input value={action.name} onChange={(e) => onChange({ name: e.target.value })} className="font-mono" /></Label>
      <Label text={t("What it does")}><Textarea rows={2} value={action.description ?? ""} onChange={(e) => onChange({ description: e.target.value })} /></Label>
      <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Taken from")}</legend>
        <Toggles options={states.map((s) => ({ value: s.name, label: s.title }))} value={action.from} onChange={(from) => onChange({ from })} />
      </fieldset>
      {roles.length > 0 && <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Taken by")}</legend>
        <Toggles options={roles.map((r) => ({ value: r, label: r }))} value={action.roles ?? []} onChange={(r) => onChange({ roles: r.length ? r : undefined })} />
        <p className="text-muted">{t("None chosen: every role that may read the object. The builder always may.")}</p>
      </fieldset>}
      <Label text={t("Leaves it in")}>
        <Select value={action.to ?? ""} onChange={(e) => onChange({ to: e.target.value || undefined })}>
          <option value="">{t("Where it was")}</option>
          {states.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}
        </Select>
      </Label>
      <Disclosure summary={<span className="text-xs font-semibold">{t("Approval settings")}</span>} className="border-t border-border pt-2">
      <fieldset className="grid gap-2 text-xs"><legend className="sr-only">{t("Approval")}</legend>
        <Checkbox checked={!!action.approval} onChange={(enabled) => onChange({ approval: enabled ? {
          pending: states.find((s) => s.name !== action.from[0] && s.name !== action.to)?.name ?? "",
          levels: [{ title: t("Approver"), role: approverRoles[0] ?? "builder" }],
        } : undefined })}>{t("Wait for approval")}</Checkbox>
        {action.approval && <>
          <p className="text-muted">{t("Use one starting state. The approver decides in the work inbox; the action runs after the last approval.")}</p>
          <Label text={t("While it waits")}><Select value={action.approval.pending} onChange={(e) => onChange({ approval: { ...action.approval!, pending: e.target.value } })}>
            <option value="">{t("Choose a state")}</option>{states.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}
          </Select></Label>
          <Label text={t("If rejected")}><Select value={action.approval.rejected ?? ""} onChange={(e) => onChange({ approval: { ...action.approval!, rejected: e.target.value || undefined } })}>
            <option value="">{t("Return to the starting state")}</option>{states.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}
          </Select></Label>
          <Rows<ApproverLevel> title={t("Approver levels, in order")} add={t("Add a level")} items={action.approval.levels}
            make={() => ({ title: t("Approver"), role: approverRoles[0] ?? "builder" })}
            onChange={(levels) => onChange({ approval: { ...action.approval!, levels } })}
            row={(level, set) => <>
              <Label text={t("What this level is called")}><Input value={level.title} onChange={(e) => set({ title: e.target.value })} /></Label>
              <Label text={t("Approver role")}><Select value={level.role} onChange={(e) => set({ role: e.target.value })}>{approverRoles.map((role) => <option key={role} value={role}>{role}</option>)}</Select></Label>
              <Checkbox checked={!!level.all} onChange={(all) => set({ all })}>{t("Everyone in the role must approve")}</Checkbox>
            </>} />
        </>}
      </fieldset>
      </Disclosure>
      <Disclosure summary={<span className="text-xs font-semibold">{t("Inputs and rules")}</span>} className="border-t border-border pt-2">
      <Rows<Input_> title={t("What people give")} add={t("Add an input")} items={inputs}
        make={() => ({ name: nameOf("input", inputs.map((i) => i.name)), title: t("Input"), type: "text" })}
        onChange={(next) => onChange({ inputs: next })}
        row={(input, set) => <>
          <Input aria-label={t("Label")} value={input.title} onChange={(e) => set({ title: e.target.value })} />
          <Input aria-label={t("Name")} className="font-mono" value={input.name} onChange={(e) => set({ name: e.target.value })} />
          <Select aria-label={t("Type")} value={input.type} onChange={(e) => set({ type: e.target.value })}>{inputTypes.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select>
          {input.type === "choice" && <Input aria-label={t("Choices")} placeholder="a, b, c" value={input.choices ?? ""} onChange={(e) => set({ choices: e.target.value })} />}
          <Checkbox className="text-xs" checked={!!input.required} onChange={(required) => set({ required })}>{t("Required")}</Checkbox>
        </>} />
      <Rows<Set_> title={t("What it sets")} add={t("Set a field")} items={sets} make={() => ({ field: fields[0]?.name ?? "", from: sources[0]!.value })}
        onChange={(next) => onChange({ sets: next })}
        row={(set, patch) => <>
          <Select aria-label={t("Field")} value={set.field} onChange={(e) => patch({ field: e.target.value })}>{fields.map((f) => <option key={f.name} value={f.name}>{f.title}</option>)}</Select>
          <Select aria-label={t("From")} value={set.from.startsWith("=") ? "=" : set.from} onChange={(e) => patch({ from: e.target.value === "=" ? "=" : e.target.value })}>
            {sources.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}
            <option value="=">{t("A fixed value")}</option>
          </Select>
          {set.from.startsWith("=") && <Input aria-label={t("Value")} value={set.from.slice(1)} onChange={(e) => patch({ from: `=${e.target.value}` })} />}
        </>} />
      <Rows<Condition> title={t("What it needs")} add={t("Add a condition")} items={conditions}
        make={() => ({ field: subjects[0]?.value ?? "", operator: "=", value: "", message: "" })}
        onChange={(next) => onChange({ conditions: next })}
        row={(c, patch) => <>
          <Select aria-label={t("Field")} value={c.field} onChange={(e) => patch({ field: e.target.value })}>{subjects.map((s) => <option key={s.value} value={s.value}>{s.label}</option>)}</Select>
          <Select aria-label={t("Operator")} value={c.operator} onChange={(e) => patch({ operator: e.target.value })}>{operators.map((o) => <option key={o} value={o}>{t(o)}</option>)}</Select>
          {c.operator !== "empty" && c.operator !== "not empty" && <Input aria-label={t("Value")} placeholder={t("a value, or $me")} value={c.value ?? ""} onChange={(e) => patch({ value: e.target.value })} />}
          <Input aria-label={t("Message when it does not hold")} placeholder={t("What a person reads when it does not hold")} value={c.message} onChange={(e) => patch({ message: e.target.value })} />
        </>} />
      </Disclosure>
    </Card>
  );
}

/** A small list of rows to add to, change and remove: inputs, fields set, conditions. */
function Rows<T>({ title, add, items, make, onChange, row }: {
  title: string; add: string; items: T[]; make: () => T; onChange: (next: T[]) => void; row: (item: T, set: (patch: Partial<T>) => void) => ReactNode;
}) {
  return (
    <fieldset className="grid gap-1 border-t border-border pt-2 text-xs"><legend className="mb-1 font-semibold text-muted">{title}</legend>
      {items.map((item, i) => (
        <div key={i} className="grid gap-1 rounded-md border border-border p-2">
          {row(item, (patch) => onChange(items.map((x, at) => at === i ? { ...x, ...patch } : x)))}
          <Button size="sm" variant="ghost" className="justify-self-end" onClick={() => onChange(items.filter((_, at) => at !== i))}>
            <Trash2 className="size-3" />{t("Remove")}</Button>
        </div>
      ))}
      <Button size="sm" className="justify-self-start" onClick={() => onChange([...items, make()])}><Plus className="size-3" />{add}</Button>
    </fieldset>
  );
}

const reads = { all: () => t("Every record"), own: () => t("Only those they created"), none: () => t("Not at all") };

/** The left pane's third part: the roles of the builder app this object names, and what each may do. */
function AccessOutline({ access, chosen, onChoose, onAdd, onRemove }: {
  access: Access[]; chosen: Chosen; onChoose: (c: Chosen) => void; onAdd: () => void; onRemove: (i: number) => void;
}) {
  return (
    <Card className="mt-3 grid content-start gap-1 p-3">
      <div className="flex items-center justify-between text-xs font-semibold text-muted">{t("Who may do what")}
        <Button size="sm" variant="ghost" onClick={onAdd}><Plus className="size-3" />{t("Add a role")}</Button></div>
      <ul className="grid gap-1">
        {access.map((a, i) => {
          const inHand = chosen?.kind === "access" && chosen.at === i;
          return (
            <li key={i} className={cn("flex items-center gap-0.5 rounded-md border px-1 py-0.5", inHand ? "border-primary bg-row-selected" : "border-border")}>
              <Button variant="ghost" size="sm" className="min-w-0 flex-1 justify-start" aria-pressed={inHand} onClick={() => onChoose({ kind: "access", at: i })}>
                <span className="truncate font-mono">{a.role}</span><span className="ml-auto truncate pl-1 text-[10px] text-muted">{reads[a.read]()}</span>
              </Button>
              <Button size="sm" variant="ghost" aria-label={t("Remove {name}", { name: a.role })} onClick={() => onRemove(i)}><Trash2 className="size-3" /></Button>
            </li>
          );
        })}
      </ul>
      <p className="text-xs text-muted">{access.length === 0
        ? t("None named: everyone with a role in the builder app reads and changes every record.")
        : t("A role not named here does not see the object. The builder always sees and does everything. Roles are granted in Settings → Members.")}</p>
    </Card>
  );
}

/** The right pane for a role: which records it reads, what it may do, and the fields only it reads or sets. */
function AccessProperties({ access, fields, onChange, onFields }: {
  access: Access; fields: Field[]; onChange: (patch: Partial<Access>) => void; onFields: (fields: Field[]) => void;
}) {
  const none = access.read === "none";
  const toggle = (list: string[] | undefined, on: boolean) => {
    const next = (list ?? []).filter((r) => r !== access.role);
    return on ? [...next, access.role] : next;
  };
  const restricted = fields.filter((f) => (f.read ?? []).length > 0 || (f.write ?? []).length > 0);
  return (
    <Card className="grid content-start gap-3 p-3">
      <div className="text-xs font-semibold text-muted">{t("Role")}</div>
      <Label text={t("Role in the builder app")}><Input className="font-mono" value={access.role} onChange={(e) => onChange({ role: e.target.value })} /></Label>
      <Label text={t("Which records it reads")}>
        <Select value={access.read} onChange={(e) => {
          const read = e.target.value as Access["read"];
          onChange(read === "none" ? { read, create: false, edit: false, archive: false } : { read });
        }}>
          {(["all", "own", "none"] as const).map((r) => <option key={r} value={r}>{reads[r]()}</option>)}
        </Select>
      </Label>
      <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("What it may do")}</legend>
        <Checkbox disabled={none} checked={!!access.create} onChange={(create) => onChange({ create })}>{t("Create records")}</Checkbox>
        <Checkbox disabled={none} checked={!!access.edit} onChange={(edit) => onChange({ edit })}>{t("Edit records")}</Checkbox>
        <Checkbox disabled={none} checked={!!access.archive} onChange={(archive) => onChange({ archive })}>{t("Archive records")}</Checkbox>
      </fieldset>
      {!none && <fieldset className="grid gap-1 border-t border-border pt-2 text-xs"><legend className="mb-1 font-semibold text-muted">{t("Fields only some roles read or set")}</legend>
        {fields.map((f, i) => {
          const set = (patch: Partial<Field>) => onFields(fields.map((x, at) => at === i ? { ...x, ...patch } : x));
          const readsIt = (f.read ?? []).includes(access.role), setsIt = (f.write ?? []).includes(access.role);
          return (
            <div key={f.name} className="flex flex-wrap items-center gap-2">
              <span className="min-w-24 font-medium">{f.title}</span>
              <Checkbox checked={readsIt} onChange={(on) => set({ read: toggle(f.read, on), write: on ? f.write : toggle(f.write, false) })}>{t("only its readers")}</Checkbox>
              <Checkbox checked={setsIt} onChange={(on) => set({ write: toggle(f.write, on), read: on && (f.read ?? []).length ? toggle(f.read, true) : f.read })}>{t("only its setters")}</Checkbox>
            </div>
          );
        })}
        <p className="text-muted">{restricted.length === 0
          ? t("Every field is read by every role that reads the object.")
          : t("A restricted field is read (or set) only by the roles ticked for it, and the builder.")}</p>
      </fieldset>}
    </Card>
  );
}
