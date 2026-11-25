import { Banner } from "@astryxdesign/core/Banner";
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
import { navigate, useSearch } from "../../router";
import { reasonOf } from "../../pages/act";
import { PageTitle, useGutter } from "../../pages/frame";
import { CatalogEntryCard, PackageCard, compareVersions, kindLabel, kindPlace, packageKinds, listingLabel } from "../../pages/PackageCard";
import { type InitialPackageSource, PackageImportWizard } from "../../pages/PackageImportWizard";
import { PackageLibraryManager } from "../../pages/PackageLibraryManager";
import { PackageReview } from "../../pages/PackageReview";
import { JUST_INSTALLED_KEY } from "./installations";

type Tab = "all" | "available" | "updates" | "installed";
type LocalCard = { view: Api.PackageView; otherVersions: number };

const LIBRARY_TTL_MS = 5 * 60 * 1000;

const styles = stylex.create({
  surface: { backgroundColor: colorVars["--color-background-surface"] },
});

function libraryNeedsRefresh(lib: Api.PackageLibraryView) {
  if (lib.status === "pending" || lib.status === "stale") return true;
  if (lib.status !== "ready") return false;
  const fetched = Date.parse(lib.fetchedAt ?? "");
  return !Number.isFinite(fetched) || Date.now() - fetched > LIBRARY_TTL_MS;
}

function rankLocalVersion(view: Api.PackageView): number {
  if (view.update) return 3;
  if (view.installed && view.installed.state !== "retired" && view.installed.version === view.descriptor.version) return 2;
  return 1;
}

function collapseLocalVersions(list: Api.PackageView[], showAll: boolean): LocalCard[] {
  if (showAll) return list.map((view) => ({ view, otherVersions: 0 }));
  const groups = new Map<string, Api.PackageView[]>();
  for (const view of list) {
    const key = `${view.source}:${view.descriptor.id}`;
    const bucket = groups.get(key) ?? [];
    bucket.push(view);
    groups.set(key, bucket);
  }
  const out: LocalCard[] = [];
  for (const bucket of groups.values()) {
    bucket.sort((a, b) => rankLocalVersion(b) - rankLocalVersion(a) || compareVersions(b.descriptor.version, a.descriptor.version));
    out.push({ view: bucket[0], otherVersions: bucket.length - 1 });
  }
  return out;
}

function finishInstall(installedIds?: string[]) {
  try {
    if (installedIds?.length) sessionStorage.setItem(JUST_INSTALLED_KEY, JSON.stringify(installedIds));
    else sessionStorage.removeItem(JUST_INSTALLED_KEY);
  } catch {}
  navigate("/delivery/installations");
}

export default function Marketplace() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const libraries = useRead<Api.PackageLibraryView[]>("/v1/packages/libraries");
  const [selected, setSelected] = useState<{ view: Api.PackageView; mode: "install" | "manage" }>();
  const [importing, setImporting] = useState(false);
  const [importSource, setImportSource] = useState<InitialPackageSource>();
  const [managingLibraries, setManagingLibraries] = useState(false);
  const [showAllVersions, setShowAllVersions] = useState(false);
  const [tab, setTab] = useState<Tab>("all");
  const [source, setSource] = useState("all");
  const [industry, setIndustry] = useState("all");
  const [domain, setDomain] = useState("all");
  const initialSearch = useSearch();
  const [kind, setKind] = useState(() => new URLSearchParams(initialSearch).get("kind") || "all");
  const [stage, setStage] = useState("released");
  const [query, setQuery] = useState("");
  const gutter = useGutter();
  const rows = packages.status === "ready" ? packages.data : [];
  const autoSynced = useRef(false);
  const [syncFailure, setSyncFailure] = useState("");
  const libraryName = (lib: Api.PackageLibraryView) => (lib.builtin ? t("Official library") : (lib.title || lib.url));

  const refreshLibraries = () => {
    setSyncFailure("");
    void request<Api.PackageLibraryView[]>("/v1/packages/libraries/refresh", { body: {} })
      .then(() => reread())
      .catch((problem) => setSyncFailure(reasonOf(problem)));
  };

  useEffect(() => {
    if (autoSynced.current || libraries.status !== "ready") return;
    if (!libraries.data.some(libraryNeedsRefresh)) return;
    autoSynced.current = true;
    refreshLibraries();
  }, [libraries]);

  const installedById = useMemo(() => {
    const map = new Map<string, Api.InstalledPackage>();
    for (const view of rows) {
      if (view.installed && view.installed.state !== "retired") {
        map.set(view.descriptor.id, view.installed);
      }
    }
    return map;
  }, [rows]);

  const remoteEntries = useMemo(() => {
    if (packages.status !== "ready" || libraries.status !== "ready") return [];
    const existing = new Set(rows.map((view) => `${view.descriptor.id}@${view.descriptor.version}`));
    const seen = new Set<string>();
    const out: { entry: Api.PackageCatalogEntry; libraryTitle: string; official: boolean }[] = [];
    for (const lib of libraries.data) {
      for (const entry of lib.entries ?? []) {
        const key = `${entry.id}@${entry.version}`;
        if (existing.has(key) || seen.has(key)) continue;
        seen.add(key);
        out.push({ entry, libraryTitle: libraryName(lib), official: Boolean(lib.builtin) });
      }
    }
    return out;
  }, [libraries, packages.status, rows]);

  const facets = useMemo(() => {
    const industries = new Set<string>();
    const domains = new Set<string>();
    for (const { entry } of remoteEntries) {
      entry.listing?.industries?.forEach((value) => industries.add(value));
      entry.listing?.domains?.forEach((value) => domains.add(value));
    }
    return { industries: [...industries].sort(), domains: [...domains].sort() };
  }, [remoteEntries]);
  const classified = industry !== "all" || domain !== "all" || stage === "test";

  const isLocalInstalled = (view: Api.PackageView) =>
    Boolean(view.installed && view.installed.state !== "retired" && view.installed.version === view.descriptor.version);
  const isLocalUpdate = (view: Api.PackageView) => Boolean(view.update);
  const isLocalAvailable = (view: Api.PackageView) => !isLocalInstalled(view) && !isLocalUpdate(view);

  const isRemoteUpdate = (entry: Api.PackageCatalogEntry) => {
    const installed = installedById.get(entry.id);
    return Boolean(installed && compareVersions(entry.version, installed.version) > 0);
  };
  const isRemoteAvailable = (entry: Api.PackageCatalogEntry) => !installedById.has(entry.id);

  const baseLocalRaw = useMemo(() => {
    const term = query.trim().toLowerCase();
    return rows.filter((view) => {
      if (source !== "all" && view.source !== source) return false;
      if (classified) return false;
      if (kind !== "all" && view.kind !== kind) return false;
      if (!term) return true;
      return `${view.descriptor.title ?? ""} ${view.descriptor.id} ${view.descriptor.version} ${view.descriptor.description ?? ""}`.toLowerCase().includes(term);
    });
  }, [rows, source, query, classified, kind]);

  const baseLocal = useMemo(() => collapseLocalVersions(baseLocalRaw, showAllVersions), [baseLocalRaw, showAllVersions]);

  const baseRemote = useMemo(() => {
    if (source !== "all" && source !== "official" && source !== "third-party") return [];
    const term = query.trim().toLowerCase();
    return remoteEntries.filter(({ entry, libraryTitle, official }) => {
      if (source === "official" && !official) return false;
      if (source === "third-party" && official) return false;
      const listing = entry.listing;
      if (stage === "released" && listing?.stage === "test") return false;
      if (stage === "test" && listing?.stage !== "test") return false;
      if (industry !== "all" && !listing?.industries?.includes(industry)) return false;
      if (domain !== "all" && !listing?.domains?.includes(domain)) return false;
      if (kind !== "all" && entry.kind !== kind) return false;
      if (!term) return true;
      return `${entry.title} ${entry.id} ${entry.version} ${entry.description ?? ""} ${libraryTitle} ${(listing?.keywords ?? []).join(" ")}`.toLowerCase().includes(term);
    });
  }, [remoteEntries, source, query, industry, domain, kind, stage]);

  const counts = useMemo(() => ({
    all: baseLocal.length + baseRemote.length,
    available: baseLocal.filter(({ view }) => isLocalAvailable(view)).length + baseRemote.filter(({ entry }) => isRemoteAvailable(entry)).length,
    updates: baseLocal.filter(({ view }) => isLocalUpdate(view)).length + baseRemote.filter(({ entry }) => isRemoteUpdate(entry)).length,
    installed: baseLocal.filter(({ view }) => isLocalInstalled(view)).length,
  }), [baseLocal, baseRemote, installedById]);

  const filtered = useMemo(() => {
    if (tab === "available") return baseLocal.filter(({ view }) => isLocalAvailable(view));
    if (tab === "updates") return baseLocal.filter(({ view }) => isLocalUpdate(view));
    if (tab === "installed") return baseLocal.filter(({ view }) => isLocalInstalled(view));
    return baseLocal;
  }, [baseLocal, tab]);

  const filteredRemote = useMemo(() => {
    if (tab === "installed") return [];
    if (tab === "available") return baseRemote.filter(({ entry }) => isRemoteAvailable(entry));
    if (tab === "updates") return baseRemote.filter(({ entry }) => isRemoteUpdate(entry));
    return baseRemote;
  }, [baseRemote, tab, installedById]);

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
  const failedLibraries = libraries.status === "ready" ? libraries.data.filter((lib) => lib.status === "error") : [];
  const libraryProblem = syncFailure || failedLibraries.map((lib) => (lib.error ? `${libraryName(lib)}: ${lib.error}` : libraryName(lib))).join(" · ");
  const hasCollapsedVersions = baseLocalRaw.length > baseLocal.length;

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
                      { id: "refresh-libraries", label: t("Refresh all"), onClick: refreshLibraries },
                      ...(hasCollapsedVersions || showAllVersions
                        ? [{ id: "toggle-versions", label: showAllVersions ? t("Group versions by package") : t("Show all versions"), onClick: () => setShowAllVersions((prev) => !prev) }]
                        : []),
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
                  <SegmentedControlItem value="available" label={`${t("Available")} ${counts.available}`} />
                  <SegmentedControlItem value="updates" label={`${t("Updates")} ${counts.updates}`} />
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
                    label={t("Kind filter")}
                    isLabelHidden
                    value={kind}
                    onChange={setKind}
                    options={[{ value: "all", label: t("All kinds") }, ...packageKinds.map((value) => ({ value, label: kindLabel(value) }))]}
                  />
                  <Selector
                    label={t("Industry filter")}
                    isLabelHidden
                    value={industry}
                    onChange={setIndustry}
                    options={[{ value: "all", label: t("All industries") }, ...facets.industries.map((value) => ({ value, label: listingLabel(value) }))]}
                  />
                  <Selector
                    label={t("Domain filter")}
                    isLabelHidden
                    value={domain}
                    onChange={setDomain}
                    options={[{ value: "all", label: t("All domains") }, ...facets.domains.map((value) => ({ value, label: listingLabel(value) }))]}
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
              {libraryProblem && (
                <Banner status="warning" title={t("Some package libraries could not be synced. Retry from Manage library.")} description={libraryProblem} />
              )}
              {kind !== "all" && kindPlace(kind) && (
                <Banner status="info" title={kindLabel(kind)} description={kindPlace(kind)} />
              )}
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
              {packages.status !== "loading" && totalVisible > 0 && (
                <Grid columns={{ minWidth: 320, max: 3 }} gap={4} role="list">
                  {filtered.map(({ view, otherVersions }) => (
                    <PackageCard
                      key={`${view.descriptor.id}@${view.descriptor.version}`}
                      view={view}
                      otherVersions={otherVersions}
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
                      installed={installedById.get(entry.id)}
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
          done={(installedIds) => {
            setSelected(undefined);
            finishInstall(installedIds);
          }}
        />
      )}
      {importing && (
        <PackageImportWizard
          initialSource={importSource}
          onClose={() => { setImporting(false); setImportSource(undefined); }}
          onDone={(installedIds) => {
            setImporting(false);
            setImportSource(undefined);
            finishInstall(installedIds);
          }}
        />
      )}
      {managingLibraries && (
        <PackageLibraryManager onClose={() => { setManagingLibraries(false); setSyncFailure(""); }} />
      )}
    </>
  );
}
