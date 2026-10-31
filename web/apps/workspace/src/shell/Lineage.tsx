import { assetKey, definitionPresentation, DependencyGraph, useDependencies, useDefinitions, useProvidedCapabilities, useReadQuery, type AssetRef, type ProvidedCapability } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, DataTable, Disclosure, PageHeader, Panel, PropertyList, RelationCanvas, relationNodeClasses, SegmentedChoice, Select, useWorkspace, t, type RelationEdge, type RelationNode } from "@platform/ui";
import { RefreshCw } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";

const kindLabel = (kind: string) => t(relationNodeClasses[kind]?.title ?? kind);
const capabilityKey = (item: ProvidedCapability) => `${assetKey(item.ref)}@${item.version}:${item.capability.kind}:${item.capability.name}`;
const sideLabels: Record<string, string> = { pure: "Read only", effect: "Declare an effect", authoritative: "Change authoritative state" };
const markingTones: Record<string, string> = { internal: "info", confidential: "warning", restricted: "danger" };
const resourceViews: Record<string, string> = { connection: "connection", source: "data-source", dataset: "dataset", pipeline: "pipeline", writeback: "writeback", object: "object-type" };
type ColorBy = "kind" | "marking" | "status";

function reach(start: string, edges: Api.LineageEdge[]) {
  const seen = new Set([start]);
  for (const forward of [true, false]) {
    const queue = [start];
    while (queue.length) {
      const at = queue.shift()!;
      for (const edge of edges) {
        const next = forward ? edge.from === at && edge.to : edge.to === at && edge.from;
        if (next && !seen.has(next)) { seen.add(next); queue.push(next); }
      }
    }
  }
  return seen;
}

export function Lineage({ ref: requested, node: requestedNode }: { ref?: string; node?: string }) {
  const { open } = useWorkspace();
  const query = useReadQuery<Api.Lineage>("/v1/lineage", 15000);
  const graph = query.data ?? { nodes: [], edges: [] };
  const [colorBy, setColorBy] = useState<ColorBy>("kind");
  const [selected, setSelected] = useState<string | undefined>(requestedNode);
  const chosen = graph.nodes.find(node => node.id === selected);
  const connected = useMemo(() => chosen ? reach(chosen.id, graph.edges) : undefined, [chosen, graph.edges]);
  const nodes: RelationNode[] = graph.nodes.map(node => {
    const facts = node.version ? `v${node.version}` : node.state ? t(node.state) : "";
    const tone = colorBy === "marking" ? markingTones[node.effective ?? ""] ?? "neutral"
      : colorBy === "status" ? node.error ? "danger" : node.stale ? "warning" : node.state === "draft" || node.kind === "dataset" && !node.version ? "neutral" : "success" : undefined;
    return { id: node.id, label: node.title || node.name, class: node.kind, tone, caption: `${kindLabel(node.kind)}${facts ? ` · ${facts}` : ""}`, detail: node.name,
      dim: !!connected && !connected.has(node.id), flag: node.error ? node.error : node.stale ? t("An input changed since the last run.") : undefined };
  });
  const edges: RelationEdge[] = graph.edges.map(edge => ({ id: `${edge.from}>${edge.to}`, source: edge.from, target: edge.to, directed: true, dashed: edge.stale, tone: edge.stale ? "warning" : undefined, label: edge.version ? `v${edge.version}` : undefined }));
  const openNode = (node: Api.LineageNode) => resourceViews[node.kind] && node.record && open({ view: resourceViews[node.kind]!, params: { id: node.record } });
  const upstream = chosen ? graph.edges.filter(edge => edge.to === chosen.id) : [];
  const title = (id: string) => { const node = graph.nodes.find(item => item.id === id); return node ? node.title || node.name : id; };
  return <div className="grid min-w-0 gap-4">
    <PageHeader title={t("Data Lineage")} meta={t("{count} resources", { count: graph.nodes.length })}
      help={t("How data moves from connections through sources, datasets and pipelines into object types and back out through writebacks. Markings propagate downstream; a stale pipeline has an input newer than its last run.")}
      actions={<>
        <SegmentedChoice compact label={t("Color by")} value={colorBy} onChange={value => setColorBy(value as ColorBy)} options={[{ value: "kind", label: t("Resource") }, { value: "marking", label: t("Markings") }, { value: "status", label: t("Status") }]} />
        <Button size="icon" variant="ghost" aria-label={t("Refresh")} title={t("Refresh")} onClick={() => void query.refetch()}><RefreshCw /></Button>
      </>} />
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <Panel className="min-w-0">
        {query.isError ? <p role="alert" className="text-sm text-danger">{t("Data lineage could not be loaded.")}</p>
          : query.isPending ? <p className="text-sm text-muted">{t("Loading…")}</p>
          : !nodes.length ? <p className="text-sm text-muted">{t("No connections, datasets or pipelines yet. Start in Data Connection.")}</p>
          : <RelationCanvas layout="right" nodes={nodes} edges={edges} selected={selected} height={520} label={t("Data Lineage")} storeKey="data-lineage" onSelect={setSelected} onOpen={id => { const node = graph.nodes.find(item => item.id === id); if (node) openNode(node); }} />}
      </Panel>
      <Panel title={chosen ? chosen.title || chosen.name : t("Details")}>
        {chosen ? <div className="grid gap-3 text-xs">
          <PropertyList items={[
            [t("Kind"), kindLabel(chosen.kind)],
            [t("Name"), <code>{chosen.name}</code>],
            ...(chosen.state ? [[t("State"), t(chosen.state)] as [string, ReactNode]] : []),
            [t("Marking"), chosen.marking ? t(chosen.marking) : t("None")],
            ...(chosen.effective && chosen.effective !== chosen.marking ? [[t("Effective marking"), `${t(chosen.effective)} · ${t("inherited from upstream")}`] as [string, ReactNode]] : []),
            ...(chosen.version ? [[t("Latest version"), String(chosen.version)] as [string, ReactNode]] : []),
            ...(chosen.rows ? [[t("Rows"), String(chosen.rows)] as [string, ReactNode]] : []),
            ...(chosen.at ? [[t("Last activity"), new Date(chosen.at).toLocaleString()] as [string, ReactNode]] : []),
          ]} />
          {chosen.error && <p role="alert" className="text-danger">{chosen.error}</p>}
          {upstream.length > 0 && <div className="grid gap-1"><h3 className="font-semibold">{t("Inputs")}</h3>
            {upstream.map(edge => <Button key={edge.from} size="sm" variant="ghost" className="justify-start" onClick={() => setSelected(edge.from)}>
              <span className="min-w-0 flex-1 truncate text-left">{title(edge.from)}</span>{edge.version ? <span className={edge.stale ? "text-warning" : "text-muted"}>{edge.stale ? t("read v{n}, now newer", { n: edge.version }) : `v${edge.version}`}</span> : null}
            </Button>)}</div>}
          {resourceViews[chosen.kind] && <Button size="sm" variant="primary" className="w-fit" onClick={() => openNode(chosen)}>{t("Open")}</Button>}
        </div> : <p className="text-xs text-muted">{t("Select a resource to see its versions, markings and inputs. Its upstream and downstream stay highlighted.")}</p>}
      </Panel>
    </div>
    <Disclosure className="rounded-md border border-border bg-surface p-3" defaultOpen={!!requested} summary={<span className="text-sm font-semibold">{t("Published definition dependencies")}</span>}>
      <PublishedDependencies requested={requested} />
    </Disclosure>
  </div>;
}

function PublishedDependencies({ requested }: { requested?: string }) {
  const definitions = useDefinitions().data ?? [];
  const { open } = useWorkspace();
  const [picked, setPicked] = useState<string>();
  const [app, setApp] = useState<string>();
  const focus = picked ?? requested ?? "";
  const parts = focus.split("/");
  const ref = focus && parts.length === 3 ? { app: parts[0]!, kind: parts[1]!, name: parts[2]! } : undefined;
  const query = useDependencies(ref);
  const descriptors = query.data?.descriptors ?? [];
  const apps = [...new Set(definitions.map(d => d.ref.app))].sort();
  const scopeApp = ref?.app ?? app ?? apps[0] ?? "";
  const openAsset = (ref: AssetRef) => {
    if (ref.kind === "observation") return open({ view: "observations", params: { app: ref.app, name: ref.name } });
    if (ref.kind === "page") return open({ view: "page", params: { ...ref } });
    open({ view: "definition", params: { ...ref } });
  };
  const focused = descriptors.find(d => assetKey(d.ref) === focus);
  return <div className="mt-3 grid min-w-0 gap-4">
    <div className="flex flex-wrap items-center gap-2">
      <Select aria-label={t("App")} value={scopeApp} className="w-40" onChange={event => { setApp(event.target.value); setPicked(""); }}>{apps.map(id => <option key={id} value={id}>{id}</option>)}</Select>
      <Select aria-label={t("Asset")} value={focus} className="w-72" onChange={event => setPicked(event.target.value)}>
        <option value="">{t("Every asset of {app}", { app: scopeApp })}</option>
        {definitions.filter(d => d.ref.app === scopeApp).map(d => <option key={assetKey(d.ref)} value={assetKey(d.ref)}>{kindLabel(d.ref.kind)} · {definitionPresentation(d).title}</option>)}
      </Select>
    </div>
    <DependencyGraph asset={ref} app={scopeApp} height={400} onSelect={item => setPicked(assetKey(item))} onOpen={openAsset} />
      {focused && <div className="grid gap-2 border-t border-border pt-3 text-xs sm:grid-cols-2">
        {[{ title: "Uses", refs: query.data?.requires ?? [] }, { title: "Used by", refs: query.data?.usedBy ?? [] }].map(group => <div key={group.title}><h3 className="font-semibold">{t(group.title)}</h3><div className="flex flex-wrap gap-1">{group.refs.length ? group.refs.map(item => <Button variant="ghost" size="sm" key={assetKey(item)} onClick={() => setPicked(assetKey(item))}>{item.name}</Button>) : <span className="text-muted">{t("None")}</span>}</div></div>)}
      </div>}
    <CapabilityDiscovery app={scopeApp} focus={focused?.ref} onOpen={openAsset} />
  </div>;
}

function CapabilityDiscovery({ app, focus, onOpen }: { app: string; focus?: AssetRef; onOpen: (ref: AssetRef) => void }) {
  const [kind, setKind] = useState("");
  const [side, setSide] = useState("");
  const [selected, setSelected] = useState<string>();
  const query = useProvidedCapabilities(kind, side);
  const rows = (query.data ?? []).filter(item => focus ? assetKey(item.ref) === assetKey(focus) : item.ref.app === app);
  const chosen = rows.find(item => capabilityKey(item) === selected);
  return <Panel title={t("Available capabilities")} help={t("The host lists capabilities available to you. Choose a kind or invocation type to inspect their owner, version and typed inputs.")} className="grid min-w-0 gap-3">
    <div className="flex flex-wrap gap-2">
      <Select aria-label={t("Capability kind")} value={kind} onChange={event => setKind(event.target.value)}><option value="">{t("All capability kinds")}</option>{["action", "query", "function", "operation", "protocol", "interface"].map(value => <option key={value} value={value}>{kindLabel(value)}</option>)}</Select>
      <Select aria-label={t("Invocation type")} value={side} onChange={event => setSide(event.target.value)}><option value="">{t("All invocation types")}</option>{Object.entries(sideLabels).map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}</Select>
    </div>
    {query.isError && <p role="alert" className="text-sm text-danger">{t("Available capabilities could not be loaded.")}</p>}
    <DataTable data={rows} getRowId={capabilityKey} selectedId={selected} onRowClick={item => setSelected(capabilityKey(item))} loading={query.isPending} height={280} empty={t("No capabilities match this selection.")} columns={[
      { id: "name", header: t("Name"), accessorFn: item => item.capability.name },
      { id: "kind", header: t("Kind"), accessorFn: item => kindLabel(item.capability.kind) },
      { id: "side", header: t("Invocation type"), accessorFn: item => t(sideLabels[item.capability.side] ?? item.capability.side) },
      { id: "owner", header: t("Owner"), accessorFn: item => item.ref.app },
      { id: "version", header: t("Version"), accessorFn: item => item.version },
    ]} />
    {chosen && <div className="grid gap-2 text-xs">
      <Button size="sm" variant="ghost" onClick={() => onOpen(chosen.ref)}>{t("Open asset")}: {chosen.ref.name}</Button>
      <Disclosure summary={t("Typed inputs and outputs")}><pre className="max-h-56 overflow-auto rounded bg-canvas p-2">{JSON.stringify({ inputs: chosen.capability.inputs, outputs: chosen.capability.outputs, parameters: chosen.capability.parameters }, null, 2)}</pre></Disclosure>
    </div>}
  </Panel>;
}
