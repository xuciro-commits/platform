import { newId, useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, DataTable, Dialog, EmptyState, Form, Input, Notice, Panel, PropertyList, RelationCanvas, SegmentedChoice, Select, Workbench, t, useWorkspace, type ColumnDef, type RelationEdge, type RelationNode } from "@platform/ui";
import { ArrowLeftRight, ChevronLeft, Copy, Crosshair, Expand, RefreshCw, Search } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";

type Params = Record<string, string>;
type Graph = Api.Lineage;
type Node = Api.LineageNode;
type Edge = Api.LineageEdge;
type Group = Api.LineageGroup;
type SavedLineage = { version: number; params: Params };

const empty: Graph = {
  nodes: [], edges: [], groups: [],
  scope: { mode: "overview" },
  basis: { generation: 0, preview: "", guard: "", builtAt: "" },
  total: { nodes: 0, edges: 0 }, returned: { nodes: 0, edges: 0 },
  truncated: false, coverage: { markingsComplete: true, statusComplete: true, fieldsComplete: false }, generatedAt: "",
};

const kindView: Record<string, string> = { connection: "connection", source: "data-source", dataset: "dataset", pipeline: "pipeline", object: "object-type", writeback: "writeback" };
const kindOrder = ["connection", "source", "dataset", "pipeline", "object", "writeback"];
const kindTitle: Record<string, string> = { connection: "Connection", source: "Source", dataset: "Dataset", pipeline: "Pipeline", object: "Object type", writeback: "Writeback" };
const kindTone: Record<string, string> = { connection: "neutral", source: "info", dataset: "success", pipeline: "info", object: "neutral", writeback: "warning" };
const statusText: Record<string, string> = { current: "Current", stale: "Stale", failed: "Failed", "never-run": "Never run", unreadable: "Unreadable", unknown: "Unknown" };
type ColorBy = "resource" | "protection" | "status";
const protectionTone: Record<string, string> = { restricted: "danger", confidential: "warning", internal: "info", "": "neutral" };
const statusTone: Record<string, string> = { failed: "danger", stale: "warning", "never-run": "neutral", unknown: "neutral", unreadable: "neutral", current: "success" };

function clean(params: Params): Params {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== ""));
}

function nodeId(params: Params): string {
  return params.focus || params.node || params.ref || "";
}

function statusNode(node: Node): string {
  return statusText[node.status] ?? node.status;
}

function resourceOpen(open: ReturnType<typeof useWorkspace>["open"], node: Node, params: Params) {
  if (!node.canOpen) return;
  const view = kindView[node.kind];
  if (!view) return;
  open({ view, params: { id: node.record ?? "", ...(params.branch ? { branch: params.branch } : {}) } }, { window: "tab" });
}

function displayVersion(node: Node): string {
  if (node.datasetVersion) return `v${node.datasetVersion}`;
  if (node.definitionVersion) return `v${node.definitionVersion}`;
  return "";
}

function runningVersion(node: Node): string {
  return node.executionVersion ? `v${node.executionVersion}` : "";
}

function boundaryText(boundary: Node["boundary"]): string {
  if (!boundary) return "";
  return t("{upstream} upstream · {downstream} downstream boundary", { upstream: boundary.upstream ?? 0, downstream: boundary.downstream ?? 0 });
}

function nodeTone(node: Node, color: ColorBy): string {
  if (color === "protection") return protectionTone[node.guard ?? ""] ?? "neutral";
  if (color === "status") return statusTone[node.status] ?? "neutral";
  return kindTone[node.kind] ?? "neutral";
}

function relationNodes(visible: Node[], color: ColorBy): RelationNode[] {
  return visible.map(node => ({
    id: node.id,
    label: node.title || node.name,
    class: node.kind,
    tone: nodeTone(node, color),
    caption: `${kindTitle[node.kind] ?? node.kind}${displayVersion(node) ? ` · ${displayVersion(node)}` : ""}${runningVersion(node) ? ` → ${runningVersion(node)}` : ""} · ${statusNode(node)}${node.boundary ? ` · ${boundaryText(node.boundary)}` : ""}`,
    detail: [node.project, node.name].filter(Boolean).join(" · "),
    dim: false,
    flag: node.status === "failed" ? (node.errorCode || t("Failed")) : node.status === "stale" ? t("Stale") : undefined,
    facts: [
      { label: t("Kind"), value: kindTitle[node.kind] ?? node.kind },
      { label: t("Project"), value: node.project ?? "—" },
      { label: t("Status"), value: statusNode(node) },
      { label: t("Protection"), value: node.guard ?? t("None") },
    ],
  }));
}

function relationEdges(edges: Edge[]): RelationEdge[] {
  return edges.map(edge => ({
    id: edge.id,
    source: edge.from,
    target: edge.to,
    directed: true,
    dashed: edge.stale,
    tone: edge.stale ? "warning" : undefined,
    label: edge.role === "input" || edge.role === "join" || edge.role === "lookup" ? `${edge.role}${edge.consumedVersion ? ` v${edge.consumedVersion}` : ""}` : edge.role,
  }));
}

function fieldRelationNodes(nodes: NonNullable<Graph["fieldNodes"]>): RelationNode[] {
  return nodes.map(node => ({
    id: node.id,
    label: node.title || node.name,
    class: "field",
    tone: node.certainty === "unverified" ? "neutral" : node.certainty === "derived" ? "warning" : "info",
    caption: `${node.kind} · ${node.certainty}`,
    detail: node.project,
    dim: false,
    flag: node.certainty === "unverified" ? t("unverified") : undefined,
    facts: [
      { label: t("Resource"), value: node.node },
      { label: t("Field"), value: node.name },
      { label: t("Certainty"), value: t(node.certainty) },
    ],
  }));
}

function fieldRelationEdges(edges: NonNullable<Graph["fieldEdges"]>): RelationEdge[] {
  return edges.map(edge => ({
    id: edge.id,
    source: edge.from,
    target: edge.to,
    directed: true,
    dashed: edge.certainty === "unverified",
    tone: edge.certainty === "unverified" ? "neutral" : undefined,
    label: `${edge.operation}${edge.step ? ` · ${edge.step}` : ""}`,
  }));
}

function pathNodes(edges: Edge[], start: string, end: string): Set<string> {
  const next = new Map<string, string[]>();
  for (const edge of edges) {
    next.set(edge.from, [...(next.get(edge.from) ?? []), edge.to]);
    next.set(edge.to, [...(next.get(edge.to) ?? []), edge.from]);
  }
  const previous = new Map<string, string>();
  const queue = [start];
  previous.set(start, "");
  while (queue.length) {
    const at = queue.shift()!;
    if (at === end) break;
    for (const item of next.get(at) ?? []) {
      if (previous.has(item)) continue;
      previous.set(item, at);
      queue.push(item);
    }
  }
  if (!previous.has(end)) return new Set();
  const out = new Set<string>();
  for (let at = end; at; at = previous.get(at) ?? "") {
    out.add(at);
    if (at === start) break;
  }
  return out;
}

export function LineagePage({ params }: { params: Params }) {
  const { open, notify } = useWorkspace();
  const { holds, decide } = useHost();
  const branches = useReadQuery<Api.BranchDetail[]>("/v1/branches", undefined, holds("build", "builder"));
  const savedViews = useReadQuery<Api.SavedView[]>("/v1/views");
  const focusParam = nodeId(params);
  const mode = params.mode || (focusParam ? "focus" : "overview");
  const project = params.project ?? "";
  const branch = params.branch ?? "";
  const direction = params.direction === "upstream" || params.direction === "downstream" ? params.direction : "both";
  const depth = Math.max(1, Math.min(3, Number(params.depth || 1)));
  const color: ColorBy = params.color === "protection" || params.color === "status" ? params.color : "resource";
  const fields = params.fields === "on" || !!params.field;
  const queryPath = `/v1/lineage?${new URLSearchParams(clean({ mode, project, focus: focusParam, branch, direction, depth: String(depth), fields: fields ? "on" : "", field: params.field ?? "" })).toString()}`;
  const noFocus = mode === "focus" && !focusParam && !project;
  const query = useReadQuery<Graph>(queryPath, undefined, !noFocus);
  const graph = query.data ?? empty;
  const [search, setSearch] = useState("");
  const [kind, setKind] = useState("");
  const [status, setStatus] = useState("");
  const [hideUnconnected, setHideUnconnected] = useState(false);
  const [savingView, setSavingView] = useState(false);
  const [viewTitle, setViewTitle] = useState("");
  const [isolated, setIsolated] = useState(false);
  const selected = params.selected || graph.scope.focus || "";
  const root = graph.scope.focus || selected;
  const selectedNode = graph.nodes.find(node => node.id === selected);
  const fieldMode = !!params.field;
  const selectedField = (graph.fieldNodes ?? []).find(field => field.id === params.field);

  const update = (patch: Params) => {
    const next = clean({ ...params, ...patch });
    open({ view: "lineage", params: next }, { replace: true });
  };
  const openNode = (node: Node) => resourceOpen(open, node, params);
  const focus = (node: Node) => update({ mode: "focus", focus: node.id, selected: node.id, project: node.project ?? project });
  const focusSelected = (patch: Params = {}) => {
    if (!selectedNode) return;
    update({ mode: "focus", focus: selectedNode.id, selected: selectedNode.id, ...patch });
  };
  const copy = async (text: string, message: string) => {
    try { await navigator.clipboard.writeText(text); notify.success(message); } catch { notify.error(t("Copy failed")); }
  };
  const lineageViews = (savedViews.data ?? []).filter(view => view.entity === "lineage" && !view.archived);
  const saveView = async () => {
    if (!viewTitle.trim()) return;
    const id = newId("LINEAGE");
    if (await decide("work.view.save", { type: "work.view", id }, {
      title: viewTitle,
      entity: "lineage",
      state: JSON.stringify({ version: 1, params: clean({ ...params, selected: "" }) }),
    })) {
      setSavingView(false);
      setViewTitle("");
      void savedViews.refetch();
    }
  };
  const loadView = (view: Api.SavedView) => {
    try {
      const parsed = JSON.parse(view.state) as SavedLineage;
      if (parsed.version === 1 && parsed.params && typeof parsed.params === "object") open({ view: "lineage", params: parsed.params }, { replace: true });
    } catch {
      notify.error(t("This saved view is unreadable."));
    }
  };

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement || event.target instanceof HTMLTextAreaElement || event.target instanceof HTMLSelectElement) return;
      if (event.key.toLowerCase() === "f" && selectedNode) { event.preventDefault(); focus(selectedNode); }
      if (event.key.toLowerCase() === "i" && selectedNode) { event.preventDefault(); setIsolated(value => !value); }
      if (event.key.toLowerCase() === "x" && selectedNode) { event.preventDefault(); focusSelected({ depth: String(Math.min(3, depth + 1)) }); }
      if (event.key.toLowerCase() === "escape") { setIsolated(false); update({ selected: "" }); }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedNode, depth, focusSelected, update]);

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return graph.nodes.filter(node => {
      if (kind && node.kind !== kind) return false;
      if (status && node.status !== status) return false;
      if (hideUnconnected && !node.connected) return false;
      if (needle && !`${node.title} ${node.name} ${node.project ?? ""}`.toLowerCase().includes(needle)) return false;
      return true;
    });
  }, [graph.nodes, hideUnconnected, kind, search, status]);
  const visible = useMemo(() => {
    if (!isolated || !root || !selected || root === selected) return filtered;
    const keep = pathNodes(graph.edges, root, selected);
    return keep.size ? filtered.filter(node => keep.has(node.id)) : filtered;
  }, [filtered, graph.edges, isolated, root, selected]);
  const visibleEdges = useMemo(() => graph.edges.filter(edge => visible.some(node => node.id === edge.from) && visible.some(node => node.id === edge.to)), [graph.edges, visible]);
  const hidden = graph.nodes.length - filtered.length;
  const toolbar = <div className="flex flex-wrap items-center gap-1">
    <Select aria-label={t("Branch")} value={branch} onChange={event => update({ branch: event.target.value, focus: "", selected: "" })} className="w-44" disabled={!holds("build", "builder")}>
      <option value="">{t("main")}</option>
      {(branches.data ?? []).filter(item => item.state === "open" || item.state === "proposed").map(item => <option key={item.id} value={item.id}>{item.title}</option>)}
    </Select>
    {fieldMode && <Button size="sm" variant="ghost" onClick={() => update({ field: "", selected: params.focus || "" })}><ChevronLeft />{t("Back to resources")}</Button>}
    <SegmentedChoice compact label={t("Direction")} value={direction} onChange={value => focusSelected({ direction: value })} options={[{ value: "upstream", label: t("Upstream") }, { value: "both", label: t("Both") }, { value: "downstream", label: t("Downstream") }]} />
    <Select aria-label={t("Depth")} value={String(depth)} onChange={event => focusSelected({ depth: event.target.value })} className="w-24">
      {[1, 2, 3].map(value => <option key={value} value={value}>{t("Depth {n}", { n: value })}</option>)}
    </Select>
    <Select aria-label={t("Color by")} value={color} onChange={event => update({ color: event.target.value })} className="w-36">
      <option value="resource">{t("Resource type")}</option><option value="protection">{t("Protection")}</option><option value="status">{t("Status")}</option>
    </Select>
    <Button size="sm" variant="ghost" onClick={() => focusSelected()} disabled={!selectedNode}><Crosshair />{t("Focus")}</Button>
    <Button size="sm" variant="ghost" onClick={() => setIsolated(value => !value)} disabled={!selected}><ArrowLeftRight />{isolated ? t("Show all") : t("Isolate")}</Button>
    <Button size="sm" variant="ghost" onClick={() => focusSelected({ depth: String(Math.min(3, depth + 1)) })} disabled={!selectedNode}><Expand />{t("Expand")}</Button>
    <Button size="sm" variant="ghost" onClick={() => void query.refetch()}><RefreshCw />{t("Refresh")}</Button>
    <Button size="sm" variant="ghost" onClick={() => void copy(location.href, t("Link copied"))}><Copy />{t("Copy link")}</Button>
    <Button size="sm" variant="ghost" onClick={() => setSavingView(true)}>{t("Save view")}</Button>
  </div>;

  if (mode === "overview" && !project) return <Overview graph={graph} loading={query.isPending} error={!!query.isError} onRetry={() => void query.refetch()} onProject={id => update({ project: id, mode: "overview" })} />;
  if (noFocus) return <EmptyState title={t("Choose a resource")} hint={t("Open a data resource from Search or its resource page to start a focused graph.")} action={<Button onClick={() => update({ mode: "overview", project: "", focus: "", selected: "" })}>{t("Overview")}</Button>} />;
  if (query.isError) return <EmptyState title={t("Data lineage could not be loaded")} hint={t("Try again, or return to the overview.")} action={<div className="flex gap-2"><Button onClick={() => void query.refetch()}>{t("Retry")}</Button><Button variant="ghost" onClick={() => update({ mode: "overview", project: "", focus: "", selected: "" })}>{t("Overview")}</Button></div>} />;
  if (query.isPending) return <div className="grid h-full place-items-center"><p className="text-sm text-muted">{t("Loading…")}</p></div>;
  if (!graph.nodes.length) return <EmptyState title={t("No data resources yet")} hint={t("Start in Data Connection, then return here to trace the flow.")} action={<Button onClick={() => open({ view: "connection" })}>{t("Open Data Connection")}</Button>} />;

  const left = <div className="grid gap-3 p-2">
    <label className="grid gap-1 text-xs font-medium">{t("Find in graph")}<span className="relative"><Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" /><Input value={search} onChange={event => setSearch(event.target.value)} className="pl-7" placeholder={t("Name or API name")} /></span></label>
    {lineageViews.length > 0 && <label className="grid gap-1 text-xs font-medium">{t("Saved views")}<Select aria-label={t("Saved views")} value="" onChange={event => { const view = lineageViews.find(item => item.id === event.target.value); if (view) loadView(view); }}><option value="">{t("Choose a saved view")}</option>{lineageViews.map(view => <option key={view.id} value={view.id}>{view.title}</option>)}</Select></label>}
    <label className="grid gap-1 text-xs font-medium">{t("Kind")}<Select value={kind} onChange={event => setKind(event.target.value)}><option value="">{t("All kinds")}</option>{kindOrder.map(value => <option key={value} value={value}>{t(kindTitle[value] ?? value)}</option>)}</Select></label>
    <label className="grid gap-1 text-xs font-medium">{t("Status")}<Select value={status} onChange={event => setStatus(event.target.value)}><option value="">{t("All states")}</option>{Object.entries(statusText).map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}</Select></label>
    <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={hideUnconnected} onChange={event => setHideUnconnected(event.target.checked)} />{t("Hide unconnected")}</label>
    <div className="grid gap-1 text-xs"><span className="font-medium">{t("Coverage")}</span><span className="text-muted">{t("{visible} of {total} resources", { visible: visible.length, total: graph.total.nodes })}</span>{hidden > 0 && <span className="text-warning">{t("{n} hidden by filters", { n: hidden })}</span>}</div>
    <Button size="sm" variant="ghost" onClick={() => update({ mode: "overview", project: "", focus: "", selected: "" })}><ChevronLeft />{t("Overview")}</Button>
  </div>;

  const inspector = fieldMode
    ? <FieldInspector field={selectedField} graph={graph} onSelect={id => update({ selected: id })} />
    : <Inspector node={selectedNode} graph={graph} onOpen={openNode} onFocus={focus} onSelect={id => update({ selected: id })} onCopyRID={rid => void copy(rid, t("RID copied"))} onCopyLink={() => void copy(location.href, t("Link copied"))} onFields={() => update({ fields: "on", field: "" })} />;
  const downstream = graph.impact?.downstream ?? (root ? reachable(graph.edges, root, true).length : 0);
  const dock = {
    label: t("Analysis"),
    tabs: [
      { id: "impact", title: t("Impact"), badge: downstream, content: <Impact graph={graph} root={root} onSelect={id => update({ selected: id })} /> },
      { id: "coverage", title: t("Coverage"), badge: graph.unreadable?.length, content: <Coverage graph={graph} /> },
      { id: "fields", title: t("Fields"), content: <Fields graph={graph} node={selectedNode} selected={params.field ?? ""} onSelect={id => update({ fields: "on", field: id, selected: id })} /> },
    ],
  };
  return <><Workbench storageKey={`lineage:${root || project || "overview"}`} crumbs={[{ label: t("Data Lineage"), onClick: () => update({ mode: "overview", project: "", focus: "", selected: "" }) }, ...(project ? [{ label: project }] : [])]}
    title={selectedNode?.title || graph.scope.focus || t("Graph")} status={<span className="text-xs text-muted">{graph.basis.preview} · {new Date(graph.generatedAt).toLocaleString()}</span>}
    actions={toolbar} left={{ label: t("Scope"), content: left }} right={{ label: t("Inspector"), content: inspector }} dock={dock} mainLabel={t("Data lineage graph")} onKeyDown={() => undefined}>
    <div className="relative h-full min-h-[26rem]">
      {graph.nodes.some(node => node.markingStatus !== "consistent") && <div className="absolute left-2 top-2 z-10"><Notice tone="warning" message={t("Protection differs from preview. The saved definition is not active yet.")} /></div>}
      {graph.truncated && <div className="absolute left-2 top-2 z-10"><Notice tone="warning" message={t("Showing {returned} of {total} resources. Reduce depth or choose one direction.", { returned: graph.returned.nodes, total: graph.total.nodes })} /></div>}
      <RelationCanvas
        nodes={fieldMode ? fieldRelationNodes((graph.fieldNodes ?? []).filter(field => !search.trim() || `${field.title} ${field.name}`.toLowerCase().includes(search.trim().toLowerCase()))) : relationNodes(visible, color)}
        edges={fieldMode ? fieldRelationEdges((graph.fieldEdges ?? []).filter(edge => (graph.fieldNodes ?? []).some(field => field.id === edge.from) && (graph.fieldNodes ?? []).some(field => field.id === edge.to))) : relationEdges(visibleEdges)}
        selected={selected}
        height="100%"
        label={fieldMode ? t("Field lineage") : t("Data Lineage")}
        storeKey={`lineage:${fieldMode ? "fields:" + (params.field || "all") : root || project || "overview"}`}
        onSelect={(id?: string) => update({ selected: id ?? "" })}
        onOpen={(id: string) => { if (fieldMode) return; const node = graph.nodes.find(item => item.id === id); if (node) openNode(node); }}
      />
    </div>
  </Workbench>
    <Dialog open={savingView} onOpenChange={open => { if (!open) setSavingView(false); }} title={t("Save view")}>
      <Form className="grid gap-3" onSubmit={() => { if (viewTitle.trim()) void saveView(); }}>
        <Input aria-label={t("Name")} placeholder={t("Name of the view")} value={viewTitle} onChange={event => setViewTitle(event.target.value)} autoFocus />
        <div className="flex justify-end gap-2"><Button type="button" onClick={() => setSavingView(false)}>{t("Cancel")}</Button><Button type="submit" variant="primary" disabled={!viewTitle.trim()}>{t("Save")}</Button></div>
      </Form>
    </Dialog>
  </>;
}

function Overview({ graph, loading, error, onRetry, onProject }: { graph: Graph; loading: boolean; error: boolean; onRetry: () => void; onProject: (id: string) => void }) {
  const [search, setSearch] = useState("");
  const all = graph.groups ?? [];
  const needle = search.trim().toLowerCase();
  const rows = needle ? all.filter(row => row.project.toLowerCase().includes(needle)) : all;
  const resources = all.reduce((sum, row) => sum + row.resources, 0);
  const stale = all.reduce((sum, row) => sum + row.stale, 0);
  const failed = all.reduce((sum, row) => sum + row.failed, 0);
  const never = all.reduce((sum, row) => sum + row.neverRun, 0);
  const columns: ColumnDef<Group, unknown>[] = [
    { id: "project", header: t("Project"), accessorFn: row => row.project },
    { id: "resources", header: t("Resources"), accessorFn: row => Object.entries(row.byKind ?? {}).map(([key, value]) => `${kindTitle[key] ?? key} ${value}`).join(" · ") || String(row.resources) },
    { id: "stale", header: t("Stale"), accessorFn: row => row.stale },
    { id: "failed", header: t("Failed"), accessorFn: row => row.failed },
    { id: "never", header: t("Never run"), accessorFn: row => row.neverRun },
    { id: "cross", header: t("Cross-project"), accessorFn: row => `${row.crossIn ?? 0} in · ${row.crossOut ?? 0} out` },
    { id: "unconnected", header: t("Unconnected"), accessorFn: row => row.unconnected ?? 0 },
    { id: "updated", header: t("Updated"), accessorFn: row => row.updated ? new Date(row.updated).toLocaleString() : "—" },
    { id: "guard", header: t("Highest protection"), accessorFn: row => row.guard || t("None") },
  ];
  return <div className="grid gap-4 p-4">
    <PageTitle title={t("Data Lineage")} subtitle={t("{projects} projects · {resources} resources", { projects: all.length, resources })} onRefresh={onRetry} />
    <div className="grid gap-2 sm:grid-cols-4">{[[t("Resources"), resources], [t("Stale"), stale], [t("Failed"), failed], [t("Never run"), never]].map(([label, value]) => <Panel key={String(label)} className="p-3"><p className="text-2xl font-semibold tabular-nums">{String(value)}</p><p className="text-xs text-muted">{String(label)}</p></Panel>)}</div>
    {error && <Notice tone="danger" message={t("Data lineage could not be loaded.")} />}
    <Panel title={t("Projects")}>
      <div className="border-b border-border p-2"><Input type="search" aria-label={t("Find a project")} placeholder={t("Find a project")} value={search} onChange={event => setSearch(event.target.value)} /></div>
      <DataTable data={rows} columns={columns} getRowId={row => row.project} loading={loading} empty={t("No data resources yet. Start in Data Connection.")} onRowClick={row => onProject(row.project)} searchable={false} height={420} />
    </Panel>
  </div>;
}

function PageTitle({ title, subtitle, onRefresh }: { title: string; subtitle: string; onRefresh: () => void }) {
  return <div className="flex min-w-0 items-center justify-between gap-3"><div className="min-w-0"><h1 className="truncate text-xl font-semibold">{title}</h1><p className="text-xs text-muted">{subtitle}</p></div><Button size="sm" variant="ghost" onClick={onRefresh}><RefreshCw />{t("Refresh")}</Button></div>;
}

function FieldInspector({ field, graph, onSelect }: { field?: NonNullable<Graph["fieldNodes"]>[number]; graph: Graph; onSelect: (id: string) => void }) {
  if (!field) return <div className="p-3 text-xs text-muted">{t("Choose a field to trace its source and downstream use.")}</div>;
  const incoming = (graph.fieldEdges ?? []).filter(edge => edge.to === field.id);
  const outgoing = (graph.fieldEdges ?? []).filter(edge => edge.from === field.id);
  return <div className="grid gap-3 overflow-auto p-3 text-xs">
    <div><h2 className="truncate text-sm font-semibold">{field.title || field.name}</h2><p className="truncate text-muted">{field.kind} · {field.node}</p></div>
    <PropertyList items={[
      [t("Resource"), field.node],
      [t("Field"), field.name],
      [t("Certainty"), t(field.certainty)],
    ]} />
    <Section title={t("Inputs ({n})", { n: incoming.length })}><FieldNeighbors edges={incoming} graph={graph} side="from" onSelect={onSelect} /></Section>
    <Section title={t("Outputs ({n})", { n: outgoing.length })}><FieldNeighbors edges={outgoing} graph={graph} side="to" onSelect={onSelect} /></Section>
  </div>;
}

function FieldNeighbors({ edges, graph, side, onSelect }: { edges: NonNullable<Graph["fieldEdges"]>; graph: Graph; side: "from" | "to"; onSelect: (id: string) => void }) {
  if (!edges.length) return <p className="text-muted">{t("None")}</p>;
  return <div className="grid gap-1">{edges.map(edge => {
    const id = side === "from" ? edge.from : edge.to;
    const field = (graph.fieldNodes ?? []).find(item => item.id === id);
    return <button key={edge.id} type="button" className="grid min-w-0 grid-cols-[1fr_auto] items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-row-hover" onClick={() => onSelect(id)}><span className="truncate">{field?.title || field?.name || id}</span><span className="text-muted">{edge.operation} · {t(edge.certainty)}</span></button>;
  })}</div>;
}

function Inspector({ node, graph, onOpen, onFocus, onSelect, onCopyRID, onCopyLink, onFields }: { node?: Node; graph: Graph; onOpen: (node: Node) => void; onFocus: (node: Node) => void; onSelect: (id: string) => void; onCopyRID: (rid: string) => void; onCopyLink: () => void; onFields: () => void }) {
  if (!node) return <div className="p-3 text-xs text-muted">{t("Select a resource to inspect its versions, protection and inputs.")}</div>;
  const incoming = graph.edges.filter(edge => edge.to === node.id);
  const outgoing = graph.edges.filter(edge => edge.from === node.id);
  return <div className="grid gap-3 overflow-auto p-3 text-xs">
    <div className="flex items-start gap-2"><div className="min-w-0 flex-1"><h2 className="truncate text-sm font-semibold">{node.title || node.name}</h2><p className="truncate text-muted">{kindTitle[node.kind] ?? node.kind} · {node.name}</p></div></div>
    <div className="flex flex-wrap gap-1"><Button size="sm" variant="primary" disabled={!node.canOpen} onClick={() => onOpen(node)}>{t("Open")}</Button><Button size="sm" variant="ghost" onClick={() => onFocus(node)}><Crosshair />{t("Focus here")}</Button><Button size="sm" variant="ghost" onClick={onFields}>{t("Show fields")}</Button><Button size="sm" variant="ghost" onClick={() => node.rid && onCopyRID(node.rid)} disabled={!node.rid}><Copy />{t("RID")}</Button><Button size="sm" variant="ghost" onClick={onCopyLink}><Copy />{t("Link")}</Button></div>
    <PropertyList items={[
      [t("Status"), statusNode(node)], [t("Project"), node.project ?? "—"], [t("RID"), node.rid ? <code className="break-all">{node.rid}</code> : "—"],
      [t("Declared"), node.declared || "—"], [t("Preview"), node.effective || "—"], [t("Protection"), node.guard || "—"],
      ...(node.markingStatus !== "consistent" ? [[t("Marking state"), node.markingStatus] as [string, ReactNode]] : []),
      [t("Saved definition"), node.definitionVersion ? `v${node.definitionVersion}` : "—"], [t("Running definition"), runningVersion(node) || "—"], [t("Dataset version"), node.datasetVersion ? `v${node.datasetVersion}` : "—"],
      [t("Rows"), node.canReadData && node.rows ? String(node.rows) : "—"], [t("Last activity"), node.at ? new Date(node.at).toLocaleString() : "—"],
      ...(node.staleReasons?.length ? [[t("Stale reasons"), node.staleReasons.join(", ")] as [string, ReactNode]] : []),
      ...(node.boundary ? [[t("Boundaries"), `${boundaryText(node.boundary)}${node.boundary.reason ? ` · ${t(node.boundary.reason)}` : ""}`] as [string, ReactNode]] : []),
    ]} />
    {node.errorCode && <Notice tone={node.status === "failed" ? "danger" : "warning"} message={node.errorCode} />}
    <Section title={t("Inputs ({n})", { n: incoming.length })}><Neighbors edges={incoming} graph={graph} side="to" onSelect={onSelect} /></Section>
    <Section title={t("Outputs ({n})", { n: outgoing.length })}><Neighbors edges={outgoing} graph={graph} side="from" onSelect={onSelect} /></Section>
    <section className="grid gap-1"><h3 className="font-semibold">{t("Coverage")}</h3><p className="text-muted">{t("{markings} markings · {status} status · {fields} fields", { markings: graph.coverage.markingsComplete ? t("complete") : t("incomplete"), status: graph.coverage.statusComplete ? t("complete") : t("incomplete"), fields: graph.coverage.fieldsComplete ? t("complete") : t("not loaded") })}</p></section>
  </div>;
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return <section className="grid gap-1"><h3 className="font-semibold">{title}</h3>{children}</section>;
}

function Neighbors({ edges, graph, side, onSelect }: { edges: Edge[]; graph: Graph; side: "from" | "to"; onSelect: (id: string) => void }) {
  if (!edges.length) return <p className="text-muted">{t("None")}</p>;
  return <div className="grid gap-1">{edges.map(edge => { const id = side === "to" ? edge.from : edge.to; const node = graph.nodes.find(item => item.id === id); return <button key={edge.id} type="button" className="grid min-w-0 grid-cols-[1fr_auto] items-center gap-2 rounded px-1 py-0.5 text-left hover:bg-row-hover" onClick={() => onSelect(id)}><span className="truncate">{node?.title || id}</span><span className="text-muted">{edge.role}{edge.consumedVersion ? ` v${edge.consumedVersion}` : ""}{edge.stale ? ` · ${t("stale")}` : ""}</span></button>; })}</div>;
}

function Impact({ graph, root, onSelect }: { graph: Graph; root: string; onSelect: (id: string) => void }) {
  if (!root) return <p className="p-3 text-xs text-muted">{t("Choose a focus resource to see impact.")}</p>;
  if (graph.impact) {
    return <div className="grid gap-3 p-3 text-xs">
      <div className="flex gap-4"><span>{t("Upstream")}: <strong>{graph.impact.upstream}</strong></span><span>{t("Downstream")}: <strong>{graph.impact.downstream}</strong></span>{graph.impact.atLeast && <span className="text-muted">{t("At least {n} shown", { n: graph.impact.items.length })}</span>}</div>
      <div className="flex flex-wrap gap-1">{graph.impact.items.map(item => { const node = graph.nodes.find(value => value.id === item.id); return <Button key={item.id} size="sm" variant="ghost" onClick={() => onSelect(item.id)}>{node?.title || item.title || item.id}<span className="ml-1 text-muted">{item.distance}</span></Button>; })}</div>
    </div>;
  }
  const down = reachable(graph.edges, root, true);
  const up = reachable(graph.edges, root, false);
  return <div className="grid gap-3 p-3 text-xs"><div className="flex gap-4"><span>{t("Upstream")}: <strong>{up.length}</strong></span><span>{t("Downstream")}: <strong>{down.length}</strong></span></div><div className="flex flex-wrap gap-1">{down.map(id => { const node = graph.nodes.find(item => item.id === id); return <Button key={id} size="sm" variant="ghost" onClick={() => onSelect(id)}>{node?.title || id}</Button>; })}</div></div>;
}

function Fields({ graph, node, selected, onSelect }: { graph: Graph; node?: Node; selected: string; onSelect: (id: string) => void }) {
  const fields = (graph.fieldNodes ?? []).filter(field => !node || field.node === node.id);
  if (!graph.coverage.fieldsComplete) return <p className="p-3 text-xs text-muted">{t("Open Show fields to load field lineage.")}</p>;
  if (!fields.length) return <p className="p-3 text-xs text-muted">{t("No declared field source is available for this resource.")}</p>;
  const current = fields.find(field => field.id === selected);
  const edges = selected ? (graph.fieldEdges ?? []).filter(edge => edge.from === selected || edge.to === selected) : [];
  return <div className="grid gap-2 overflow-auto p-3 text-xs">
    <div className="flex flex-wrap gap-1">{fields.map(field => <Button key={field.id} size="sm" variant={field.id === selected ? "primary" : "ghost"} onClick={() => onSelect(field.id)}>{field.title || field.name}<span className="ml-1 text-muted">{t(field.certainty)}</span></Button>)}</div>
    {!selected && <p className="text-muted">{t("Choose a field to trace its source and downstream use.")}</p>}
    {current && <div className="grid gap-1">
      <p className="font-medium">{current.title || current.name} · {t(current.certainty)}</p>
      {edges.length ? edges.map(edge => {
        const from = (graph.fieldNodes ?? []).find(field => field.id === edge.from);
        const to = (graph.fieldNodes ?? []).find(field => field.id === edge.to);
        return <p key={edge.id} className="text-muted">{from?.title || from?.name || edge.from} → {to?.title || to?.name || edge.to}{edge.step ? ` · ${t("step {n}", { n: edge.step })}` : ""} · {t(edge.operation)}</p>;
      }) : <p className="text-muted">{t("No declared field edge is available.")}</p>}
    </div>}
  </div>;
}

function Coverage({ graph }: { graph: Graph }) {
  return <div className="grid gap-2 p-3 text-xs"><p>{t("Showing {returned} of {total} resources.", { returned: graph.returned.nodes, total: graph.total.nodes })}</p>{graph.truncated && <Notice tone="warning" message={(graph.truncationReasons ?? []).map(reason => reason === "node-limit" ? t("Node limit") : reason === "edge-limit" ? t("Edge limit") : reason).join(" · ")} />}{(graph.unreadable ?? []).map(item => <p key={item.reason} className="text-warning">{t("{n} unreadable: {reason}", { n: item.count, reason: item.reason })}</p>)}<p className="text-muted">{t("Built {time}", { time: graph.generatedAt ? new Date(graph.generatedAt).toLocaleString() : "—" })}</p></div>;
}

function reachable(edges: Edge[], start: string, forward: boolean): string[] {
  const seen = new Set<string>();
  const queue = [start];
  while (queue.length) {
    const at = queue.shift()!;
    for (const edge of edges) {
      const next = forward ? edge.from === at && edge.to : edge.to === at && edge.from;
      if (next && next !== start && !seen.has(next)) { seen.add(next); queue.push(next); }
    }
  }
  return [...seen];
}
