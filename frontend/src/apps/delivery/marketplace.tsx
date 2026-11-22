import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Grid } from "@astryxdesign/core/Grid";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useMemo, useState } from "react";
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";
import { navigate } from "../../router";
import { PageTitle, useGutter } from "../../pages/frame";
import { PackageCard, packageCounts } from "../../pages/PackageCard";
import { PackageReview } from "../../pages/PackageReview";

type Tab = "all" | "apps" | "plugins" | "installed";

export default function Marketplace() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const [selected, setSelected] = useState<{ view: Api.PackageView; mode: "install" | "manage" }>();
  const [tab, setTab] = useState<Tab>("all");
  const [source, setSource] = useState("all");
  const [query, setQuery] = useState("");
  const gutter = useGutter();
  const rows = packages.status === "ready" ? packages.data : [];

  const counts = useMemo(() => ({
    all: rows.length,
    apps: rows.filter((view) => packageCounts(view).apps > 0).length,
    plugins: rows.filter((view) => packageCounts(view).plugins > 0).length,
    installed: rows.filter((view) => Boolean(view.installed && view.installed.state !== "retired")).length,
  }), [rows]);

  const filtered = useMemo(() => {
    const term = query.trim().toLowerCase();
    return rows.filter((view) => {
      if (tab === "apps" && packageCounts(view).apps === 0) return false;
      if (tab === "plugins" && packageCounts(view).plugins === 0) return false;
      if (tab === "installed" && (!view.installed || view.installed.state === "retired")) return false;
      if (source !== "all" && view.source !== source) return false;
      if (!term) return true;
      return `${view.descriptor.title ?? ""} ${view.descriptor.id} ${view.descriptor.version} ${view.descriptor.description ?? ""}`.toLowerCase().includes(term);
    });
  }, [rows, tab, source, query]);

  const modeOf = (view: Api.PackageView): "install" | "manage" => {
    const retired = view.installed?.state === "retired";
    const current = Boolean(view.installed && !retired && view.installed.version === view.descriptor.version && !view.update && !view.downgrade && !view.rollback);
    return current ? "manage" : "install";
  };

  return (
    <>
      <Layout
        height="fill"
        header={
          <LayoutHeader padding={gutter} hasDivider>
            <VStack gap={4}>
              <PageTitle title={t("Marketplace")}>
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
                      { value: "product", label: t("Product version") },
                      { value: "imported", label: t("Local library") },
                      { value: "installed", label: t("Installed") },
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
              {packages.status !== "loading" && filtered.length === 0 && (
                <EmptyState
                  headingLevel={2}
                  title={rows.length === 0 ? t("No package versions are available.") : t("No matches")}
                  description={rows.length === 0 ? t("Review versions, dependencies and their installation scope before adding them to this workspace.") : t("Try a different search.")}
                />
              )}
              {filtered.length > 0 && (
                <Grid columns={{ minWidth: 320, max: 3 }} gap={4} role="list">
                  {filtered.map((view) => (
                    <PackageCard
                      key={`${view.descriptor.id}@${view.descriptor.version}`}
                      view={view}
                      onInspect={() => setSelected({ view, mode: modeOf(view) })}
                      onAction={() => setSelected({ view, mode: modeOf(view) })}
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
    </>
  );
}
