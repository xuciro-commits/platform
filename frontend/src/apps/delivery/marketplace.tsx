import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Grid } from "@astryxdesign/core/Grid";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useMemo, useRef, useState } from "react";
import { type Api, request, reread, useRead } from "../../host";
import { t } from "../../i18n";
import { navigate } from "../../router";
import { PageTitle, useGutter } from "../../pages/frame";
import { CatalogEntryCard, PackageCard, listingLabel, packageCounts } from "../../pages/PackageCard";
import { type InitialPackageSource, PackageImportWizard } from "../../pages/PackageImportWizard";
import { PackageLibraryManager } from "../../pages/PackageLibraryManager";
import { PackageReview } from "../../pages/PackageReview";

type Tab = "all" | "apps" | "plugins" | "installed";

const styles = stylex.create({
  surface: { backgroundColor: colorVars["--color-background-surface"] },
});

export default function Marketplace() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const libraries = useRead<Api.PackageLibraryView[]>("/v1/packages/libraries");
  const [selected, setSelected] = useState<{ view: Api.PackageView; mode: "install" | "manage" }>();
  const [importing, setImporting] = useState(false);
  const [importSource, setImportSource] = useState<InitialPackageSource>();
  const [managingLibraries, setManagingLibraries] = useState(false);
  const [tab, setTab] = useState<Tab>("all");
  const [source, setSource] = useState("all");
  const [industry, setIndustry] = useState("all");
  const [capability, setCapability] = useState("all");
  const [stage, setStage] = useState("released");
  const [query, setQuery] = useState("");
  const gutter = useGutter();
  const rows = packages.status === "ready" ? packages.data : [];
  const autoSynced = useRef(false);

  useEffect(() => {
    if (autoSynced.current || libraries.status !== "ready") return;
    if (!libraries.data.some((lib) => lib.status === "pending")) return;
    autoSynced.current = true;
    void request<Api.PackageLibraryView[]>("/v1/packages/libraries/refresh", { body: {} }).then(() => reread()).catch(() => {});
  }, [libraries]);

  const remoteEntries = useMemo(() => {
    if (libraries.status !== "ready") return [];
    const existing = new Set(rows.map((view) => `${view.descriptor.id}@${view.descriptor.version}`));
    const seen = new Set<string>();
    const out: { entry: Api.PackageCatalogEntry; libraryTitle: string; official: boolean }[] = [];
    for (const lib of libraries.data) {
      for (const entry of lib.entries ?? []) {
        const key = `${entry.id}@${entry.version}`;
        if (existing.has(key) || seen.has(key)) continue;
        seen.add(key);
        out.push({ entry, libraryTitle: lib.builtin ? t("Official library") : (lib.title || lib.url), official: Boolean(lib.builtin) });
      }
    }
    return out;
  }, [libraries, rows]);

  const facets = useMemo(() => {
    const industries = new Set<string>();
    const capabilities = new Set<string>();
    for (const { entry } of remoteEntries) {
      entry.listing?.industries?.forEach((value) => industries.add(value));
      entry.listing?.capabilities?.forEach((value) => capabilities.add(value));
    }
    return { industries: [...industries].sort(), capabilities: [...capabilities].sort() };
  }, [remoteEntries]);
  const classified = industry !== "all" || capability !== "all" || stage === "test";

  const counts = useMemo(() => ({
    all: rows.length + remoteEntries.length,
    apps: rows.filter((view) => packageCounts(view).apps > 0).length,
    plugins: rows.filter((view) => packageCounts(view).plugins > 0).length,
    installed: rows.filter((view) => Boolean(view.installed && view.installed.state !== "retired")).length,
  }), [rows, remoteEntries]);

  const filtered = useMemo(() => {
    const term = query.trim().toLowerCase();
    return rows.filter((view) => {
      if (tab === "apps" && packageCounts(view).apps === 0) return false;
      if (tab === "plugins" && packageCounts(view).plugins === 0) return false;
      if (tab === "installed" && (!view.installed || view.installed.state === "retired")) return false;
      if (source !== "all" && view.source !== source) return false;
      if (classified) return false;
      if (!term) return true;
      return `${view.descriptor.title ?? ""} ${view.descriptor.id} ${view.descriptor.version} ${view.descriptor.description ?? ""}`.toLowerCase().includes(term);
    });
  }, [rows, tab, source, query, classified]);

  const filteredRemote = useMemo(() => {
    if (tab !== "all") return [];
    if (source !== "all" && source !== "official" && source !== "third-party") return [];
    const term = query.trim().toLowerCase();
    return remoteEntries.filter(({ entry, libraryTitle, official }) => {
      if (source === "official" && !official) return false;
      if (source === "third-party" && official) return false;
      const listing = entry.listing;
      if (stage === "released" && listing?.stage === "test") return false;
      if (stage === "test" && listing?.stage !== "test") return false;
      if (industry !== "all" && !listing?.industries?.includes(industry)) return false;
      if (capability !== "all" && !listing?.capabilities?.includes(capability)) return false;
      if (!term) return true;
      return `${entry.title} ${entry.id} ${entry.version} ${entry.description ?? ""} ${libraryTitle} ${(listing?.keywords ?? []).join(" ")}`.toLowerCase().includes(term);
    });
  }, [remoteEntries, tab, source, query, industry, capability, stage]);

  const modeOf = (view: Api.PackageView): "install" | "manage" => {
    const retired = view.installed?.state === "retired";
    const current = Boolean(view.installed && !retired && view.installed.version === view.descriptor.version && !view.update && !view.downgrade && !view.rollback);
    return current ? "manage" : "install";
  };

  const openRemoteImport = (entry: Api.PackageCatalogEntry) => {
    setImportSource({ url: entry.url, sha256: entry.sha256 });
    setImporting(true);
  };

  const totalVisible = filtered.length + filteredRemote.length;

  return (
    <>
      <Layout
        height="fill"
        xstyle={styles.surface}
        header={
          <LayoutHeader padding={gutter} hasDivider>
            <VStack gap={4}>
              <PageTitle
                title={t("Marketplace")}
                actions={
                  <MoreMenu
                    label={t("More actions")}
                    alignment="end"
                    items={[
                      { id: "import-package", label: t("Install from URL or file"), onClick: () => { setImportSource(undefined); setImporting(true); } },
                      { id: "manage-library", label: t("Manage library"), onClick: () => setManagingLibraries(true) },
                    ]}
                  />
                }
              >
                <Text type="body" color="secondary">{t("Review versions, dependencies and their installation scope before adding them to this workspace.")}</Text>
              </PageTitle>
              <HStack gap={3} vAlign="center" wrap="wrap">
                <SegmentedControl size="sm" label={t("Filter")} value={tab} onChange={(next) => setTab(next as Tab)}>
                  <SegmentedControlItem value="all" label={`${t("All")} ${counts.all}`} />
                  <SegmentedControlItem value="apps" label={`${t("Apps only")} ${counts.apps}`} />
                  <SegmentedControlItem value="plugins" label={`${t("Plugins")} ${counts.plugins}`} />
                  <SegmentedControlItem value="installed" label={`${t("Installed")} ${counts.installed}`} />
                </SegmentedControl>
                <StackItem size="fill" />
                <HStack gap={2} vAlign="center" wrap="wrap">
                  <Selector
                    label={t("Source filter")}
                    isLabelHidden
                    value={source}
                    onChange={setSource}
                    options={[
                      { value: "all", label: t("All sources") },
                      { value: "index", label: t("Built-in index") },
                      { value: "official", label: t("Official library") },
                      { value: "third-party", label: t("Third-party library") },
                      { value: "product", label: t("Product version") },
                      { value: "imported", label: t("Local library") },
                      { value: "installed", label: t("Installed") },
                    ]}
                  />
                  <Selector
                    label={t("Industry filter")}
                    isLabelHidden
                    value={industry}
                    onChange={setIndustry}
                    options={[{ value: "all", label: t("All industries") }, ...facets.industries.map((value) => ({ value, label: listingLabel(value) }))]}
                  />
                  <Selector
                    label={t("Capability filter")}
                    isLabelHidden
                    value={capability}
                    onChange={setCapability}
                    options={[{ value: "all", label: t("All capabilities") }, ...facets.capabilities.map((value) => ({ value, label: listingLabel(value) }))]}
                  />
                  <Selector
                    label={t("Stage filter")}
                    isLabelHidden
                    value={stage}
                    onChange={setStage}
                    options={[
                      { value: "released", label: t("Released packages") },
                      { value: "test", label: t("Test packages") },
                      { value: "all", label: t("All stages") },
                    ]}
                  />
                  <TextInput
                    label={t("Search")}
                    isLabelHidden
                    placeholder={t("Search packages…")}
                    value={query}
                    onChange={setQuery}
                  />
                </HStack>
              </HStack>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={5}>
              {packages.status === "loading" && (
                <Grid columns={{ minWidth: 320, max: 3 }} gap={4}>
                  {[0, 1, 2].map((index) => <Skeleton key={index} height={156} index={index} />)}
                </Grid>
              )}
              {packages.status !== "loading" && totalVisible === 0 && (
                <EmptyState
                  headingLevel={2}
                  title={counts.all === 0 ? t("No package versions are available.") : t("No matches")}
                  description={counts.all === 0 ? t("Review versions, dependencies and their installation scope before adding them to this workspace.") : t("Try a different search.")}
                />
              )}
              {totalVisible > 0 && (
                <Grid columns={{ minWidth: 320, max: 3 }} gap={4} role="list">
                  {filtered.map((view) => (
                    <PackageCard
                      key={`${view.descriptor.id}@${view.descriptor.version}`}
                      view={view}
                      onInspect={() => setSelected({ view, mode: modeOf(view) })}
                      onAction={() => setSelected({ view, mode: modeOf(view) })}
                    />
                  ))}
                  {filteredRemote.map(({ entry, libraryTitle, official }) => (
                    <CatalogEntryCard
                      key={`remote:${entry.id}@${entry.version}`}
                      entry={entry}
                      libraryTitle={libraryTitle}
                      official={official}
                      onSelect={() => openRemoteImport(entry)}
                    />
                  ))}
                </Grid>
              )}
              <Text type="supporting" color="secondary">
                {t("Packages are immutable declarative artifacts. No package code runs on the server, and installing never grants data or action permissions.")}
              </Text>
            </VStack>
          </LayoutContent>
        }
      />
      {selected && (
        <PackageReview
          view={selected.view}
          mode={selected.mode}
          close={() => setSelected(undefined)}
          done={() => {
            setSelected(undefined);
            navigate("/delivery/installations");
          }}
        />
      )}
      {importing && (
        <PackageImportWizard
          initialSource={importSource}
          onClose={() => { setImporting(false); setImportSource(undefined); }}
          onDone={() => {
            setImporting(false);
            setImportSource(undefined);
            navigate("/delivery/installations");
          }}
        />
      )}
      {managingLibraries && (
        <PackageLibraryManager onClose={() => setManagingLibraries(false)} />
      )}
    </>
  );
}
