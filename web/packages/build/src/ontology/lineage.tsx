import { useApplicationWorkspace } from "../projects/application-scope";
import { useReadQuery } from "@platform/app";
import { DataTable, Panel, RelationCanvas, t, type RelationEdge, type RelationNode } from "@platform/ui";
import { useState } from "react";

// Lineage (ADR-0072 §3): where an object's data comes from and where its
// decisions go, read from the integration definitions themselves - no second
// model. Sources feed datasets or objects; pipelines read datasets and write
// datasets or objects; writebacks send an object's actions to a connection.
type Conn = { id: string; title: string; kind: string; state: string };
type Src = { id: string; title: string; name: string; connection?: string; profile?: string; url?: string; entity?: string; object?: string; dataset?: string; key?: string; mapping?: { from: string; to: string }[]; state: string };
type Ds = { id: string; title: string; name: string; producer?: string; version?: number; schema?: { name: string }[] };
type Pipe = { id: string; title: string; name: string; input: string; outputDataset?: string; outputObject?: string; key?: string; state: string; steps?: { kind: string; from?: string; to?: string; dataset?: string; columns?: string[]; measures?: { to: string }[] }[] };
type Wb = { id: string; title: string; name: string; connection: string; object: string; on: string; path?: string; state: string; sent?: number; failed?: number; result?: { from: string; to: string }[] };

export function useLineage() {
  const q = <T,>(type: string) => useReadQuery<{ records: T[] }>(`/v1/records/${type}?limit=200`, 15000).data?.records ?? [];
  return { connections: q<Conn>("build.connection"), sources: q<Src>("build.source"), datasets: q<Ds>("build.dataset"), pipelines: q<Pipe>("build.pipeline"), writebacks: q<Wb>("build.writeback") };
}

/** One element of the graph, and where a reader goes to open it. */
type Route = { view: string; params?: Record<string, string> };
export type LineageGraph = { nodes: RelationNode[]; edges: RelationEdge[]; routes: Record<string, Route> };

/** The whole declared lineage around one dataset or one object, as a relation
 * graph rather than a chain of arrows: a connection feeds a source, a source loads
 * a dataset or writes an object, a pipeline reads datasets and writes a dataset or
 * an object, a writeback sends an object's actions back out through a connection.
 * A line here says "this feeds that" — there is no order to read and no port to
 * join — so it is the relation canvas that draws it (ADR-0086 D3). */
export function lineageGraph(l: ReturnType<typeof useLineage>, focus: { kind: "object" | "dataset"; id: string }): LineageGraph {
  const nodes = new Map<string, RelationNode>();
  const edges: RelationEdge[] = [];
  const routes: Record<string, Route> = {};
  const put = (id: string, kind: string, node: Omit<RelationNode, "id" | "class">, route?: Route) => {
    if (nodes.has(id)) return;
    // The class decides the glyph, tone and caption word (ADR-0090 D1); a node's
    // own caption and flag still override the class where they are present.
    nodes.set(id, { ...node, id, class: kind, detail: [node.caption, node.flag].filter(Boolean).join(" · ") });
    if (route) routes[id] = route;
  };
  const feed = (from: string, to: string, label: string) => edges.push({ id: `${from}>${to}`, source: from, target: to, label, directed: true, tree: true });

  const addConnection = (id?: string) => {
    const c = l.connections.find((x) => x.id === id);
    if (!c) return "";
    const key = `connection:${c.id}`;
    put(key, "connection", { label: c.title, caption: `${t("Connection")} · ${c.kind}`, flag: c.state }, { view: "connection", params: { id: c.id } });
    return key;
  };
  const addSource = (s: Src) => {
    const key = `source:${s.id}`;
    put(key, "source", { label: s.title, caption: `${t("Source")} · ${s.profile || "json"} · ${s.entity || s.url || s.name}`, flag: s.state,
      facts: [{ label: t("Key"), value: s.key || "—" }, ...(s.mapping?.length ? [{ label: t("Mapping"), value: s.mapping.map((m) => `${m.from} → ${m.to}`).join(", ") }] : [])] },
      { view: "data-source", params: { id: s.id } });
    const c = addConnection(s.connection);
    if (c) feed(c, key, t("feeds"));
    return key;
  };
  const addDataset = (id: string, seen: Set<string>) => {
    const ds = l.datasets.find((x) => x.id === id);
    if (!ds || seen.has(ds.id)) return "";
    seen.add(ds.id);
    const key = `dataset:${ds.id}`;
    put(key, "dataset", { label: ds.title, caption: `${t("Dataset")}${ds.version ? ` · v${ds.version}` : ""} · ${ds.name}`,
      facts: [{ label: t("Columns"), value: ds.schema?.map((c) => c.name).join(", ") || "—" }, ...(ds.producer ? [{ label: t("Producer"), value: ds.producer }] : [])] },
      { view: "dataset", params: { id: ds.id } });
    for (const s of l.sources.filter((x) => x.dataset === ds.id)) feed(addSource(s), key, t("loads"));
    for (const p of l.pipelines.filter((x) => x.outputDataset === ds.id)) feed(addPipeline(p, seen), key, t("writes"));
    return key;
  };
  const addPipeline = (p: Pipe, seen: Set<string>) => {
    const key = `pipeline:${p.id}`;
    if (!nodes.has(key)) {
      put(key, "pipeline", { label: p.title, caption: `${t("Pipeline")} · ${p.name}`, flag: p.state,
        facts: [{ label: t("Steps"), value: String(p.steps?.length ?? 0) }, ...(p.key ? [{ label: t("Key"), value: p.key }] : [])] },
        { view: "pipeline", params: { id: p.id } });
      const input = addDataset(p.input, seen);
      if (input) feed(input, key, t("reads"));
      for (const step of p.steps ?? []) if (step.dataset) { const joined = addDataset(step.dataset, seen); if (joined) feed(joined, key, t("joins")); }
    }
    return key;
  };

  const seen = new Set<string>();
  if (focus.kind === "dataset") {
    addDataset(focus.id, seen);
    const key = `dataset:${focus.id}`;
    for (const p of l.pipelines.filter((x) => x.input === focus.id || x.steps?.some((s) => s.dataset === focus.id))) {
      const reader = addPipeline(p, seen);
      feed(key, reader, t("read by"));
      const out = p.outputObject ? addObject(p.outputObject) : addDataset(p.outputDataset ?? "", seen);
      if (out) feed(reader, out, t("writes"));
    }
  } else {
    const key = addObject(focus.id);
    for (const s of l.sources.filter((x) => x.object === focus.id)) feed(addSource(s), key, t("writes"));
    for (const p of l.pipelines.filter((x) => x.outputObject === focus.id)) feed(addPipeline(p, seen), key, t("writes"));
  }
  function addObject(id: string) {
    const key = `object:${id}`;
    put(key, "object", { label: id, caption: t("Object type") });
    for (const w of l.writebacks.filter((x) => x.object === id)) {
      const wb = `writeback:${w.id}`;
      put(wb, "writeback", { label: w.title, caption: `${t("Writeback")} · ${t("after")} ${w.on}${w.path ? ` · ${w.path}` : ""}`, flag: w.state,
        facts: [{ label: t("Delivered"), value: String(w.sent ?? 0) }, ...(w.failed ? [{ label: t("Failed"), value: String(w.failed) }] : []),
          ...(w.result?.length ? [{ label: t("answer writes"), value: w.result.map((r) => `${r.from} → ${r.to}`).join(", ") }] : [])] },
        { view: "writeback", params: { id: w.id } });
      feed(key, wb, t("sends"));
      const c = addConnection(w.connection);
      if (c) feed(wb, c, t("to"));
    }
    return key;
  }
  // A pipeline may name a dataset that is not defined; an edge to nowhere is not drawn.
  return { nodes: [...nodes.values()], edges: edges.filter((e) => nodes.has(e.source) && nodes.has(e.target)), routes };
}

/** Which step of a pipeline produces a column, if any renames or computes it. */
function producedBy(p: Pipe, column: string): string {
  for (const s of [...(p.steps ?? [])].reverse()) {
    if ((s.kind === "rename" || s.kind === "compute") && s.to === column) return s.kind === "rename" ? `← ${s.from}` : `= ${t("formula")}`;
    if (s.kind === "aggregate" && s.measures?.some((m) => m.to === column)) return `= ${t("aggregate")}`;
  }
  return "";
}

export function ObjectLineage({ object, fields }: { object: string; fields: { name: string; title: string }[] }) {
  const l = useLineage(), { open } = useApplicationWorkspace();
  const [picked, setPicked] = useState<string>();
  const direct = l.sources.filter((s) => s.object === object);
  const pipes = l.pipelines.filter((p) => p.outputObject === object);
  const wbs = l.writebacks.filter((w) => w.object === object);
  const graph = lineageGraph(l, { kind: "object", id: object });
  const feeders = (field: string) => [
    ...direct.filter((s) => s.mapping?.some((m) => m.to === field)).map((s) => `${t("Source")} ${s.title} (${s.mapping!.find((m) => m.to === field)!.from})`),
    ...pipes.map((p) => `${t("Pipeline")} ${p.title} ${producedBy(p, field)}`.trim()),
  ];
  if (!direct.length && !pipes.length && !wbs.length) return <Panel title={t("Data lineage")} description={t("Read from the integration definitions themselves: what is configured to feed this object's records and where its actions are configured to go. It is not a record-by-record history.")}><p className="text-xs text-muted">{t("Nothing feeds or follows this object yet. A data source or a pipeline writes its records; a writeback sends its actions to an external system.")}</p></Panel>;
  return <div className="grid gap-3">
    <Panel title={t("Data lineage")} className="grid gap-3" description={t("Declared, not observed: these are the sources and pipelines configured to write this object's records, and the writebacks configured to send its actions out. A dataset's version and a writeback's delivered count are the run evidence kept beside the declaration.")}>
      <RelationCanvas layout="tree-right" nodes={graph.nodes} edges={graph.edges} selected={picked} onSelect={setPicked}
        onOpen={(id) => { const route = graph.routes[id]; if (route) open(route); }} height={360} label={t("Data lineage")} />
    </Panel>
    <Panel title={t("Field by field")}>
      <DataTable data={fields} getRowId={(f) => f.name} searchable={false} height={Math.min(360, 40 + fields.length * 28)} columns={[
        { id: "title", header: t("Field"), accessorFn: (f) => f.title },
        { id: "name", header: t("Name"), accessorFn: (f) => f.name },
        { id: "origin", header: t("Comes from"), accessorFn: (f) => feeders(f.name).join(" · ") || t("entered here") },
      ]} />
    </Panel>
  </div>;
}

export function DatasetLineage({ dataset }: { dataset: string }) {
  const l = useLineage(), { open } = useApplicationWorkspace();
  const [picked, setPicked] = useState<string>();
  const graph = lineageGraph(l, { kind: "dataset", id: dataset });
  return <Panel title={t("Lineage")} className="grid gap-2 text-xs"
    description={t("Declared, not observed: what is configured to load this dataset and what is configured to read it.")}>
    {graph.nodes.length > 1
      ? <RelationCanvas layout="tree-right" nodes={graph.nodes} edges={graph.edges} selected={picked} onSelect={setPicked}
          onOpen={(id) => { const route = graph.routes[id]; if (route) open(route); }} height={320} label={t("Lineage")} />
      : <p className="text-muted">{t("Nothing yet.")}</p>}
  </Panel>;
}
