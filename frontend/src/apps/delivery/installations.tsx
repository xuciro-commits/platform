import { Button } from "@astryxdesign/core/Button";
import { Layout, LayoutContent, LayoutHeader } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Text } from "@astryxdesign/core/Text";
import { useState } from "react";
import { type Api, useRead } from "../../host";
import { t } from "../../i18n";
import { PageTitle, useGutter } from "../../pages/frame";
import { PackageReview } from "../../pages/PackageReview";

export default function Installations() {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const [selected, setSelected] = useState<Api.PackageView>();
  const gutter = useGutter();
  const rows = packages.status === "ready" ? packages.data.filter((view) => view.installed?.version === view.descriptor.version) : [];
  return (
    <>
      <Layout
        height="fill"
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
                {rows.map((view) => (
                  <ListItem
                    key={view.descriptor.id}
                    label={view.descriptor.title || view.descriptor.id}
                    description={`${view.descriptor.id} · ${view.installed?.version ?? view.descriptor.version} · ${t(view.installed?.state ?? "Unknown")}${view.installed?.space && view.installed.space !== view.descriptor.id ? ` · ${view.installed.space}` : ""}`}
                    endContent={<Button label={t("Manage installation")} variant="secondary" onClick={() => setSelected(view)} />}
                  />
                ))}
              </List>
            )}
          </LayoutContent>
        }
      />
      {selected && <PackageReview view={selected} mode="manage" close={() => setSelected(undefined)} done={() => setSelected(undefined)} />}
    </>
  );
}
