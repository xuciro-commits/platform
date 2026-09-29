import { useHost, useRecordInventory } from "@platform/app";
import { Button, Card, Input, NodeCanvas, PageHeader, Panel, StatusTag, canvasNodeHeight, defineStatuses, layout, t, useWorkspace,
  type CanvasEdge, type CanvasNode, type NodeCatalog, type Route } from "@platform/ui";
import { useEffect, useMemo, useRef, useState } from "react";

type Asset = { id: string; name: string; title: string; object?: string; pages?: string[]; fields?: { name: string; type: string; ref?: string }[];
  state?: string; version?: number; published?: string };
type Kind = "object" | "page" | "workflow" | "function" | "application" | "source";
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
  { id: "application", title: t("Application"), category: "asset", inputs: [{ id: "page", label: t("Pages"), type: "asset" }], outputs: [] },
];

function StudioInventory() {
  const { open } = useWorkspace();
  const objects = useRecordInventory<Asset>("build.object");
  const pages = useRecordInventory<Asset>("build.page");
  const workflows = useRecordInventory<Asset>("build.process");
  const functions = useRecordInventory<Asset>("build.function");
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
      if (!assets.some((asset) => asset.key === key)) assets.push({ key, kind: "source", label: name, detail: t("Installed object") });
      return key;
    };
    for (const record of objects.data?.records ?? []) for (const field of record.fields ?? []) {
      if (field.type === "reference" && field.ref) edges.push({ id: `reference:${record.id}:${field.name}`,
        source: source(field.ref), sourcePort: "used", target: `object:${record.id}`, targetPort: "reference" });
    }
    for (const [kind, records, view] of [
      ["page", pages.data?.records ?? [], "compose"], ["workflow", workflows.data?.records ?? [], "workflow"],
      ["function", functions.data?.records ?? [], "function"],
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
  }, [applications.data, functions.data, objects.data, pages.data, workflows.data]);
  const visible = useMemo(() => {
    const q = search.trim().toLocaleLowerCase();
    const matches = q ? all.assets.filter((asset) => `${asset.label} ${asset.detail}`.toLocaleLowerCase().includes(q)) : all.assets;
    const keys = new Set(matches.slice(0, 80).map((asset) => asset.key));
    if (q) for (const edge of all.edges) if (keys.has(edge.source) || keys.has(edge.target)) { keys.add(edge.source); keys.add(edge.target); }
    const assets = all.assets.filter((asset) => keys.has(asset.key));
    const edges = all.edges.filter((edge) => keys.has(edge.source) && keys.has(edge.target));
    const places = layout(assets.map((asset) => ({ id: asset.key, label: asset.label })), edges.map((edge) => ({ from: edge.source, to: edge.target })), "right",
      { width: 160, height: Math.max(...catalog.map(canvasNodeHeight)), gapX: 58, gapY: 32 });
    const nodes: CanvasNode[] = assets.map((asset) => ({ id: asset.key, kind: asset.kind, label: asset.label, detail: asset.detail,
      position: places.get(asset.key) ?? { x: 0, y: 0 } }));
    return { assets, edges, nodes, matches: matches.length };
  }, [all, search]);
  const current = all.assets.find((asset) => asset.key === selected);
  const connections = all.edges.filter((edge) => edge.source === selected || edge.target === selected)
    .map((edge) => all.assets.find((asset) => asset.key === (edge.source === selected ? edge.target : edge.source)))
    .filter((asset): asset is StudioAsset => Boolean(asset));
  const loading = [objects, pages, workflows, functions, applications].some((query) => query.isLoading);
  const failed = [objects, pages, workflows, functions, applications].some((query) => query.isError);
  return <div className="grid gap-4">
    <PageHeader title={t("Application Studio")} description={t("See how objects, pages, workflows and functions form an application. Select an asset to edit it.")}
      actions={<div className="flex flex-wrap gap-2">
        <Button onClick={() => open({ view: "page", params: { app: "build", kind: "page", name: "objects" } })}>{t("Objects")}</Button>
        <Button onClick={() => open({ view: "pages" })}>{t("Pages")}</Button>
        <Button onClick={() => open({ view: "workflow" })}>{t("Workflows")}</Button>
      </div>} />
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-5">
      {([[t("Objects"), objects], [t("Pages"), pages], [t("Workflows"), workflows], [t("AI functions"), functions], [t("Applications"), applications]] as const)
        .map(([label, query]) => <Card key={label} className="p-3"><p className="text-xs text-muted">{label}</p>
          <p className="mt-1 text-xl font-semibold tabular-nums">{query.data?.records.length ?? (query.isLoading ? "…" : 0)}</p></Card>)}
    </div>
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
