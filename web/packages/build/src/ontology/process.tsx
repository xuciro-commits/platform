import { ObjectLineage } from "./lineage";
import {actionDestinations,actionResultEdges} from "./process-rules";
import { fieldTypes, nameOf, tones, type Access, type Action, type Chosen, type Field, type ObjectRecord, type Numbering_, type ObjectScope, type Process, type State } from "./object-model";
import { ShapeEditor } from "./shape";
import { DraftStatus, PublishMenu, WorkbenchMessage, savingState } from "../editor/workbench";
// The object's process editor (ADR-0037): its states and the actions people
// take on its records, in the page editor's three panes — what there is on the
// left, what a person will see in the middle, the piece in hand on the right.
// It writes the object's own record through its own action; the host checks
// everything again when the object is published.
import { PayloadFields, SemanticObjectSelect, SemanticPropertyTypeSelect, semanticPropertyTypes, assetBindingKey, useHost } from "@platform/app";
import {
  Button, Card, Checkbox, Input, NodeCanvas, Panel, PanelSection, ProblemList, Select, StatusBar, StructureRow, Textarea, Workbench, canvasNodeHeight, canvasNodeWidth, cn, layout, notify, t, type WorkbenchProblem,
  type CanvasEdge, type CanvasNode, type NodeCatalog,
} from "@platform/ui";
import { Boxes, Link2, Plus, Shield, Tags, Trash2, Zap } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useObjectDraft } from "./object-draft";

/** The objects of this organisation: open one to give it states and actions. */
type Tab = "overview" | "properties" | "links" | "actions" | "lifecycle" | "permissions" | "data" | "preview";
export function ObjectTypeEditor({ id, initialField, initialAction, initialAccess, initialTab }: { id: string; initialField?: string; initialAction?: string; initialAccess?: boolean; initialTab?: string }) {
  const [chosen, setChosen] = useState<Chosen>();
  const draft = useObjectDraft(id, () => setChosen(undefined));
  const { object, process, dirty, busy, refused, change, issues, parent, entities, session, discardChanges, open } = draft;
  const initiallyChosen = useRef("");
  const [tab, setTab] = useState<Tab>((["overview", "properties", "links", "actions", "lifecycle", "permissions", "data", "preview"] as Tab[]).includes(initialTab as Tab) ? initialTab as Tab : "overview");
  useEffect(() => {
    const key = `${id}:${initialField ?? ""}:${initialAction ?? ""}:${initialAccess ?? false}`;
    if (!object || initiallyChosen.current === key) return;
    initiallyChosen.current = key;
    if (initialField) { const at = object.fields.findIndex((field) => field.name === initialField); if (at >= 0) { setChosen({ kind: "field", at }); setTab("properties"); } }
    else if (initialAction) { const at = object.actions?.findIndex((action) => action.name === initialAction) ?? -1; if (at >= 0) { setChosen({ kind: "action", at }); setTab("actions"); } }
    else if (initialAccess && object.access?.length) { setChosen({ kind: "access", at: 0 }); setTab("permissions"); }
  }, [object, id, initialField, initialAction, initialAccess]);
  if (!object) return <Workbench storageKey="object-type" title={t("Object type")}><WorkbenchMessage>{draft.failed ? t("The object type could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
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
  const { publish, review, perform } = draft;
  const action = chosen?.kind === "action" ? process.actions[chosen.at] : undefined;
  const state = chosen?.kind === "state" ? process.states[chosen.at] : undefined;
  const field = chosen?.kind === "field" ? process.fields[chosen.at] : undefined;
  const problems: WorkbenchProblem[] = issues.map((message, i) => ({ id: `issue:${i}`, text: message, locate: () => { const at = process.actions.findIndex((a) => message.startsWith(`${a.title || a.name}:`)); if (at >= 0) { setChosen({ kind: "action", at }); setTab("actions"); } } }));
  const choose = (next: Chosen, section?: Tab) => { setChosen(next); if (section) setTab(section); };
  const addField = () => { change({ ...process, fields: [...process.fields, { name: nameOf("field", process.fields.map((f) => f.name)), title: t("Field"), type: "text" }] }); choose({ kind: "field", at: process.fields.length }, "properties"); };
  const addAccess = () => { change({ ...process, access: [...process.access, { role: nameOf("role", process.access.map((a) => a.role)), read: "own", create: true, edit: true }] }); choose({ kind: "access", at: process.access.length }, "permissions"); };
  const references = process.fields.filter((f) => f.type === "reference");
  const incoming = entities.filter((entity) => entity.type !== parent && entity.fields.some((field) => field.type === "reference" && field.ref === parent));
  const tabs: { id: Tab; title: string }[] = [
    { id: "overview", title: t("Overview") }, { id: "properties", title: t("Properties") }, { id: "links", title: t("Links") }, { id: "actions", title: t("Actions") },
    { id: "lifecycle", title: t("Lifecycle") }, { id: "permissions", title: t("Permissions") }, { id: "data", title: t("Data") }, { id: "preview", title: t("Preview") },
  ];
  const structure = <div className="grid min-w-0 content-start">
    <PanelSection title={t("Object type")}>
      <StructureRow icon={<Boxes />} label={object.title} selected={!chosen && tab === "overview"} onClick={() => choose(undefined, "overview")} />
    </PanelSection>
    <PanelSection title={t("Properties")} actions={<Button size="sm" variant="ghost" aria-label={t("Add a property")} title={t("Add a property")} onClick={addField}><Plus /></Button>}>
      {process.fields.map((f, at) => <StructureRow key={`${f.name}:${at}`} depth={1} icon={<Tags />} label={f.title || f.name} meta={t(f.type)} selected={chosen?.kind === "field" && chosen.at === at} onClick={() => choose({ kind: "field", at }, "properties")} />)}
      {!process.fields.length && <p className="px-2 text-[11px] text-muted">{t("No properties yet.")}</p>}
    </PanelSection>
    <PanelSection title={t("Lifecycle")} actions={<Button size="sm" variant="ghost" aria-label={t("Add a state")} title={t("Add a state")} onClick={() => { addState(); setTab("lifecycle"); }}><Plus /></Button>}>
      {process.states.map((s, at) => <StructureRow key={`${s.name}:${at}`} depth={1} icon={<span className={cn("size-2 rounded-full", `bg-[var(--tone-${s.tone ?? "info"})]`)} />} label={s.title || s.name} selected={chosen?.kind === "state" && chosen.at === at} onClick={() => choose({ kind: "state", at }, "lifecycle")} />)}
    </PanelSection>
    <PanelSection title={t("Actions")} actions={<Button size="sm" variant="ghost" aria-label={t("Add an action")} title={t("Add an action")} disabled={!process.states.length} onClick={() => { addAction(); setTab("actions"); }}><Plus /></Button>}>
      {process.actions.map((a, at) => <StructureRow key={`${a.name}:${at}`} depth={1} icon={<Zap />} label={a.title || a.name} meta={a.to ? `→ ${process.states.find((s) => s.name === a.to)?.title ?? a.to}` : undefined} selected={chosen?.kind === "action" && chosen.at === at} onClick={() => choose({ kind: "action", at }, "actions")} />)}
    </PanelSection>
    <PanelSection title={t("Permissions")} actions={<Button size="sm" variant="ghost" aria-label={t("Add a role")} title={t("Add a role")} onClick={addAccess}><Plus /></Button>}>
      {process.access.map((a, at) => <StructureRow key={`${a.role}:${at}`} depth={1} icon={<Shield />} label={a.role} meta={t(a.read)} selected={chosen?.kind === "access" && chosen.at === at} onClick={() => choose({ kind: "access", at }, "permissions")} />)}
    </PanelSection>
  </div>;
  const inspector = <div className="p-2">
    {field && chosen && <FieldProperties field={field} others={process.fields.filter((_, at) => at !== chosen.at)} onRemove={() => { change({ ...process, fields: process.fields.filter((_, at) => at !== chosen.at) }); setChosen(undefined); }} onChange={(patch) => change({ ...process, fields: process.fields.map((f, i) => i === chosen.at ? { ...f, ...patch } : f) })} />}
    {state && chosen && <StateProperties state={state} onChange={(patch) => change({ ...process, states: process.states.map((s, i) => i === chosen.at ? { ...s, ...patch } : s) })} />}
    {chosen?.kind === "access" && process.access[chosen.at] && <AccessProperties access={process.access[chosen.at]!} fields={process.fields}
      onChange={(patch) => change({ ...process, access: process.access.map((a, i) => i === chosen.at ? { ...a, ...patch } : a) })}
      onFields={(fields) => change({ ...process, fields })} />}
    {action && chosen && <ActionSummary action={action} states={process.states} issues={draft.issuesOf(action.name).length}
      onOpen={() => open({ view: "action-type", params: { id, action: action.name } })}
      onRemove={() => { change({ ...process, actions: process.actions.filter((_, at) => at !== chosen.at) }); setChosen(undefined); }} />}
    {!chosen && <p className="p-2 text-xs text-muted">{t("Select a property, state, action or role to edit it.")}</p>}
  </div>;
  const main: Record<Tab, ReactNode> = {
    overview: <div className="grid max-w-3xl gap-4 p-4">
      <div><h2 className="text-lg font-semibold">{object.title}</h2><p className="text-xs text-muted">{parent}</p></div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
        {[[t("Properties"), process.fields.length], [t("States"), process.states.length], [t("Actions"), process.actions.length], [t("Roles"), process.access.length]].map(([label, n]) => <div key={String(label)} className="rounded-md border border-border p-3"><dt className="text-xs text-muted">{label}</dt><dd className="text-xl font-semibold">{n}</dd></div>)}
      </dl>
      <p className="text-sm text-muted">{t("Properties describe a record; the lifecycle says which states it passes through; actions are the governed steps people take; permissions say who may read and change it. Preview shows the object as people will see it.")}</p>
      <ShapeEditor process={process} parent={parent} entities={entities} onChange={change} />
      {object.state === "published" && <Button className="w-fit" onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: object.name } })}>{t("Open records")}</Button>}
    </div>,
    properties: <div className="grid content-start gap-3 p-4">
      <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Properties")}</h2><Button size="sm" onClick={addField}><Plus />{t("Add a property")}</Button></div>
      <table className="w-full text-sm"><thead><tr className="text-left text-xs text-muted"><th className="px-2 py-1 font-medium">{t("Title")}</th><th className="px-2 py-1 font-medium">{t("Name")}</th><th className="px-2 py-1 font-medium">{t("Type")}</th><th className="px-2 py-1 font-medium">{t("Required")}</th><th className="px-2 py-1 font-medium">{t("Shared property")}</th></tr></thead>
        <tbody>{process.fields.map((f, at) => <tr key={`${f.name}:${at}`} aria-selected={chosen?.kind === "field" && chosen.at === at} className={cn("cursor-pointer border-t border-border hover:bg-row-hover", chosen?.kind === "field" && chosen.at === at && "bg-row-selected")} onClick={() => choose({ kind: "field", at })}>
          <td className="px-2 py-1.5">{f.title || f.name}</td><td className="px-2 py-1.5 font-mono text-xs">{f.name}</td><td className="px-2 py-1.5">{t(f.type)}{f.type === "reference" && f.ref ? ` → ${f.ref}` : ""}</td><td className="px-2 py-1.5">{f.required ? "✓" : ""}</td><td className="px-2 py-1.5 text-xs text-muted">{f.property ? assetBindingKey(f.property) : ""}</td>
        </tr>)}</tbody></table>
      {!process.fields.length && <p className="text-sm text-muted">{t("No properties yet. Add one to describe the record.")}</p>}
      {process.fields.some((f) => f.type === "text") && <NumberingFields process={process} onChange={(numbering) => change({ ...process, numbering })} />}
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
        <span className="text-[11px] text-muted">{a.from.map((s) => process.states.find((x) => x.name === s)?.title ?? s).join(", ") || t("any state")} → {a.to ? process.states.find((s) => s.name === a.to)?.title ?? a.to : a.toInput ? t("chosen by input") : t("same state")}</span>
        <span className="text-[11px] text-muted">{t("{inputs} parameters · {rules} rules · {conditions} criteria", { inputs: a.inputs?.length ?? 0, rules: (a.sets?.length ?? 0) + (a.creates?.length ?? 0), conditions: a.conditions?.length ?? 0 })}</span>
      </Button>)}</div>
    </div>,
    lifecycle: <div className="min-h-0 flex-1 overflow-auto p-3"><ProcessGraph key={id} process={process} chosen={chosen} onChoose={setChosen} onChange={change} onAddState={addState} onAddAction={addAction} /></div>,
    permissions: <div className="grid content-start gap-3 p-4">
      <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Who may do what")}</h2><Button size="sm" onClick={addAccess}><Plus />{t("Add a role")}</Button></div>
      <table className="w-full text-sm"><thead><tr className="text-left text-xs text-muted"><th className="px-2 py-1 font-medium">{t("Role")}</th><th className="px-2 py-1 font-medium">{t("Read")}</th><th className="px-2 py-1 font-medium">{t("Create")}</th><th className="px-2 py-1 font-medium">{t("Edit")}</th><th className="px-2 py-1 font-medium">{t("Archive")}</th></tr></thead>
        <tbody>{process.access.map((a, at) => <tr key={`${a.role}:${at}`} className={cn("border-t border-border", chosen?.kind === "access" && chosen.at === at && "bg-row-selected")}>
          <td className="px-2 py-1.5"><Button variant="row" type="button" className="font-medium hover:underline" onClick={() => choose({ kind: "access", at })}>{a.role}</Button></td>
          <td className="px-2 py-1.5"><Select value={a.read} onChange={(e) => change({ ...process, access: process.access.map((x, i) => i === at ? { ...x, read: e.target.value as Access["read"] } : x) })}>{readLevels.map((r) => <option key={r} value={r}>{t(r)}</option>)}</Select></td>
          {(["create", "edit", "archive"] as const).map((key) => <td key={key} className="px-2 py-1.5"><Checkbox checked={!!a[key]} onChange={(checked) => change({ ...process, access: process.access.map((x, i) => i === at ? { ...x, [key]: checked } : x) })}>{""}</Checkbox></td>)}
        </tr>)}</tbody></table>
      {!process.access.length && <p className="text-sm text-muted">{t("No roles yet. Without roles only builders see these records.")}</p>}
      {process.access.length > 0 && <ScopeFields process={process} onChange={(scope) => change({ ...process, scope })} />}
    </div>,
    data: <div className="grid max-w-4xl content-start gap-3 p-4"><ObjectLineage object={`build.${object.name}`} fields={process.fields.map((f) => ({ name: f.name, title: f.title || f.name }))} /></div>,
    preview: <div className="min-h-0 flex-1 overflow-auto p-3"><Preview object={object} process={process} action={action} /></div>,
  };
  return <Workbench storageKey="object-type" crumbs={[{ label: t("Object types"), onClick: () => open({ view: "object-type" }) }]} title={object.title}
    status={<DraftStatus state={object.state} problems={issues.length} />} saving={savingState(dirty, busy, refused)}
    history={{ canUndo: session.canUndo && !busy, canRedo: session.canRedo && !busy, undo: () => { session.undo(); setChosen(undefined); }, redo: () => { session.redo(); setChosen(undefined); } }}
    actions={<PublishMenu type="build.object" record={object} dirty={dirty} busy={busy} invalid={issues.length > 0} onReview={() => void perform(review)} onInstall={() => void perform(publish)} onDiscard={discardChanges} route={{ view: "object-type", params: { id } }} />}
    left={{ label: t("Object structure"), content: structure }}
    right={{ label: t("Object inspector"), content: inspector }}
    dock={{ label: t("Object dock"), tabs: [{ id: "problems", title: t("Problems"), badge: issues.length, content: <ProblemList problems={problems} empty={t("No problems. The object type can be published.")} /> }] }}>
    {refused && <Panel role="alert" className="m-2 text-sm text-danger">{t("The host refused it:")} {refused}</Panel>}
    <div role="tablist" aria-label={t("Object sections")} className="flex shrink-0 gap-1 overflow-x-auto border-b border-border px-3 pt-2">
      {tabs.map((item) => <Button variant="row" key={item.id} type="button" role="tab" aria-selected={tab === item.id} onClick={() => setTab(item.id)}
        className={cn("whitespace-nowrap rounded-t border-b-2 px-3 py-1.5 text-sm", tab === item.id ? "border-primary font-semibold" : "border-transparent text-muted hover:text-foreground")}>{item.title}</Button>)}
    </div>
    <fieldset disabled={busy} className="flex min-h-0 min-w-0 flex-1 flex-col overflow-auto">{main[tab]}</fieldset>
  </Workbench>;
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
  const normal=catalog[1]!;const graphCatalog:NodeCatalog=[...catalog,{...normal,id:"action-input",addable:false,outputs:normal.outputs.map(p=>({...p,limit:undefined}))}];
  const links = process.actions.flatMap((a) => [
    ...a.from.map((s) => ({ from: `state:${s}`, to: `action:${a.name}` })),
    ...actionResultEdges(a).map(to=>({from:`action:${a.name}`,to:`state:${to}`})),
  ]);
  const places = layout([
    ...process.states.map((s) => ({ id: `state:${s.name}`, label: s.title })),
    ...process.actions.map((a) => ({ id: `action:${a.name}`, label: a.title })),
  ], links, "right", { width: canvasNodeWidth, height: Math.max(...catalog.map(canvasNodeHeight)), gapX: 40, gapY: 30 });
  const nodes: CanvasNode[] = [
    ...process.states.map((s) => ({ id: `state:${s.name}`, kind: "state", label: s.title || s.name, detail: s.name, position: places.get(`state:${s.name}`) ?? { x: 0, y: 0 } })),
    ...process.actions.map((a) => ({ id: `action:${a.name}`, kind:a.toInput?"action-input":"action", label: a.title || a.name, detail: a.name, position: places.get(`action:${a.name}`) ?? { x: 0, y: 0 } })),
  ];
  const edges: CanvasEdge[] = process.actions.flatMap((a) => [
    ...a.from.map((s) => ({ id: `from:${a.name}:${s}`, source: `state:${s}`, sourcePort: "take", target: `action:${a.name}`, targetPort: "from" })),
    ...actionResultEdges(a).map(to=>({id:`to:${a.name}:${to}`,source:`action:${a.name}`,sourcePort:"to",target:`state:${to}`,targetPort:"result",dashed:!!a.toInput,label:a.toInput})),
  ]);
  const selected = chosen?.kind === "state" ? `state:${process.states[chosen.at]?.name}` : chosen?.kind === "action" ? `action:${process.actions[chosen.at]?.name}` : undefined;
  return <div className="mb-4 grid gap-2">
    <div><h3 className="text-sm font-semibold">{t("Process map")}</h3>
      <p className="text-xs text-muted">{t("Connect a state to an action to allow it; connect an action to a state for its result. Select a node to edit it. Delete a selected line to remove it.")}</p></div>
    <NodeCanvas label={t("Process map")} catalog={graphCatalog} nodes={nodes} edges={edges} selected={selected}
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
          if(process.actions.find(a=>a.name===name)?.toInput){notify.info(t("Edit the state input choices to change these derived connections."));return;}
          onChange({ ...process, actions: process.actions.map((a) => a.name === name ? { ...a, to: state } : a) });
        }
      }}
      onDelete={(removedNodes, removedEdges) => {
        if(removedEdges.some(e=>process.actions.some(a=>a.toInput&&e.source===`action:${a.name}`)))notify.info(t("Edit the state input choices to change these derived connections."));
        const states = process.states.filter((state) => !removedNodes.some((n) => n.id === `state:${state.name}`));
        const actions = process.actions.filter((action) => !removedNodes.some((n) => n.id === `action:${action.name}`)).map((action) => ({
          ...action, from: action.from.filter((state) => states.some((s) => s.name === state) && !removedEdges.some((e) => e.source === `state:${state}` && e.target === `action:${action.name}`)),
          to: states.some((state) => state.name === action.to) && !removedEdges.some((e) => e.source === `action:${action.name}` && e.target === `state:${action.to}`) ? action.to : undefined,
        }));
        onChoose(undefined); onChange({ ...process, states, actions });
      }}
      onDisconnect={(removed) => {
        if(removed.some(e=>process.actions.some(a=>a.toInput&&e.source===`action:${a.name}`)))notify.info(t("Edit the state input choices to change these derived connections."));
        onChange({ ...process, actions: process.actions.map((a) => ({ ...a,
          from: a.from.filter((s) => !removed.some((e) => e.source === `state:${s}` && e.target === `action:${a.name}`)),
          to: removed.some((e) => e.source === `action:${a.name}` && e.target === `state:${a.to}`) ? undefined : a.to,
        })) });
      }} />
  </div>;
}


function FieldProperties({ field, others, onChange, onRemove }: { field: Field; others: Field[]; onChange: (patch: Partial<Field>) => void; onRemove: () => void }) {
  const {definitions}=useHost();
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
    {field.property&&<><p className="break-all text-xs text-muted">{field.property.ref.name}@{field.property.sourceVersion}</p><p className="text-xs text-muted">{t("The shared source controls type and title. Local names, required values and access stay with this object.")}</p>{!bound&&<p role="alert" className="text-xs text-danger">{t("The bound shared property version is unavailable.")}</p>}</>}
    <Label text={t("What people call it")}><Input disabled={!!field.property} value={field.title} onChange={(e) => onChange({ title: e.target.value })} /></Label>
    <Label text={t("Name")}><Input className="font-mono" value={field.name} onChange={(e) => onChange({ name: e.target.value })} /></Label>
    <Label text={t("Type")}><Select disabled={!!field.property} value={field.type} onChange={(e) => onChange({ type: e.target.value })}>{fieldTypes.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select></Label>
    {field.type === "choice" && <Label text={t("Choices")}><Input value={field.choices ?? ""} onChange={(e) => onChange({ choices: e.target.value })} /></Label>}
    {field.type === "reference" && <Label text={t("Reference object")}><SemanticObjectSelect label={t("Reference object")} value={field.ref} onChange={(ref) => onChange({ ref: ref?.name, inverse: undefined })} /></Label>}
    {field.type === "reference" && <Label text={t("Seen from there as")}><Input className="font-mono" placeholder="visits" value={field.inverse ?? ""} onChange={(e) => onChange({ inverse: e.target.value || undefined })} /></Label>}
    {(field.type === "integer" || field.type === "decimal") && <Label text={t("Computed by formula")}><Input className="font-mono" placeholder="price * qty" value={field.formula ?? ""} onChange={(e) => onChange({ formula: e.target.value || undefined })} /></Label>}
    {field.formula && <p className="text-xs text-muted">{t("Computed from the object's other number fields at every change; nobody sets it by hand.")}</p>}
    <Checkbox checked={!!field.required} onChange={(required) => onChange({ required })}>{t("Required")}</Checkbox>
    <Checkbox checked={!!field.search} onChange={(search) => onChange({ search })}>{t("Searchable")}</Checkbox>
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
          to: action.toInput? actionDestinations(action).map(s=>process.states.find(x=>x.name===s)?.title??s).join(", "):action.to ? process.states.find((x) => x.name === action.to)?.title ?? action.to : t("where it was"),
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
          {action.conditions!.map((c, i) => <li key={i}>{t("Needs {field} {operator} {value}; otherwise: “{message}”", { field: c.field, operator: c.operator, value: c.valueField ?? c.value ?? "", message: c.message })}</li>)}
        </ul>}
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

/** Gapless document numbers (ADR-0076): a text field the platform fills on create, counted per object and optionally per year. */
function NumberingFields({ process, onChange }: { process: Process; onChange: (numbering: Numbering_ | undefined) => void }) {
  const n = process.numbering;
  const texts = process.fields.filter((f) => f.type === "text");
  const sample = n ? `${n.prefix ?? ""}${n.yearly ? `${new Date().getFullYear()}-` : ""}${"1".padStart(n.width || 6, "0")}` : "";
  return <Card className="grid gap-3 p-3">
    <div><h3 className="text-sm font-semibold">{t("Document numbering")}</h3><p className="text-xs text-muted">{t("Each new record takes the next number, without gaps: a prefix, the year if yearly, and a zero-padded count. The field is then read-only.")}</p></div>
    <Checkbox checked={!!n} onChange={(on) => onChange(on ? { field: texts[0]!.name, prefix: "", width: 6 } : undefined)}>{t("Number records automatically")}</Checkbox>
    {n && <div className="grid gap-3 sm:grid-cols-4">
      <Label text={t("Number field")}><Select value={n.field} onChange={(e) => onChange({ ...n, field: e.target.value })}>{texts.map((f) => <option key={f.name} value={f.name}>{f.title || f.name}</option>)}</Select></Label>
      <Label text={t("Prefix")}><Input placeholder="GR" value={n.prefix ?? ""} onChange={(e) => onChange({ ...n, prefix: e.target.value || undefined })} /></Label>
      <Label text={t("Digits")}><Input type="number" min={1} max={12} value={n.width ?? 6} onChange={(e) => onChange({ ...n, width: Number(e.target.value) || 6 })} /></Label>
      <Label text={t("Yearly")}><Checkbox checked={!!n.yearly} onChange={(yearly) => onChange({ ...n, yearly: yearly || undefined })}>{t("Restart every year")}</Checkbox></Label>
      <p className="text-xs text-muted sm:col-span-4">{t("First number: {sample}", { sample })}</p>
    </div>}
  </Card>;
}

/** Which fields place a record for the row scopes above (ADR-0066): owner for "own", unit + structure for "unit" and "below". */
function ScopeFields({ process, onChange }: { process: Process; onChange: (scope: ObjectScope) => void }) {
  const scope = process.scope ?? {};
  const candidates = process.fields.filter((f) => f.type === "text" || f.type === "reference");
  const byUnit = process.access.some((a) => a.read === "unit" || a.read === "below");
  const set = (patch: ObjectScope) => { const next = { ...scope, ...patch }; onChange(Object.fromEntries(Object.entries(next).filter(([, v]) => v)) as ObjectScope); };
  const pick = (value: string | undefined, onPick: (v: string | undefined) => void, empty: string) => <Select value={value ?? ""} onChange={(e) => onPick(e.target.value || undefined)}>
    <option value="">{empty}</option>{candidates.map((f) => <option key={f.name} value={f.name}>{f.title || f.name}{f.type === "reference" && f.ref ? ` → ${f.ref}` : ""}</option>)}</Select>;
  return <Card className="grid gap-3 p-3">
    <div><h3 className="text-sm font-semibold">{t("What places a record")}</h3><p className="text-xs text-muted">{t("Row scopes read these fields: own = the owner field (or whoever created it); unit and below = the unit field, following a structure of the organisation.")}</p></div>
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
      <Label text={t("Role in the builder app")}><Input className="font-mono" value={access.role} onChange={(e) => onChange({ role: e.target.value })} /></Label>
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
    <p className="text-xs text-muted">{action.from.map(named).join(", ") || t("any state")} → {action.to ? named(action.to) : action.toInput ? t("chosen by input") : t("same state")}</p>
    <p className="text-xs text-muted">{t("{inputs} parameters · {rules} rules · {conditions} criteria", { inputs: action.inputs?.length ?? 0, rules: (action.sets?.length ?? 0) + (action.creates?.length ?? 0), conditions: action.conditions?.length ?? 0 })}</p>
    {issues > 0 && <p className="text-xs text-danger">{t("{n} problems", { n: issues })}</p>}
    <div className="flex gap-2"><Button size="sm" variant="primary" onClick={onOpen}><Zap />{t("Open action type")}</Button><Button size="sm" variant="ghost" onClick={onRemove}><Trash2 />{t("Remove")}</Button></div>
  </Card>;
}
