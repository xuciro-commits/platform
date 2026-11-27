import { Button } from "@astryxdesign/core/Button";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useMemo, useState } from "react";
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";
import { navigate } from "../../router";
import { PageTitle, useGutter } from "../../pages/frame";
import { PackageAccessDialog } from "../../pages/PackageAccessDialog";
import { destinationOf, PackagePostInstallPanel } from "../../pages/PackagePostInstallPanel";
import { PackageReview } from "../../pages/PackageReview";

export const JUST_INSTALLED_KEY = "platform:just-installed";

const styles = stylex.create({
  surface: { backgroundColor: colorVars["--color-background-surface"] },
});

function stateColor(state: string): "green" | "orange" | "gray" {
  if (state === "active") return "green";
  if (state === "draining") return "orange";
  return "gray";
}

function readJustInstalled(): string[] {
  try {
    const raw = sessionStorage.getItem(JUST_INSTALLED_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((item): item is string => typeof item === "string" && item.length > 0) : [];
  } catch {
    return [];
  }
}

export default function Installations() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const [selected, setSelected] = useState<{ view: Api.PackageView; mode: "install" | "manage" }>();
  const [installedIds, setInstalledIds] = useState<string[]>(readJustInstalled);
  const [accessFor, setAccessFor] = useState<string[]>(readJustInstalled);
  const closeAccess = () => setAccessFor([]);
  const gutter = useGutter();
  const rows = packages.status === "ready" ? packages.data.filter((view) => view.installed?.version === view.descriptor.version) : [];

  const justInstalled = useMemo(() => {
    if (!installedIds.length) return [];
    const byId = new Map(rows.filter((view) => view.installed?.state === "active").map((view) => [view.descriptor.id, view]));
    return installedIds.map((id) => byId.get(id)).filter((view): view is Api.PackageView => Boolean(view));
  }, [installedIds, rows]);

  const dismissBanner = () => {
    try { sessionStorage.removeItem(JUST_INSTALLED_KEY); } catch {}
    setInstalledIds([]);
  };

  return (
    <>
      <Layout
        height="fill"
        xstyle={styles.surface}
        header={
          <LayoutHeader padding={gutter} hasDivider>
            <PageTitle title={t("Installations")}>
              <Text type="body" color="secondary">{t("Installed versions, their state and the retained business data they leave behind.")}</Text>
            </PageTitle>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={5}>
              {justInstalled.length > 0 && (
                <PackagePostInstallPanel
                  views={justInstalled}
                  onDismiss={dismissBanner}
                />
              )}
              {packages.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
              {packages.status !== "loading" && rows.length === 0 && <Text type="body">{t("Nothing is installed.")}</Text>}
              {rows.length > 0 && (
                <List header={t("Installed versions")} hasDividers>
                  {rows.map((view) => {
                    const state = view.installed?.state ?? "Unknown";
                    const retired = state === "retired";
                    const dest = !retired ? destinationOf(view) : undefined;
                    return (
                      <ListItem
                        key={view.descriptor.id}
                        label={view.descriptor.title || view.descriptor.id}
                        description={`${view.descriptor.id} · ${view.installed?.version ?? view.descriptor.version} · ${t(state)}${view.installed?.space && view.installed.space !== view.descriptor.id ? ` · ${view.installed.space}` : ""}`}
                        endContent={
                          <HStack gap={2} vAlign="center">
                            <Token label={t(state)} color={stateColor(state)} size="sm" />
                            {dest && (
                              <Button label={dest.label} variant="ghost" size="sm" onClick={() => navigate(dest.href)} />
                            )}
                            {retired && (
                              <Button label={t("Reinstall")} variant="primary" size="sm" onClick={() => setSelected({ view, mode: "install" })} />
                            )}
                            <Button label={t("Manage installation")} variant="secondary" size="sm" onClick={() => setSelected({ view, mode: "manage" })} />
                          </HStack>
                        }
                      />
                    );
                  })}
                </List>
              )}
            </VStack>
          </LayoutContent>
        }
      />
      {selected && (
        <PackageReview
          view={selected.view}
          mode={selected.mode}
          close={() => setSelected(undefined)}
          done={(ids) => {
            setSelected(undefined);
            if (ids?.length) {
              setInstalledIds(ids);
              setAccessFor(ids);
            }
          }}
        />
      )}
      {accessFor.length > 0 && <PackageAccessDialog ids={accessFor} onClose={closeAccess} />}
    </>
  );
}
