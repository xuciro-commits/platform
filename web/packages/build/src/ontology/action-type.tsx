import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { TransactionEditor } from "./TransactionEditor";
import {ConfirmationEditor} from "./ConfirmationEditor";
import { DispatchEditor } from "./DispatchEditor";
import {RefusalEditor} from "./RefusalEditor";
// The action type workbench (ADR-0047, built under ADR-0019): an action is a
// first-class resource with its own sections — Overview, Parameters, Form,
// Transaction, Approval, Permissions. Left: the
// object's actions and this action's sections. Main: the section as a table or
// preview. Right: the row in hand. Dock: this action's problems. The action
// still lives in its object's record: the object draft session is shared.
import { PayloadFields, SemanticObjectSelect, assetKey, useHost, useRecordInventory, type Definition } from "@platform/app";
import type { Api } from "@platform/kernel";
import { projectGroup, useProjectIndex } from "../projects/membership";
import { Button, Card, Checkbox, GroupedList, Input, PageHeader, Panel, PanelSection, ProblemList, Select, StructureRow, Textarea, Toggles, Workbench, cn, t, type EntityInfo, type WorkbenchProblem } from "@platform/ui";
import { useApplicationWorkspace } from "../projects/application-scope";
import { DraftStatus } from "../editor/workbench";
import { SystemDefinitionsToggle, useSystemDefinitionsVisibility } from "../editor/SystemDefinitions";
import { Boxes, FileInput, ListChecks, Plus, Shield, Trash2, Zap } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { ResourceControls, WorkbenchMessage, savingState } from "../editor/workbench";
import { EnterpriseReferenceTypes, Label } from "./process";
import { actionInputFields, conditionSubjects, inputTypes, nameOf, type Action, type ApproverLevel, type Input_ } from "./object-model";
import { actionResultEdges } from "./process-rules";
import { useObjectDraft } from "./object-draft";

type Section = "overview" | "parameters" | "form" | "transaction" | "approval" | "permissions";
type Pick = { kind: "parameter"; at: number } | undefined;
const sections: { id: Section; title: () => string; icon: ReactNode }[] = [
  { id: "overview", title: () => t("Overview"), icon: <Zap /> },
  { id: "parameters", title: () => t("Parameters"), icon: <FileInput /> },
  { id: "form", title: () => t("Form"), icon: <ListChecks /> },
  { id: "transaction", title: () => t("Transaction"), icon: <ListChecks /> },
  { id: "approval", title: () => t("Approval"), icon: <Shield /> },
  { id: "permissions", title: () => t("Permissions"), icon: <Shield /> },
];

type ObjectDraft = { id: string; name: string; title: string; state: string; archived?: boolean; actions?: Action[]; states?: { name: string; title: string }[] };
type ActionRow = { key: string; object: ObjectDraft; action: Action };

/** Every action type of every object type this tenant drafts (Ontology Manager's Action types list): one row per action, opening its workbench. */
type BuiltInRow = { key: string; definition: Definition; object?: Definition };

/** Editable tenant actions and read-only published declarations share one list.
 * Code-owned declarations appear only when explicitly requested. */
export function ActionTypes() {
  const { role, definitions, me } = useHost(), { open } = useApplicationWorkspace();
  const inventory = useRecordInventory<ObjectDraft>("build.object");
  const projects = useProjectIndex();
  const [showSystem, setShowSystem] = useSystemDefinitionsVisibility();
  if (role("build") !== "builder") return <PageHeader title={t("Action types")} description={t("Only a builder can edit action types.")} />;
  const drafts = (inventory.data?.records ?? []).filter((object) => !object.archived);
  const rows: ActionRow[] = drafts.flatMap((object) => (object.actions ?? []).map((action) => ({ key: `${object.id}:${action.name}`, object, action })));
  const stateTitle = (object: ObjectDraft, name?: string) => object.states?.find((s) => s.name === name)?.title ?? name ?? "";
  const transition = (row: ActionRow) => `${row.action.from.map((f) => stateTitle(row.object, f)).join(", ")} → ${actionResultEdges(row.action).map(name=>stateTitle(row.object,name)).join(", ") || t("Where it was")}`;
  const own = new Set(rows.map((row) => assetKey({ app: "build", kind: "action", name: `build.${row.object.name}.${row.action.name}` })));
  const objects = new Map(definitions.filter((d) => d.ref.kind === "object").map((d) => [assetKey(d.ref), d]));
  const builtIn: BuiltInRow[] = definitions.filter((d) => d.ref.kind === "action" && d.action && !own.has(assetKey(d.ref)) && (showSystem || d.source !== "code")).map((d) => ({ key: assetKey(d.ref), definition: d, object: objects.get(assetKey({ app: d.ref.app, kind: "object", name: d.action!.target })) }));
  const appTitle = (id: string) => me.apps.find((a) => a.id === id)?.title ?? id;
  const objectRef = (object: ObjectDraft): Api.AssetRef => ({ app: "build", kind: "object", name: `build.${object.name}` });
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={t("Action types")} help={t("An action type is how people change an object: its parameters, rules, criteria and side effects. Every action belongs to one object type; open the object type to add one. The host already ships create, edit and archive for every object type and each module its own actions — they are listed below so you declare only what is missing.")} />
    <SystemDefinitionsToggle checked={showSystem} onChange={setShowSystem} />
    <Panel title={t("Declared in this organisation")} description={t("{n} action types on {objects} object types", { n: rows.length, objects: drafts.filter((o) => o.actions?.length).length })}>
      <GroupedList<ActionRow> items={rows} id={(row) => row.key} text={(row) => `${row.action.title} ${row.action.name} ${row.object.title} ${row.object.name}`}
        onSelect={(row) => open({ view: "action-type", params: { id: row.object.id, action: row.action.name } })}
        groupings={[
          { id: "object", label: t("Object type"), of: (row) => ({ id: row.object.id, label: row.object.title || row.object.name, hint: `build.${row.object.name}` }) },
          { id: "project", label: t("Project"), of: (row) => projectGroup(projects, objectRef(row.object)) },
          { id: "status", label: t("Status"), of: (row) => ({ id: row.object.state, label: t(row.object.state === "published" ? "Published" : "Draft") }) },
        ]}
        row={(row) => <><Zap className="size-3 shrink-0 text-muted" /><span className="min-w-0 flex-1 truncate">{row.action.title || row.action.name}</span>
          <span className="hidden min-w-0 truncate text-muted md:inline">{transition(row)}</span>
          {!!row.action.inputs?.length && <span className="shrink-0 text-muted">{t("{n} parameters", { n: row.action.inputs.length })}</span>}
          {row.action.approval && <span className="shrink-0 text-muted">{t("Approval")}</span>}
          <DraftStatus state={row.object.state} /></>}
        empty={inventory.isLoading ? t("Loading…") : t("No action types yet. Add a lifecycle state and an action to an object type.")} />
    </Panel>
    <Panel aria-label={t("Published action definitions")} title={t("Published action definitions")} help={t("Read-only action definitions. Open an action to inspect its parameters, rules and access.")}>
      <GroupedList<BuiltInRow> items={builtIn} id={(row) => row.key} text={(row) => `${row.definition.action!.title} ${row.key} ${row.object?.entity?.title ?? ""}`}
        onSelect={(row) => open({ view: "definition", params: { ...row.definition.ref, surface: "studio" } })}
        groupings={[
          { id: "object", label: t("Object type"), of: (row) => ({ id: `${row.definition.ref.app}/${row.definition.action!.target || "_"}`, label: row.object?.entity?.title ?? row.definition.action!.target ?? t("No object"), hint: row.definition.action!.target }) },
          { id: "module", label: t("Module"), of: (row) => ({ id: row.definition.ref.app, label: appTitle(row.definition.ref.app) }) },
        ]}
        row={(row) => <><Zap className="size-3 shrink-0 text-muted" /><span className="min-w-0 flex-1 truncate">{row.definition.action!.title}</span>
          <span className="hidden min-w-0 truncate font-mono text-[10px] text-muted md:inline">{row.key}</span>
          {!!row.definition.action!.payload?.length && <span className="shrink-0 text-muted">{t("{n} parameters", { n: row.definition.action!.payload.length })}</span>}</>} />
    </Panel>
  </div>;
}

export function ActionTypeEditor({ id, action: initial }: { id: string; action?: string }) {
  const [pick, setPick] = useState<Pick>();
  const draft = useObjectDraft(id, () => setPick(undefined));
  const { object, process, dirty, busy, blocked, refused, change, parent, entities, roles, approverRoles, session, discardChanges, open } = draft;
  const [name, setName] = useState(initial);
  const [section, setSection] = useState<Section>("overview");
  useEffect(() => { if (initial) setName(initial); }, [initial]);
  if (!object) return <Workbench storageKey="action-type" title={t("Action type")}><WorkbenchMessage>{draft.failed ? t("The object type could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const at = process.actions.findIndex(action => action.name === name), action = process.actions[at];
  const patch = (next: Partial<Action>) => { if (at >= 0) change({ ...process, actions: process.actions.map((action, index) => index === at ? { ...action, ...next } : action) }); };
  const addAction = () => {
    const fresh = nameOf("action", process.actions.map(action => action.name)), from = process.states[0]?.name;
    change({ ...process, actions: [...process.actions, { name: fresh, title: t("Step {n}", { n: process.actions.length + 1 }), from: from ? [from] : [], transaction: { subject: parent, steps: [{ name: "update", kind: "update", target: { type: parent, id: "$subject" }, changes: [] }] } }] });
    setName(fresh); setSection("overview"); setPick(undefined);
  };
  const issues = action ? draft.issuesOf(action.name) : [];
  const problems: WorkbenchProblem[] = issues.map((text, index) => ({ id: `issue:${index}`, text }));
  const inputs = action?.inputs ?? [], subjects = action ? conditionSubjects(process, action, parent, entities) : [];
  const reviewerRoles = [...new Set(["builder", ...process.access.map(access => access.role)])];
  const stateTitle = (name: string) => process.states.find(state => state.name === name)?.title ?? name;
  const main: Record<Section, ReactNode> = !action ? {} as Record<Section, ReactNode> : {
    overview: <div className="grid max-w-2xl content-start gap-3 p-4">
      <Label text={t("What people call it")}><Input value={action.title} onChange={event => patch({ title: event.target.value })} /></Label>
      <Label text={t("Name")}><Input value={action.name} className="font-mono" onChange={event => { patch({ name: event.target.value }); setName(event.target.value); }} /></Label>
      <Label text={t("What it does")}><Textarea rows={2} value={action.description ?? ""} onChange={event => patch({ description: event.target.value })} /></Label>
      <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Taken from")}</legend><Toggles options={process.states.map(state => ({ value: state.name, label: state.title }))} value={action.from} onChange={from => patch({ from })} /></fieldset>
      <p className="text-sm text-muted">{t("State changes, rules and related work are declared in Transaction. A state field can use a fixed value or a required choice parameter.")}</p>
      <div className="grid grid-cols-2 gap-2 text-sm">{([["parameters", inputs.length], ["transaction", action.transaction?.steps.length ?? 0]] as const).map(([id, count]) => <Button key={id} variant="row" className="rounded-md border border-border p-3 text-left" onClick={() => { setSection(id); setPick(undefined); }}><span className="block text-xs text-muted">{sections.find(section => section.id === id)!.title()}</span><span className="text-lg font-semibold">{count}</span></Button>)}</div>
    </div>,
    parameters: <div className="grid content-start gap-2 p-4"><div className="flex flex-wrap items-center justify-between gap-2"><h2 className="text-sm font-semibold">{t("Parameters")}</h2><Button size="sm" onClick={() => { patch({ inputs: [...inputs, { name: nameOf("input", inputs.map(input => input.name)), title: t("Input"), type: "text" }] }); setPick({ kind: "parameter", at: inputs.length }); }}><Plus />{t("Add a parameter")}</Button></div>
      {inputs.length ? <ul className="divide-y divide-border rounded-md border border-border">{inputs.map((input, index) => <li key={index}><Button variant="row" aria-pressed={pick?.at === index} className={cn("flex w-full items-center gap-3 px-3 py-2 text-left text-sm", pick?.at === index && "bg-row-selected")} onClick={() => setPick({ kind: "parameter", at: index })}><span className="min-w-0 flex-1 truncate font-medium">{input.title}</span><code className="text-xs text-muted">{input.name}</code><span className="text-xs text-muted">{t(input.type)}{input.required ? " *" : ""}</span></Button></li>)}</ul> : <p className="text-sm text-muted">{t("No parameters. People take the action with no form.")}</p>}
    </div>,
    form: <div className="grid max-w-xl content-start gap-3 p-4"><p className="text-xs text-muted">{t("The form as people will see it. Nothing here is saved or taken.")}</p><Card className="grid gap-2 p-3"><h3 className="text-sm font-semibold">{action.title}</h3>{action.description && <p className="text-sm text-muted">{action.description}</p>}<div className="flex flex-wrap gap-1">{inputs.map((input, at) => <Button key={input.name} size="sm" onClick={() => setPick({ kind: "parameter", at })}>{input.title}</Button>)}</div><FormPreview key={JSON.stringify(inputs)} inputs={inputs} />{action.transaction?.message && <p className="text-xs text-muted">{action.transaction.message}</p>}<Button variant="primary" size="sm" className="w-fit" disabled>{action.title}</Button></Card></div>,
    transaction: <div className="grid min-w-0 max-w-4xl content-start gap-3 p-4"><h2 className="text-sm font-semibold">{t("Transaction")}</h2>
      {action.transaction ? <TransactionEditor value={action.transaction} onChange={transaction => patch({ transaction })} process={process} inputs={inputs} roles={reviewerRoles} /> : <Button className="w-fit" onClick={() => patch({ transaction: { subject: parent, steps: [{ name: "update", kind: "update", target: { type: parent, id: "$subject" }, changes: [] }] } })}>{t("Define transaction")}</Button>}
 <Checkbox checked={!!action.adoption} disabled={!action.transaction} onChange={enabled=>patch({adoption:enabled?{function:{app:"build",name:"",version:0},input:"proposal",mapping:[]}:undefined,inputs:enabled&&!inputs.some(input=>input.name==="proposal")?[...inputs,{name:"proposal",title:t("Retained proposal"),type:"reference",ref:"build.function-call",required:true}]:inputs})}>{t("Adopt an AI proposal")}</Checkbox>
 {action.adoption&&<div className="grid gap-2 rounded border border-border p-3">
 <p className="text-xs text-muted">{t("The proposal stays unchanged until this transaction is accepted. AI destinations must declare generated provenance.")}</p>
 <label className="grid gap-1 text-xs">{t("Proposal function name")}<Input value={action.adoption.function.name} onChange={event=>patch({adoption:{...action.adoption!,function:{...action.adoption!.function,name:event.target.value}}})}/></label>
 <label className="grid gap-1 text-xs">{t("Proposal version (0: retained call version)")}<Input type="number" min={0} max={64} value={action.adoption.function.version} onChange={event=>patch({adoption:{...action.adoption!,function:{...action.adoption!.function,version:Number(event.target.value)}}})}/></label>
 <label className="grid gap-1 text-xs">{t("Proposal reference input")}<Select value={action.adoption.input} onChange={event=>patch({adoption:{...action.adoption!,input:event.target.value}})}>{inputs.filter(input=>input.type==="reference"&&input.ref==="build.function-call").map(input=><option key={input.name} value={input.name}>{input.title}</option>)}</Select></label>
 {action.adoption.mapping.map((mapping,index)=><div key={index} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]"><label className="grid gap-1 text-xs">{t("Proposal output field")}<Input value={mapping.output} onChange={event=>patch({adoption:{...action.adoption!,mapping:action.adoption!.mapping.map((value,i)=>i===index?{...value,output:event.target.value}:value)}})}/></label><label className="grid gap-1 text-xs">{t("Business action input")}<Select value={mapping.input} onChange={event=>patch({adoption:{...action.adoption!,mapping:action.adoption!.mapping.map((value,i)=>i===index?{...value,input:event.target.value}:value)}})}><option value="">{t("Choose an input")}</option>{inputs.filter(input=>input.name!==action.adoption!.input).map(input=><option key={input.name} value={input.name}>{input.title}</option>)}</Select></label><Button onClick={()=>patch({adoption:{...action.adoption!,mapping:action.adoption!.mapping.filter((_,i)=>i!==index)}})}>{t("Remove")}</Button></div>)}
 <Button onClick={()=>patch({adoption:{...action.adoption!,mapping:[...action.adoption!.mapping,{output:"",input:inputs.find(input=>input.name!==action.adoption!.input)?.name??""}]}})}>{t("Add proposal mapping")}</Button>
 </div>}


      {action.transaction && <>
        <ConfirmationEditor value={action.transaction.confirmation} onChange={confirmation => patch({ transaction: { ...action.transaction!, confirmation } })} actions={process.actions.filter(candidate => candidate.name !== action.name).map(candidate => candidate.name)} roles={reviewerRoles} inputs={inputs} />
        <DispatchEditor value={action.transaction.dispatch} onChange={dispatch => patch({ transaction: { ...action.transaction!, dispatch } })} commands={process.dispatchCommands ?? []}
          onCommandsChange={(dispatchCommands, rename) => change({ ...process, dispatchCommands: dispatchCommands.map(command => rename && command.compensate === rename.before ? { ...command, compensate: rename.after } : command), actions: rename ? process.actions.map(candidate => candidate.transaction?.dispatch?.command === rename.before ? { ...candidate, transaction: { ...candidate.transaction, dispatch: { ...candidate.transaction.dispatch, command: rename.after } } } : candidate) : process.actions })}
          object={parent} fields={process.fields} choices={[{ value: "input.$subject", label: t("Subject record ID"), type: "reference", ref: parent }, ...subjects]} roles={reviewerRoles} />
        <RefusalEditor value={action.transaction} onChange={transaction => patch({ transaction })} choices={subjects} actions={process.actions.filter(candidate => candidate.name !== action.name && candidate.transaction && !candidate.approval && !candidate.adoption).map(candidate => candidate.name)} entities={entities} roles={reviewerRoles} />
      </>}
    </div>,
    approval: <div className="grid max-w-xl content-start gap-3 p-4 text-sm"><Checkbox checked={!!action.approval} onChange={enabled => patch({ approval: enabled ? { pending: "", levels: [{ title: t("Approver"), role: approverRoles[0] ?? "builder" }] } : undefined })}>{t("Wait for approval")}</Checkbox>
      {action.approval && <><p className="text-xs text-muted">{t("Business records stay unchanged while approval waits. After the final approval, the transaction runs with the requester's current permissions.")}</p>
        <Rows<ApproverLevel> title={t("Approver levels, in order")} add={t("Add a level")} items={action.approval.levels} make={() => ({ title: t("Approver"), role: approverRoles[0] ?? "builder" })} onChange={levels => patch({ approval: { ...action.approval!, levels } })} row={(level, set) => <><Label text={t("What this level is called")}><Input value={level.title} onChange={event => set({ title: event.target.value })} /></Label><Label text={t("Approver role")}><Select value={level.role} onChange={event => set({ role: event.target.value })}>{approverRoles.map(role => <option key={role}>{role}</option>)}</Select></Label><Checkbox checked={!!level.all} onChange={all => set({ all })}>{t("Everyone in the role must approve")}</Checkbox></>} /></>}
    </div>,
    permissions: <div className="grid max-w-xl content-start gap-3 p-4 text-sm">{roles.length ? <fieldset className="grid gap-1 text-xs"><legend className="mb-1">{t("Taken by")}</legend><Toggles options={roles.map(role => ({ value: role, label: role }))} value={action.roles ?? []} onChange={roles => patch({ roles: roles.length ? roles : undefined })} /><p className="text-muted">{t("None chosen: every role that may read the object. The builder always may.")}</p></fieldset> : <p className="text-muted">{t("The object has no roles yet; only builders may take this action.")}</p>}<Button size="sm" className="w-fit" onClick={() => open({ view: "object-type", params: { id, tab: "permissions" } })}><Shield />{t("Edit the object's roles")}</Button></div>,
  };
  const inspector = action && pick && inputs[pick.at] ? <div className="p-2"><RowCard title={t("Parameter")} onRemove={() => { patch({ inputs: inputs.filter((_, index) => index !== pick.at) }); setPick(undefined); }}><ParameterEditor input={inputs[pick.at]!} inputs={inputs} entities={entities} onChange={change => patch({ inputs: inputs.map((input, index) => index === pick.at ? { ...input, ...change } : input) })} /></RowCard></div> : <p className="p-3 text-xs text-muted">{t("Select a row to edit it here.")}</p>;
  return <Workbench storageKey="action-type" title={action?.title || t("Action type")}
    crumbs={[{ label: t("Object types"), onClick: () => open({ view: "object-type" }) }, { label: object.title, onClick: () => open({ view: "object-type", params: { id, tab: "actions" } }) }]}
    status={<DraftStatus state={object.state} problems={issues.length} />} saving={savingState(dirty, busy, refused)}
    history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: () => { session.undo(); setPick(undefined); }, redo: () => { session.redo(); setPick(undefined); } }}
    actions={<ResourceControls type="build.object" record={object} dirty={dirty} busy={busy} invalid={draft.issues.length > 0} onSave={() => void draft.perform(draft.save)} onReview={() => void draft.perform(draft.review)} onInstall={() => void draft.perform(draft.publish)} onCancel={discardChanges} route={{ view: "action-type", params: { id, action: name ?? "" } }} />}
    left={{ label: t("Action structure"), content: <div className="grid min-w-0 content-start"><PanelSection title={object.title} actions={<Button size="sm" variant="ghost" aria-label={t("Add an action")} disabled={!process.states.length} onClick={addAction}><Plus /></Button>}><StructureRow icon={<Boxes />} label={t("Object type")} onClick={() => open({ view: "object-type", params: { id } })} />{process.actions.map(action => <StructureRow key={action.name} depth={1} icon={<Zap />} label={action.title || action.name} meta={action.from.map(stateTitle).join(", ")} selected={action.name === name} onClick={() => { setName(action.name); setPick(undefined); }} />)}{!process.actions.length && <p className="px-2 text-[11px] text-muted">{t("No actions yet.")}</p>}</PanelSection>{action && <PanelSection title={t("Sections")}>{sections.map(item => <StructureRow key={item.id} icon={item.icon} label={item.title()} selected={item.id === section} onClick={() => { setSection(item.id); setPick(undefined); }} meta={item.id === "parameters" ? String(inputs.length) : item.id === "transaction" ? String(action.transaction?.steps.length ?? 0) : item.id === "approval" && action.approval ? "✓" : undefined} />)}</PanelSection>}</div> }}
    right={{ label: t("Action inspector"), scope: JSON.stringify([at, pick]), locate: path => { const scope = path?.split("/").find(part => part.startsWith("[")); setPick(scope ? (JSON.parse(scope) as [number, Pick])[1] : pick); }, content: <fieldset disabled={blocked} className="contents">{inspector}</fieldset> }}
    dock={{ label: t("Action dock"), tabs: [{ id: "problems", title: t("Problems"), badge: issues.length, content: <ProblemList problems={problems} empty={t("No problems. The action can be published with its object type.")} /> }] }}>
    {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {refused}</Panel>}
    {action ? <StructuredEditor expectedRevision={draft.structured.expectedRevision} fieldInput={sourceFieldInput} value={{ ...action }} fields={draft.structured.fields.find(field => field.name === "actions")?.fields} scope={`build.object:${id}:action:${name}`} disabled={busy || blocked} onTrial={next => draft.structured.onTrial({ ...process, actions: process.actions.map((item, index) => index === at ? next as Action : item) })} onChange={next => { change({ ...process, actions: process.actions.map((item, index) => index === at ? next as Action : item) }); if (typeof next.name === "string") setName(next.name); }}><fieldset disabled={busy || blocked} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto">{main[section]}</fieldset></StructuredEditor> : <WorkbenchMessage>{process.actions.length ? t("Choose an action on the left.") : t("This object type has no actions yet.")}</WorkbenchMessage>}
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
  return <PayloadFields preview values={values} onChange={setValues} fields={actionInputFields(inputs)} />;
}

function ParameterEditor({ input, inputs, entities, onChange }: { input: Input_; inputs:Input_[]; entities: EntityInfo[]; onChange: (patch: Partial<Input_>) => void }) {
  return <>
    <Label text={t("Label")}><Input value={input.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
    <Label text={t("Name")}><Input className="font-mono" value={input.name} onChange={(e) => onChange({ name: e.target.value })} /></Label>
    <Label text={t("Type")}><Select value={input.type} onChange={(e) => onChange({ type: e.target.value, ref: undefined, stereotype:undefined, stereotypes:undefined, choices: undefined, minLength: undefined, constraints:undefined,range:undefined })}>{inputTypes.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select></Label>
    {input.type === "choice" && <Label text={t("Choices")}><Input placeholder="a, b, c" value={input.choices ?? ""} onChange={(e) => onChange({ choices: e.target.value })} /></Label>}
    {input.type === "reference" && <Label text={t("Reference object")}><SemanticObjectSelect enterprise value={input.ref} label={t("Reference object")} drafts={entities.map(entity => ({name:entity.type,title:entity.title}))} onChange={ref => onChange({ ref:ref?.name, stereotype:undefined, stereotypes:undefined })} /></Label>}
    {input.type === "reference" && input.ref === "enterprise.element" && <EnterpriseReferenceTypes value={input} onChange={onChange} />}
    {["text", "longtext"].includes(input.type) && <Label text={t("Minimum length")}><Input type="number" min={0} max={4096} value={input.minLength ?? ""} onChange={(e) => onChange({ minLength: e.target.value === "" ? undefined : e.target.valueAsNumber })} /></Label>}
    <Label text={t("Group")}><Input value={input.group??""} onChange={e=>onChange({group:e.target.value||undefined})}/></Label>
    {["integer","decimal"].includes(input.type)&&(["min","max"] as const).map(bound=><Label key={bound} text={bound==="min"?t("Minimum"):t("Maximum")}><Input type="number" value={input.constraints?.[bound]??""} onChange={e=>onChange({constraints:{...input.constraints,[bound]:e.target.value===""?undefined:e.target.valueAsNumber}})}/><Checkbox checked={!!input.constraints?.[bound==="min"?"exclusiveMin":"exclusiveMax"]} onChange={exclusive=>onChange({constraints:{...input.constraints,[bound==="min"?"exclusiveMin":"exclusiveMax"]:exclusive}})}>{t("Exclusive bound")}</Checkbox></Label>)}
    {["text","longtext"].includes(input.type)&&<Label text={t("Maximum length")}><Input type="number" min={0} max={4096} value={input.constraints?.maxLength??""} onChange={e=>onChange({constraints:{...input.constraints,maxLength:e.target.value===""?undefined:e.target.valueAsNumber}})}/></Label>}
    {["date","datetime","integer","decimal"].includes(input.type)&&(["before","after"] as const).map(relation=><Label key={relation} text={relation==="before"?t("Before parameter"):t("After parameter")}><Select value={input.constraints?.[relation]??""} onChange={e=>onChange({constraints:{...input.constraints,[relation]:e.target.value||undefined}})}><option value="">—</option>{inputs.filter(other=>other.name!==input.name&&other.type===input.type).map(other=><option key={other.name} value={other.name}>{other.title}</option>)}</Select></Label>)}
    {(input.constraints?.before||input.constraints?.after)&&<Checkbox checked={!!input.constraints.inclusive} onChange={inclusive=>onChange({constraints:{...input.constraints,inclusive}})}>{t("Allow equal endpoints")}</Checkbox>}
    {input.type==="date"&&<><Label text={t("Date range end")}><Select value={input.range?.end??""} onChange={e=>onChange({range:e.target.value?{end:e.target.value,inclusive:input.range?.inclusive}:undefined})}><option value="">—</option>{inputs.filter(other=>other.name!==input.name&&other.type==="date"&&!other.range).map(other=><option key={other.name} value={other.name}>{other.title}</option>)}</Select></Label>{input.range&&<Checkbox checked={!!input.range.inclusive} onChange={inclusive=>onChange({range:{...input.range!,inclusive}})}>{t("Include end date")}</Checkbox>}</>}
    <Checkbox className="text-xs" checked={!!input.required} onChange={(required) => onChange({ required })}>{t("Required")}</Checkbox>
  </>;
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

