import { useState } from "react";
import { localizeEntry, type CatalogEntry, type CatalogIndex } from "@platform/catalog";
import { Button, Input, InspectorSection, Tag, language, t } from "@platform/ui";
import { Box, ChevronDown, ChevronRight, Copy, FileCode, Hammer, Layers, Search } from "lucide-react";

export interface AssetInspectorProps {
  entry: CatalogEntry;
  index: CatalogIndex;
  shown: ReturnType<typeof localizeEntry>;
  onSelect: (id: string) => void;
  canBuild?: boolean;
  builderOnly?: boolean;
  onOpenBuilder?: () => void;
}

type AssetNav = Pick<AssetInspectorProps, "index" | "onSelect">;
type LinkKind = "composition" | "declared";

/** One local row style for both directions of the asset relationship. */
function AssetLink({ id, kind, index, onSelect }: AssetNav & { id: string; kind: LinkKind }) {
  const asset = index.entries.find((item) => item.id === id);
  const Icon = kind === "composition" ? Box : Layers;
  const label = kind === "composition" ? t("Composition") : t("Declared");
  return (
    <Button type="button" variant="row" disabled={!asset}
      className="flex w-full items-center justify-between gap-2 rounded border-0 p-1.5 text-left text-[length:inherit] whitespace-normal hover:bg-row-hover disabled:pointer-events-auto disabled:cursor-default disabled:opacity-60"
      onClick={() => asset && onSelect(asset.id)}>
      <span className="flex min-w-0 items-center gap-1.5 font-medium">
        <Icon className="size-3.5 shrink-0 text-muted" />
        <span className="min-w-0 break-words">{asset ? localizeEntry(index, asset, language()).name : id}</span>
      </span>
      <Tag label={label} tone={kind === "composition" ? "info" : "neutral"} />
    </Button>
  );
}

function AssetLinks({ ids, kind, index, onSelect, empty }: AssetNav & {
  ids: string[]; kind: LinkKind; empty?: string;
}) {
  if (!ids.length) return empty ? <p className="text-muted">{empty}</p> : null;
  return <div className="grid gap-1">{ids.map((id) =>
    <AssetLink key={id} id={id} kind={kind} index={index} onSelect={onSelect} />)}</div>;
}

/** 1. Static structure: slots, directly rendered public components, declared dependencies. */
export function StructureTab({ entry, index, onSelect }: Pick<AssetInspectorProps, "entry" | "index" | "onSelect">) {
  const slots = entry.slots ?? [];
  return <div className="grid content-start text-xs">
    <InspectorSection title={t("Component identity")}>
      <dl className="grid gap-1.5 font-mono text-[11px]">
        {[[t("ID"), entry.id], [t("Owner"), entry.owner], [t("Source"), entry.source],
        ...(entry.type ? [[t("Source type reference"), entry.type]] : [])].map(([label, value]) =>
          <div key={label} className="grid min-w-0 gap-0.5">
            <dt className="text-muted">{label}</dt>
            <dd className="break-all text-foreground">{value}</dd>
          </div>)}
      </dl>
    </InspectorSection>
    <InspectorSection title={t("Composition slots")} actions={<span className="font-mono text-[10px] text-muted">{slots.length}</span>}>
      {slots.length ? <div className="grid gap-1.5">{slots.map((slot) =>
        <div key={slot.name} className="grid gap-1 rounded border border-border bg-background/50 p-2">
          <div className="flex items-center justify-between gap-2">
            <code className="font-semibold">{slot.name}</code>
            <Tag label={slot.optional ? t("Optional") : t("Required")} tone={slot.optional ? "neutral" : "warning"} />
          </div>
          <code className="break-all text-muted">{slot.type}</code>
        </div>)}</div>
        : <p className="text-muted">{t("No dedicated structural slots. Component accepts standard React children or direct props.")}</p>}
    </InspectorSection>
    <InspectorSection title={t("Direct composition")} actions={<span className="font-mono text-[10px] text-muted">{entry.composed?.length ?? 0}</span>}>
      <AssetLinks ids={entry.composed ?? []} kind="composition" index={index} onSelect={onSelect}
        empty={t("No internal catalog components confirmed in template.")} />
    </InspectorSection>
    <InspectorSection title={t("Declared dependencies")} actions={<span className="font-mono text-[10px] text-muted">{entry.dependencies?.length ?? 0}</span>}>
      <AssetLinks ids={entry.dependencies ?? []} kind="declared" index={index} onSelect={onSelect}
        empty={t("No declared catalog dependencies.")} />
    </InspectorSection>
  </div>;
}

/** 2. API for the selected asset; package-wide exports are supplementary only. */
export function ApiTab({ entry, index }: Pick<AssetInspectorProps, "entry" | "index">) {
  const [filter, setFilter] = useState("");
  const [expandedType, setExpandedType] = useState<string>();
  const [showPackageApi, setShowPackageApi] = useState(false);
  const props = entry.props ?? [];
  const found = props.filter((p) => `${p.name} ${p.type}`.toLowerCase().includes(filter.toLowerCase()));
  const packageApi = index.api?.[entry.owner];
  return <div className="grid content-start text-xs">
    {!!entry.exports?.length && <InspectorSection title={t("Public exports")}>
      <div className="flex flex-wrap gap-1">{entry.exports.map((name) =>
        <code key={name} className="rounded border border-border bg-background px-1.5 py-0.5 text-[11px]">{name}</code>)}</div>
    </InspectorSection>}
    <InspectorSection title={t("Props & Interface")} actions={<span className="font-mono text-[10px] text-muted">{props.length}</span>}>
      {props.length > 4 && <div className="relative mb-1">
        <Search className="absolute left-2 top-2 size-3 text-muted" />
        <Input type="search" aria-label={t("Filter props")} placeholder={t("Filter props…")}
          value={filter} onChange={(e) => setFilter(e.target.value)} className="h-7 pl-6 text-xs" />
      </div>}
      {found.length ? <div className="grid gap-1.5">{found.map((prop) =>
        <div key={prop.name} className="grid gap-1 rounded border border-border bg-background/50 p-2">
          <div className="flex flex-wrap items-center gap-1">
            <code className="mr-auto font-semibold">{prop.name}{!prop.optional && <span className="text-danger"> *</span>}</code>
            {prop.isCallback && <Tag label={t("Callback")} tone="info" />}
            {prop.isSlot && <Tag label={t("Slot")} tone="neutral" />}
            <Tag label={prop.optional ? t("Optional") : t("Required")} tone={prop.optional ? "neutral" : "warning"} />
          </div>
          <code className="break-all text-[11px] text-muted">{prop.type}</code>
          {prop.defaultValue !== undefined && <p className="text-[10px] text-muted">{t("Default")}: <code>{prop.defaultValue}</code></p>}
        </div>)}</div>
        : props.length ? <p className="text-muted">{t("No matching props found.")}</p>
          : <div className="grid gap-1 text-muted">
            <p>{t("No dedicated props interface found.")}</p>
            {entry.type && <p>{t("Declared type reference")}: <code>{entry.type}</code></p>}
            <p className="text-[11px]">{t("Props may inherit native HTML element attributes or context.")}</p>
          </div>}
    </InspectorSection>
    {!!entry.relatedTypes?.length && <InspectorSection title={t("Related types")}
      actions={<span className="font-mono text-[10px] text-muted">{entry.relatedTypes.length}</span>}>
      <div className="grid gap-1.5">{entry.relatedTypes.map((type) =>
        <div key={type.name} className="rounded border border-border bg-background/40">
          <Button type="button" variant="row" className="flex w-full items-center justify-between gap-0 rounded-none border-0 p-2 text-left font-mono text-[11px] whitespace-normal hover:bg-row-hover [&_svg]:size-3"
            aria-expanded={expandedType === type.name} onClick={() => setExpandedType(expandedType === type.name ? undefined : type.name)}>
            <span className="font-medium">{type.name}</span>
            <span className="flex items-center gap-1 text-muted">{type.kind}
              {expandedType === type.name ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
            </span>
          </Button>
          {expandedType === type.name && type.signature && <pre className="max-h-40 overflow-auto border-t border-border p-2 font-mono text-[10px] text-muted"><code>{type.signature}</code></pre>}
        </div>)}</div>
    </InspectorSection>}
    {packageApi && <InspectorSection title={t("Owner package API")}
      actions={<Button size="sm" variant="ghost" aria-label={t("Owner package API")}
        onClick={() => setShowPackageApi(!showPackageApi)}>
        {showPackageApi ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
      </Button>}>
      <p className="text-[11px] text-muted">{t("Package export barrel reference; not specific to this component.")}</p>
      <code className="break-all font-mono text-[10px] text-muted">{packageApi.source}</code>
      {showPackageApi && <p className="mt-1 max-h-48 overflow-auto break-all font-mono text-[11px]">{packageApi.symbols.join(" · ")}</p>}
    </InspectorSection>}
  </div>;
}

/** 3. Upstream users and downstream dependencies are different directed edges. */
export function RelationsTab({ entry, index, onSelect }: Pick<AssetInspectorProps, "entry" | "index" | "onSelect">) {
  const composing = index.entries.filter((asset) => asset.composed?.includes(entry.id)).map((asset) => asset.id);
  const referring = index.entries.filter((asset) => asset.dependencies?.includes(entry.id)).map((asset) => asset.id);
  const files = index.consumers?.[entry.id] ?? [];
  return <div className="grid content-start text-xs">
    <InspectorSection title={t("Upstream (Used by)")}
      actions={<span className="font-mono text-[10px] text-muted">{composing.length + referring.length + files.length}</span>}>
      <div className="grid gap-2">
        {!!composing.length && <div className="grid gap-1">
          <span className="font-medium text-muted">{t("Composed by assets")}</span>
          <AssetLinks ids={composing} kind="composition" index={index} onSelect={onSelect} />
        </div>}
        {!!referring.length && <div className="grid gap-1">
          <span className="font-medium text-muted">{t("Referenced by assets")}</span>
          <AssetLinks ids={referring} kind="declared" index={index} onSelect={onSelect} />
        </div>}
        {files.length ? <div className="grid gap-1">
          <div className="flex justify-between font-medium text-muted"><span>{t("Static consumers")}</span><span>{files.length}</span></div>
          <ul className="max-h-48 overflow-auto rounded border border-border bg-background p-1.5 font-mono text-[11px] text-muted">
            {files.map((file) => <li key={file} className="flex items-start gap-1 break-all py-0.5">
              <FileCode className="mt-0.5 size-3 shrink-0" />{file}
            </li>)}
          </ul>
        </div> : <p className="text-muted">{t("No static consumer files found in workspace.")}</p>}
        <p className="text-[10px] text-muted">{t("Analysis covers declared dependencies and static imports across workspace packages. Dynamic imports and external consumers are not tracked.")}</p>
      </div>
    </InspectorSection>
    <InspectorSection title={t("Downstream (Uses)")}
      actions={<span className="font-mono text-[10px] text-muted">{(entry.composed?.length ?? 0) + (entry.dependencies?.length ?? 0)}</span>}>
      <div className="grid gap-2">
        {!!entry.composed?.length && <div className="grid gap-1">
          <span className="font-medium text-muted">{t("Composed internal assets")}</span>
          <AssetLinks ids={entry.composed} kind="composition" index={index} onSelect={onSelect} />
        </div>}
        {!!entry.dependencies?.length && <div className="grid gap-1">
          <span className="font-medium text-muted">{t("Declared dependencies")}</span>
          <AssetLinks ids={entry.dependencies} kind="declared" index={index} onSelect={onSelect} />
        </div>}
        {!entry.composed?.length && !entry.dependencies?.length && <p className="text-muted">{t("No downstream dependencies.")}</p>}
      </div>
    </InspectorSection>
  </div>;
}

/** 4. Reuse the existing import and Builder flow; no new runtime or permissions. */
export function UsageTab({ entry, shown, canBuild, builderOnly, onOpenBuilder }:
  Pick<AssetInspectorProps, "entry" | "shown" | "canBuild" | "builderOnly" | "onOpenBuilder">) {
  const [copied, setCopied] = useState("");
  const snippet = entry.snippet ?? (entry.exports?.length
    ? `import { ${[...new Set(entry.exports.map((name) => name.split(".")[0]))].join(", ")} } from "${entry.owner}";`
    : undefined);
  const copy = async () => {
    if (!snippet) return;
    try { await navigator.clipboard.writeText(snippet); setCopied(t("Copied")); }
    catch { setCopied(t("Clipboard unavailable. Select and copy the code below.")); }
  };
  return <div className="grid content-start text-xs">
    {canBuild && <InspectorSection title={t("Builder integration")}>
      <div className="grid gap-1.5">
        <Button variant="primary" size="sm" disabled={builderOnly} onClick={onOpenBuilder}
          title={builderOnly ? t("Only builders can open Projects.") : undefined}>
          <Hammer />{t(entry.template ? "Create in Builder" : "Open in Builder")}
        </Button>
        <span className="text-[11px] text-muted">{t("Creates or edits a controlled draft; nothing is published automatically.")}</span>
      </div>
    </InspectorSection>}
    {snippet && <InspectorSection title={t("Import snippet")}
      actions={<Button size="sm" variant="ghost" onClick={() => void copy()}><Copy className="size-3" />{t("Copy")}</Button>}>
      <pre className="max-h-48 overflow-auto rounded border border-border bg-background p-2 font-mono text-[11px]"><code>{snippet}</code></pre>
      {copied && <p role="status" className="text-muted">{copied}</p>}
    </InspectorSection>}
    <InspectorSection title={t("Usage recommendations")}>
      <div className="grid gap-2">
        {!!shown.constraints?.length && <div className="grid gap-1">
          <span className="font-medium">{t("Constraints & boundaries")}:</span>
          <ul className="list-disc space-y-1 pl-4 text-muted">{shown.constraints.map((rule) => <li key={rule}>{rule}</li>)}</ul>
        </div>}
        {!!shown.states?.length && <div className="grid gap-1">
          <span className="font-medium">{t("Supported states")}:</span>
          <p className="text-muted">{shown.states.join(" · ")}</p>
        </div>}
        <dl className="grid gap-1 text-muted">
          {[[t("Scope"), entry.scope === "platform" ? t("Platform-wide") : t("Domain-specific")],
          [t("Maturity"), t(entry.maturity)], [t("Authority"), t(entry.authority)]].map(([label, value]) =>
            <div key={label} className="flex justify-between gap-2"><dt>{label}</dt><dd className="text-foreground">{value}</dd></div>)}
        </dl>
      </div>
    </InspectorSection>
  </div>;
}
