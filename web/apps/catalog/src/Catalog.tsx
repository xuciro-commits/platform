// The Asset Library (ADR-0054 D3): one IDE-shaped view over the public code
// assets. Left: a searchable tree by layer. Main: the asset's documentation and
// its live example. Right: how to use it (import, source, API, composition,
// consumers, open in Builder). Dock: Health of the index and the runtime
// capabilities of the signed-in host. Browsing never opens new tabs.
import "./i18n";
import { HostContext, useCapabilities } from "@platform/app";
import { localizeEntry, type CatalogEntry, type CatalogIndex } from "@platform/catalog";
import { Button, Input, PanelSection, Select, StructureRow, Tag, Workbench, language, t, useWorkspace } from "@platform/ui";
import { Box, Copy, Hammer, Maximize2, Minimize2 } from "lucide-react";
import { useContext, useEffect, useMemo, useState } from "react";
import data from "./gen/catalog.json";
import { CatalogPreview } from "./Preview";

export { CatalogPreview };

const index = data as CatalogIndex;
const catalogLayers = ["Foundations", "Primitives", "Composites", "Semantic UI", "Patterns", "Scenarios"];
export function catalogTitle(id?: string, layer?: string): string {
  const entry = index.entries.find((asset) => asset.id === id);
  if (entry) return localizeEntry(index, entry, language()).name;
  const label = layer === undefined ? undefined : catalogLayers[Number(layer)];
  return label ? t(label) : t("Asset Library");
}
const authorities = { specification: "Specification", api: "Current API", recommendation: "Recommendation", example: "Example" };
const maturities = { recommended: "Recommended", experimental: "Experimental", deprecated: "Deprecated" };
const uses = { code: "Code reference", widget: "Page widget", block: "Flow block", template: "Studio template", reference: "Reference" };

/** Where an asset is used in Builder: templates create, blocks open the flows, widgets open the modules. */
function builderRoute(entry: CatalogEntry) {
  return entry.template ? { view: "studio-templates", params: { template: entry.template } }
    : { view: entry.uses.includes("block") ? "flow" : "module" };
}
const matches = (entry: CatalogEntry, terms: string[], use: string) => {
  if (use && !entry.uses.includes(use as CatalogEntry["uses"][number])) return false;
  if (!terms.length) return true;
  const zh = localizeEntry(index, entry, "zh-CN");
  const text = [entry.id, entry.name, entry.summary, zh.name, zh.summary, entry.type ?? "", ...(entry.exports ?? []), ...entry.tags].join(" ").toLocaleLowerCase();
  return terms.every((term) => text.includes(term));
};

export function Catalog({ initialID, initialLayer, initialExpand = false }: { initialID?: string; initialLayer?: string; initialExpand?: boolean }) {
  const host = useContext(HostContext);
  const { open } = useWorkspace();
  const [query, setQuery] = useState("");
  const [use, setUse] = useState("");
  const [selected, setSelected] = useState<string | undefined>(initialID ?? "ui/button");
  const [expanded, setExpanded] = useState(initialExpand);
  const [dock, setDock] = useState("health");
  const [copied, setCopied] = useState("");
  useEffect(() => { if (initialID) setSelected(initialID); setExpanded(initialExpand); }, [initialID, initialExpand]);
  const terms = query.toLocaleLowerCase().trim().split(/\s+/).filter(Boolean);
  const tree = useMemo(() => catalogLayers.map((label, layer) => ({ label, layer,
    entries: index.entries.filter((e) => e.layer === layer && matches(e, terms, use) && (initialLayer === undefined || Number(initialLayer) === layer))
      .map((e) => localizeEntry(index, e, language())) })).filter((group) => group.entries.length), [terms.join(" "), use, initialLayer]);
  const total = tree.reduce((n, group) => n + group.entries.length, 0);
  const entry = index.entries.find((e) => e.id === selected);
  const shown = entry && localizeEntry(index, entry, language());
  const snippet = entry?.snippet ?? (entry?.exports?.length ? `import { ${[...new Set(entry.exports.map((name) => name.split(".")[0]))].join(", ")} } from "${entry.owner}";` : undefined);
  const copy = async (text: string, label: string) => {
    try { await navigator.clipboard.writeText(text); setCopied(label); setTimeout(() => setCopied(""), 1500); } catch { setCopied(t("Clipboard unavailable. Select and copy the code below.")); }
  };
  const canBuild = !!entry && entry.uses.some((u) => ["template", "widget", "block"].includes(u));
  const builderOnly = !!host && host.role("build") !== "builder";
  const health = useMemo(() => {
    const owners = new Map<string, number>();
    for (const e of index.entries) owners.set(e.owner, (owners.get(e.owner) ?? 0) + 1);
    return { owners: [...owners.entries()].sort((a, b) => b[1] - a[1]),
      noExample: index.entries.filter((e) => !e.example && !e.uses.includes("reference")),
      deprecated: index.entries.filter((e) => e.maturity === "deprecated"), experimental: index.entries.filter((e) => e.maturity === "experimental") };
  }, []);
  const pick = (id: string) => <Button key={id} size="sm" variant="ghost" onClick={() => { setSelected(id); setExpanded(false); }}>{catalogTitle(id)}</Button>;

  return <Workbench storageKey="asset-library" mainLabel={t("Asset")} crumbs={[{ label: t("Asset Library"), onClick: () => setExpanded(false) }, ...(shown ? [{ label: shown.name }] : [])]}
    status={<span className="text-xs text-muted">{t("Source revision")} <code>{index.revision.slice(0, 12)}</code></span>}
    actions={entry?.example ? <Button size="sm" variant="ghost" onClick={() => setExpanded((v) => !v)}>{expanded ? <Minimize2 /> : <Maximize2 />}{expanded ? t("Show documentation") : t("Expand example")}</Button> : undefined}
    left={{ label: t("Assets"), content: <div className="grid min-h-0 content-start gap-2 p-2">
      <Input type="search" aria-label={t("Search assets")} placeholder={t("Search a task, component or owner…")} value={query} onChange={(e) => setQuery(e.target.value)} />
      <Select aria-label={t("Usage filter")} value={use} onChange={(e) => setUse(e.target.value)}>
        <option value="">{t("All usage modes")}</option>
        {Object.entries(uses).map(([key, label]) => <option key={key} value={key}>{t(label)}</option>)}
      </Select>
      <p className="px-1 text-xs text-muted">{t("{n} matching assets", { n: total })}</p>
      {tree.map((group) => <PanelSection key={group.layer} title={<span><span className="mr-1 font-mono text-[10px] text-muted">L{group.layer}</span>{t(group.label)}</span>}>
        {group.entries.map((asset) => <StructureRow key={asset.id} icon={<Box />} label={asset.name} meta={asset.owner.replace("@platform/", "").replace("@pkg/", "")} selected={asset.id === selected} onClick={() => { setSelected(asset.id); setExpanded(false); }} />)}
      </PanelSection>)}
      {total === 0 && <p className="p-2 text-sm text-muted">{t("No matching assets. Try another task or remove a filter.")}</p>}
    </div> }}
    right={entry && shown && !expanded ? { label: t("Use it"), content: <div className="grid content-start gap-3 p-3 text-xs">
      {canBuild && <div className="grid gap-1">
        <Button variant="primary" size="sm" disabled={builderOnly} title={builderOnly ? t("Only builders can open Projects.") : undefined} onClick={() => open(builderRoute(entry))}><Hammer />{t(entry.template ? "Create in Builder" : "Open in Builder")}</Button>
        <span className="text-muted">{t("Creates or edits a controlled draft; nothing is published automatically.")}</span>
      </div>}
      {snippet && <div className="grid gap-1">
        <div className="flex items-center justify-between"><span className="font-medium">{t("Import")}</span><Button size="sm" variant="ghost" onClick={() => void copy(snippet, t("Copied"))}><Copy />{t("Copy")}</Button></div>
        <pre className="max-h-48 overflow-auto rounded-sm bg-background p-2"><code>{snippet}</code></pre>
        {copied && <p role="status" className="text-muted">{copied}</p>}
      </div>}
      <div className="grid gap-1">
        <span className="font-medium">{t("Source")}</span>
        <code className="break-all text-muted">{entry.source}</code>
        {entry.type && <p>{t("Source type reference")}: <code>{entry.type}</code></p>}
      </div>
      {index.api?.[entry.owner] && <div className="grid gap-1">
        <span className="font-medium">{t("Owner API")}</span>
        <p className="break-all font-mono text-muted">{index.api[entry.owner]!.source}</p>
        <p className="break-all">{index.api[entry.owner]!.symbols.join(" · ")}</p>
      </div>}
      {entry.dependencies?.length ? <div className="grid gap-1"><span className="font-medium">{t("Composed from")}</span><div className="flex flex-wrap gap-1">{entry.dependencies.map(pick)}</div></div> : null}
      <div className="grid gap-1">
        <span className="font-medium">{t("Used by")}</span>
        <div className="flex flex-wrap gap-1">{index.entries.filter((e) => e.dependencies?.includes(entry.id)).map((e) => pick(e.id))}</div>
        {index.consumers?.[entry.id]?.length ? <ul className="max-h-40 overflow-auto text-muted">{index.consumers[entry.id]!.map((file) => <li key={file} className="break-all font-mono">{file}</li>)}</ul> : <p className="text-muted">{t("Declared references are not a complete consumer graph.")}</p>}
      </div>
    </div> } : undefined}
    dock={{ label: t("Library dock"), value: dock, onChange: setDock, tabs: [
      { id: "health", title: t("Health"), badge: health.deprecated.length || undefined, content: <div className="grid gap-3 p-3 text-xs sm:grid-cols-3">
        <div><p className="text-muted">{t("Assets")}</p><p className="text-2xl font-semibold">{index.entries.length}</p>
          <ul className="mt-1 text-muted">{health.owners.map(([owner, n]) => <li key={owner}>{owner} · {n}</li>)}</ul></div>
        <div><p className="text-muted">{t("Without a live example")} · {health.noExample.length}</p><div className="flex flex-wrap gap-1">{health.noExample.map((e) => pick(e.id))}</div></div>
        <div><p className="text-muted">{t("Deprecated")} · {health.deprecated.length}</p><div className="flex flex-wrap gap-1">{health.deprecated.map((e) => pick(e.id))}</div>
          <p className="mt-2 text-muted">{t("Experimental")} · {health.experimental.length}</p><div className="flex flex-wrap gap-1">{health.experimental.map((e) => pick(e.id))}</div></div>
      </div> },
      { id: "runtime", title: t("Runtime"), content: <div className="p-3">{host ? <RuntimeCapabilities query={query} /> : <p className="text-sm text-muted">{t("Connection required. Open the Asset Library inside your workspace to discover authorized capabilities.")}</p>}</div> },
    ] }}>
    {entry && shown ? <div className="grid min-h-0 flex-1 content-start gap-3 overflow-auto p-4" data-catalog-asset={entry.id}>
      {!expanded && <div className="grid gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <h1 className="text-lg font-semibold">{shown.name}</h1>
          <Tag label={t(authorities[entry.authority])} tone="info" />
          <Tag label={t(maturities[entry.maturity])} tone={entry.maturity === "recommended" ? "success" : "warning"} />
          <span className="text-xs text-muted">{entry.owner} · L{entry.layer} · {entry.scope === "platform" ? t("Platform-wide") : t("Domain-specific")}</span>
        </div>
        <p className="text-sm">{shown.summary}</p>
        <code className="break-all text-xs text-muted">{entry.id}</code>
        <div className="flex flex-wrap gap-1">{entry.uses.map((u) => <Tag key={u} label={t(uses[u])} />)}{entry.tags.map((tag) => <Tag key={tag} label={tag} tone="neutral" />)}</div>
        {shown.constraints?.length ? <ul className="list-disc space-y-1 pl-4 text-xs text-muted">{shown.constraints.map((c) => <li key={c}>{c}</li>)}</ul> : null}
        {shown.states?.length ? <p className="text-xs text-muted">{t("State handling")}: {shown.states.join(" · ")}</p> : null}
      </div>}
      <div role="region" aria-label={t("Live example")} className="rounded-md border border-border">
        <div className="flex items-center justify-between border-b border-border px-3 py-1 text-xs text-muted"><span>{t("Live example")}</span><span>{t("Synthetic fixtures. This preview does not call a tenant host.")}</span></div>
        <div className="p-3"><CatalogPreview key={entry.id} id={entry.id} /></div>
      </div>
    </div> : <p className="p-4 text-sm text-muted">{t("Select an asset from the tree, or search by the task you want to complete.")}</p>}
  </Workbench>;
}

function RuntimeCapabilities({ query }: { query: string }) {
  const host = useContext(HostContext)!;
  const capabilities = useCapabilities();
  const [picked, setPicked] = useState<string>();
  const [online, setOnline] = useState(navigator.onLine);
  useEffect(() => {
    const connect = () => { setOnline(true); void capabilities.refetch(); };
    const disconnect = () => { setOnline(false); setPicked(undefined); };
    addEventListener("online", connect); addEventListener("offline", disconnect);
    return () => { removeEventListener("online", connect); removeEventListener("offline", disconnect); };
  }, [capabilities.refetch]);
  const key = (c: NonNullable<typeof capabilities.data>[number]) => `${c.ref.app}/${c.ref.kind}/${c.ref.name}`;
  // Do not retain a previous identity's discoverable inventory after a failed read.
  const rows = !online || capabilities.isError ? [] : (capabilities.data ?? []).filter((c) => `${c.title} ${c.description} ${key(c)}`.toLowerCase().includes(query.toLowerCase()));
  const selected = rows.find((c) => key(c) === picked);
  return <div className="space-y-2 text-xs">
    <p className="text-muted">{location.host} · {host.me.tenantId} · {host.me.principalId} — {t("Discoverable in this context; inputs and execution permissions are checked by the original owner.")}</p>
    {!online || capabilities.isError ? <div role="alert"><p>{t("Currently unavailable. Reconnect or check your permissions.")}</p><Button size="sm" onClick={() => void capabilities.refetch()}>{t("Retry")}</Button></div>
      : capabilities.isPending ? <p>{t("Loading authorized capabilities…")}</p> : <>
        <div className="flex max-h-32 flex-wrap gap-1 overflow-auto">{rows.map((c) => <Button key={key(c)} size="sm" variant={picked === key(c) ? "primary" : "default"} onClick={() => setPicked(key(c))}>{c.title || key(c)}</Button>)}</div>
        {rows.length === 0 && <p>{t("No discoverable capabilities in this context.")}</p>}
        {selected && <div className="space-y-1">
          <code>{key(selected)} · {selected.version || selected.revision || "—"}</code>
          <p>{selected.description}</p>
          <pre className="max-h-48 overflow-auto bg-background p-2">{JSON.stringify({ input: selected.input, output: selected.output, effects: selected.effects }, null, 2)}</pre>
        </div>}
      </>}
  </div>;
}
