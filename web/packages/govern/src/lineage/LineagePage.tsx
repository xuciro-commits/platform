import { newId, useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { ActionMenu, Button, DataTable, Dialog, EmptyState, Form, Input, Notice, Panel, PropertyList, RelationCanvas, SegmentedChoice, Select, Tag, ViewVisibilityContext, Workbench, t, useWorkspace, type ColumnDef, type RelationEdge, type RelationNode, type Tone } from "@platform/ui";
import { ArrowLeftRight, ChevronLeft, Copy, Crosshair, Download, Expand, RefreshCw, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent, type ReactNode, type RefObject } from "react";
import { lineageExportFileName, lineageImpactCSV, lineageImpactDocument, lineageSubgraphCSV, lineageSubgraphDocument, type LineageExportFormat, type LineageExportTarget } from "./export";

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
const kindTone: Record<string, Tone> = { connection: "neutral", source: "info", dataset: "success", pipeline: "info", object: "neutral", writeback: "warning" };
const statusText: Record<string, string> = { current: "Current", stale: "Stale", failed: "Failed", "never-run": "Never run", unreadable: "Unreadable", unknown: "Unknown" };
type ColorBy = "resource" | "protection" | "status";
const protectionTone: Record<string, Tone> = { restricted: "danger", confidential: "warning", internal: "info", "": "neutral" };
const statusTone: Record<string, Tone> = { failed: "danger", stale: "warning", "never-run": "neutral", unknown: "neutral", unreadable: "neutral", current: "success" };

function clean(params: Params): Params {
  return Object.fromEntries(Object.entries(params).filter(([, value]) => value !== ""));
}

function nodeId(params: Params): string {
  return params.focus || params.node || params.ref || "";
}

function statusNode(node: Node): string {
  return statusText[node.status] ?? node.status;
}

function resourceLabel(node: Node): string {
  return [
    node.title || node.name,
    t(kindTitle[node.kind] ?? node.kind),
    t(statusNode(node)),
    node.guard ? `${t("Protection")}: ${node.guard}` : "",
  ].filter(Boolean).join(" · ");
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

function saveExport(filename: string, body: string, format: LineageExportFormat) {
  const url = URL.createObjectURL(new Blob([body], { type: format === "csv" ? "text/csv;charset=utf-8" : "application/json;charset=utf-8" }));
  Object.assign(document.createElement("a"), { href: url, download: filename }).click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
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
  const queryPath = `/v1/lineage?${new URLSearchParams(clean({ mode, project, focus: focusParam, branch, direction, depth: String(depth), nodeLimit: "2000", edgeLimit: "5000", fields: fields ? "on" : "", field: params.field ?? "" })).toString()}`;
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
  const [announcement, setAnnouncement] = useState("");
  const [inspectorOpenRequest, setInspectorOpenRequest] = useState(0);
  const resourceListRef = useRef<HTMLUListElement>(null);
  const inspectorRef = useRef<HTMLDivElement>(null);
  const selected = params.selected || graph.scope.focus || "";
  const root = graph.scope.focus || selected;
  const viewRoot = graph.scope.focus || project || "overview";
  const selectedNode = graph.nodes.find(node => node.id === selected);
  const fieldMode = !!params.field;
  const selectedField = (graph.fieldNodes ?? []).find(field => field.id === params.field);
  const focusMode = mode === "focus" && !!focusParam;
  const projectID = project || graph.scope.project || graph.nodes.find(node => node.id === graph.scope.focus)?.project || "";

  const update = (patch: Params, replace = true) => {
    const next = clean({ ...params, ...patch });
    open({ view: "lineage", params: next }, { replace });
  };
  const openNode = (node: Node) => resourceOpen(open, node, params);
  const focus = (node: Node) => update({ mode: "focus", focus: node.id, selected: node.id, project: node.project ?? projectID }, false);
  const focusNode = (node: Node) => {
    focus(node);
    setAnnouncement(t("Focused on {name}.", { name: node.title || node.name }));
  };
  const expandSelected = () => {
    if (!selectedNode || !focusMode) return;
    const next = Math.min(3, depth + 1);
    if (next === depth) return;
    update({ depth: String(next) }, false);
    setAnnouncement(t("Expanded graph to depth {depth}.", { depth: next }));
  };
  const backToProject = () => {
    if (!focusMode) return;
    update({ mode: "overview", project: projectID, focus: "", node: "", ref: "", selected: "", field: "", fields: "", depth: "" }, false);
    setAnnouncement(projectID ? t("Returned to the project graph.") : t("Returned to the overview."));
  };
  const toggleIsolated = () => {
    if (!focusMode || !selected || root === selected) {
      setAnnouncement(t("Select another resource to isolate its path."));
      return;
    }
    const next = !isolated;
    setIsolated(next);
    setAnnouncement(next
      ? t("Isolated path shown for {name}.", { name: selectedNode?.title || selectedNode?.name || selected })
      : t("Isolation cleared. All filtered resources are visible."));
  };
  const chooseNode = (node: Node) => {
    update({ selected: node.id });
    setAnnouncement(t("Selected {name}. Details are in the Inspector.", { name: node.title || node.name }));
  };
  const chooseNodeByID = (id: string) => {
    const node = graph.nodes.find(item => item.id === id);
    if (node) chooseNode(node);
    else update({ selected: id });
  };
  const moveToInspector = (node: Node) => {
    chooseNode(node);
    setInspectorOpenRequest(value => value + 1);
    requestAnimationFrame(() => requestAnimationFrame(() => inspectorRef.current?.focus()));
  };
  const chooseField = (id: string, name: string) => {
    update({ fields: "on", field: id, selected: id });
    setAnnouncement(t("Selected field {name}.", { name }));
  };
  const chooseSelection = (id?: string) => {
    if (!id) {
      update({ selected: "" });
      setAnnouncement(t("Selection returned to the focus resource."));
      return;
    }
    if (fieldMode) {
      const field = (graph.fieldNodes ?? []).find(item => item.id === id);
      if (field) chooseField(id, field.title || field.name);
      else update({ selected: id });
      return;
    }
    chooseNodeByID(id);
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
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      const key = event.key.toLowerCase();
      if (key === "f" && selectedNode) { event.preventDefault(); focusNode(selectedNode); }
      if (key === "i" && focusMode && selectedNode && selectedNode.id !== focusParam) { event.preventDefault(); toggleIsolated(); }
      if (key === "x" && focusMode && selectedNode) { event.preventDefault(); expandSelected(); }
      if (key === "escape") {
        if (isolated) {
          setIsolated(false);
          setAnnouncement(t("Isolation cleared."));
        } else if (selected && root && selected !== root) {
          update({ selected: "" });
          setAnnouncement(t("Selection returned to the focus resource."));
        } else if (focusMode) {
          backToProject();
        }
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedNode, isolated, depth, focusMode, focusParam, root, selected, focusNode, expandSelected, toggleIsolated, backToProject, update]);

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
  const shownFieldNodes = useMemo(() => fieldMode ? (graph.fieldNodes ?? []).filter(field => !search.trim() || `${field.title} ${field.name}`.toLowerCase().includes(search.trim().toLowerCase())) : [], [fieldMode, graph.fieldNodes, search]);
  const shownFieldEdges = useMemo(() => fieldMode ? (graph.fieldEdges ?? []).filter(edge => shownFieldNodes.some(field => field.id === edge.from) && shownFieldNodes.some(field => field.id === edge.to)) : [], [fieldMode, graph.fieldEdges, shownFieldNodes]);
  const fieldResourceIDs = useMemo(() => new Set(shownFieldNodes.map(field => field.node)), [shownFieldNodes]);
  const exportNodes = useMemo(() => fieldMode ? graph.nodes.filter(node => fieldResourceIDs.has(node.id)) : visible, [fieldMode, graph.nodes, fieldResourceIDs, visible]);
  const exportEdges = useMemo(() => fieldMode ? graph.edges.filter(edge => fieldResourceIDs.has(edge.from) && fieldResourceIDs.has(edge.to)) : visibleEdges, [fieldMode, fieldResourceIDs, graph.edges, visibleEdges]);
  const chooseSearchResult = () => {
    if (fieldMode) {
      const needle = search.trim().toLowerCase();
      const matches = (graph.fieldNodes ?? []).filter(field => !needle || `${field.title} ${field.name}`.toLowerCase().includes(needle));
      if (!matches.length) {
        setAnnouncement(t("No fields match the current search."));
        return;
      }
      const current = matches.findIndex(field => field.id === params.field);
      const next = matches[current < 0 ? 0 : (current + 1) % matches.length]!;
      chooseField(next.id, next.title || next.name);
      return;
    }
    if (!visible.length) {
      setAnnouncement(t("No resources match the current search and filters."));
      return;
    }
    const current = visible.findIndex(node => node.id === selected);
    const next = visible[current < 0 ? 0 : (current + 1) % visible.length]!;
    resourceListRef.current?.querySelector<HTMLButtonElement>(`[data-lineage-resource="${next.id}"]`)?.focus();
    chooseNode(next);
  };
  const moveResource = (event: ReactKeyboardEvent<HTMLButtonElement>, node: Node) => {
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key) || !visible.length) return;
    event.preventDefault();
    const current = Math.max(0, visible.findIndex(item => item.id === node.id));
    const index = event.key === "Home" ? 0
      : event.key === "End" ? visible.length - 1
      : (current + (event.key === "ArrowDown" ? 1 : -1) + visible.length) % visible.length;
    const next = visible[index]!;
    resourceListRef.current?.querySelector<HTMLButtonElement>(`[data-lineage-resource="${next.id}"]`)?.focus();
    chooseNode(next);
  };
  const hidden = graph.nodes.length - filtered.length;
  const download = (target: LineageExportTarget, format: LineageExportFormat) => {
    if (target === "impact") {
      if (!graph.impact) return;
      const exported = lineageImpactDocument(graph, graph.impact);
      const body = format === "csv" ? lineageImpactCSV(exported) : JSON.stringify(exported, null, 2);
      saveExport(lineageExportFileName(target, format, exported.exportedAt), body, format);
      return;
    }
    const exported = lineageSubgraphDocument(graph, {
      nodes: exportNodes, edges: exportEdges,
      fields: fieldMode ? shownFieldNodes : undefined, fieldEdges: fieldMode ? shownFieldEdges : undefined,
      filters: { search, kind, status, hideUnconnected, isolated, fieldMode },
    });
    const body = format === "csv" ? lineageSubgraphCSV(exported) : JSON.stringify(exported, null, 2);
    saveExport(lineageExportFileName(target, format, exported.exportedAt), body, format);
  };
  const toolbar = <div role="group" aria-label={t("Lineage actions")} className="flex flex-wrap items-center gap-1">
    {focusMode && <Button size="sm" variant="ghost" onClick={backToProject}><ChevronLeft />{projectID ? t("Back to project") : t("Back to overview")}</Button>}
    <Select aria-label={t("Branch")} value={branch} onChange={event => update({ branch: event.target.value, focus: "", selected: "" }, false)} className="w-44" disabled={!holds("build", "builder")}>
      <option value="">{t("main")}</option>
      {(branches.data ?? []).filter(item => item.state === "open" || item.state === "proposed").map(item => <option key={item.id} value={item.id}>{item.title}</option>)}
    </Select>
    {fieldMode && <Button size="sm" variant="ghost" onClick={() => { update({ field: "", selected: params.focus || "" }); setAnnouncement(t("Back to resources")); }}><ChevronLeft />{t("Back to resources")}</Button>}
    {focusMode && <>
      <SegmentedChoice compact label={t("Direction")} value={direction} onChange={value => update({ direction: value, focus: focusParam, selected }, false)} options={[{ value: "upstream", label: t("Upstream") }, { value: "both", label: t("Both") }, { value: "downstream", label: t("Downstream") }]} />
      <Select aria-label={t("Depth")} value={String(depth)} onChange={event => update({ depth: event.target.value, focus: focusParam, selected }, false)} className="w-24">
        {[1, 2, 3].map(value => <option key={value} value={value}>{t("Depth {n}", { n: value })}</option>)}
      </Select>
    </>}
    <Select aria-label={t("Color by")} value={color} onChange={event => update({ color: event.target.value })} className="w-36">
      <option value="resource">{t("Resource type")}</option><option value="protection">{t("Protection")}</option><option value="status">{t("Status")}</option>
    </Select>
    <Button size="sm" variant="ghost" onClick={() => selectedNode && focusNode(selectedNode)} disabled={!selectedNode || selectedNode.id === focusParam} title={t("Make the selected resource the graph root")}><Crosshair />{t("Focus")}</Button>
    {focusMode && <Button size="sm" variant="ghost" onClick={toggleIsolated} disabled={!selectedNode || selectedNode.id === focusParam} title={t("Show only the path between the root and the selected resource")}><ArrowLeftRight />{isolated ? t("Show all") : t("Isolate")}</Button>}
    {focusMode && <Button size="sm" variant="ghost" onClick={expandSelected} disabled={!selectedNode || depth >= 3} title={t("Show one more hop from the current root")}><Expand />{t("Expand")}</Button>}
    <Button size="sm" variant="ghost" onClick={() => void query.refetch()}><RefreshCw />{t("Refresh")}</Button>
    <ActionMenu label={t("Export")} icon={<Download />} commands={[
      { id: "subgraph-json", label: t("Visible subgraph · JSON"), run: () => download("subgraph", "json") },
      { id: "subgraph-csv", label: t("Visible subgraph · CSV"), run: () => download("subgraph", "csv") },
      { id: "impact-json", label: t("Impact list · JSON"), separatorBefore: true, disabled: !graph.impact, reason: !graph.impact ? t("Impact export is available in a focused graph.") : undefined, run: () => download("impact", "json") },
      { id: "impact-csv", label: t("Impact list · CSV"), disabled: !graph.impact, reason: !graph.impact ? t("Impact export is available in a focused graph.") : undefined, run: () => download("impact", "csv") },
    ]}>{t("Export")}</ActionMenu>
    <Button size="sm" variant="ghost" onClick={() => void copy(location.href, t("Link copied"))}><Copy />{t("Copy link")}</Button>
    <Button size="sm" variant="ghost" onClick={() => setSavingView(true)}>{t("Save view")}</Button>
  </div>;

  if (mode === "overview" && !project) return <Overview graph={graph} loading={query.isPending} error={!!query.isError} onRetry={() => void query.refetch()} onProject={id => update({ project: id, mode: "overview" })} />;
  if (noFocus) return <div role="status"><EmptyState title={t("Choose a resource")} hint={t("Open a data resource from Search or its resource page to start a focused graph.")} action={<Button onClick={() => update({ mode: "overview", project: "", focus: "", selected: "" })}>{t("Overview")}</Button>} /></div>;
  if (query.isError) return <div role="alert"><EmptyState title={t("Data lineage could not be loaded")} hint={t("Try again, or return to the overview.")} action={<div className="flex gap-2"><Button onClick={() => void query.refetch()}>{t("Retry")}</Button><Button variant="ghost" onClick={() => update({ mode: "overview", project: "", focus: "", selected: "" })}>{t("Overview")}</Button></div>} /></div>;
  if (query.isPending) return <div className="grid h-full place-items-center"><p role="status" className="text-sm text-muted">{t("Loading lineage graph…")}</p></div>;
  if (!graph.nodes.length) return <div role="status"><EmptyState title={t("No data resources yet")} hint={t("Start in Data Connection, then return here to trace the flow.")} action={<Button onClick={() => open({ view: "connection" })}>{t("Open Data Connection")}</Button>} /></div>;

  const left = <div className="grid gap-3 p-2">
    <section aria-label={t("Filters")} className="grid gap-2">
      <label className="grid gap-1 text-xs font-medium">{t("Find in graph")}<span className="relative"><Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" /><Input value={search} onChange={event => setSearch(event.target.value)} onKeyDown={event => {
        if (event.key === "Enter") { event.preventDefault(); chooseSearchResult(); }
        else if (event.key === "Escape" && search) { event.preventDefault(); setSearch(""); setAnnouncement(t("Search cleared.")); }
      }} className="pl-7" placeholder={t("Name or API name")} /></span></label>
      {lineageViews.length > 0 && <label className="grid gap-1 text-xs font-medium">{t("Saved views")}<Select aria-label={t("Saved views")} value="" onChange={event => { const view = lineageViews.find(item => item.id === event.target.value); if (view) loadView(view); }}><option value="">{t("Choose a saved view")}</option>{lineageViews.map(view => <option key={view.id} value={view.id}>{view.title}</option>)}</Select></label>}
      <label className="grid gap-1 text-xs font-medium">{t("Kind")}<Select value={kind} onChange={event => setKind(event.target.value)}><option value="">{t("All kinds")}</option>{kindOrder.map(value => <option key={value} value={value}>{t(kindTitle[value] ?? value)}</option>)}</Select></label>
      <label className="grid gap-1 text-xs font-medium">{t("Status")}<Select value={status} onChange={event => setStatus(event.target.value)}><option value="">{t("All states")}</option>{Object.entries(statusText).map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}</Select></label>
      <label className="flex items-center gap-2 text-xs"><input type="checkbox" checked={hideUnconnected} onChange={event => setHideUnconnected(event.target.checked)} />{t("Hide unconnected")}</label>
      <div className="grid gap-1 text-xs"><span className="font-medium">{t("Coverage")}</span><span role="status" aria-live="polite" aria-atomic="true" className="text-muted">{t("{visible} of {total} resources", { visible: visible.length, total: graph.total.nodes })}</span>{hidden > 0 && <span className="text-warning">{t("{n} hidden by filters", { n: hidden })}</span>}</div>
    </section>
    {!fieldMode && <ViewVisibilityContext.Consumer>{shown => shown ? <section aria-label={t("Graph resources")} className="grid gap-1">
      <div className="flex items-center justify-between gap-2"><h3 className="text-xs font-semibold">{t("Resources")}</h3><span className="text-2xs text-muted">{t("Showing {n} resources", { n: visible.length })}</span></div>
      {visible.length > 0 ? <ul ref={resourceListRef} aria-label={t("Graph resources")} className="grid max-h-64 gap-0.5 overflow-y-auto pr-1">
        {visible.map(node => <li key={node.id}>
          <Button data-lineage-resource={node.id}
            variant="ghost" aria-pressed={node.id === selected} aria-label={resourceLabel(node)}
            className={`h-auto w-full flex-col items-stretch justify-start gap-1 whitespace-normal px-2 py-1.5 text-left ${node.id === selected ? "bg-row-selected" : ""}`}
            onClick={() => chooseNode(node)}
            onKeyDown={event => {
              if (event.key === "Enter") { event.preventDefault(); moveToInspector(node); return; }
              moveResource(event, node);
            }}>
            <span className="truncate text-xs font-medium">{node.title || node.name}</span>
            <span className="flex flex-wrap items-center gap-1">
              <Tag label={t(kindTitle[node.kind] ?? node.kind)} tone={kindTone[node.kind] ?? "neutral"} />
              <Tag label={t(statusNode(node))} tone={statusTone[node.status] ?? "neutral"} />
              {node.guard && <Tag label={`${t("Protection")}: ${node.guard}`} tone={protectionTone[node.guard] ?? "neutral"} />}
              {node.id === selected && <Tag label={t("Selected")} tone="info" />}
            </span>
          </Button>
        </li>)}
      </ul> : <p role="status" className="text-xs text-muted">{t("No resources match the current search and filters.")}</p>}
      <p className="text-2xs text-muted">{t("Use Up and Down to move through resources. Enter opens the Inspector; F, I, X and Escape keep working.")}</p>
    </section> : null}</ViewVisibilityContext.Consumer>}
    <Button size="sm" variant="ghost" onClick={() => update({ mode: "overview", project: "", focus: "", selected: "" })}><ChevronLeft />{t("Overview")}</Button>
  </div>;

  const inspector = fieldMode
    ? <FieldInspector field={selectedField} graph={graph} containerRef={inspectorRef} onSelect={id => {
      const field = (graph.fieldNodes ?? []).find(item => item.id === id);
      if (field) chooseField(id, field.title || field.name);
      else update({ selected: id });
    }} />
    : <Inspector node={selectedNode} graph={graph} containerRef={inspectorRef} onOpen={openNode} onFocus={focusNode} onSelect={chooseNodeByID} onCopyRID={rid => void copy(rid, t("RID copied"))} onCopyLink={() => void copy(location.href, t("Link copied"))} onFields={() => update({ fields: "on", field: "" })} />;
  const downstream = graph.impact?.downstream ?? (root ? reachable(graph.edges, root, true).length : 0);
  const dock = {
    label: t("Analysis"),
    tabs: [
      { id: "impact", title: t("Impact"), badge: downstream, content: <Impact graph={graph} root={root} onSelect={chooseNodeByID} /> },
      { id: "coverage", title: t("Coverage"), badge: graph.unreadable?.length, content: <Coverage graph={graph} /> },
      { id: "fields", title: t("Fields"), content: <Fields graph={graph} node={selectedNode} selected={params.field ?? ""} onSelect={id => {
        const field = (graph.fieldNodes ?? []).find(item => item.id === id);
        if (field) chooseField(id, field.title || field.name);
        else update({ fields: "on", field: id, selected: id });
      }} /> },
    ],
  };
  return <>
    <div role="status" aria-live="polite" aria-atomic="true" aria-label={t("Lineage status")} className="sr-only">{announcement}</div>
    <Workbench storageKey={`lineage:${viewRoot}`} crumbs={[{ label: t("Data Lineage"), onClick: () => update({ mode: "overview", project: "", focus: "", selected: "" }, false) }, ...(projectID ? [{ label: t("Project"), ...(focusMode ? { onClick: backToProject } : {}) }] : [])]}
    title={selectedNode?.title || graph.scope.focus || t("Graph")} status={<span aria-label={t("Graph basis")} className="text-xs text-muted">{graph.basis.preview} · {new Date(graph.generatedAt).toLocaleString()}</span>}
    actions={toolbar} left={{ label: t("Scope"), content: left }} right={{ label: t("Inspector"), content: inspector, selection: String(inspectorOpenRequest) }} dock={dock} mainLabel={t("Data lineage graph")}>
    <div className="relative h-full min-h-[26rem]">
      <div className="absolute left-2 top-2 z-10 grid max-w-[32rem] gap-1">
        {isolated && <div role="status" className="flex items-center gap-2 rounded-md border border-border bg-surface p-2 text-xs shadow-sm">
          <ArrowLeftRight aria-hidden className="size-3.5 shrink-0 text-primary" />
          <span className="min-w-0 flex-1">{t("Isolated path · {n} resources shown", { n: visible.length })}</span>
          <Button size="sm" variant="ghost" onClick={toggleIsolated}>{t("Show all")}</Button>
        </div>}
        {graph.nodes.some(node => node.markingStatus !== "consistent") && <Notice tone="warning" message={t("Protection differs from preview. The saved definition is not active yet.")} />}
        {graph.truncated && <Notice tone="warning" message={t("Showing {returned} of {total} resources. Reduce depth or choose one direction.", { returned: graph.returned.nodes, total: graph.total.nodes })} />}
      </div>
      <RelationCanvas
        nodes={fieldMode ? fieldRelationNodes(shownFieldNodes) : relationNodes(visible, color)}
        edges={fieldMode ? fieldRelationEdges(shownFieldEdges) : relationEdges(visibleEdges)}
        selected={selected}
        height="100%"
        label={fieldMode ? t("Field lineage") : t("Data Lineage")}
        storeKey={`lineage:${fieldMode ? "fields:" + (params.field || "all") : viewRoot}`}
        onSelect={chooseSelection}
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

function FieldInspector({ field, graph, containerRef, onSelect }: { field?: NonNullable<Graph["fieldNodes"]>[number]; graph: Graph; containerRef: RefObject<HTMLDivElement | null>; onSelect: (id: string) => void }) {
  if (!field) return <div ref={containerRef} role="group" aria-label={t("Selected field details")} tabIndex={-1} className="p-3 text-xs text-muted outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">{t("Choose a field to trace its source and downstream use.")}</div>;
  const incoming = (graph.fieldEdges ?? []).filter(edge => edge.to === field.id);
  const outgoing = (graph.fieldEdges ?? []).filter(edge => edge.from === field.id);
  return <div ref={containerRef} role="group" aria-label={t("Selected field details")} tabIndex={-1} className="grid gap-3 overflow-auto p-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
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

function Inspector({ node, graph, containerRef, onOpen, onFocus, onSelect, onCopyRID, onCopyLink, onFields }: { node?: Node; graph: Graph; containerRef: RefObject<HTMLDivElement | null>; onOpen: (node: Node) => void; onFocus: (node: Node) => void; onSelect: (id: string) => void; onCopyRID: (rid: string) => void; onCopyLink: () => void; onFields: () => void }) {
  if (!node) return <div ref={containerRef} role="group" aria-label={t("Selected resource details")} tabIndex={-1} className="p-3 text-xs text-muted outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">{t("Select a resource to inspect its versions, protection and inputs.")}</div>;
  const incoming = graph.edges.filter(edge => edge.to === node.id);
  const outgoing = graph.edges.filter(edge => edge.from === node.id);
  return <div ref={containerRef} role="group" aria-label={t("Selected resource details")} tabIndex={-1} className="grid gap-3 overflow-auto p-3 text-xs outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
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
