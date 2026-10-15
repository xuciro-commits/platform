// ADR-0118: declaration, reverse impact and discovery read one owner descriptor.
import { assetKey, DependencyGraph, useDependencies, useDefinitions, useProvidedCapabilities, type AssetRef, type Definition, type ProvidedCapability } from "@platform/app";
import { Button, DataTable, Disclosure, PageHeader, Panel, relationNodeClasses, Select, useWorkspace, t } from "@platform/ui";
import { useState } from "react";

const kindLabel = (kind: string) => t(relationNodeClasses[kind]?.title ?? kind);
const titleOf = (d?: Definition) => d?.entity?.title ?? d?.action?.title ?? d?.page?.title ?? d?.application?.title ?? d?.ref.name;
const capabilityKey = (item: ProvidedCapability) => `${assetKey(item.ref)}@${item.version}:${item.capability.kind}:${item.capability.name}`;
const sideLabels: Record<string, string> = { pure: "Read only", effect: "Declare an effect", authoritative: "Change authoritative state" };

export function Lineage({ ref: requested }: { ref?: string }) {
  const definitions = useDefinitions().data ?? [];
  const { open } = useWorkspace();
  const [picked, setPicked] = useState<string>();
  const [app, setApp] = useState<string>();
  const focus = picked ?? requested ?? "";
  const parts = focus.split("/");
  const ref = focus && parts.length === 3 ? { app: parts[0]!, kind: parts[1]!, name: parts[2]! } : undefined;
  const query = useDependencies(ref);
  const descriptors = query.data?.descriptors ?? [];
  const apps = [...new Set(descriptors.map(d => d.ref.app))].sort();
  const scopeApp = ref?.app ?? app ?? apps[0] ?? "";
  const openAsset = (ref: AssetRef) => {
    if (ref.kind === "page") return open({ view: "page", params: { ...ref } });
    open({ view: "definition", params: { ...ref } });
  };
  const focused = descriptors.find(d => assetKey(d.ref) === focus);
  return <div className="grid min-w-0 gap-4">
    <PageHeader title={t("Lineage")} description={t("Declared dependencies, reverse impact and available capabilities share one versioned description. Select an asset to see what it uses and what a change reaches.")}
      actions={<>
        <Select aria-label={t("App")} value={scopeApp} className="w-40" onChange={event => { setApp(event.target.value); setPicked(""); }}>{apps.map(id => <option key={id} value={id}>{id}</option>)}</Select>
        <Select aria-label={t("Asset")} value={focus} className="w-72" onChange={event => setPicked(event.target.value)}>
          <option value="">{t("Every asset of {app}", { app: scopeApp })}</option>
          {descriptors.filter(d => d.ref.app === scopeApp).map(d => <option key={assetKey(d.ref)} value={assetKey(d.ref)}>{kindLabel(d.ref.kind)} · {titleOf(definitions.find(item => assetKey(item.ref) === assetKey(d.ref))) ?? d.ref.name}</option>)}
        </Select>
      </>} />
    <Panel title={t("Declared dependencies")}>
      <DependencyGraph asset={ref} app={scopeApp} height={400} onSelect={item => setPicked(assetKey(item))} onOpen={openAsset} />
      {focused && <div className="grid gap-2 border-t border-border pt-3 text-xs sm:grid-cols-2">
        {[{ title: "Uses", refs: query.data?.requires ?? [] }, { title: "Used by", refs: query.data?.usedBy ?? [] }].map(group => <div key={group.title}><h3 className="font-semibold">{t(group.title)}</h3><div className="flex flex-wrap gap-1">{group.refs.length ? group.refs.map(item => <Button variant="ghost" size="sm" key={assetKey(item)} onClick={() => setPicked(assetKey(item))}>{item.name}</Button>) : <span className="text-muted">{t("None")}</span>}</div></div>)}
      </div>}
    </Panel>
    <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(16rem,1fr)]">
      <CapabilityDiscovery app={scopeApp} focus={focused?.ref} onOpen={openAsset} />
      <Panel title={t("Runtime evidence")} description={t("This graph describes declared relationships. Recorded runs, delivered effects and source results remain in their original diagnostics.")}>
        {focused && <Button size="sm" onClick={() => openAsset(focused.ref)}>{t("Open asset")}</Button>}
        <Button size="sm" variant="ghost" onClick={() => open({ view: "integration-health" })}>{t("Integration health")}</Button>
        {focused?.ref.kind === "object" && <Button size="sm" variant="ghost" onClick={() => open({ view: "object-type", params: { object: focused.ref.name, tab: "data" } })}>{t("Object data and evidence")}</Button>}
      </Panel>
    </div>
  </div>;
}

function CapabilityDiscovery({ app, focus, onOpen }: { app: string; focus?: AssetRef; onOpen: (ref: AssetRef) => void }) {
  const [kind, setKind] = useState("");
  const [side, setSide] = useState("");
  const [selected, setSelected] = useState<string>();
  const query = useProvidedCapabilities(kind, side);
  const rows = (query.data ?? []).filter(item => focus ? assetKey(item.ref) === assetKey(focus) : item.ref.app === app);
  const chosen = rows.find(item => capabilityKey(item) === selected);
  return <Panel title={t("Available capabilities")} description={t("The host lists capabilities available to you. Choose a kind or invocation type to inspect their owner, version and typed inputs.")} className="grid min-w-0 gap-3">
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
      <Disclosure summary={t("Typed inputs and outputs")}><pre className="max-h-56 overflow-auto rounded bg-subtle p-2">{JSON.stringify({ inputs: chosen.capability.inputs, outputs: chosen.capability.outputs, parameters: chosen.capability.parameters }, null, 2)}</pre></Disclosure>
    </div>}
  </Panel>;
}
