import { useMemo, useState } from "react";
import type { Api } from "@platform/kernel";
import { assetKey, definitionPresentation, useDependencies, useHost, useRecordInventory } from "@platform/app";
import { Button, EmptyState, IconGlyph, Input, Notice, PageHeader, PanelSection, RelationCanvas, SegmentedChoice, Tag, Toggles, relationNodeClasses, t, toneNames, type RelationEdge, type RelationNode, type Route, type Tone } from "@platform/ui";
import { useApplicationWorkspace } from "./application-scope";
import { kindIcon } from "./resource-icons";
import { matchesResourceKind, refKey, resourceKinds, resourceRef, resourceRoute, type ResourceKindInfo, type ResourceRecord } from "./resources";
import { studioAreas } from "./studio-model";
import { ExploreRecords } from "./explore-records";

const areaTone: Record<string, Tone> = { model: "info", logic: "purple", interface: "success", integration: "orange", release: "teal" };
const types = [...new Set(resourceKinds.map(kind => kind.type))];
const limit = 300;
const areaOf = (kind: string) => studioAreas.find(area => area.kinds.includes(kind) || kind === "action" && area.id === "logic")?.id;
const kindTitle = (kind: string) => {
  const info = resourceKinds.find(item => (item.ref ?? item.kind) === kind);
  return t(info?.label ?? relationNodeClasses[kind]?.title ?? kind);
};

function useResourceRoutes() {
  const { definitions, opens } = useHost();
  const inventories = types.map(type => useRecordInventory<ResourceRecord>(type, 1000));
  const drafts = useMemo(() => {
    const found = new Map<string, { kind: ResourceKindInfo; record: ResourceRecord }>();
    for (const kind of resourceKinds) for (const record of inventories[types.indexOf(kind.type)]?.data?.records ?? []) {
      if (record.archived || !record.name || !matchesResourceKind(kind, record)) continue;
      const key = refKey(resourceRef(kind, record.name));
      if (!found.has(key)) found.set(key, { kind, record });
    }
    return found;
  }, inventories.map(inventory => inventory.data));
  return (ref: Api.AssetRef): Route => {
    const draft = drafts.get(refKey(ref));
    if (draft) {
      const route = resourceRoute(draft.kind, draft.record.id);
      return draft.kind.kind === "automation" || draft.kind.kind === "page" ? route : { ...route, view: opens.get(draft.kind.type) ?? route.view };
    }
    if (ref.kind === "action") {
      const target = definitions.find(definition => refKey(definition.ref) === refKey(ref))?.action?.target;
      const object = target ? drafts.get(refKey({ app: ref.app, kind: "object", name: target })) : undefined;
      const name = object && ref.name.slice(target!.length + 1);
      if (object?.record.actions?.some(action => action.name === name)) return { view: "action-type", params: { id: object.record.id, action: name! } };
    }
    if (ref.kind === "observation") return { view: "observations", params: { app: ref.app, name: ref.name } };
    return { view: "definition", params: { ...ref, surface: "studio" } };
  };
}

export function Discover() {
  const { definitions, entities } = useHost(), { open } = useApplicationWorkspace();
  const dependencies = useDependencies();
  const routeOf = useResourceRoutes();
  const [scope, setScope] = useState("authored");
  const [hidden, setHidden] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<string>();
  const descriptors = dependencies.data?.descriptors ?? [];

  const graph = useMemo(() => {
    const byKey = new Map(descriptors.map(item => [assetKey(item.ref), item]));
    const defined = new Map(definitions.map(item => [assetKey(item.ref), item]));
    const shown = (key: string) => !hidden.includes(areaOf(byKey.get(key)!.ref.kind) ?? "");
    const authored = (key: string) => defined.get(key)?.source === "tenant";
    const kept = new Set<string>();
    for (const [key, item] of byKey) {
      if (scope === "authored" && !authored(key) || !shown(key)) continue;
      kept.add(key);
      for (const required of item.requires) { const other = assetKey(required); if (byKey.has(other) && shown(other)) kept.add(other); }
    }
    const edgesOf = (keys: Set<string>) => [...keys].flatMap(key => byKey.get(key)!.requires.map(assetKey).filter(other => keys.has(other)).map(other => ({ source: other, target: key })));
    const text = search.trim().toLocaleLowerCase();
    let keys = kept;
    if (text) {
      const label = (key: string) => `${definitionPresentation(defined.get(key) ?? { ref: byKey.get(key)!.ref } as never).title} ${byKey.get(key)!.ref.name}`.toLocaleLowerCase();
      const matching = new Set([...kept].filter(key => label(key).includes(text)));
      keys = new Set(matching);
      for (const edge of edgesOf(kept)) { if (matching.has(edge.source)) keys.add(edge.target); if (matching.has(edge.target)) keys.add(edge.source); }
    }
    const edges = edgesOf(keys);
    return { byKey, defined, keys, edges, areas: new Set([...kept].map(key => areaOf(byKey.get(key)!.ref.kind) ?? "")) };
  }, [descriptors, definitions, scope, hidden, search]);

  const { nodes, edges } = useMemo(() => {
    if (graph.keys.size > limit) return { nodes: [] as RelationNode[], edges: [] as RelationEdge[] };
    const nodes = [...graph.keys].map((key): RelationNode => {
      const descriptor = graph.byKey.get(key)!, kind = descriptor.ref.kind, definition = graph.defined.get(key);
      const entity = kind === "object" ? entities.find(item => item.type === descriptor.ref.name) : undefined;
      const area = areaOf(kind);
      const tone = entity?.color && (toneNames as readonly string[]).includes(entity.color) ? entity.color : area ? areaTone[area] : "neutral";
      return { id: key, label: definition ? definitionPresentation(definition).title : descriptor.ref.name, tone, caption: `${kindTitle(kind)} · ${descriptor.ref.app}`,
        detail: `${kindTitle(kind)} · ${descriptor.ref.app}/${descriptor.ref.name} · ${descriptor.version}`,
        icon: entity?.icon ? <IconGlyph name={entity.icon} /> : kindIcon[kind] };
    });
    return { nodes, edges: graph.edges.map((edge): RelationEdge => ({ id: `${edge.source}>${edge.target}`, source: edge.source, target: edge.target, directed: true })) };
  }, [graph, entities]);

  const current = selected && graph.byKey.get(selected);
  const neighbours = (key: string, direction: "uses" | "usedBy") => graph.edges.filter(edge => direction === "uses" ? edge.target === key : edge.source === key).map(edge => direction === "uses" ? edge.source : edge.target);
  const label = (key: string) => { const definition = graph.defined.get(key); return definition ? definitionPresentation(definition).title : graph.byKey.get(key)?.ref.name ?? key; };
  const openEditor = (key: string) => { const descriptor = graph.byKey.get(key); if (descriptor) open(routeOf(descriptor.ref)); };

  return <div className="grid min-w-0 gap-3">
    <PageHeader title={t("Discover")} help={t("See how definitions depend on each other. Hover a node to focus its neighbours, select one to inspect it, double-click to open its editor.")} />
    <div className="flex flex-wrap items-center gap-2">
      <SegmentedChoice label={t("Definitions shown")} value={scope} onChange={setScope} options={[{ value: "authored", label: t("Authored") }, { value: "all", label: t("All definitions") }]} />
      <Input type="search" aria-label={t("Search definitions")} placeholder={t("Search definitions")} value={search} onChange={event => setSearch(event.target.value)} className="h-7 w-56 text-xs" />
      <Toggles options={studioAreas.filter(area => graph.areas.has(area.id) || hidden.includes(area.id)).map(area => ({ value: area.id, label: t(area.title) }))} value={studioAreas.map(area => area.id).filter(id => !hidden.includes(id))}
        onChange={visible => setHidden(studioAreas.map(area => area.id).filter(id => !visible.includes(id)))} />
      <span className="ml-auto text-xs tabular-nums text-muted">{t("{count} definitions", { count: graph.keys.size })}</span>
    </div>
    {dependencies.isError ? <Notice tone="danger" role="alert" message={t("Asset dependencies could not be loaded.")} />
      : dependencies.isPending ? <p role="status" className="text-sm text-muted">{t("Loading…")}</p>
      : graph.keys.size > limit ? <EmptyState title={t("Too many definitions to draw ({count}).", { count: graph.keys.size })} hint={t("Narrow the graph by kind or search.")} />
      : !nodes.length ? <EmptyState title={t("No definitions match.")} />
      : <div className="grid min-w-0 gap-3 lg:grid-cols-[minmax(0,1fr)_320px]">
        <RelationCanvas layout="right" nodes={nodes} edges={edges} selected={selected} focusOnHover height="calc(100vh - 220px)" label={t("Declared dependencies")} onSelect={setSelected} onOpen={openEditor}
          viewportKey={`${scope}|${hidden.join(",")}|${search}`} />
        <aside aria-label={t("Definition inspector")} className="min-w-0 rounded-md border border-border bg-surface">
          {current ? <div className="grid gap-1">
            <PanelSection title={t("Definition")}>
              <div className="grid gap-2 px-1 text-xs">
                <h2 className="break-words text-sm font-semibold">{label(selected!)}</h2>
                <div className="flex flex-wrap items-center gap-1"><Tag label={kindTitle(current.ref.kind)} tone={areaTone[areaOf(current.ref.kind) ?? ""] ?? "neutral"} /><span className="text-muted">{current.ref.app}</span></div>
                <p className="break-all font-mono text-xs text-muted">{current.ref.app}/{current.ref.kind}/{current.ref.name} · {current.version}</p>
                <div><Button size="sm" variant="primary" onClick={() => openEditor(selected!)}>{t("Open editor")}</Button></div>
              </div>
            </PanelSection>
            {current.ref.kind === "object" && entities.some(item => item.type === current.ref.name) && <PanelSection title={t("Explore records of this type")}><ExploreRecords key={current.ref.name} object={current.ref.name} /></PanelSection>}
            {(["uses", "usedBy"] as const).map(direction => {
              const keys = neighbours(selected!, direction);
              return <PanelSection key={direction} title={direction === "uses" ? t("Uses") : t("Used by")}>
                {keys.length ? <ul className="grid gap-0.5">{keys.map(key => <li key={key}><Button size="sm" variant="row" onClick={() => setSelected(key)}><span className="min-w-0 flex-1 truncate">{label(key)}</span><span className="text-muted">{kindTitle(graph.byKey.get(key)!.ref.kind)}</span></Button></li>)}</ul>
                  : <p className="px-1 text-xs text-muted">{t("None")}</p>}
              </PanelSection>;
            })}
          </div> : <EmptyState className="p-6" title={t("Select a definition")} hint={t("Its dependencies and the editor that owns it appear here.")} />}
        </aside>
      </div>}
  </div>;
}
