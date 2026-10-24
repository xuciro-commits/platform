import { PropertyList, ContextHelp, StructuredEditor } from "@platform/ui";
import { mixedNames } from "../editor/names";
import { ObjectLineage } from "./lineage";
import {actionDestinations,actionResultEdges,actionStateInput,setActionDestination} from "./process-rules";
import { actionInputFields, fieldTypes, nameOf, tones, type Access, type Action, type Chosen, type Field, type ObjectRecord, type ObjectScope, type Process, type State } from "./object-model";
import { ShapeEditor } from "./shape";
import { DraftStatus, ResourceControls, WorkbenchMessage, savingState } from "../editor/workbench";
// The object's process editor: its states and the actions people
// take on its records. The main tables and map own selection; the Inspector
// edits that selection, without a second copy in a structure sidebar.
// It writes the object's own record through its own action; the host checks
// everything again when the object is published.
import { PayloadFields, RoleSelect, SemanticObjectSelect, SemanticPropertyTypeSelect, semanticPropertyTypes, assetBindingKey, useHost, useReadQuery } from "@platform/app";
import {
  Button, Card, Checkbox, ContentTabs, DataTable, Disclosure, Input, FlowCanvas, Panel, ProblemList, Select, StatusBar, Textarea, Workbench, useFlowArrangement, cn, notify, t, useViewCall, type WorkbenchProblem,
  type FlowEdge, type FlowNode, type FlowCatalog,
} from "@platform/ui";
import { Boxes, Link2, Plus, Trash2, Zap } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useObjectDraft } from "./object-draft";
import { ConstraintsEditor } from "./constraints";
import { RuleCollectionsEditor } from "./RuleCollectionsEditor";
import type { Api } from "@platform/kernel";
import { TransactionEditor } from "./TransactionEditor";
import { RecordSetsEditor } from "./record-sets";
import { ObjectProtocolsEditor } from "./object-protocols";
import { BusinessVersionEditor } from "./business-version";
import { authoringObject, useAuthoringModel } from "./authoring-model";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";

/** The objects of this organisation: open one to give it states and actions. */
const objectGroups = [
  { id: "structure", title: "Object structure", sections: [{ id: "overview", title: "Overview" }, { id: "properties", title: "Properties" }, { id: "links", title: "Links" }, { id: "sets", title: "Record sets" }] },
  { id: "behavior", title: "Behavior", sections: [{ id: "lifecycle", title: "Lifecycle" }, { id: "actions", title: "Actions" }] },
  { id: "rules", title: "Rules and access", sections: [{ id: "constraints", title: "Business constraints" }, { id: "versions", title: "Business versions" }, { id: "protocols", title: "Protocols" }, { id: "permissions", title: "Permissions" }] },
  { id: "data", title: "Data and preview", sections: [{ id: "data", title: "Data" }, { id: "preview", title: "Preview" }] },
] as const;
type ObjectArea = typeof objectGroups[number]["id"];
const objectSections = objectGroups.flatMap(group => [...group.sections]);
type Tab = typeof objectSections[number]["id"];
const sectionForLegacyTab: Record<Tab, ObjectArea> = {
  overview: "structure", properties: "structure", links: "structure", sets: "structure",
  lifecycle: "behavior", actions: "behavior",
  constraints: "rules", versions: "rules", protocols: "rules", permissions: "rules",
  data: "data", preview: "data",
};
export function ObjectTypeEditor({ id, initialField, initialAction, initialAccess, initialTab }: { id: string; initialField?: string; initialAction?: string; initialAccess?: boolean; initialTab?: string }) {
  const call = useViewCall();
  const callInput = call?.input;
  const returnLabel = callInput && typeof callInput === "object" && "returnLabel" in callInput && typeof callInput.returnLabel === "string" ? callInput.returnLabel : t("Return");
  const [chosen, setChosen] = useState<Chosen>();
  const draft = useObjectDraft(id, () => setChosen(undefined));
  const { object, ready, process, dirty, busy, blocked, refused, change, issues, parent, entities, session, discardChanges, open } = draft;
  const initiallyChosen = useRef("");
  const [tab, setTab] = useState<Tab>(objectSections.find(section => section.id === initialTab)?.id ?? "overview");
  useEffect(() => {
    const key = JSON.stringify([id, initialField, initialAction, initialAccess, initialTab]);
    if (!ready || initiallyChosen.current === key) return;
    initiallyChosen.current = key;
    if (!initialField && !initialAction && !initialAccess && !initialTab) return;
    const section = objectSections.find(section => section.id === initialTab);
    setChosen(undefined);
    setTab(section?.id ?? "overview");
    if (initialField) { const at = process.fields.findIndex((field) => field.name === initialField); if (at >= 0) { setChosen({ kind: "field", at }); setTab("properties"); } }
    else if (initialAction) { const at = process.actions.findIndex((action) => action.name === initialAction); if (at >= 0) { setChosen({ kind: "action", at }); setTab("actions"); } }
    else if (initialAccess && process.access.length) { setChosen({ kind: "access", at: 0 }); setTab("permissions"); }
  }, [ready, id, initialField, initialAction, initialAccess, initialTab, process.fields, process.actions, process.access]);
  if (!object || !ready) return <Workbench storageKey="object-type" title={t("Object type")}><WorkbenchMessage>{draft.failed ? t("The object type could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const addState = () => {
    const title = process.states.length === 0 ? t("New") : t("State {n}", { n: process.states.length + 1 });
    change({ ...process, states: [...process.states, { name: nameOf("state", process.states.map((s) => s.name)), title, tone: "info" }] });
    setChosen({ kind: "state", at: process.states.length });
  };
  const addAction = () => {
    const title = t("Step {n}", { n: process.actions.length + 1 });
    const from = process.states[0]?.name;
    change({ ...process, actions: [...process.actions, { name: nameOf("action", process.actions.map((a) => a.name)), title, from: from ? [from] : [], transaction: {subject:parent,steps:[{name:"update",kind:"update",target:{type:parent,id:"$subject"},changes:[]}] } }] });
    setChosen({ kind: "action", at: process.actions.length });
  };
  const { save, publish, review, perform } = draft;
  const action = chosen?.kind === "action" ? process.actions[chosen.at] : undefined;
  const state = chosen?.kind === "state" ? process.states[chosen.at] : undefined;
  const field = chosen?.kind === "field" ? process.fields[chosen.at] : undefined;
  const problems: WorkbenchProblem[] = [...mixedNames(process).map((name) => ({ id: `mixed:${name}`, severity: "warning" as const, text: t("\"{name}\" mixes two languages. Write it in one; the other languages are set in Control Panel → Languages and words.", { name }) })), ...issues.map((message, i) => ({ id: `issue:${i}`, text: message, locate: () => { const at = process.actions.findIndex((a) => message.startsWith(`${a.title || a.name}:`)); if (at >= 0) { setChosen({ kind: "action", at }); setTab("actions"); } else if (message.startsWith(`${t("Creation transaction")}:`)) setTab("properties"); } }))];
  const choose = (next: Chosen, section?: Tab) => { setChosen(next); if (section) setTab(section); };
  const addField = () => { change({ ...process, fields: [...process.fields, { name: nameOf("field", process.fields.map((f) => f.name)), title: t("Field"), type: "text" }] }); choose({ kind: "field", at: process.fields.length }, "properties"); };
  const addAccess = () => { change({ ...process, access: [...process.access, { role: nameOf("role", process.access.map((a) => a.role)), read: "own", create: true, edit: true }] }); choose({ kind: "access", at: process.access.length }, "permissions"); };
  const references = process.fields.filter((f) => f.type === "reference");
  const incoming = entities.filter((entity) => entity.type !== parent && entity.fields.some((field) => field.type === "reference" && field.ref === parent));
  const inspector = <div className="p-2">
    {field && chosen && <FieldProperties field={field} states={process.states} reads={process.reads} others={process.fields.filter((_, at) => at !== chosen.at)} onRemove={() => { change({ ...process, fields: process.fields.filter((_, at) => at !== chosen.at) }); setChosen(undefined); }} onChange={(patch) => change({ ...process, fields: process.fields.map((f, i) => i === chosen.at ? { ...f, ...patch } : f) })} />}
    {state && chosen && <StateProperties state={state} onChange={(patch) => change({ ...process, states: process.states.map((s, i) => i === chosen.at ? { ...s, ...patch } : s) })} />}
    {chosen?.kind === "access" && process.access[chosen.at] && <AccessProperties access={process.access[chosen.at]!} fields={process.fields}
      onChange={(patch) => change({ ...process, access: process.access.map((a, i) => i === chosen.at ? { ...a, ...patch } : a) })}
      onFields={(fields) => change({ ...process, fields })} />}
    {action && chosen && <ActionSummary action={action} states={process.states} issues={draft.issuesOf(action.name).length}
      onOpen={() => void perform(async () => { if (!dirty || await save()) open({ view: "action-type", params: { id, action: action.name } }); })}
      onRemove={() => { change({ ...process, actions: process.actions.filter((_, at) => at !== chosen.at) }); setChosen(undefined); }} />}
    {!chosen && <p className="p-2 text-xs text-muted">{t("Select a property, state, action or role to edit it.")}</p>}
  </div>;
  const main: Record<Tab, ReactNode> = {
    overview: <div className="grid max-w-3xl gap-4 p-4">
      <div><h2 className="text-lg font-semibold">{object.title}</h2><p className="text-xs text-muted">{parent}</p></div>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
        {([
          ["properties", "Properties", process.fields.length], ["lifecycle", "States", process.states.length],
          ["actions", "Actions", process.actions.length], ["permissions", "Roles", process.access.length],
        ] as const).map(([section, label, count]) => <Button key={section} variant="ghost" onClick={() => setTab(section)} className="grid h-auto justify-items-start gap-1 rounded-md border border-border p-3 text-left">
          <span className="text-xs text-muted">{t(label)}</span><span className="text-xl font-semibold">{count}</span>
        </Button>)}
      </div>
      <ContextHelp title={t("Object type")}><p className="text-sm text-muted">{t("Properties describe a record; the lifecycle says which states it passes through; actions are the governed steps people take; permissions say who may read and change it. Preview shows the object as people will see it.")}</p></ContextHelp>
      <ShapeEditor process={process} parent={parent} entities={entities} source={object.extensionSource} onChange={change} />
      <fieldset className="grid gap-2"><Checkbox checked={!!process.enterpriseUse} onChange={enabled => change({ ...process, enterpriseUse: enabled ? { op: "", kind: "boolean", lit: { value: true } } : undefined })}>{t("Declare when records currently use enterprise elements")}</Checkbox>
        <ContextHelp title={t("Declare when records currently use enterprise elements")}><p className="text-xs text-muted">{t("A true result marks the current record as active use. Without a rule, every unarchived record counts. Only this record's fields are available.")}</p></ContextHelp>
        {process.enterpriseUse && <ExpressionEditor value={process.enterpriseUse} onChange={enterpriseUse => change({ ...process, enterpriseUse })} choices={[...process.fields, ...(process.states.length ? [{ name: "state", title: t("State"), type: "choice" }] : [])].filter(field => !field.readOnly).map(field => ({ value: field.name, label: field.title, type: field.type, ref: field.ref }))} />}
      </fieldset>
      {object.state === "published" && <Button className="w-fit" onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: object.name } })}>{t("Open records")}</Button>}
    </div>,
    properties: <div className="grid content-start gap-3 p-4">
      <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Properties")}</h2><Button size="sm" onClick={addField}><Plus />{t("Add a property")}</Button></div>
      <DataTable data={process.fields.map((field, at) => ({ field, at }))} getRowId={({ at }) => String(at)}
        searchable={false} height={Math.max(96, Math.min(480, 30 + process.fields.length * 36))} rowHeight={36}
        selectedId={chosen?.kind === "field" ? String(chosen.at) : undefined} onRowClick={({ at }) => choose({ kind: "field", at })}
        empty={t("No properties yet. Add one to describe the record.")} columns={[
          { id: "title", header: t("Title"), accessorFn: ({ field }) => field.title || field.name },
          { id: "name", header: t("Name"), cell: ({ row }) => <span className="font-mono text-xs">{row.original.field.name}</span> },
          { id: "type", header: t("Type"), accessorFn: ({ field }) => `${t(field.type)}${field.type === "reference" && field.ref ? ` → ${field.ref}` : ""}` },
          { id: "required", header: t("Required"), accessorFn: ({ field }) => field.required ? "✓" : "" },
          { id: "property", header: t("Shared property"), accessorFn: ({ field }) => field.property ? assetBindingKey(field.property) : "" },
        ]} />
      <RuleCollectionsEditor value={process.reads} subject={authoringObject({...process,name:object.name,title:object.title})} onChange={reads=>change({...process,reads})}/>
      <Disclosure summary={t("Creation transaction")}><div className="grid gap-3 pt-2"><ContextHelp title={t("Creation transaction")}><p className="text-xs text-muted">{t("The new subject exists in the private view. Use a sequence step and assign its result to the number field in an update step.")}</p></ContextHelp><Checkbox checked={!!process.createTransaction} onChange={enabled => change({...process,createTransaction:enabled?{subject:parent,steps:[{name:"update",kind:"update",target:{type:parent,id:"$subject"},changes:[]}]}:undefined})}>{t("Apply a transaction when creating records")}</Checkbox>{process.createTransaction&&<TransactionEditor value={process.createTransaction} onChange={createTransaction=>change({...process,createTransaction})} process={process} roles={[...new Set(["builder",...process.access.map(access=>access.role)])]}/>}</div></Disclosure>
    </div>,
    links: <div className="grid content-start gap-4 p-4">
      <div className="grid gap-2"><h2 className="text-sm font-semibold">{t("References from this object")}</h2>
        {references.length ? <ul className="divide-y divide-border rounded-md border border-border">{references.map((f) => <li key={f.name}><Button variant="row" type="button" className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-row-hover" onClick={() => choose({ kind: "field", at: process.fields.indexOf(f) }, "links")}><Link2 className="size-4 text-muted" /><span className="min-w-0 flex-1">{f.title || f.name}</span><span className="font-mono text-xs text-muted">→ {f.ref ?? t("unset")}</span></Button></li>)}</ul> : <p className="text-sm text-muted">{t("No reference properties. Add a property of type reference to link records.")}</p>}
      </div>
      <div className="grid gap-2"><h2 className="text-sm font-semibold">{t("Objects that reference this one")}</h2>
        {incoming.length ? <ul className="divide-y divide-border rounded-md border border-border">{incoming.map((entity) => <li key={entity.type} className="flex items-center gap-2 px-3 py-2 text-sm"><Boxes className="size-4 text-muted" /><span className="min-w-0 flex-1">{entity.title}</span><span className="font-mono text-xs text-muted">{entity.type}</span></li>)}</ul> : <p className="text-sm text-muted">{t("Nothing references this object yet.")}</p>}
      </div>
      <Button className="w-fit" size="sm" onClick={() => open({ view: "link-type" })}>{t("Manage link types")}</Button>
    </div>,
    actions: <div className="grid content-start gap-3 p-4">
      <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Action types")}</h2><Button size="sm" disabled={!process.states.length} onClick={() => { addAction(); }}><Plus />{t("Add an action")}</Button></div>
      {!process.states.length && <p className="text-sm text-muted">{t("Add a lifecycle state first: every action starts from a state.")}</p>}
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">{process.actions.map((a, at) => <Button variant="row" key={`${a.name}:${at}`} type="button" aria-pressed={chosen?.kind === "action" && chosen.at === at} onClick={() => choose({ kind: "action", at })}
        className={cn("grid gap-1 rounded-md border p-3 text-left hover:border-primary", chosen?.kind === "action" && chosen.at === at ? "border-primary bg-row-selected" : "border-border")}>
        <span className="flex items-center gap-2 text-sm font-medium"><Zap className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate">{a.title || a.name}</span></span>
        <span className="text-xs text-muted">{a.from.map((s) => process.states.find((x) => x.name === s)?.title ?? s).join(", ") || t("any state")} → {actionResultEdges(a).map(name=>process.states.find(state=>state.name===name)?.title??name).join(", ") || t("same state")}</span>
        <span className="text-xs text-muted">{t("{inputs} parameters · {steps} transaction steps", {inputs:a.inputs?.length??0,steps:a.transaction?.steps.length??0})}</span>
      </Button>)}</div>
    </div>,
    lifecycle: <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
      <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Lifecycle")}</h2><Button size="sm" onClick={addState}><Plus />{t("Add a state")}</Button></div>
      <ProcessGraph key={id} objectId={id} process={process} chosen={chosen} onChoose={setChosen} onChange={change} onAddState={addState} onAddAction={addAction} />
    </div>,
    protocols: <ObjectProtocolsEditor process={process} onChange={change} />,
    versions: <BusinessVersionEditor process={process} entities={entities} onChange={change} />,
    constraints: <ConstraintsEditor parent={parent} process={process} onChange={change} />,
    sets: <RecordSetsEditor process={process} parent={parent} entities={entities} onChange={change} />,
    permissions: <div className="grid content-start gap-3 p-4">
      <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Who may do what")}</h2><Button size="sm" onClick={addAccess}><Plus />{t("Add a role")}</Button></div>
      <DataTable data={process.access.map((access, at) => ({ access, at }))} getRowId={({ at }) => String(at)}
        searchable={false} height={Math.max(96, Math.min(480, 30 + process.access.length * 40))} rowHeight={40}
        selectedId={chosen?.kind === "access" ? String(chosen.at) : undefined}
        empty={t("No roles yet. Without roles only builders see these records.")} columns={[
          { id: "role", header: t("Role"), cell: ({ row: { original: { access, at } } }) => <Button variant="row" type="button" className="font-medium hover:underline" onClick={() => choose({ kind: "access", at })}>{access.role}</Button> },
          { id: "read", header: t("Read"), cell: ({ row: { original: { access, at } } }) => <Select aria-label={`${access.role} · ${t("Read")}`} value={access.read} onChange={(event) => change({ ...process, access: process.access.map((entry, index) => index === at ? { ...entry, read: event.target.value as Access["read"] } : entry) })}>{readLevels.map((level) => <option key={level} value={level}>{t(level)}</option>)}</Select> },
          ...(["create", "edit", "archive"] as const).map((key) => ({ id: key, header: t(key), cell: ({ row: { original: { access, at } } }: { row: { original: { access: Access; at: number } } }) => <Checkbox ariaLabel={`${access.role} · ${t(key)}`} checked={!!access[key]} onChange={(checked) => change({ ...process, access: process.access.map((entry, index) => index === at ? { ...entry, [key]: checked } : entry) })} /> })),
        ]} />
      {process.access.length > 0 && <ScopeFields process={process} onChange={(scope) => change({ ...process, scope })} />}
    </div>,
    data: <div className="grid max-w-4xl content-start gap-3 p-4"><ObjectLineage object={`build.${object.name}`} fields={process.fields.map((f) => ({ name: f.name, title: f.title || f.name }))} /></div>,
    preview: <div className="min-h-0 flex-1 overflow-auto p-3"><Preview object={object} process={process} action={action} /></div>,
  };
  return <Workbench storageKey="object-type" crumbs={[{ label: t("Object types"), onClick: () => open({ view: "object-type" }) }]} title={object.title}
    status={<DraftStatus state={object.state} problems={issues.length} />} saving={savingState(dirty, busy, refused)}
    history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: () => { session.undo(); setChosen(undefined); }, redo: () => { session.redo(); setChosen(undefined); } }}
    actions={<>{call?.returnValue && <Button disabled={busy} variant="ghost" onClick={() => call.returnValue?.(undefined)}>{returnLabel}</Button>}<ResourceControls type="build.object" record={object} dirty={dirty} busy={busy} invalid={issues.length > 0} onSave={() => void perform(save)} onReview={() => void perform(review)} onInstall={() => void perform(publish)} onCancel={discardChanges} route={{ view: "object-type", params: { id } }} /></>}
    right={{ label: t("Object inspector"),scope:JSON.stringify(chosen),locate:path=>{const scope=path?.split("/").find(part=>part.startsWith("{"));setChosen(scope?JSON.parse(scope) as Chosen:chosen);}, content: <fieldset disabled={blocked} className="contents">{inspector}</fieldset> }}
    dock={{ label: t("Object dock"), tabs: [{ id: "problems", title: t("Problems"), badge: issues.length, content: <ProblemList problems={problems} empty={t("No problems. The object type can be published.")} /> }] }}>
    {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {refused}</Panel>}
    <StructuredEditor {...draft.structured}><ContentTabs compact wrap label={t("Object areas")} value={sectionForLegacyTab[tab]} onChange={value => { const group = objectGroups.find(group => group.id === value); if (group) setTab(group.sections[0].id); }} items={objectGroups.map(group => ({
      id: group.id, title: t(group.title), content: <ContentTabs compact wrap label={t("Object sections")} value={group.sections.some(section => section.id === tab) ? tab : group.sections[0].id} onChange={value => setTab(value as Tab)} items={group.sections.map(section => ({
        id: section.id, title: t(section.title), content: <fieldset disabled={busy || blocked} className="flex h-full min-h-0 min-w-0 flex-col">{main[section.id]}</fieldset>,
      }))} />,
    }))} /></StructuredEditor>
  </Workbench>;
}

/** Enterprise types come from the original metamodel read, not a copied model. */
export function EnterpriseReferenceTypes({ value, onChange }: { value: { stereotype?: string; stereotypes?: string[] }; onChange: (patch: { stereotype?: string; stereotypes?: string[] }) => void }) {
  const metadata = useReadQuery<{ profile: { stereotype: string }[] }>("/v1/enterprise-metamodel");
  const supported = ["ActualOrganization", "ActualPerson", "ActualPost", "ActualLocation"];
  const available = supported.filter(stereotype => metadata.data?.profile.some(item => item.stereotype === stereotype));
  const selected = [...new Set([value.stereotype, ...value.stereotypes ?? []].filter((entry): entry is string => !!entry))];
  const labels: Record<string, string> = { ActualOrganization: "Organisation", ActualPerson: "Person", ActualPost: "Post", ActualLocation: "Location" };
  return <fieldset className="grid gap-1 text-xs"><legend className="mb-1 font-medium">{t("Enterprise element types")}</legend>
    <ContextHelp title={t("Enterprise element types")}><p className="text-muted">{t("Choose the enterprise families this reference accepts. No selection accepts every readable enterprise element.")}</p></ContextHelp>
    {[...new Set([...available, ...selected])].map(stereotype => <Checkbox key={stereotype} checked={selected.includes(stereotype)} onChange={checked => { const next = checked ? [...selected, stereotype] : selected.filter(entry => entry !== stereotype); onChange(next.length === 1 ? { stereotype: next[0], stereotypes: undefined } : { stereotype: undefined, stereotypes: next.length ? next : undefined }); }}>{t(labels[stereotype] ?? stereotype)}</Checkbox>)}
    {metadata.isError && <p role="alert" className="text-danger">{t("Enterprise element types could not be loaded.")}</p>}
  </fieldset>;
}

/** The lifecycle is the semantic source. Canvas edges only edit its From/To declarations. */
function ProcessGraph({ objectId, process, chosen, onChoose, onChange, onAddState, onAddAction }: {
  objectId: string;
  process: Process; chosen: Chosen; onChoose: (c: Chosen) => void; onChange: (p: Process) => void;
  onAddState: () => void; onAddAction: () => void;
}) {
  const catalog: FlowCatalog = [
    { id: "state", title: t("State"), class: "lifecycle", description: t("A record's named status"),
      inputs: [{ id: "result", label: t("Arrives here"), type: "action-result" }],
      outputs: [{ id: "take", label: t("May take"), type: "action-start" }] },
    { id: "action", title: t("Action"), class: "action", description: t("A governed step on one record"),
      inputs: [{ id: "from", label: t("Taken from"), type: "action-start" }],
      outputs: [{ id: "to", label: t("Leaves it in"), type: "action-result", limit: 1 }] },
  ];
  const normal=catalog[1]!;const graphCatalog:FlowCatalog=[...catalog,{...normal,id:"action-input",addable:false,outputs:normal.outputs.map(p=>({...p,limit:undefined}))}];
  const edges: FlowEdge[] = process.actions.flatMap((a) => [
    ...a.from.map((s) => ({ id: `from:${a.name}:${s}`, source: `state:${s}`, sourcePort: "take", target: `action:${a.name}`, targetPort: "from" })),
    ...actionResultEdges(a).map(to=>({id:`to:${a.name}:${to}`,source:`action:${a.name}`,sourcePort:"to",target:`state:${to}`,targetPort:"result",dashed:!!actionStateInput(a),label:actionStateInput(a)})),
  ]);
  // One layout brain decides where a state or an action sits: the
  // same arrangement the canvas's own tidy-up would choose, sizes included.
  const places = useFlowArrangement([
    ...process.states.map((s) => ({ id: `state:${s.name}`, kind: "state", label: s.title || s.name, position: { x: 0, y: 0 } })),
    ...process.actions.map((a) => ({ id: `action:${a.name}`, kind: actionStateInput(a) ? "action-input" : "action", label: a.title || a.name, position: { x: 0, y: 0 } })),
  ], edges, "right", graphCatalog);
  const nodes: FlowNode[] = [
    ...process.states.map((s) => ({ id: `state:${s.name}`, kind: "state", label: s.title || s.name, detail: s.name, position: places[`state:${s.name}`] ?? { x: 0, y: 0 } })),
    ...process.actions.map((a) => ({ id: `action:${a.name}`, kind:actionStateInput(a)?"action-input":"action", label: a.title || a.name, detail: a.name, position: places[`action:${a.name}`] ?? { x: 0, y: 0 } })),
  ];
  const selected = chosen?.kind === "state" ? `state:${process.states[chosen.at]?.name}` : chosen?.kind === "action" ? `action:${process.actions[chosen.at]?.name}` : undefined;
  return <div className="mb-4 grid gap-2">
    <div><h3 className="text-sm font-semibold">{t("Process map")}</h3>
      <ContextHelp title={t("States and actions")}><p className="text-xs text-muted">{t("Connect a state to an action to allow it; connect an action to a state for its result. Select a node to edit it. Delete a selected line to remove it.")}</p></ContextHelp></div>
    <FlowCanvas label={t("Process map")} catalog={graphCatalog} nodes={nodes} edges={edges} selected={selected} storeKey={`process:${objectId}`}
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
          if(actionStateInput(process.actions.find(a=>a.name===name)!)){notify.info(t("Edit the state input choices to change these derived connections."));return;}
          onChange({ ...process, actions: process.actions.map((a) => a.name === name ? setActionDestination(a,state,a.transaction!.subject) : a) });
        }
      }}
      onDelete={(removedNodes, removedEdges) => {
        if(removedEdges.some(e=>process.actions.some(a=>actionStateInput(a)&&e.source===`action:${a.name}`)))notify.info(t("Edit the state input choices to change these derived connections."));
        const states = process.states.filter((state) => !removedNodes.some((n) => n.id === `state:${state.name}`));
        const actions = process.actions.filter(action => !removedNodes.some(node => node.id === `action:${action.name}`)).map(action => {
          const keep = actionStateInput(action) || actionResultEdges(action).every(name => states.some(state => state.name === name) && !removedEdges.some(edge => edge.source === `action:${action.name}` && edge.target === `state:${name}`));
          return {...keep?action:setActionDestination(action,undefined,action.transaction!.subject),from:action.from.filter(name=>states.some(state=>state.name===name)&&!removedEdges.some(edge=>edge.source===`state:${name}`&&edge.target===`action:${action.name}`))};
        });
        onChoose(undefined); onChange({ ...process, states, actions });
      }}
      onDisconnect={(removed) => {
        if(removed.some(e=>process.actions.some(a=>actionStateInput(a)&&e.source===`action:${a.name}`)))notify.info(t("Edit the state input choices to change these derived connections."));
        onChange({...process,actions:process.actions.map(action=>({...actionStateInput(action)||!removed.some(edge=>edge.source===`action:${action.name}`)?action:setActionDestination(action,undefined,action.transaction!.subject),from:action.from.filter(name=>!removed.some(edge=>edge.source===`state:${name}`&&edge.target===`action:${action.name}`))}))});
      }} />
  </div>;
}


function FieldProperties({ field, others, reads, states, onChange, onRemove }: { states: State[]; reads?: Api.RuleReadSpec; field: Field; others: Field[]; onChange: (patch: Partial<Field>) => void; onRemove: () => void }) {
  const {definitions}=useHost();
  const { drafts, entities } = useAuthoringModel();
  const objects = (drafts.data?.records ?? []).filter(draft => !draft.archived).map(draft => ({ name: `build.${draft.name}`, title: draft.title }));
  const expressionType = field.type === "longtext" ? "text" : field.type;
  const computedChoices: ExpressionChoice[] = [{ value: "id", label: t("Record ID"), type: "text" }, { value: "caller.id", label: t("The person taking it"), type: "text" }, ...others.map(other => ({ value: other.name, label: other.title, type: other.type === "longtext" ? "text" : other.type, ref: other.ref })), ...(states.length ? [{value:"state",label:t("State"),type:"choice"}] : [])];
  // "Only when" (#138): another unconditional choice or boolean field, and the values that make this one apply.
  const conditions = others.filter((f) => (f.type === "choice" || f.type === "boolean") && !f.when && f.name);
  const [whenField, whenValues] = (field.when ?? "").split("=") as [string, string | undefined];
  const on = conditions.find((f) => f.name === whenField);
  const allowed = on ? (on.type === "boolean" ? ["true", "false"] : (on.choices ?? "").split(",").map((c) => c.trim()).filter(Boolean)) : [];
  const picked = (whenValues ?? "").split(",").filter(Boolean);
  const bound=field.property?semanticPropertyTypes(definitions).find(p=>assetBindingKey(p.binding)===assetBindingKey(field.property!)):undefined;
  return <Card className="grid content-start gap-3 p-3">
    <div className="text-xs font-semibold text-muted">{t("Field")}</div>
    <Label text={t("Shared property version")}><SemanticPropertyTypeSelect label={t("Shared property version")} value={field.property} filter={p=>p.definition.source==="tenant"&&p.binding.ref.app==="build"} onChange={p=>onChange(p?{property:p.binding,type:p.property.type,title:p.property.title,choices:undefined,ref:undefined,inverse:undefined}:{property:undefined})}/></Label>
    {field.property&&<><p className="break-all text-xs text-muted">{field.property.ref.name}@{field.property.sourceVersion}</p><ContextHelp title={t("Shared property")}><p className="text-xs text-muted">{t("The shared source controls type and title. Local names, required values and access stay with this object.")}</p></ContextHelp>{!bound&&<p role="alert" className="text-xs text-danger">{t("The bound shared property version is unavailable.")}</p>}</>}
    <Label text={t("What people call it")}><Input disabled={!!field.property} value={field.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
    <Label text={t("Name")}><Input className="font-mono" value={field.name} onChange={(e) => onChange({ name: e.target.value })} /></Label>
    <Label text={t("Type")}><Select disabled={!!field.property} value={field.type} onChange={(e) => onChange({ type: e.target.value, ref:undefined, inverse:undefined, stereotype:undefined, stereotypes:undefined })}>{fieldTypes.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select></Label>
    {field.type === "choice" && <Label text={t("Choices")}><Input value={field.choices ?? ""} onChange={(e) => onChange({ choices: e.target.value })} /></Label>}
    {field.type === "reference" && <Label text={t("Reference object")}><SemanticObjectSelect enterprise label={t("Reference object")} value={field.ref} drafts={objects} onChange={(ref) => onChange({ ref: ref?.name, inverse: undefined, stereotype:undefined, stereotypes:undefined })} /></Label>}
    {field.type === "reference" && field.ref === "enterprise.element" && <EnterpriseReferenceTypes value={field} onChange={onChange} />}
    {field.type === "reference" && field.ref !== "enterprise.element" && <Label text={t("Seen from there as")}><Input className="font-mono" placeholder="visits" value={field.inverse ?? ""} onChange={(e) => onChange({ inverse: e.target.value || undefined })} /></Label>}
    {field.formula && <Label text={t("Computed by formula")}><Input className="font-mono" value={field.formula} onChange={(e) => onChange({ formula: e.target.value || undefined, formulaExpr: undefined })} /></Label>}
    {["text", "choice", "reference", "boolean", "integer", "decimal", "money", "date", "datetime"].includes(expressionType) && !field.formula && <Checkbox disabled={!!field.aliasOf} checked={!!field.formulaExpr} onChange={enabled => onChange({ formulaExpr: enabled ? literalExpression(expressionType) : undefined, formula: undefined })}>{t("Computed by expression")}</Checkbox>}
    {field.formulaExpr && <ExpressionEditor draftKey={`field:${field.name}:formula`} collections={reads?.collections} value={field.formulaExpr} kind={expressionType} choices={computedChoices} lookup={type => entities.find(entity => entity.type === type)} onChange={formulaExpr => onChange({ formulaExpr, formula: undefined })} />}
    {(field.formula||field.formulaExpr) && <ContextHelp title={t("Calculated value")}><p className="text-xs text-muted">{t("Computed by the declared expression at every change; nobody sets it by hand.")}</p></ContextHelp>}
    <Checkbox checked={!!field.required} onChange={(required) => onChange({ required })}>{t("Required")}</Checkbox>
    <Checkbox checked={!!field.search} onChange={(search) => onChange({ search })}>{t("Searchable")}</Checkbox>
    <Checkbox disabled={!!field.aliasOf} checked={!!field.readOnly || !!field.aliasOf} onChange={(readOnly) => onChange({ readOnly })}>{t("Read only")}</Checkbox>
    <Label text={t("Read alias of")}><Select aria-label={t("Read alias of")} value={field.aliasOf ?? ""} onChange={(event) => onChange({ aliasOf: event.target.value || undefined, readOnly: !!event.target.value || field.readOnly })}>
      <option value="">{t("No read alias")}</option>
      {others.filter(other => !other.aliasOf && other.type === field.type && (other.ref ?? "") === (field.ref ?? "") && !!other.required === !!field.required).map(other => <option key={other.name} value={other.name}>{other.title} · {other.name}</option>)}
    </Select><span className="text-xs text-muted">{t("Keep an old field name readable after migration. It follows the selected field and cannot be edited directly.")}</span></Label>
	<Label text={t("AI context policy")}><Select value={field.aiInput===false?"deny":field.aiInput===true?"allow":"declared"} onChange={event=>onChange({aiInput:event.target.value==="declared"?undefined:event.target.value==="allow"})}><option value="declared">{t("Only fields selected by a function")}</option><option value="allow">{t("Allow declared AI functions")}</option><option value="deny">{t("Exclude from AI context")}</option></Select></Label>
 {["text","longtext","integer","decimal","boolean","reference","choice"].includes(field.type)&&<Label text={t("Text source type")}><Select value={field.sourceType??""} onChange={event=>onChange({sourceType:event.target.value||undefined})}><option value="">{t("Undeclared source")}</option><option value="record">{t("Original record")}</option><option value="user-input">{t("User input")}</option><option value="external-input">{t("External input")}</option><option value="generated">{t("Generated from declared sources")}</option></Select><ContextHelp title={t("Text source type")}><p className="text-xs text-muted">{t("Undeclared text is withheld from derived reads. Generated text also requires record provenance.")}</p></ContextHelp></Label>}
    {conditions.length > 0 && <Label text={t("Only when")}>
      <Select value={on?.name ?? ""} onChange={(e) => onChange({ when: e.target.value ? `${e.target.value}=` : undefined })}>
        <option value="">{t("Always")}</option>{conditions.map((f) => <option key={f.name} value={f.name}>{f.title || f.name}</option>)}
      </Select>
      {on && <span className="mt-1 flex flex-wrap gap-2">{allowed.map((v) => <Checkbox key={v} checked={picked.includes(v)} onChange={(c) => onChange({ when: `${on.name}=${(c ? [...picked, v] : picked.filter((x) => x !== v)).join(",")}` })}>{t(v)}</Checkbox>)}</span>}
      {on && picked.length === 0 && <span className="text-xs text-danger">{t("Pick at least one value.")}</span>}
      {on && <span className="text-xs text-muted">{t("The field is asked for — and required, if marked so — only while {field} is one of these; otherwise it stays empty.", { field: on.title || on.name })}</span>}
    </Label>}
    <Button variant="danger" onClick={onRemove}>{t("Remove field")}</Button>
  </Card>;
}

/** The left pane: the states in order (a new record starts in the first), then the actions. */

/** The middle pane: the status bar a record will show, and the chosen action's form, as people meet them. */
function Preview({ object, process, action }: { object: ObjectRecord; process: Process; action?: Action }) {
  const [values, setValues] = useState<Record<string, unknown>>({});
  if (process.states.length === 0) return <Panel role="status" className="text-sm text-muted">{t("Add a state to see what people will see.")}</Panel>;
  const lifecycle = {
    field: "state", initial: process.states[0]!.name,
    states: process.states.map((s) => ({ name: s.name, title: s.title, tone: s.tone as never, description: s.description })),
    transitions: process.actions.map((a) => ({ name: a.name, schema: `build.${object.name}.${a.name}`, title: a.title, from: a.from, to: actionDestinations(a) })),
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
          to: actionResultEdges(action).map(name=>process.states.find(state=>state.name===name)?.title??name).join(", ") || t("where it was"),
        })}</p>
        {action.approval && <p className="text-xs text-muted">{t("Waits in {state} for {levels}", {
          state: process.states.find((s) => s.name === action.approval?.pending)?.title ?? action.approval.pending,
          levels: action.approval.levels.map((level) => `${level.title} (${level.role})`).join(" → "),
        })}</p>}
        <PayloadFields preview values={values} onChange={setValues} fields={actionInputFields(action.inputs ?? [])} />
        {action.transaction?.message&&<p className="text-xs text-muted">{action.transaction.message}</p>}
      </Card>}
    </div>
  );
}

export const Label = ({ text, children }: { text: string; children: ReactNode }) => <label className="grid gap-1 text-xs">{text}{children}</label>;

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

const reads = { all: () => t("Every record"), below: () => t("Those of their units and the units below"), unit: () => t("Those of their own units"), own: () => t("Only their own"), none: () => t("Not at all") };
const readLevels = ["all", "below", "unit", "own", "none"] as const;

/** Which fields place a record for the row scopes above: owner for "own", unit + structure for "unit" and "below". */
function ScopeFields({ process, onChange }: { process: Process; onChange: (scope: ObjectScope) => void }) {
  const scope = process.scope ?? {};
  const candidates = process.fields.filter((f) => f.type === "text" || f.type === "reference");
  const byUnit = process.access.some((a) => a.read === "unit" || a.read === "below");
  const set = (patch: ObjectScope) => { const next = { ...scope, ...patch }; onChange(Object.fromEntries(Object.entries(next).filter(([, v]) => v)) as ObjectScope); };
  const pick = (value: string | undefined, onPick: (v: string | undefined) => void, empty: string) => <Select value={value ?? ""} onChange={(e) => onPick(e.target.value || undefined)}>
    <option value="">{empty}</option>{candidates.map((f) => <option key={f.name} value={f.name}>{f.title || f.name}{f.type === "reference" && f.ref ? ` → ${f.ref}` : ""}</option>)}</Select>;
  return <Card className="grid gap-3 p-3">
    <div><h3 className="text-sm font-semibold">{t("What places a record")}</h3><ContextHelp title={t("What places a record")}><p className="text-xs text-muted">{t("Row scopes read these fields: own = the owner field (or whoever created it); unit and below = the unit field, following a structure of the organisation.")}</p></ContextHelp></div>
    <div className="grid gap-3 sm:grid-cols-3">
      <Label text={t("Owner field")}>{pick(scope.owner, (owner) => set({ owner }), t("Whoever created it"))}</Label>
      <Label text={t("Unit field")}>{pick(scope.unit, (unit) => set({ unit }), byUnit ? t("Required for unit and below") : t("None"))}</Label>
      <Label text={t("Structure")}><Input placeholder="management" value={scope.structure ?? ""} onChange={(e) => set({ structure: e.target.value || undefined })} /></Label>
    </div>
    {byUnit && (!scope.unit || !scope.structure) && <p role="alert" className="text-xs text-danger">{t("A role reads by unit: choose the unit field and name the structure, or publishing is refused.")}</p>}
  </Card>;
}

/** The left pane's third part: the roles of the builder app this object names, and what each may do. */

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
      <Label text={t("Role in the builder app")}><RoleSelect app="build" value={access.role} onChange={(role) => onChange({ role })} /></Label>
      <Label text={t("Which records it reads")}>
        <Select value={access.read} onChange={(e) => {
          const read = e.target.value as Access["read"];
          onChange(read === "none" ? { read, create: false, edit: false, archive: false } : { read });
        }}>
          {readLevels.map((r) => <option key={r} value={r}>{reads[r]()}</option>)}
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

/** The object inspector's view of an action: what it is and where it goes; everything else is edited in its own workbench. */
function ActionSummary({ action, states, issues, onOpen, onRemove }: { action: Action; states: State[]; issues: number; onOpen: () => void; onRemove: () => void }) {
  const named = (name?: string) => states.find((s) => s.name === name)?.title ?? name;
  return <Card className="grid content-start gap-2 p-3 text-sm">
    <div className="text-xs font-semibold text-muted">{t("Action type")}</div>
    <div className="font-medium">{action.title || action.name}</div>
    {action.description && <p className="text-xs text-muted">{action.description}</p>}
    <p className="text-xs text-muted">{action.from.map(named).join(", ") || t("any state")} → {actionResultEdges(action).map(named).join(", ") || t("same state")}</p>
    <PropertyList columns={2} items={[[t("Parameters"), action.inputs?.length ?? 0], [t("Transaction steps"), action.transaction?.steps.length ?? 0]]} />
    {issues > 0 && <p className="text-xs text-danger">{t("{n} problems", { n: issues })}</p>}
    <div className="flex gap-2"><Button size="sm" variant="primary" onClick={onOpen}><Zap />{t("Open action type")}</Button><Button size="sm" variant="ghost" onClick={onRemove}><Trash2 />{t("Remove")}</Button></div>
  </Card>;
}
