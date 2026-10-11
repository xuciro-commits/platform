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

/** Tab 1 — Structure: Internal composition, declared dependencies, and extension slots. */
export function StructureTab({ entry, index, onSelect }: Pick<AssetInspectorProps, "entry" | "index" | "onSelect">) {
  const currentLang = language();
  const slots = entry.slots ?? [];
  const composed = entry.composed ?? [];
  const dependencies = entry.dependencies ?? [];

  return (
    <div className="grid content-start text-xs">
      <InspectorSection title={t("Component identity")}>
        <div className="grid gap-1.5 font-mono text-[11px]">
          <div className="flex items-center justify-between text-muted">
            <span>{t("ID")}</span>
            <code className="text-foreground">{entry.id}</code>
          </div>
          <div className="flex items-center justify-between text-muted">
            <span>{t("Owner")}</span>
            <code className="text-foreground">{entry.owner}</code>
          </div>
          <div className="grid gap-0.5 text-muted">
            <span>{t("Source")}</span>
            <code className="break-all rounded bg-background px-1.5 py-0.5 text-foreground">{entry.source}</code>
          </div>
          {entry.type && (
            <div className="grid gap-0.5 text-muted">
              <span>{t("Source type reference")}</span>
              <code className="break-all rounded bg-background px-1.5 py-0.5 text-foreground">{entry.type}</code>
            </div>
          )}
        </div>
      </InspectorSection>

      <InspectorSection
        title={t("Composition slots")}
        actions={<span className="font-mono text-[10px] text-muted">{slots.length}</span>}
      >
        {slots.length > 0 ? (
          <div className="grid gap-1.5">
            {slots.map((slot) => (
              <div key={slot.name} className="rounded border border-border bg-background/50 p-2">
                <div className="flex items-center justify-between">
                  <span className="font-mono font-semibold text-foreground">{slot.name}</span>
                  <Tag label={slot.optional ? t("Optional") : t("Required")} tone={slot.optional ? "neutral" : "warning"} />
                </div>
                {slot.type && (
                  <code className="mt-1 block break-all text-[11px] text-muted">{slot.type}</code>
                )}
              </div>
            ))}
          </div>
        ) : (
          <p className="text-muted">
            {t("No dedicated structural slots. Component accepts standard React children or direct props.")}
          </p>
        )}
      </InspectorSection>

      <InspectorSection
        title={t("Direct composition")}
        actions={<span className="font-mono text-[10px] text-muted">{composed.length}</span>}
      >
        {composed.length > 0 ? (
          <div className="grid gap-1">
            {composed.map((id) => {
              const target = index.entries.find((e) => e.id === id);
              const label = target ? localizeEntry(index, target, currentLang).name : id;
              return (
                <button
                  key={id}
                  type="button"
                  className="flex items-center justify-between rounded p-1.5 text-left hover:bg-row-hover"
                  onClick={() => onSelect(id)}
                >
                  <span className="flex items-center gap-1.5 font-medium">
                    <Box className="size-3.5 text-muted" />
                    <span>{label}</span>
                  </span>
                  <Tag label={t("Composition")} tone="info" />
                </button>
              );
            })}
          </div>
        ) : (
          <p className="text-muted">{t("No internal catalog components confirmed in template.")}</p>
        )}
      </InspectorSection>

      <InspectorSection
        title={t("Declared dependencies")}
        actions={<span className="font-mono text-[10px] text-muted">{dependencies.length}</span>}
      >
        {dependencies.length > 0 ? (
          <div className="grid gap-1">
            {dependencies.map((id) => {
              const target = index.entries.find((e) => e.id === id);
              const label = target ? localizeEntry(index, target, currentLang).name : id;
              return (
                <button
                  key={id}
                  type="button"
                  className="flex items-center justify-between rounded p-1.5 text-left hover:bg-row-hover"
                  onClick={() => onSelect(id)}
                >
                  <span className="flex items-center gap-1.5 font-medium">
                    <Layers className="size-3.5 text-muted" />
                    <span>{label}</span>
                  </span>
                  <Tag label={t("Declared")} tone="neutral" />
                </button>
              );
            })}
          </div>
        ) : (
          <p className="text-muted">{t("No declared catalog dependencies.")}</p>
        )}
      </InspectorSection>
    </div>
  );
}

/** Tab 2 — API: Component-specific exports, props, callback signatures, and related types. */
export function ApiTab({ entry, index }: Pick<AssetInspectorProps, "entry" | "index">) {
  const [filter, setFilter] = useState("");
  const [expandedType, setExpandedType] = useState<string | null>(null);
  const [showPackageApi, setShowPackageApi] = useState(false);

  const props = entry.props ?? [];
  const relatedTypes = entry.relatedTypes ?? [];
  const packageApi = index.api?.[entry.owner];

  const filteredProps = props.filter((p) => {
    if (!filter) return true;
    const term = filter.toLowerCase();
    return p.name.toLowerCase().includes(term) || (p.type && p.type.toLowerCase().includes(term));
  });

  return (
    <div className="grid content-start text-xs">
      {entry.exports?.length ? (
        <InspectorSection title={t("Public exports")}>
          <div className="flex flex-wrap gap-1">
            {entry.exports.map((exp) => (
              <span key={exp} className="rounded bg-background px-1.5 py-0.5 font-mono text-[11px] text-foreground border border-border">
                {exp}
              </span>
            ))}
          </div>
        </InspectorSection>
      ) : null}

      <InspectorSection
        title={t("Props & Interface")}
        actions={<span className="font-mono text-[10px] text-muted">{props.length}</span>}
      >
        {props.length > 4 && (
          <div className="relative mb-1">
            <Search className="absolute left-2 top-2 size-3 text-muted" />
            <Input
              type="search"
              aria-label={t("Filter props")}
              placeholder={t("Filter props…")}
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="h-7 pl-6 text-xs"
            />
          </div>
        )}

        {filteredProps.length > 0 ? (
          <div className="grid gap-2">
            {filteredProps.map((p) => (
              <div key={p.name} className="grid gap-1 rounded border border-border bg-background/50 p-2">
                <div className="flex items-center justify-between gap-1">
                  <div className="flex items-center gap-1 font-mono font-semibold text-foreground">
                    <span>{p.name}</span>
                    {!p.optional && <span className="text-danger" title={t("Required")}>*</span>}
                  </div>
                  <div className="flex items-center gap-1">
                    {p.isCallback && <Tag label={t("Callback")} tone="info" />}
                    {p.isSlot && <Tag label={t("Slot")} tone="neutral" />}
                    <Tag label={p.optional ? t("Optional") : t("Required")} tone={p.optional ? "neutral" : "warning"} />
                  </div>
                </div>
                <div className="font-mono text-[11px] text-muted break-all">
                  <code>{p.type}</code>
                </div>
                {p.defaultValue && (
                  <div className="flex items-center gap-1 text-[10px] text-muted">
                    <span>{t("Default")}:</span>
                    <code className="rounded bg-surface px-1 text-foreground">{p.defaultValue}</code>
                  </div>
                )}
              </div>
            ))}
          </div>
        ) : props.length > 0 ? (
          <p className="text-muted">{t("No matching props found.")}</p>
        ) : (
          <div className="grid gap-1 text-muted">
            <p>{t("No dedicated props interface found.")}</p>
            {entry.type && (
              <p>
                {t("Declared type reference")}: <code className="text-foreground">{entry.type}</code>
              </p>
            )}
            <p className="text-[11px]">{t("Props may inherit native HTML element attributes or context.")}</p>
          </div>
        )}
      </InspectorSection>

      {relatedTypes.length > 0 && (
        <InspectorSection
          title={t("Related types")}
          actions={<span className="font-mono text-[10px] text-muted">{relatedTypes.length}</span>}
        >
          <div className="grid gap-1.5">
            {relatedTypes.map((rt) => {
              const isOpen = expandedType === rt.name;
              return (
                <div key={rt.name} className="rounded border border-border bg-background/40">
                  <button
                    type="button"
                    className="flex w-full items-center justify-between p-2 text-left font-mono text-[11px] hover:bg-row-hover"
                    onClick={() => setExpandedType(isOpen ? null : rt.name)}
                  >
                    <span className="font-medium text-foreground">{rt.name}</span>
                    <span className="flex items-center gap-1 text-muted">
                      <span>{rt.kind}</span>
                      {isOpen ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                    </span>
                  </button>
                  {isOpen && rt.signature && (
                    <div className="border-t border-border p-2">
                      <pre className="max-h-40 overflow-auto font-mono text-[10px] text-muted">
                        <code>{rt.signature}</code>
                      </pre>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        </InspectorSection>
      )}

      {packageApi && (
        <InspectorSection
          title={t("Owner package API")}
          actions={
            <Button size="sm" variant="ghost" onClick={() => setShowPackageApi(!showPackageApi)}>
              {showPackageApi ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
            </Button>
          }
        >
          <div className="grid gap-1">
            <span className="text-[11px] text-muted">
              {t("Package export barrel reference; not specific to this component.")}
            </span>
            <code className="break-all font-mono text-[10px] text-muted">{packageApi.source}</code>
            {showPackageApi && (
              <p className="mt-1 max-h-48 overflow-auto break-all font-mono text-[11px] text-foreground">
                {packageApi.symbols.join(" · ")}
              </p>
            )}
          </div>
        </InspectorSection>
      )}
    </div>
  );
}

/** Tab 3 — Relations: Upstream callers/consumers and downstream composed dependencies. */
export function RelationsTab({ entry, index, onSelect }: Pick<AssetInspectorProps, "entry" | "index" | "onSelect">) {
  const currentLang = language();

  // Upstream: assets that depend on this entry or compose it
  const dependentAssets = index.entries.filter((e) => e.dependencies?.includes(entry.id));
  const composingAssets = index.entries.filter((e) => e.composed?.includes(entry.id));
  const consumerFiles = index.consumers?.[entry.id] ?? [];

  // Downstream: assets this entry depends on or composes
  const dependencies = entry.dependencies ?? [];
  const composed = entry.composed ?? [];

  return (
    <div className="grid content-start text-xs">
      <InspectorSection
        title={t("Upstream (Used by)")}
        actions={
          <span className="font-mono text-[10px] text-muted">
            {dependentAssets.length + composingAssets.length + consumerFiles.length}
          </span>
        }
      >
        <div className="grid gap-2">
          {composingAssets.length > 0 && (
            <div className="grid gap-1">
              <span className="text-[11px] font-medium text-muted">{t("Composed by assets")}</span>
              {composingAssets.map((asset) => (
                <button
                  key={`comp-${asset.id}`}
                  type="button"
                  className="flex items-center justify-between rounded p-1.5 text-left hover:bg-row-hover"
                  onClick={() => onSelect(asset.id)}
                >
                  <span className="flex items-center gap-1.5 font-medium">
                    <Box className="size-3.5 text-muted" />
                    <span>{localizeEntry(index, asset, currentLang).name}</span>
                  </span>
                  <Tag label={t("Composition")} tone="info" />
                </button>
              ))}
            </div>
          )}

          {dependentAssets.length > 0 && (
            <div className="grid gap-1">
              <span className="text-[11px] font-medium text-muted">{t("Referenced by assets")}</span>
              {dependentAssets.map((asset) => (
                <button
                  key={`dep-${asset.id}`}
                  type="button"
                  className="flex items-center justify-between rounded p-1.5 text-left hover:bg-row-hover"
                  onClick={() => onSelect(asset.id)}
                >
                  <span className="flex items-center gap-1.5 font-medium">
                    <Layers className="size-3.5 text-muted" />
                    <span>{localizeEntry(index, asset, currentLang).name}</span>
                  </span>
                  <Tag label={t("Declared")} tone="neutral" />
                </button>
              ))}
            </div>
          )}

          {consumerFiles.length > 0 ? (
            <div className="grid gap-1">
              <div className="flex items-center justify-between text-[11px] font-medium text-muted">
                <span>{t("Static consumers")}</span>
                <span className="font-mono text-[10px]">{consumerFiles.length}</span>
              </div>
              <ul className="max-h-48 overflow-auto rounded border border-border bg-background p-1.5 text-[11px] font-mono text-muted">
                {consumerFiles.map((file) => (
                  <li key={file} className="flex items-center gap-1 break-all py-0.5">
                    <FileCode className="size-3 shrink-0 text-muted" />
                    <span>{file}</span>
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <p className="text-muted">{t("No static consumer files found in workspace.")}</p>
          )}

          <p className="text-[10px] text-muted">
            {t("Analysis covers declared dependencies and static imports across workspace packages. Dynamic imports and external consumers are not tracked.")}
          </p>
        </div>
      </InspectorSection>

      <InspectorSection
        title={t("Downstream (Uses)")}
        actions={<span className="font-mono text-[10px] text-muted">{dependencies.length + composed.length}</span>}
      >
        <div className="grid gap-2">
          {composed.length > 0 && (
            <div className="grid gap-1">
              <span className="text-[11px] font-medium text-muted">{t("Composed internal assets")}</span>
              {composed.map((id) => {
                const target = index.entries.find((e) => e.id === id);
                const label = target ? localizeEntry(index, target, currentLang).name : id;
                return (
                  <button
                    key={`down-comp-${id}`}
                    type="button"
                    className="flex items-center justify-between rounded p-1.5 text-left hover:bg-row-hover"
                    onClick={() => onSelect(id)}
                  >
                    <span className="flex items-center gap-1.5 font-medium">
                      <Box className="size-3.5 text-muted" />
                      <span>{label}</span>
                    </span>
                    <Tag label={t("Composition")} tone="info" />
                  </button>
                );
              })}
            </div>
          )}

          {dependencies.length > 0 && (
            <div className="grid gap-1">
              <span className="text-[11px] font-medium text-muted">{t("Declared dependencies")}</span>
              {dependencies.map((id) => {
                const target = index.entries.find((e) => e.id === id);
                const label = target ? localizeEntry(index, target, currentLang).name : id;
                return (
                  <button
                    key={`down-dep-${id}`}
                    type="button"
                    className="flex items-center justify-between rounded p-1.5 text-left hover:bg-row-hover"
                    onClick={() => onSelect(id)}
                  >
                    <span className="flex items-center gap-1.5 font-medium">
                      <Layers className="size-3.5 text-muted" />
                      <span>{label}</span>
                    </span>
                    <Tag label={t("Declared")} tone="neutral" />
                  </button>
                );
              })}
            </div>
          )}

          {composed.length === 0 && dependencies.length === 0 && (
            <p className="text-muted">{t("No downstream dependencies.")}</p>
          )}
        </div>
      </InspectorSection>
    </div>
  );
}

/** Tab 4 — Usage: Import snippet, Studio/Builder links, guidelines, and runtime states. */
export function UsageTab({
  entry,
  shown,
  canBuild,
  builderOnly,
  onOpenBuilder,
}: Pick<AssetInspectorProps, "entry" | "shown" | "canBuild" | "builderOnly" | "onOpenBuilder">) {
  const [copied, setCopied] = useState("");

  const snippet =
    entry.snippet ??
    (entry.exports?.length
      ? `import { ${[...new Set(entry.exports.map((name) => name.split(".")[0]))].join(", ")} } from "${entry.owner}";`
      : undefined);

  const copy = async (text: string, label: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(label);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      setCopied(t("Clipboard unavailable. Select and copy the code below."));
    }
  };

  return (
    <div className="grid content-start text-xs">
      {canBuild && (
        <InspectorSection title={t("Builder integration")}>
          <div className="grid gap-1.5">
            <Button
              variant="primary"
              size="sm"
              disabled={builderOnly}
              title={builderOnly ? t("Only builders can open Projects.") : undefined}
              onClick={onOpenBuilder}
            >
              <Hammer />
              {t(entry.template ? "Create in Builder" : "Open in Builder")}
            </Button>
            <span className="text-[11px] text-muted">
              {t("Creates or edits a controlled draft; nothing is published automatically.")}
            </span>
          </div>
        </InspectorSection>
      )}

      {snippet && (
        <InspectorSection
          title={t("Import snippet")}
          actions={
            <Button size="sm" variant="ghost" onClick={() => void copy(snippet, t("Copied"))}>
              <Copy className="size-3" />
              {t("Copy")}
            </Button>
          }
        >
          <div className="grid gap-1">
            <pre className="max-h-48 overflow-auto rounded border border-border bg-background p-2 font-mono text-[11px]">
              <code>{snippet}</code>
            </pre>
            {copied && <p role="status" className="text-muted">{copied}</p>}
          </div>
        </InspectorSection>
      )}

      <InspectorSection title={t("Usage recommendations")}>
        <div className="grid gap-2">
          {shown.constraints?.length ? (
            <div className="grid gap-1">
              <span className="font-medium text-foreground">{t("Constraints & boundaries")}:</span>
              <ul className="list-disc space-y-1 pl-4 text-muted">
                {shown.constraints.map((c) => (
                  <li key={c}>{c}</li>
                ))}
              </ul>
            </div>
          ) : null}

          {shown.states?.length ? (
            <div className="grid gap-1">
              <span className="font-medium text-foreground">{t("Supported states")}:</span>
              <p className="text-muted">{shown.states.join(" · ")}</p>
            </div>
          ) : null}

          <div className="grid gap-1 text-muted">
            <div className="flex items-center justify-between">
              <span>{t("Scope")}</span>
              <span className="text-foreground">{entry.scope === "platform" ? t("Platform-wide") : t("Domain-specific")}</span>
            </div>
            <div className="flex items-center justify-between">
              <span>{t("Maturity")}</span>
              <span className="text-foreground">{t(entry.maturity)}</span>
            </div>
            <div className="flex items-center justify-between">
              <span>{t("Authority")}</span>
              <span className="text-foreground">{t(entry.authority)}</span>
            </div>
          </div>
        </div>
      </InspectorSection>
    </div>
  );
}
