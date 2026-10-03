import { useMemo, useRef, useState, type ReactNode } from "react";
import { Plus } from "lucide-react";
import { z } from "zod";
import {
  Histogram, TermCounts, SearchInput, Spacer, Separator, Notice, DateTimeInput, DateInput, MultipleChoiceInput, ChoiceInput, FacetChoices,Button,ButtonGroup, CollectionTitle, CommandMenu, MetalButton, LiquidButton, RetroButton, Input, Select, Textarea, Card, Panel, Switch, Checkbox, Form, Disclosure, FilePicker, Toggles, Tree, Dialog, Sheet,
  StatusTag, Tag, submissionStatuses, DataTable, EntityForm, RecordForm, Markdown, MarkdownEditor, field,
  defineEntity, columnsFor, applyFilters, FilterBar, EntityCard, PropertyList, PageHeader, NotificationList,
  RangeInput, RecordLeaderboard, SummaryStatistics, Gauge, Progress, RecordGantt, RecordCalendar, RecordEvents, CountMatrix, RecordScatter, RecordChart, RecordCards, RecordKanban, RecordTimeline, RecordList, RecordPage, RecordLinks, RecordStatus, RecordHistory, RecordLookup, RecordWorkspace, Tasks, Inbox, StatusBar,
  Chart, Pivot, Graph, BlockCanvas, FlowView, FlowGraph, Workspace, EditorWorkbench, LayoutRegion, LayoutStack, ContentTabs, FlowLayout, VirtualStack, notify, t,
  type FieldType, type Filter, type EntityInfo, type EntityRecord, type RecordSource, type RecordView,
  type InboxTask, type Lifecycle as LifecycleInfo, type NodeCatalog, type CanvasNode, type CanvasEdge,
  type FlowDefinition, type FlowInstanceData, type ChartSpec, type Route,
} from "./index";
import { WorkspaceContext } from "./shell/Workspace";

export function EditorPanels() {
  return <div className="flex h-[30rem] flex-col"><EditorWorkbench leftLabel={t("Library")} centerLabel={t("Canvas")} rightLabel={t("Inspector")}
    left={<Panel title={t("Library")}><Button>{t("Table")}</Button></Panel>}
    right={<Panel title={t("Inspector")}><Input aria-label={t("Title")} defaultValue={t("Example")} /></Panel>}>
    <Panel title={t("Canvas")} description={t("Example")} />
  </EditorWorkbench></div>;
}
export function ButtonGroups(){
 const [selected,setSelected]=useState("");
 return <div className="grid gap-2"><ButtonGroup label={t("Button group")} buttons={[{id:"open",title:t("Open"),variant:"primary",icon:"arrow"},{id:"close",title:t("Close"),variant:"default"}]} isBound={()=>true} onActivate={setSelected}/><output>{selected}</output></div>;
}

// Runnable owner examples use public APIs and synthetic values only. Nothing in
// this bundle fetches a host or suggests that a fixture proves authorization.
export function PreviewWorkspace({ children, onOpen }: { children: ReactNode; onOpen?: (route: Route) => void }) {
  const workspace = useMemo(() => ({ open: (route: Route) => onOpen?.(route), close: () => {}, notify }), [onOpen]);
  return <WorkspaceContext.Provider value={workspace}>{children}</WorkspaceContext.Provider>;
}

const stamp = { by: "demo", at: "2026-09-30T12:00:00Z" };
const lifecycle: LifecycleInfo = { field: "state", initial: "open",
  states: [{ name: "open", title: t("Pending"), tone: "info" }, { name: "done", title: t("Done"), tone: "success" }],
  transitions: [{ name: "finish", from: ["open"], to: ["done"], schema: "demo.finish", title: t("Done") }],
};
export const demoInfo: EntityInfo = { type: "demo.record", app: "demo", title: t("Record"), plural: t("Records"), display: "name", standard: [], lifecycle,
  fields: [{ name: "name", title: t("Name"), type: "text", required: true, search: true },
    { name: "quantity", title: t("Quantity"), type: "integer" },
    { name: "state", title: t("Status"), type: "choice", choices: ["open", "done"], choiceTitles: [t("Pending"), t("Done")] }],
};
const demoRows: EntityRecord[] = [
  { id: "DEMO-001", revision: 1, name: "Sample Alpha", quantity: 12, state: "open", created: stamp, changed: stamp },
  { id: "DEMO-002", revision: 1, name: "Sample Beta", quantity: 24, state: "done", created: stamp, changed: stamp },
];
const history = [{ change: "demo-change", schema: "demo.create", by: "demo", at: stamp.at, fields: [{ field: "quantity", after: 12 }] }];
export const demoSource: RecordSource = {
  entity: (type) => type === demoInfo.type ? demoInfo : undefined,
  list: async (_type, query) => {
    const rows = demoRows.filter((row) => Boolean(row.archived) === Boolean(query.archived) && `${row.id} ${row.name}`.toLowerCase().includes((query.search ?? "").toLowerCase()));
    const sort = query.sort?.[0] ?? "id", key = sort.replace(/^-/, ""), direction = sort.startsWith("-") ? -1 : 1;
    rows.sort((a, b) => direction * (key === "quantity" ? Number(a[key]) - Number(b[key]) : String(a[key] ?? "").localeCompare(String(b[key] ?? ""))));
    return { total: rows.length, records: rows.slice(query.offset ?? 0, (query.offset ?? 0) + (query.limit ?? rows.length)) };
  },
  get: async (_type, id): Promise<RecordView> => {
    const record = demoRows.find((row) => row.id === id);
    if (!record) throw new Error("The record was not found or is outside your scope.");
    return { record, history, related: [], linked: [], activity: [], processes: [], approvals: [], tasks: [], files: [], comments: [], following: false };
  },
};
const demoEntity = defineEntity<{ id: string; name: string; quantity: number }>({ name: "demo", primary: "name", fields: {
  name: field.text({ label: t("Name"), required: true }), quantity: field.number({ label: t("Quantity"), min: 0 }),
} });
const tableRows = [{ id: "DEMO-001", name: "Sample Alpha", quantity: 12 }, { id: "DEMO-002", name: "Sample Beta", quantity: 24 }];

export function Tokens() {
  return <div className="grid gap-3 sm:grid-cols-2">
    {["background", "surface", "foreground", "muted", "border", "ring", "row-selected"].map((token) =>
      <Card key={token} className="flex items-center gap-3 p-3"><span className="size-8 shrink-0 rounded border border-border" style={{ background: `var(--${token})` }} /><code className="text-xs">--{token}</code></Card>)}
    <div className="flex flex-wrap items-center gap-2">{(["neutral", "info", "success", "warning", "danger"] as const).map((tone) => <Tag key={tone} label={tone} tone={tone} />)}</div>
  </div>;
}
export function Buttons() {
  const [count, setCount] = useState(0);
  return <div className="grid gap-3"><div className="flex flex-wrap gap-2">
    {(["default", "primary", "ghost", "danger", "link", "row"] as const).map((variant) => <Button key={variant} variant={variant} onClick={() => setCount(count + 1)}>{variant}</Button>)}
  </div><div className="flex items-center gap-2"><Button size="sm">{t("Small")}</Button><Button size="icon" aria-label={t("Add")}><Plus /></Button><Button disabled>{t("Disabled")}</Button><span className="text-xs text-muted">{t("Clicks")}: {count}</span></div></div>;
}
export function Inputs() {
  return <div className="grid max-w-lg gap-3"><label className="grid gap-1 text-sm">{t("Name")}<Input defaultValue="Sample Alpha" /></label>
    <label className="grid gap-1 text-sm">{t("Status")}<Select defaultValue="open"><option value="open">{t("Pending")}</option><option value="done">{t("Done")}</option></Select></label>
    <label className="grid gap-1 text-sm">{t("Description")}<Textarea defaultValue="Synthetic example data." /></label>
    <Input disabled aria-label={t("Disabled")} value={t("Disabled")} /><Input aria-label={t("Invalid")} aria-invalid defaultValue="Invalid" />
  </div>;
}
export function Checkboxes() {
  const [checked, setChecked] = useState(false);
  return <div className="grid gap-2"><Checkbox checked={checked} onChange={setChecked}>{t("Select this item")}</Checkbox><Checkbox checked disabled onChange={() => {}}>{t("Disabled")}</Checkbox></div>;
}
export function Panels() {
  return <div className="grid gap-3"><Card className="p-3">{t("Shared content surface")}</Card><Panel title={t("Inspector")} description={t("A heading, properties and actions share one panel.")} actions={<Button size="sm">{t("Edit")}</Button>}><PropertyList items={[[t("Name"), "Sample Alpha"], [t("Status"), <Tag label={t("Pending")} tone="info" />]]} /></Panel></div>;
}
export function Forms() {
  const [submitted, setSubmitted] = useState(false);
  return <Form className="flex flex-wrap items-center gap-2" onSubmit={() => setSubmitted(true)}><Input aria-label={t("Name")} required placeholder={t("Name")} /><Button type="submit">{t("Save")}</Button>{submitted && <Tag tone="success" label={t("Submitted locally")} />}</Form>;
}
export function Disclosures() { return <Disclosure summary={t("Supporting details")}><p className="text-sm">{t("Details remain beside their summary.")}</p></Disclosure>; }
export function FilePicking() {
  const [file, setFile] = useState<File>();
  return <div className="flex items-center gap-3"><FilePicker onFile={setFile}>{t("Choose file")}</FilePicker><span className="text-sm">{file?.name ?? t("No files")}</span></div>;
}
export function Choices() {
  const [value, setValue] = useState(["name"]);
  return <Toggles options={[{ value: "name", label: t("Name") }, { value: "quantity", label: t("Quantity") }, { value: "state", label: t("Status") }]} value={value} onChange={setValue} />;
}
type Branch = { id: string; children: Branch[] };
const branches: Branch[] = [{ id: "Organization", children: [{ id: "Operations", children: [{ id: "Team Alpha", children: [] }] }, { id: "Finance", children: [] }] }];
export function Hierarchy() {
  const [selected, setSelected] = useState<string>();
  return <Tree roots={branches} children={(node) => node.children} row={(node) => node.id} id={(node) => node.id} selected={selected} onSelect={(node) => setSelected(node.id)} />;
}
export function Dialogs() {
  const [open, setOpen] = useState(false);
  return <><Button onClick={() => setOpen(true)}>{t("Open dialog")}</Button><Dialog open={open} onOpenChange={setOpen} title={t("Confirm action")}><p className="mb-3 text-sm">{t("This example only changes local preview state.")}</p><Button onClick={() => setOpen(false)}>{t("Done")}</Button></Dialog></>;
}
export function Sheets() {
  const [open, setOpen] = useState(false);
  return <><Button onClick={() => setOpen(true)}>{t("Open inspector")}</Button><Sheet open={open} onOpenChange={setOpen} title={t("Inspector")}><RecordSummaries /></Sheet></>;
}
export function Statuses() {
  return <div className="flex flex-wrap gap-2">{Object.keys(submissionStatuses).map((status) => <StatusTag key={status} status={status} registry={submissionStatuses} />)}<StatusTag status="demo-unmapped" registry={submissionStatuses} /></div>;
}
export function Tables() {
  const [rows, setRows] = useState(tableRows);
  return <DataTable data={rows} columns={columnsFor(demoEntity)} getRowId={(row) => row.id} height={230}
    onCellEdit={(row, key, value) => setRows(rows.map((item) => item.id === row.id ? { ...item, [key]: value } : item))} />;
}
export function MarkdownContent() {
  const [value, setValue] = useState<string | undefined>("## Sample notes\n\nA **shared** editor with a [reference](https://example.com).\n\n- Read\n- Edit");
  return <div className="grid gap-3 sm:grid-cols-2"><Markdown content={value ?? ""} /><MarkdownEditor value={value} onChange={setValue} /></div>;
}
export function Headers() { return <div className="grid gap-3"><PageHeader title={t("Records")} description={t("Browse declared records and their available work.")} actions={<Button variant="primary">{t("Add")}</Button>}/><PageHeader compact level={2} title="Operational overview"/></div>; }
export function CollectionTitles(){return <div className="grid gap-3"><CollectionTitle title="Permitted assets" value="620"/><CollectionTitle title="Permitted assets"/><CollectionTitle title="Permitted assets" error={t("Resource read failed")}/></div>;}

const fieldSamples: [string, FieldType, unknown][] = [
  ["text", field.text({ label: "text" }), "Sample"], ["longText", field.longText({ label: "longText" }), "Several lines of text."],
  ["markdown", field.markdown({ label: "markdown" }), "**Sample**"], ["number", field.number({ label: "number" }), 12],
  ["currency", field.currency({ label: "currency", currency: "EUR" }), 42.5], ["percent", field.percent({ label: "percent" }), 0.65],
  ["checkbox", field.checkbox({ label: "checkbox" }), true], ["date", field.date({ label: "date" }), "2026-09-30"],
  ["datetime", field.datetime({ label: "datetime" }), "2026-09-30T12:00"], ["duration", field.duration({ label: "duration" }), 90],
  ["singleSelect", field.singleSelect({ label: "singleSelect", options: [{ value: "alpha", label: "Alpha" }, { value: "beta", label: "Beta" }] }), "alpha"],
  ["multiSelect", field.multiSelect({ label: "multiSelect", options: [{ value: "alpha", label: "Alpha" }, { value: "beta", label: "Beta" }] }), ["alpha"]],
  ["tags", field.tags({ label: "tags" }), ["sample", "preview"]], ["email", field.email({ label: "email" }), "demo@example.com"],
  ["url", field.url({ label: "url" }), "https://example.com"], ["phone", field.phone({ label: "phone" }), "+1 555 0100"],
  ["barcode", field.barcode({ label: "barcode" }), "DEMO-001"], ["rating", field.rating({ label: "rating" }), 3],
  ["attachment", field.attachment({ label: "attachment", upload: async (file) => ({ name: file.name, url: "https://example.com", size: file.size }) }), []],
  ["link", field.link({ label: "link", to: (id) => ({ view: "demo", params: { id } }) }), "DEMO-001"],
  ["formula", field.formula({ label: "formula", compute: () => 24, as: field.number({ label: "formula" }) }), 24],
  ["timestamp", field.timestamp({ label: "timestamp", of: () => "2026-09-30T12:00" }), "2026-09-30T12:00"],
];
function FieldSample({ name, type, initial }: { name: string; type: FieldType; initial: unknown }) {
  const [value, setValue] = useState(initial);
  return <Panel title={<code>{name}</code>}><div className="mb-2 text-sm">{type.display(value, {})}</div>{type.editor?.({ id: `catalog-${name}`, value, onChange: setValue })}</Panel>;
}
export function Fields() { return <div className="grid gap-3 md:grid-cols-2">{fieldSamples.map(([name, type, value]) => <FieldSample key={name} name={name} type={type} initial={value} />)}</div>; }
const schema = z.object({ name: z.string().min(1), quantity: z.number().min(0) });
export function SchemaForms() {
  const [saved, setSaved] = useState(false);
  return <div className="grid max-w-lg gap-3"><EntityForm schema={schema} fields={[{ name: "name", label: t("Name"), required: true }, { name: "quantity", label: t("Quantity"), kind: "number" }]} defaultValues={{ name: "Sample Alpha", quantity: 12 }} onSubmit={() => setSaved(true)} />{saved && <Tag label={t("Submitted locally")} tone="success" />}</div>;
}
export function RecordForms() {
  const [saved, setSaved] = useState(false);
  return <div className="grid max-w-lg gap-3"><RecordForm entity={demoEntity} defaultValues={tableRows[0]} onSubmit={() => setSaved(true)} />{saved && <Tag label={t("Submitted locally")} tone="success" />}</div>;
}
export function Filters() {
  const [filters, setFilters] = useState<Filter[]>([{ field: "name", operator: "contains", arg: "Alpha" }]);
  return <div className="grid gap-3"><FilterBar entity={demoEntity} filters={filters} onChange={setFilters} /><DataTable data={applyFilters(demoEntity, tableRows, filters)} columns={columnsFor(demoEntity)} getRowId={(row) => row.id} height={180} /></div>;
}
export function RecordSummaries() { return <EntityCard title="Sample Alpha" subtitle="DEMO-001" status={<Tag label={t("Pending")} tone="info" />} properties={[[t("Quantity"), 12], [t("Name"), "Sample Alpha"]]} />; }
export function Notifications() {
  const [items, setItems] = useState([{ id: "notice-1", title: "Sample Alpha changed", body: "Quantity updated to 12", at: stamp.at, read: false }]);
  return <NotificationList items={items} onRead={(notice) => setItems(items.map((item) => item.id === notice.id ? { ...item, read: true } : item))} />;
}
export function RecordLists() {
  const [mode, setMode] = useState("ready"), [selected, setSelected] = useState<string>();
  const source = useMemo(() => mode === "ready" ? demoSource : { ...demoSource, list: async () => { if (mode === "error") throw new Error("Preview read failed"); return { records: [], total: 0 }; } }, [mode]);
  return <div className="grid gap-3"><Select aria-label={t("Preview state")} value={mode} onChange={(event) => setMode(event.target.value)}><option value="ready">{t("Ready")}</option><option value="empty">{t("Empty")}</option><option value="error">{t("Error")}</option></Select><RecordList source={source} type={demoInfo.type} height={260} onOpen={(record) => setSelected(record.id)} />{selected && <span className="text-xs">{t("Selected record")}: {selected}</span>}</div>;
}
export function RecordDetails() {
 const child={...demoInfo,type:"demo.child",fields:[...demoInfo.fields,{name:"parent",title:t("Record"),type:"reference" as const,ref:demoInfo.type,inverse:"children"}]},source:RecordSource={...demoSource,entity:type=>type===child.type?child:demoSource.entity(type),get:async(type,id)=>({...await demoSource.get(type,id),related:[{type:child.type,field:"parent",title:child.plural,relation:"children",records:demoRows,total:2}]})};
 return <div className="grid gap-3"><RecordPage source={demoSource} type={demoInfo.type} id="DEMO-001" /><RecordLinks source={source} type={demoInfo.type} id="DEMO-001" groups={[{object:{app:"demo",kind:"object",name:child.type},field:"parent"}]}/><RecordHistory info={demoInfo} history={history} /></div>;
}
export function RecordLookups() {
  const [value, setValue] = useState<string>();
  return <div className="max-w-lg"><label className="mb-1 block text-sm" htmlFor="catalog-record-lookup">{t("Record")}</label><RecordLookup id="catalog-record-lookup" source={demoSource} type={demoInfo.type} value={value} onChange={setValue} /></div>;
}
const task: InboxTask = { id: "demo-task", revision: 1, created: stamp, changed: stamp, title: "Review Sample Alpha", body: "Synthetic task for preview only", app: "demo", candidates: ["demo"], state: "open", answers: ["approve", "reject"], answerTitles: [t("Approved"), t("Rejected")] };
export function TaskInbox() {
  const [tasks, setTasks] = useState([task]);
  return <div className="grid gap-4"><Inbox tasks={tasks} /><Tasks list={tasks} tasks={{ answer: async (answered) => setTasks(tasks.filter((item) => item.id !== answered.id)) }} />{tasks.length === 0 && <Inbox tasks={[]} />}</div>;
}
export function Lifecycle() {
  const [state, setState] = useState("open");
  return <div className="grid gap-3"><StatusBar lifecycle={lifecycle} state={state} can={(schema) => schema === "demo.finish"} onTransition={() => setState("done")} /><RecordStatus source={demoSource} type={demoInfo.type} id="DEMO-001" config={{field:"state",stages:["open","done"]}}/></div>;
}
const chart: ChartSpec = { title: t("Quantity by group"), data: { values: [{ group: "Alpha", quantity: 12 }, { group: "Beta", quantity: 24 }] }, mark: "bar", encoding: { x: { field: "group", type: "nominal" }, y: { field: "quantity", type: "quantitative", aggregate: "sum" } } };
export function Charts() { return <Chart spec={chart} height={240} />; }
const aggregateSource = { aggregate: async () => ({ columns: [{ name: "group", title: t("Group"), kind: "group" as const, type: "nominal" as const }, { name: "count", title: t("Count"), kind: "measure" as const, type: "quantitative" as const }], rows: [{ group: "Alpha", count: 12 }, { group: "Beta", count: 24 }] }) };
export function Pivots() { return <Pivot source={aggregateSource} type="demo.record" query={{}} rows="group" measure="count" />; }
export function Graphs() { return <Graph nodes={[{ id: "a", label: "Sample Alpha", tone: "success" }, { id: "b", label: "Sample Beta", tone: "info", current: true }]} edges={[{ from: "a", to: "b", label: t("Next") }]} height={230} />; }
const nodeCatalog: NodeCatalog = [{ id: "value", title: t("Value"), category: "data", inputs: [], outputs: [{ id: "out", label: t("Quantity"), type: "number" }] }, { id: "transform", title: t("Transform"), category: "logic", inputs: [{ id: "in", label: t("Quantity"), type: "number" }], outputs: [{ id: "out", label: t("Quantity"), type: "number" }] }];
export function Blocks() {
  const counter = useRef(3);
  const [nodes, setNodes] = useState<CanvasNode[]>([{ id: "n1", kind: "value", label: t("Value"), position: { x: 30, y: 60 } }, { id: "n2", kind: "transform", label: t("Transform"), position: { x: 320, y: 60 } }]);
  const [edges, setEdges] = useState<CanvasEdge[]>([{ id: "e1", source: "n1", sourcePort: "out", target: "n2", targetPort: "in" }]);
  const [selected, setSelected] = useState<string>();
  return <BlockCanvas catalog={nodeCatalog} nodes={nodes} edges={edges} height={360} mode="edit" selected={selected} onSelect={setSelected}
    onAdd={(kind, context) => setNodes([...nodes.map((node) => ({ ...node, position: context.positions?.[node.id] ?? node.position })), { id: `n${counter.current++}`, kind, label: nodeCatalog.find((item) => item.id === kind)!.title, position: context.position }])}
    onConnect={(connection) => setEdges([...edges, { id: `e${counter.current++}`, source: connection.source, target: connection.target, sourcePort: connection.sourceHandle ?? "out", targetPort: connection.targetHandle ?? "in" }])}
    onDisconnect={(disconnected) => setEdges(edges.filter((edge) => !disconnected.some((item) => item.id === edge.id)))}
    onPositionsChange={(positions) => setNodes(nodes.map((node) => ({ ...node, position: positions[node.id] ?? node.position })))}
    onLayout={(positions) => setNodes(nodes.map((node) => ({ ...node, position: positions[node.id] ?? node.position })))}
    onDelete={(deleted, disconnected) => { setNodes(nodes.filter((node) => !deleted.some((item) => item.id === node.id))); setEdges(edges.filter((edge) => !disconnected.some((item) => item.id === edge.id))); }} />;
}
const definition: FlowDefinition = { id: "demo.review", app: "demo", title: "Sample review", version: 1, start: ["demo.created"], steps: [{ name: "prepare", title: t("Prepare"), kind: "action", next: ["review"] }, { name: "review", title: t("Review"), kind: "ask", next: [] }] };
const instance: FlowInstanceData = { id: "demo-flow", flow: "demo.review", title: "Sample Alpha review", version: 1, key: "preview", state: "waiting", tokens: [{ id: 1, step: "review", waits: "ask" }], undo: [], trace: [{ at: stamp.at, step: "prepare", what: "done", by: "demo" }] };
export function FlowObservation() { return <div className="grid gap-4"><FlowGraph definition={definition} height={200} /><FlowView definition={definition} instance={instance} /></div>; }
export function MasterDetail() {
  const [selected, setSelected] = useState<string>();
  return <RecordWorkspace title={t("Records")} source={demoSource} type={demoInfo.type} selected={selected} onSelect={setSelected} detail={(id) => <RecordPage source={demoSource} type={demoInfo.type} id={id} />} />;
}
const views = [{ id: "catalog-records", title: () => t("Records"), render: () => <MasterDetail /> }, { id: "catalog-graph", title: () => t("Steps"), render: () => <Blocks /> }];
export function WorkspaceShell() {
  return <Workspace product={t("Catalog preview")} storageKey="platform.catalog.workspace-preview" views={views} home={{ view: "catalog-records" }} nav={[{ label: t("Preview"), items: [{ label: t("Records"), route: { view: "catalog-records" } }, { label: t("Steps"), route: { view: "catalog-graph" } }] }]} />;
}

export function MetalButtons() {
  const [clicked, setClicked] = useState("");
  return <div className="grid gap-5">
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted">{t("Metallic variants")}</h4>
      <div className="flex flex-wrap items-center gap-3">
        {(["default", "primary", "success", "error", "gold", "bronze"] as const).map((variant) => (
          <MetalButton key={variant} variant={variant} onClick={() => setClicked(`Metal ${variant}`)}>
            {variant.charAt(0).toUpperCase() + variant.slice(1)}
          </MetalButton>
        ))}
      </div>
    </div>
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted">{t("Liquid glass buttons")}</h4>
      <div className="flex flex-wrap items-center gap-3">
        <LiquidButton size="default" onClick={() => setClicked("Liquid default")}>Liquid Default</LiquidButton>
        <LiquidButton size="lg" variant="destructive" onClick={() => setClicked("Liquid destructive")}>Liquid Destructive</LiquidButton>
        <LiquidButton size="xl" onClick={() => setClicked("Liquid XL")}>Liquid XL</LiquidButton>
      </div>
    </div>
    {clicked && <p className="text-xs text-muted">{t("Last clicked")}: {clicked}</p>}
  </div>;
}

export function RetroButtons() {
  const [pressed, setPressed] = useState("");
  return <div className="grid gap-5">
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted">{t("Retro variants")}</h4>
      <div className="flex flex-wrap items-center gap-3">
        {(["default", "darkGray", "white", "lightGray", "gray"] as const).map((variant) => (
          <RetroButton key={variant} variant={variant} onClick={() => setPressed(variant)}>
            {variant}
          </RetroButton>
        ))}
      </div>
    </div>
    <div className="space-y-2">
      <h4 className="text-xs font-semibold uppercase tracking-wider text-muted">{t("Mechanical keypad")}</h4>
      <div className="flex flex-wrap items-center gap-3">
        <RetroButton variant="default" onClick={() => setPressed("Run")}>RUN</RetroButton>
        <RetroButton variant="darkGray" onClick={() => setPressed("Step")}>STEP</RetroButton>
        <RetroButton variant="white" onClick={() => setPressed("Reset")}>RESET</RetroButton>
        <RetroButton variant="default" disabled>HALT</RetroButton>
      </div>
    </div>
    {pressed && <p className="text-xs text-muted">{t("Last clicked")}: {pressed}</p>}
  </div>;
}

export function ContentTabsExample() {
  const [value, setValue] = useState("overview");
  return <ContentTabs label={t("Content tabs")} value={value} onChange={setValue} items={[
    { id: "overview", title: t("Overview"), content: <p>{t("Choose a tab to see its content.")}</p> },
    { id: "notes", title: t("Notes"), content: <Input aria-label={t("Notes")} /> },
  ]} />;
}

export function FlowLayouts() {
  return <FlowLayout toolbar label={t("Toolbar")}><Button>{t("Open")}</Button><Button>{t("Save")}</Button><Button disabled>{t("Delete")}</Button></FlowLayout>;
}

export function VirtualItems() {
  return <VirtualStack label={t("Repeated records")} items={Array.from({ length: 100 }, (_, i) => ({ id: `sample-${i}`, title: t("Item {n}", { n: i + 1 }) }))} itemKey={(item) => item.id} renderItem={(item) => <Card className="p-3">{item.title}<Button>{t("Open")}</Button></Card>} />;
}

export function RecordTimelines() {
 const [selected,setSelected]=useState<string>();
 const records=[{id:"EXAMPLE-A",title:"Incoming review",resource:"Team A",start:"2026-10-01",end:"2026-10-03"},{id:"EXAMPLE-B",title:"Inspection",resource:"Team A",start:"2026-10-04",end:"2026-10-05"}];
 return <RecordTimeline records={records.map(r=>({...r,revision:1,created:stamp,changed:stamp}))} fields={{start:"start",end:"end",label:"title",group:"resource",kind:"date"}} selected={selected} onSelect={r=>setSelected(r?.id)} label={t("Record timeline")}/>;
}

export function RecordKanbans() {
 const [selected,setSelected]=useState<string>();
 return <RecordKanban records={demoRows} lanes={[{name:"open",title:t("Open")},{name:"done",title:t("Done")}]} stateField="state" labelField="name" selected={selected} onSelect={r=>setSelected(r?.id)} label={t("Kanban board")}/>;
}

export function SizedLayouts() {
 return <LayoutRegion size={{height:240}}><LayoutStack direction="columns"><LayoutRegion parent="columns" size={{weight:2,scroll:"auto"}}><Panel>{t("Primary region")}</Panel></LayoutRegion><LayoutRegion parent="columns" size={{weight:1}}><Panel>{t("Secondary region")}</Panel></LayoutRegion></LayoutStack></LayoutRegion>;
}

export function ContextCommands() {
 const [value,setValue]=useState("");return <div><CommandMenu label={t("Region commands")} commands={[{id:"select",label:t("Select"),run:()=>setValue(t("Selected"))},{id:"disabled",label:t("Unavailable"),disabled:true,run:()=>{}}]}><Button>{t("Primary region")}</Button></CommandMenu><p role="status">{value}</p></div>;
}

export function FacetChoicesExample(){const [value,set]=useState<string[]>([]);return <FacetChoices title={t("Status")} options={[{value:"Open",count:12},{value:"Done",count:8}]} value={value} onChange={set} histogram/>;}

export function RecordCardsExample(){const [selected,onSelect]=useState<EntityRecord>();return <RecordCards records={demoRows} info={demoInfo} fields={["quantity"]} labelField="name" layout="grid" selected={selected?.id} onSelect={onSelect}/>;}
export function RecordChartExample(){return <RecordChart records={demoRows} info={demoInfo} fields={{mark:"line",xField:"name",yField:"quantity"}}/>;}
export function RecordCalendarExample(){const [selected,setSelected]=useState<EntityRecord>();return <RecordCalendar records={[{id:"W1",revision:1,created:stamp,changed:stamp,title:"Inspect equipment",due:"2026-10-02"}]} fields={{dateField:"due",labelField:"title",initialMonth:"2026-10",kind:"date"}} selected={selected?.id} onSelect={setSelected}/>;}
export function RecordEventsExample(){const info:EntityInfo={...demoInfo,fields:[{name:"title",title:"Event title",type:"text"},{name:"raised",title:"Business time",type:"datetime"},{name:"severity",title:"Severity",type:"choice",choices:["high","low"]}]};return <RecordEvents info={info} records={[{id:"E1",revision:1,created:stamp,changed:stamp,title:"Temperature exceeded threshold",raised:"2026-10-02T08:00:00Z",severity:"high"}]} fields={{timeField:"raised",titleField:"title",severityField:"severity",tones:[{value:"high",tone:"danger"}]}}/>;}

export function RecordGanttExample(){const info:EntityInfo={...demoInfo,fields:[{name:"title",title:"Task",type:"text"},{name:"begin",title:"Start",type:"datetime"},{name:"due",title:"Due",type:"date"},{name:"status",title:"Status",type:"choice",choices:["open","done"]}]};return <RecordGantt info={info} records={[{id:"WO1",revision:1,created:stamp,changed:stamp,title:"Inspect bearing",begin:"2026-09-20T08:00:00Z",due:"2026-10-04",status:"open"}]} fields={{startField:"begin",endField:"due",titleField:"title",statusField:"status",rangeStart:"2026-09-01",rangeEnd:"2026-11-01",tones:[{value:"open",tone:"warning"}]}}/>;}

export function ProgressExample(){return <Progress value="83" total="100" label="Completed work"/>;}

export function GaugeExample(){return <Gauge value={83.2} max={100} warnAt={95} suffix="%" label="Fleet availability"/>;}

export function SummaryStatisticsExample(){return <SummaryStatistics value={{kind:"statistics",count:"6",min:20,mean:63.333,max:100,sum:380}} fieldTitle="Availability"/>;}

export function RecordLeaderboardExample(){const info:EntityInfo={...demoInfo,fields:[{name:"title",title:"Title",type:"text"},{name:"qty",title:"Exposure",type:"decimal"}]};return <RecordLeaderboard records={[{id:"A",revision:1,created:stamp,changed:stamp,title:"Pump A",qty:100},{id:"B",revision:1,created:stamp,changed:stamp,title:"Pump B",qty:50}]} total={20} info={info} fields={{valueField:"qty",labelField:"title",limit:8,ascending:false}} onSelect={()=>{}}/>;}

export function RangeInputExample(){const [values,setValues]=useState<[string,string]>(["",""]);return <RangeInput lower={values[0]} upper={values[1]} min="0" max="45" step="1" label="Pressure" unit="bar" onChange={(lower,upper)=>setValues([lower,upper])}/>;}

export function SwitchExample(){const [on,setOn]=useState(false);return <Switch label="Active assets" checked={on} onChange={setOn}/>;}

export function ChoiceInputExample(){const [value,setValue]=useState("");return <ChoiceInput value={value} options={["Open","In progress","Done"]} variant="segments" label="Status" title="Task status" onChange={setValue}/>;}

export function MultipleChoiceInputExample(){const [value,setValue]=useState(["Open"]);return <MultipleChoiceInput clearable value={value} options={["Open","In progress","Done"]} label="Status" title="Task status" onChange={setValue}/>;}

export function DateInputExample(){const [value,setValue]=useState("2028-02-29");return <DateInput value={value} title="Business date" label="Date" onChange={setValue}/>;}

export function DateTimeInputExample(){const [value,setValue]=useState("2028-02-29T08:30:45.123456789+08:00");return <DateTimeInput value={value} offset="Z" title="Business time" label="Date and time" onChange={setValue}/>;}

export function NoticeExample(){return <Notice title={t("Alert banner")} message={t("Review the affected records before continuing.")} tone="warning"/>;}

export function SeparatorExample(){return <Separator name={t("Separator")} label={t("Operator instructions")}/>;}

export function SpacerExample(){return <div className="grid"><p>{t("Operator instructions")}</p><Spacer size={16}/><p>{t("Review the affected records before continuing.")}</p></div>;}

export function SearchInputExample(){const [value,setValue]=useState("");return <SearchInput aria-label="Asset search" value={value} onChange={setValue} scope={["Assets"]}/>;}

export function TermCountsExample(){return <TermCounts label="Status terms" terms={[{value:"Active",count:24},{value:"Warning",count:8},{value:"",count:2},{value:null,count:1}]}/>;}

export function HistogramExample(){return <Histogram label="Values" value={{field:"value",requestedBins:3,valid:8,missing:1,minimum:"0",maximum:"1",buckets:[{lower:"0",upper:"1/3",upperInclusive:false,count:2},{lower:"1/3",upper:"2/3",upperInclusive:false,count:5},{lower:"2/3",upper:"1",upperInclusive:true,count:1}]}}/>;}

export function RecordScatterExample(){const info:EntityInfo={...demoInfo,fields:[{name:"title",title:"Title",type:"text"},{name:"x",title:"Pressure",type:"decimal"},{name:"y",title:"Exposure",type:"decimal"},{name:"status",title:"Status",type:"text"}]};return <RecordScatter records={[{id:"A",revision:1,created:stamp,changed:stamp,title:"Pump A",x:10,y:50,status:"Open"},{id:"B",revision:1,created:stamp,changed:stamp,title:"Pump B",x:10,y:50,status:"Closed"},{id:"C",revision:1,created:stamp,changed:stamp,title:"Pump C",x:20,y:80,status:"Open"}]} info={info} fields={{xField:"x",yField:"y",colorField:"status",labelField:"title"}} onSelect={()=>{}}/>;}

export function CountMatrixExample(){return <CountMatrix heatmap rows="status" columns="priority" data={{columns:[{name:"status",title:"Status",kind:"group",type:"nominal"},{name:"priority",title:"Priority",kind:"group",type:"nominal"},{name:"count",title:"Count",kind:"measure",type:"quantitative"}],rows:[{status:"Open",priority:"High",count:8},{status:"Closed",priority:"Low",count:3}]}} onSelect={()=>{}}/>;}
