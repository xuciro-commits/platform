import { AssetControls } from "./asset-controls";
import { useHost, useRecordInventory } from "@platform/app";
import { Button, Card, Input, NodeCanvas, PageHeader, Panel, StatusTag, canvasNodeHeight, canvasNodeWidth, defineStatuses, layout, t, useWorkspace,
  type CanvasEdge, type CanvasNode, type NodeCatalog, type Route } from "@platform/ui";
import { useEffect, useMemo, useRef, useState } from "react";

type Asset = { id: string; revision: number; archived?: boolean; name: string; title: string; object?: string; pages?: string[]; fields?: { name: string; type: string; ref?: string }[];
  state?: string; version?: number; published?: string };
type Kind = "object" | "page" | "workflow" | "function" | "compute" | "application" | "source";
type StudioAsset = { key: string; kind: Kind; label: string; detail: string; record?: Asset; route?: Route };

const states = defineStatuses({
  draft: { label: t("Draft"), tone: "warning" }, published: { label: t("Published"), tone: "success" },
});

const catalog: NodeCatalog = [
  { id: "object", title: t("Object"), category: "asset", inputs: [{ id: "reference", label: t("References"), type: "asset" }], outputs: [{ id: "used", label: t("Used by"), type: "asset" }] },
  { id: "source", title: t("Source object"), category: "asset", inputs: [], outputs: [{ id: "used", label: t("Used by"), type: "asset" }] },
  { id: "page", title: t("Page"), category: "asset", inputs: [{ id: "source", label: t("Object"), type: "asset" }], outputs: [{ id: "app", label: t("Application"), type: "asset" }] },
  { id: "workflow", title: t("Workflow"), category: "asset", inputs: [{ id: "source", label: t("Object"), type: "asset" }], outputs: [] },
  { id: "function", title: t("AI function"), category: "asset", inputs: [{ id: "source", label: t("Object"), type: "asset" }], outputs: [] },
  { id: "compute", title: t("Code function"), category: "asset", inputs: [], outputs: [{ id: "used", label: t("Used by"), type: "asset" }] },
  { id: "application", title: t("Application"), category: "asset", inputs: [{ id: "page", label: t("Pages"), type: "asset" }], outputs: [] },
];

function StudioInventory() {
  const { open } = useWorkspace();
  const { definitions } = useHost();
  const objects = useRecordInventory<Asset>("build.object");
  const pages = useRecordInventory<Asset>("build.page");
  const workflows = useRecordInventory<Asset>("build.process");
  const functions = useRecordInventory<Asset>("build.function");
  const computes = useRecordInventory<Asset>("build.code");
  const applications = useRecordInventory<Asset>("build.app");
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<string>();
  const [narrow, setNarrow] = useState(() => typeof matchMedia !== "undefined" && matchMedia("(max-width: 639px)").matches);
  const inspectorRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const media = matchMedia("(max-width: 639px)");
    const changed = () => setNarrow(media.matches);
    media.addEventListener("change", changed);
    return () => media.removeEventListener("change", changed);
  }, []);
  const selectAsset = (key: string) => {
    setSelected(key);
    if (narrow) requestAnimationFrame(() => inspectorRef.current?.scrollIntoView({ block: "nearest" }));
  };
  const all = useMemo(() => {
    const assets: StudioAsset[] = [];
    const edges: CanvasEdge[] = [];
    const byObject = new Map<string, string>();
    const byPage = new Map<string, string>();
    for (const record of objects.data?.records ?? []) {
      const key = `object:${record.id}`;
      byObject.set(`build.${record.name}`, key);
      assets.push({ key, kind: "object", label: record.title || record.name, detail: `build.${record.name}`, record,
        route: { view: "process", params: { id: record.id } } });
    }
    const source = (name: string) => {
      const own = byObject.get(name);
      if (own) return own;
      const key = `source:${name}`;
      const definition = definitions.find((d) => d.ref.kind === "object" && d.ref.name === name);
      if (!assets.some((asset) => asset.key === key)) assets.push({ key, kind: "source", label: definition?.entity?.title ?? name, detail: t("Installed object"),
        route: definition ? { view: "definition", params: definition.ref } : undefined });
      return key;
    };
    for (const record of objects.data?.records ?? []) for (const field of record.fields ?? []) {
      if (field.type === "reference" && field.ref) edges.push({ id: `reference:${record.id}:${field.name}`,
        source: source(field.ref), sourcePort: "used", target: `object:${record.id}`, targetPort: "reference" });
    }
    for (const [kind, records, view] of [
      ["page", pages.data?.records ?? [], "compose"], ["workflow", workflows.data?.records ?? [], "workflow"],
      ["function", functions.data?.records ?? [], "function"],
      ["compute", computes.data?.records ?? [], "code"],
    ] as const) {
      for (const record of records) {
        const key = `${kind}:${record.id}`;
        assets.push({ key, kind, label: record.title || record.name, detail: record.object || record.name, record,
          route: { view, params: { id: record.id } } });
        if (kind === "page") byPage.set(record.name, key);
        if (record.object) edges.push({ id: `uses:${key}`, source: source(record.object), sourcePort: "used", target: key, targetPort: "source" });
      }
    }
    for (const record of applications.data?.records ?? []) {
      const key = `application:${record.id}`;
      assets.push({ key, kind: "application", label: record.title || record.name, detail: record.name, record,
        route: { view: "record", params: { type: "build.app", id: record.id } } });
      for (const page of record.pages ?? []) {
        const from = byPage.get(page);
        if (from) edges.push({ id: `contains:${key}:${from}`, source: from, sourcePort: "app", target: key, targetPort: "page" });
      }
    }
    return { assets, edges };
  }, [applications.data, functions.data, computes.data, objects.data, pages.data, workflows.data, definitions]);
  const visible = useMemo(() => {
    const q = search.trim().toLocaleLowerCase();
    const matches = q ? all.assets.filter((asset) => `${asset.label} ${asset.detail}`.toLocaleLowerCase().includes(q)) : all.assets;
    const keys = new Set(matches.slice(0, 80).map((asset) => asset.key));
    if (q) for (const edge of all.edges) if (keys.has(edge.source) || keys.has(edge.target)) { keys.add(edge.source); keys.add(edge.target); }
    const assets = all.assets.filter((asset) => keys.has(asset.key));
    const edges = all.edges.filter((edge) => keys.has(edge.source) && keys.has(edge.target));
    const places = layout(assets.map((asset) => ({ id: asset.key, label: asset.label })), edges.map((edge) => ({ from: edge.source, to: edge.target })), "right",
      { width: canvasNodeWidth, height: Math.max(...catalog.map(canvasNodeHeight)), gapX: 58, gapY: 32 });
    const nodes: CanvasNode[] = assets.map((asset) => ({ id: asset.key, kind: asset.kind, label: asset.label, detail: asset.detail,
      position: places.get(asset.key) ?? { x: 0, y: 0 } }));
    return { assets, edges, nodes, matches: matches.length };
  }, [all, search]);
  const current = all.assets.find((asset) => asset.key === selected);
  const connections = all.edges.filter((edge) => edge.source === selected || edge.target === selected)
    .map((edge) => all.assets.find((asset) => asset.key === (edge.source === selected ? edge.target : edge.source)))
    .filter((asset): asset is StudioAsset => Boolean(asset));
  const loading = [objects, pages, workflows, functions, computes, applications].some((query) => query.isLoading);
  const failed = [objects, pages, workflows, functions, computes, applications].some((query) => query.isError);
  return <div className="grid gap-4">
    <PageHeader title={t("Application Studio")} description={t("Build with the capabilities already available in this workspace.")}
      actions={<Button onClick={() => open({ view: "studio-templates" })}>{t("Studio templates")}</Button>} />
    <Panel role="region" aria-label={t("Studio capabilities")} className="grid gap-3 border-0 bg-transparent p-0 sm:grid-cols-2 xl:grid-cols-3">
      {[
        { title: "Objects and relationships", detail: "Define fields, relationships, actions and access.", route: { view: "page", params: { app: "build", kind: "page", name: "objects" } }, query: objects },
        { title: "Pages", detail: "Compose an interface over your business objects.", route: { view: "pages" }, query: pages },
        { title: "Workflows", detail: "Connect actions, people and published functions.", route: { view: "workflow" }, query: workflows },
        { title: "AI functions", detail: "Configure typed advice and review its results.", route: { view: "function" }, query: functions },
        { title: "Code functions", detail: "Compile typed Go or TinyGo algorithms for pages and workflows.", route: { view: "code" }, query: computes },
        { title: "Applications", detail: "Give published pages to the people who use them.", route: { view: "page", params: { app: "build", kind: "page", name: "applications" } }, query: applications },
        { title: "Test and release", detail: "Try saved drafts, review dependencies and activate a candidate.", route: { view: "candidate-test" } },
      ].map((capability) => <Card key={capability.title} className="grid gap-2 p-4">
        <div className="flex items-center justify-between gap-3">
          <Button variant="ghost" className="-ml-2 text-sm font-semibold" onClick={() => open(capability.route)}>{t(capability.title)}</Button>
          {capability.query && <span className="text-sm tabular-nums text-muted">{capability.query.isError ? "—" : capability.query.data?.records.length ?? "…"}</span>}
        </div>
        <p className="text-xs text-muted">{t(capability.detail)}</p>
        {capability.title === "Test and release" && <Button size="sm" onClick={() => open({ view: "release-review" })}>{t("Release review")}</Button>}
      </Card>)}
    </Panel>
    {failed && <Panel role="alert" className="text-sm text-danger">{t("Some studio assets could not be loaded. Retry the workspace.")}</Panel>}
    <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_18rem]">
      <Card className="min-w-0 grid gap-3 p-3">
        <div className="grid gap-1 sm:flex sm:items-center sm:gap-3">
          <label className="min-w-0 flex-1 text-xs">{t("Find an asset")}
            <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={t("Search objects, pages and workflows")} />
          </label>
          <p className="text-xs text-muted" role="status">{t("Showing {shown} of {total} assets", { shown: visible.assets.length, total: all.assets.length })}</p>
        </div>
        {loading ? <p role="status" className="p-6 text-sm text-muted">{t("Loading…")}</p>
          : visible.nodes.length ? <NodeCanvas label={t("Application map")} catalog={catalog} nodes={visible.nodes} edges={visible.edges}
            selected={selected} onSelect={selectAsset} height={narrow ? 300 : 460} />
            : <p className="p-6 text-sm text-muted">{t("No matching assets. Create an object or change the search.")}</p>}
      </Card>
      <div ref={inspectorRef}><Panel role="region" aria-label={t("Asset inspector")} className="grid content-start gap-3 p-4">
        {current ? <>
          <p className="text-xs uppercase tracking-wide text-muted">{t("Selected asset")}</p>
          <h2 className="text-base font-semibold">{current.label}</h2>
          <p className="break-all text-xs text-muted">{current.detail}</p>
          {current.record?.state && <StatusTag status={current.record.state} registry={states} />}
          {current.record?.version ? <p className="text-xs">{t("Version")} {current.record.version}</p> : null}
          {current.route && <Button variant="primary" onClick={() => open(current.route!)}>{t("Open selected asset")}</Button>}
          {current.record && <div className="flex flex-wrap gap-2">
            <AssetControls type={`build.${({ object: "object", page: "page", workflow: "process", function: "function", compute: "code", application: "app", source: "object" })[current.kind]}`} record={current.record} />
            {["object", "workflow", "function"].includes(current.kind) && <Button size="sm" onClick={() => open({ view: "candidate-test", params: {
              [current.kind === "workflow" ? "processId" : current.kind === "function" ? "functionId" : "objectId"]: current.record!.id,
            } })}>{t("Test selected asset")}</Button>}
            <Button size="sm" onClick={() => open({ view: "release-review", params: { kind: current.kind === "workflow" ? "flow" : current.kind === "application" ? "app" : current.kind, id: current.record!.id } })}>{t("Review selected release")}</Button>
          </div>}
          {current.kind === "source" && <p className="text-xs text-muted">{t("This source is installed outside Application Studio.")}</p>}
          {connections.length > 0 && <div className="mt-2 grid gap-1 border-t border-border pt-3">
            <h3 className="text-xs font-semibold uppercase tracking-wide text-muted">{t("Connected assets")}</h3>
            {connections.map((asset) => <Button key={asset.key} variant="row" onClick={() => selectAsset(asset.key)}>{asset.label}</Button>)}
          </div>}
        </> : <p className="text-sm text-muted">{t("Select a node to inspect its status and open its editor.")}</p>}
      </Panel></div>
    </div>
  </div>;
}

export function StudioOverview() {
  const { role } = useHost();
  return role("build") === "builder" ? <StudioInventory />
    : <PageHeader title={t("Application Studio")} description={t("Only a builder can edit application assets.")} />;
}
