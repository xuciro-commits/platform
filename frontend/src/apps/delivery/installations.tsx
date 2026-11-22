import { Button } from "@astryxdesign/core/Button";
import { HStack, Layout, LayoutContent, LayoutHeader } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useState } from "react";

const styles = stylex.create({
  surface: { backgroundColor: colorVars["--color-background-surface"] },
});
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";
import { PageTitle, useGutter } from "../../pages/frame";
import { PackageReview } from "../../pages/PackageReview";

function stateColor(state: string): "green" | "orange" | "gray" {
  if (state === "active") return "green";
  if (state === "draining") return "orange";
  return "gray";
}

export default function Installations() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const [selected, setSelected] = useState<{ view: Api.PackageView; mode: "install" | "manage" }>();
  const gutter = useGutter();
  const rows = packages.status === "ready" ? packages.data.filter((view) => view.installed?.version === view.descriptor.version) : [];
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
            {packages.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
            {packages.status !== "loading" && rows.length === 0 && <Text type="body">{t("Nothing is installed.")}</Text>}
            {rows.length > 0 && (
              <List header={t("Installed versions")} hasDividers>
                {rows.map((view) => {
                  const state = view.installed?.state ?? "Unknown";
                  const retired = state === "retired";
                  return (
                    <ListItem
                      key={view.descriptor.id}
                      label={view.descriptor.title || view.descriptor.id}
                      description={`${view.descriptor.id} · ${view.installed?.version ?? view.descriptor.version} · ${t(state)}${view.installed?.space && view.installed.space !== view.descriptor.id ? ` · ${view.installed.space}` : ""}`}
                      endContent={
                        <HStack gap={2} vAlign="center">
                          <Token label={t(state)} color={stateColor(state)} size="sm" />
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
          </LayoutContent>
        }
      />
      {selected && <PackageReview view={selected.view} mode={selected.mode} close={() => setSelected(undefined)} done={() => setSelected(undefined)} />}
    </>
  );
}
