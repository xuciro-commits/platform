import "./i18n";
import { HostContext, useCapabilities } from "@platform/app";
import { localizeEntry, queryCatalog, type CatalogEntry, type CatalogIndex } from "@platform/catalog";
import { Button, Card, Disclosure, Input, PageHeader, Panel, Select, Tag, language, routeToHash, t, useWorkspace } from "@platform/ui";
import { BookOpen, Code, Copy, Layers, Maximize2, Search } from "lucide-react";
import { useContext, useEffect, useMemo, useState } from "react";
import data from "./gen/catalog.json";
import { CatalogPreview } from "./Preview";

export { CatalogPreview };

const index = data as CatalogIndex;
export const catalogLayers = ["Foundations", "Primitives", "Composites", "Semantic UI", "Patterns", "Scenarios"];
export function catalogTitle(id?: string, layer?: string): string {
  const entry = index.entries.find((asset) => asset.id === id);
  if (entry) return localizeEntry(index, entry, language()).name;
  const label = layer === undefined ? undefined : catalogLayers[Number(layer)];
  return label ? t(label) : t("Platform Catalog");
}
const authorities = { specification: "Specification", api: "Current API", recommendation: "Recommendation", example: "Example" };
const maturities = { recommended: "Recommended", experimental: "Experimental", deprecated: "Deprecated" };
const uses = { code: "Code reference", widget: "Page widget", block: "Flow block", template: "Studio template", reference: "Reference" };

function studioRoute(entry: CatalogEntry) {
  return entry.template ? { view: "studio-templates", params: { template: entry.template } }
    : { view: entry.uses.includes("block") ? "workflow" : "pages" };
}

/** This same view runs offline and inside the existing authenticated workspace. */
export function Catalog({ initialID, initialLayer, initialMode = "builder" }: { initialID?: string; initialLayer?: string; initialMode?: "builder" | "developer" }) {
  const host = useContext(HostContext);
  const { open } = useWorkspace();
  const [mode, setMode] = useState(initialMode);
  const [query, setQuery] = useState("");
  const [layer, setLayer] = useState(initialLayer ?? "");
  const [use, setUse] = useState("");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string | undefined>(initialID ?? (initialLayer !== undefined ? undefined : initialMode === "builder" ? "scenario/record-handling" : "ui/button"));
  const [workspaceURL, setWorkspaceURL] = useState("http://localhost:5176/");
  const [copyStatus, setCopyStatus] = useState("");
  useEffect(() => { setLayer(initialLayer ?? ""); setMode(initialMode);
    setSelected(initialID ?? (initialLayer !== undefined ? undefined : initialMode === "builder" ? "scenario/record-handling" : "ui/button"));
  }, [initialID, initialLayer, initialMode]);
  const results = useMemo(() => queryCatalog(index, { query, layer: layer === "" ? undefined : Number(layer),
    use: use === "" ? undefined : use as CatalogEntry["uses"][number], language: language(), offset }), [query, layer, use, offset]);
  const entry = index.entries.find((e) => e.id === (selected ?? results.items[0]?.id));
  const shown = entry && localizeEntry(index, entry, language());
  const snippet = entry?.snippet ?? (entry?.exports?.length
    ? `import { ${[...new Set(entry.exports.map((name) => name.split(".")[0]))].join(", ")} } from "${entry.owner}";` : undefined);
  useEffect(() => setCopyStatus(""), [entry?.id]);
  // Browsing and filters belong to this task, not a new workspace tab per click.
  const choose = (id: string) => setSelected(id);
  const changeMode = (next: "builder" | "developer") => setMode(next);
  const changeLayer = (next: string) => { setLayer(next); setOffset(0); setSelected(undefined); };
  const reset = () => { setOffset(0); setSelected(undefined); };
  const openStudio = (asset: CatalogEntry) => {
    const hash = routeToHash(studioRoute(asset));
    if (host) open(studioRoute(asset));
    else {
      try {
        const url = new URL(workspaceURL);
        if (!["http:", "https:"].includes(url.protocol)) throw new Error();
        url.hash = hash;
        window.open(url.href, "_blank", "noopener,noreferrer");
      } catch { setWorkspaceURL(""); }
    }
  };
  const copyExample = async () => {
    try {
      if (!navigator.clipboard || !snippet) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(snippet);
      setCopyStatus(t("Copied"));
    } catch { setCopyStatus(t("Clipboard unavailable. Select and copy the code below.")); }
  };
  return <div className="space-y-4 p-3 sm:p-5" data-catalog-view={mode}>
    <PageHeader title={t("Platform Catalog")} description={t("Find the right asset. Understand its limits. Reuse its owner.")}
      actions={<div className="flex gap-1">
        <Button variant={mode === "builder" ? "primary" : "default"} onClick={() => changeMode("builder")}><Layers />{t("Build use")}</Button>
        <Button variant={mode === "developer" ? "primary" : "default"} onClick={() => changeMode("developer")}><Code />{t("Developer integration")}</Button>
      </div>} />
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
      <Tag label={t("Public code assets")} tone="info" />
      <span>{t("Source revision")}: <code>{index.revision.slice(0, 16)}</code></span>
      <span>{t("Layers organize discovery; they do not grant permissions.")}</span>
    </div>
    <div className="flex flex-wrap items-center gap-2">
      <Search className="size-4 text-muted" />
      <Input aria-label={t("Search assets")} placeholder={t("Search a task, component or owner…")} value={query}
        className="min-w-48 flex-1" onChange={(e) => { setQuery(e.target.value); reset(); }} />
      <Select aria-label={t("Usage filter")} value={use} onChange={(e) => { setUse(e.target.value); reset(); }}>
        <option value="">{t("All usage modes")}</option>
        {Object.entries(uses).map(([key, label]) => <option key={key} value={key}>{t(label)}</option>)}
      </Select>
    </div>
    <div className="flex flex-wrap gap-1" aria-label={t("Asset layers")}>
      <Button variant={layer === "" ? "primary" : "default"} onClick={() => changeLayer("")}>{t("All layers")}</Button>
      {catalogLayers.map((label, n) => <Button key={label} variant={layer === String(n) ? "primary" : "default"}
        onClick={() => changeLayer(String(n))}><span className="font-mono text-xs">L{n}</span>{t(label)}</Button>)}
    </div>
    {mode === "builder" && <div className="flex flex-wrap items-center gap-2 text-xs">
      <span className="text-muted">{t("Start with a task")}</span>
      {index.entries.filter((e) => e.layer >= 4 && e.maturity === "recommended").map((e) =>
        <Button key={e.id} size="sm" onClick={() => choose(e.id)}>{localizeEntry(index, e, language()).name}</Button>)}
    </div>}
    <div className="grid items-start gap-4 xl:grid-cols-[17rem_minmax(0,1fr)]">
      <Card className={`min-w-0 p-2 xl:block ${selected ? "hidden" : "block"}`}>
        <p className="px-2 py-1 text-xs text-muted">{t("{n} matching assets", { n: results.total })}</p>
        {results.items.length === 0 && <p className="p-3 text-sm text-muted">{t("No matching assets. Try another task or remove a filter.")}</p>}
        <div className="grid gap-1">
          {results.items.map((asset) => <Button key={asset.id} variant={entry?.id === asset.id ? "primary" : "row"}
            className="h-auto whitespace-normal py-2 text-left" onClick={() => choose(asset.id)}>
            <div className="min-w-0">
              <div className="font-medium">{asset.name}</div>
              <div className="mt-1 text-xs opacity-75">L{asset.layer} · {asset.owner} · {t(authorities[asset.authority])}</div>
            </div>
          </Button>)}
        </div>
        <div className="mt-2 flex justify-between gap-2">
          <Button size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - 10))}>{t("Previous")}</Button>
          <Button size="sm" disabled={results.nextOffset === undefined} onClick={() => setOffset(results.nextOffset ?? offset)}>{t("Next")}</Button>
        </div>
      </Card>
      {entry && shown ? <div className={`min-w-0 space-y-3 xl:block ${selected ? "block" : "hidden"}`} data-catalog-asset={entry.id}>
        <Button size="sm" className="xl:hidden" onClick={() => setSelected(undefined)}>{t("Browse matching assets")}</Button>
        <Panel title={shown.name} description={shown.summary}>
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <Tag label={t(authorities[entry.authority])} tone="info" />
            <Tag label={t(maturities[entry.maturity])} tone={entry.maturity === "recommended" ? "success" : "warning"} />
            <span>{entry.owner} · L{entry.layer} · {entry.scope === "platform" ? t("Platform-wide") : t("Domain-specific")}</span>
          </div>
          <code className="mt-2 block break-all text-xs text-muted">{entry.id}</code>
          <div className="mt-3 flex flex-wrap gap-1">{entry.uses.map((u) => <Tag key={u} label={t(uses[u])} />)}</div>
          {shown.constraints?.length ? <ul className="mt-3 list-disc space-y-1 pl-4 text-xs text-muted">{shown.constraints.map((c) => <li key={c}>{c}</li>)}</ul> : null}
          {shown.states?.length ? <p className="mt-2 text-xs text-muted">{t("State handling")}: {shown.states.join(" · ")}</p> : null}
          {entry.uses.some((u) => ["template", "widget", "block"].includes(u)) ? <div className="mt-3 flex flex-wrap items-center gap-2">
            {!host && <Input aria-label={t("Workspace URL")} className="w-64" value={workspaceURL} placeholder={t("Workspace URL")}
              onChange={(e) => setWorkspaceURL(e.target.value)} />}
            <Button variant="primary" disabled={!!host && host.role("build") !== "builder"} onClick={() => openStudio(entry)}
              title={host && host.role("build") !== "builder" ? t("Only builders can open Application Studio.") : undefined}>
              {t(entry.template ? "Create in Studio" : "Open in Studio")}
            </Button>
            <span className="text-xs text-muted">{t("Creates or edits a controlled draft; nothing is published automatically.")}</span>
          </div> : mode === "builder" && <p className="mt-3 text-xs text-muted">{t("Requires platform developer integration; TSX is not tenant configuration.")}</p>}
        </Panel>
        <Panel
          title={t("Live example")}
          description={t("Synthetic fixtures. This preview does not call a tenant host.")}
          actions={
            <Button
              size="sm"
              variant="ghost"
              onClick={() => open({ view: "catalog-example", params: { id: entry.id, mode } })}
              aria-label={t("Expand example")}
            >
              <Maximize2 className="size-3.5" />
              {t("Expand example")}
            </Button>
          }
        >
          <CatalogPreview key={entry.id} id={entry.id} />
        </Panel>
        {mode === "developer" && <Panel title={t("Public integration")}>
          <p className="break-all text-xs">{t("Source")}: <code>{entry.source}</code></p>
          {entry.type && <p className="mt-2 text-xs">{t("Source type reference")}: <code>{entry.type}</code></p>}
          <p className="mt-2 text-xs text-muted">{t("Source types are authoritative; complex Props are not duplicated here.")}</p>
          {snippet && <div className="mt-3 space-y-2">
            <Button size="sm" onClick={() => void copyExample()}><Copy />{t("Copy developer example")}</Button>
            {copyStatus && <p role="status" className="text-xs text-muted">{copyStatus}</p>}
            <pre className="max-h-80 overflow-auto rounded-sm bg-background p-3 text-xs"><code>{snippet}</code></pre>
          </div>}
          {index.api?.[entry.owner] && <Disclosure className="mt-3 text-xs" summary={t("Owner API references")}>
            <p className="mt-2 break-all font-mono">{index.api[entry.owner]!.source}</p>
            <p className="mt-2 break-all">{index.api[entry.owner]!.symbols.join(" · ")}</p>
            <p className="mt-2 break-all text-muted">{t("Public types")}: {index.api[entry.owner]!.types.join(" · ")}</p>
          </Disclosure>}
        </Panel>}
        {entry.dependencies?.length ? <Panel title={t("Composed from")}>
          <div className="flex flex-wrap gap-2">{entry.dependencies.map((id) => {
            const dep = index.entries.find((e) => e.id === id);
            return <Button size="sm" key={id} onClick={() => choose(id)}>{dep ? localizeEntry(index, dep, language()).name : id}</Button>;
          })}</div>
        </Panel> : null}
        <Panel title={t("Declared recipes and examples")} description={t("Declared references are not a complete consumer graph.")}>
          <div className="flex flex-wrap gap-2">{index.entries.filter((e) => e.dependencies?.includes(entry.id)).map((e) =>
            <Button key={e.id} size="sm" onClick={() => choose(e.id)}>{localizeEntry(index, e, language()).name}</Button>)}</div>
          {mode === "developer" && index.consumers?.[entry.id]?.length ? <Disclosure className="mt-2 text-xs text-muted" summary={t("Static direct consumers")}>
            <ul className="mt-2 max-h-40 overflow-auto">{index.consumers[entry.id]!.map((file) => <li key={file} className="break-all font-mono">{file}</li>)}</ul>
          </Disclosure> : null}
        </Panel>

      </div> : <Panel title={t("Select an asset")} description={t("Browse layers or search by the task you want to complete.")} />}
    </div>
    <Panel title={t("Tenant runtime capabilities")} description={t("Runtime discovery uses the current host, tenant and identity. It is separate from public code assets.")}>
      {host ? <RuntimeCapabilities query={query} /> : <p className="text-sm text-muted"><BookOpen className="mr-1 inline size-4" />
        {t("Connection required. Open Catalog inside your workspace to discover authorized capabilities.")}</p>}
    </Panel>
  </div>;
}

/** Expanded examples remain normal application views inside the shared shell. */
export function CatalogExample({ id, mode = "developer" }: { id?: string; mode?: "builder" | "developer" }) {
  const { open } = useWorkspace();
  const entry = index.entries.find((asset) => asset.id === id);
  return <div className="space-y-4 p-3 sm:p-5">
    <PageHeader title={catalogTitle(id)} description={t("Synthetic fixtures. This preview does not call a tenant host.")}
      actions={<Button onClick={() => open({ view: "catalog", params: { ...(id ? { id } : {}), mode } })}>{t("View asset details")}</Button>} />
    <Card className="p-4">{entry ? <CatalogPreview id={entry.id} /> : <p className="text-sm text-muted">{t("Select an asset")}</p>}</Card>
  </div>;
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
  const rows = !online || capabilities.isError ? [] : (capabilities.data ?? []).filter((c) =>
    `${c.title} ${c.description} ${key(c)}`.toLowerCase().includes(query.toLowerCase()));
  const selected = rows.find((c) => key(c) === picked);
  return <div className="space-y-2 text-xs">
    <p>{location.host} · {host.me.tenantId} · {host.me.principalId}</p>
    {!online || capabilities.isError ? <div role="alert"><p>{t("Currently unavailable. Reconnect or check your permissions.")}</p>
        <Button size="sm" onClick={() => void capabilities.refetch()}>{t("Retry")}</Button></div>
        : capabilities.isPending ? <p>{t("Loading authorized capabilities…")}</p> : <>
          <p className="text-muted">{t("Discoverable in this context; inputs and execution permissions are checked by the original owner.")}</p>
          <Button size="sm" disabled={capabilities.isFetching} onClick={() => void capabilities.refetch()}>{t("Refresh capabilities")}</Button>
          <div className="flex max-h-40 flex-wrap gap-1 overflow-auto">{rows.map((c) => <Button key={key(c)} size="sm"
            variant={picked === key(c) ? "primary" : "default"} onClick={() => setPicked(key(c))}>{c.title || key(c)}</Button>)}</div>
          {rows.length === 0 && <p>{t("No discoverable capabilities in this context.")}</p>}
          {selected && <div className="space-y-1">
            <code>{key(selected)} · {selected.version || selected.revision || "—"}</code>
            <p>{selected.description}</p>
            <p>{t("Execution is disabled in Catalog. Use the owning Studio or application.")}</p>
            <pre className="max-h-48 overflow-auto bg-background p-2">{JSON.stringify({ input: selected.input, output: selected.output, effects: selected.effects }, null, 2)}</pre>
          </div>}
        </>}
  </div>;
}
